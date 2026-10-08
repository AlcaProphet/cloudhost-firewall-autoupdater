package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// handleConfigExport 导出 version 3 完整敏感配置包（Build6 §3.3、§12.11；版本 3 见 Build7 §4.3）。
//
// 协议要点：
//   - 端点固定为 POST，请求不携带业务参数；
//   - 在一个 SQLite 只读事务中读取 targets/rules/settings/alerts/push，
//     全部读取共用同一事务，保证导出快照内部一致；
//   - 目标与规则按数据库 ID 升序、每条规则的 target_export_ids 按 export_id 升序；
//   - marshal 在写响应头之前完成，避免发送半个附件；
//   - 响应体是唯一允许包含敏感配置值的 HTTP 响应，因此绝不写日志、不进 console。
func (d *Deps) handleConfigExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tx, err := d.Store.BeginReadOnlyTx(ctx)
	if err != nil {
		writeInternalError(w, "导出配置失败", err)
		return
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			slog.Error("回滚导出事务失败", "error", rbErr)
		}
	}()

	snapshot, err := d.Store.LoadBusinessSnapshotTx(ctx, tx)
	if err != nil {
		writeInternalError(w, "导出配置失败", err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeInternalError(w, "导出配置失败", err)
		return
	}
	committed = true

	bundle := toBundleV3(snapshot, time.Now().UTC())

	// 先完成所有可能失败的 marshal，再写任何响应头
	body, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		writeInternalError(w, "导出配置失败", err)
		return
	}
	body = append(body, '\n')

	exportedAt, err := time.Parse(bundleV3TimeLayout, bundle.Metadata.ExportedAt)
	if err != nil {
		writeInternalError(w, "导出配置失败", err)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", attachmentName(exportedAt))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		// 响应体本身是敏感配置，日志里只记录错误类型，不记录内容
		slog.Warn("发送配置导出失败", "error", err)
	}
}

// attachmentName 构造固定的配置包附件文件名（UTC 时间格式 20060102T150405Z）。
func attachmentName(exportedAt time.Time) string {
	return fmt.Sprintf(`attachment; filename="%s%s.json"`,
		bundleV3FilenamePrefix, exportedAt.UTC().Format(bundleV3FileTimeLayout))
}

// handleConfigImport 导入 version 3 完整敏感配置包（Build6 §3.5、§12.12；版本 3 见 Build7 §4.3）。
//
// 执行顺序固定：
//
//	10 MiB 严格解码 → 全部纯数据预校验 → 协调器单事务（清表 → 插目标取
//	LastInsertId → 建 export_id 映射 → 规则副本重写引用 → 显式写完整 settings
//	→ 写完整 policy/email/webhook/push → 清 scanned_resources；保留 sync_logs、不碰
//	sqlite_sequence）→ 事务内构造候选 RuntimeState 与候选告警集合 → commit
//	→ 日志级别 → 告警集合 → 运行健康监督器唤醒 → Uptime Kuma Push 唤醒
//	→ 发布 RuntimeState → 返回成功（Build7 §4.3、Step 7）
//
// 任一阶段失败都完整回滚：旧数据库、旧 RuntimeState、旧日志级别、旧告警订阅
// 与扫描缓存全部保持不变，也不返回成功。
func (d *Deps) handleConfigImport(w http.ResponseWriter, r *http.Request) {
	var wire bundleV3Wire
	if err := decodeBundleV3Strict(w, r, &wire); err != nil {
		writeRequestError(w, err)
		return
	}

	// 全部纯数据校验必须在打开写事务之前完成（非法配置包零写入、零 apply）
	bundle, err := validateAndNormalizeBundle(&wire)
	if err != nil {
		writeRequestError(w, err)
		return
	}

	storeSettings := bundleSettingsToStore(bundle.Settings)

	// 完整导入使用 MutateImport：候选状态 BreakerReset，清空原 DNS 熔断失败计数
	// （Build6 §12.3 第 7 条、Issue6 A8）。
	err = d.coordinator().MutateImport(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		// 1) 按依赖顺序清空被覆盖的业务表（rules → targets → settings → 告警）
		if derr := d.Store.DeleteImportOwnedTablesTx(ctx, tx); derr != nil {
			return fmt.Errorf("清空旧配置失败: %w", derr)
		}

		// 2) 插入目标并通过 LastInsertId 建立 export_id → 新数据库 ID 映射
		idMap := make(map[int]int64, len(bundle.Targets))
		for _, t := range bundle.Targets {
			newID, ierr := d.Store.AddTargetTx(ctx, tx, config.TargetConfig{
				CloudType:  config.CloudType(t.CloudType),
				Region:     t.Region,
				ResourceID: t.ResourceID,
			})
			if ierr != nil {
				return fmt.Errorf("导入目标失败: %w", ierr)
			}
			idMap[t.ExportID] = newID
		}

		// 3) 为每条规则创建副本并按映射重写引用；不修改原始请求 DTO
		for _, rule := range bundle.Rules {
			remapped := make([]int, 0, len(rule.TargetExportIDs))
			for _, exportID := range rule.TargetExportIDs {
				newID, ok := idMap[exportID]
				if !ok {
					// 预校验已保证闭包成立；这里属于内部一致性错误
					return fmt.Errorf("导出目标映射缺失: export_id=%d", exportID)
				}
				remapped = append(remapped, int(newID))
			}
			if rerr := d.Store.AddRuleTx(ctx, tx, config.DomainRule{
				Host:       rule.Host,
				Protocol:   rule.Protocol,
				Ports:      rule.Ports,
				Action:     rule.Action,
				Targets:    remapped,
				Comment:    rule.Comment,
				EnableIPv6: rule.EnableIPv6,
			}); rerr != nil {
				return fmt.Errorf("导入规则失败: %w", rerr)
			}
		}

		// 4) 显式写入完整设置键集合（不遍历任意 map）
		if serr := d.Store.ReplaceBusinessSettingsTx(ctx, tx, storeSettings); serr != nil {
			return fmt.Errorf("导入设置失败: %w", serr)
		}

		// 5) 写入完整告警配置：触发策略、邮件（主题/正文）、Webhook 与 Push
		if aerr := d.Store.ReplaceBusinessAlertsTx(ctx, tx, bundle.Policy, bundle.Email, bundle.Webhook, bundle.Push); aerr != nil {
			return fmt.Errorf("导入告警失败: %w", aerr)
		}

		// 6) 清空扫描缓存；保留 sync_logs，不触碰 sqlite_sequence 与自增序列
		if cerr := d.Store.ClearScannedResourcesTx(ctx, tx); cerr != nil {
			return fmt.Errorf("清空扫描缓存失败: %w", cerr)
		}
		return nil
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "导入成功"})
}
