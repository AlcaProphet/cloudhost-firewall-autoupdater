package api

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

func (d *Deps) handleGetTargets(w http.ResponseWriter, r *http.Request) {
	targets, err := d.Store.GetTargets()
	if err != nil {
		writeInternalError(w, "读取目标失败", err)
		return
	}
	writeJSON(w, http.StatusOK, targets)
}

// targetRequest 目标请求体（Build6 §12.9：独立 DTO，不含数据库 ID 或内部字段）
type targetRequest struct {
	CloudType  string `json:"cloud_type"`
	Region     string `json:"region"`
	ResourceID string `json:"resource_id"`
}

// toConfig 转换为领域值；归一化与校验统一由 config.NormalizeTarget 完成
func (req targetRequest) toConfig() config.TargetConfig {
	return config.TargetConfig{
		CloudType:  config.CloudType(req.CloudType),
		Region:     req.Region,
		ResourceID: req.ResourceID,
	}
}

func (d *Deps) handleAddTarget(w http.ResponseWriter, r *http.Request) {
	var req targetRequest
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &req); err != nil {
		writeRequestError(w, err)
		return
	}
	target, err := config.NormalizeTarget(req.toConfig())
	if err != nil {
		writeRequestError(w, err)
		return
	}

	err = d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, aerr := d.Store.AddTargetTx(ctx, tx, target)
		return aerr
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"message": "添加成功"})
}

func (d *Deps) handleUpdateTarget(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	var req targetRequest
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &req); err != nil {
		writeRequestError(w, err)
		return
	}
	target, err := config.NormalizeTarget(req.toConfig())
	if err != nil {
		writeRequestError(w, err)
		return
	}

	err = d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		// 不存在的目标按 RowsAffected=0 判定 404（Build6 §12.7、AGENTS §9.1）
		rows, uerr := d.Store.UpdateTargetTx(ctx, tx, id, target)
		if uerr != nil {
			return uerr
		}
		if rows == 0 {
			return notFound("目标不存在")
		}
		return nil
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "更新成功"})
}

func (d *Deps) handleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeRequestError(w, err)
		return
	}

	err = d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		// 同一事务内检查规则引用：被引用返回 409，不静默删除引用，
		// 也不把规则扩大为“适用于全部目标”
		refs, rerr := d.Store.ReferencingRuleIDsTx(ctx, tx, id)
		if rerr != nil {
			return rerr
		}
		if len(refs) > 0 {
			return conflict(fmt.Sprintf("目标被 %d 条规则引用，请先修改规则", len(refs)))
		}
		// 不存在的目标按 RowsAffected=0 判定 404（Build6 §12.7、AGENTS §9.1）
		rows, derr := d.Store.DeleteTargetTx(ctx, tx, id)
		if derr != nil {
			return derr
		}
		if rows == 0 {
			return notFound("目标不存在")
		}
		return nil
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "删除成功"})
}

// testConnectionReq 测试连接请求（固定三字段，Build6 §12.9）
type testConnectionReq struct {
	CloudType  string `json:"cloud_type"`
	Region     string `json:"region"`
	ResourceID string `json:"resource_id"`
}

func (d *Deps) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	var req testConnectionReq
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &req); err != nil {
		writeRequestError(w, err)
		return
	}
	// 与目标 CRUD 复用同一组基础校验（cloud_type / region / resource_id）
	target, err := config.NormalizeTarget(config.TargetConfig{
		CloudType:  config.CloudType(req.CloudType),
		Region:     req.Region,
		ResourceID: req.ResourceID,
	})
	if err != nil {
		writeRequestError(w, err)
		return
	}

	// 只取一次运行时快照：凭据与 ClientPool 都来自该快照，不再读取 Store
	// 或覆盖进程级全局凭据（Build6 §12.3 第 6 条）。
	state := d.runtimeSnapshot()
	if state == nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "运行时状态尚未就绪，请稍后重试"})
		return
	}
	// 凭据空值快速失败：避免暴露 SDK 原始报错
	if ready, hint := providerCredentialsReady(target.CloudType, state.Config.Credentials); !ready {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": hint})
		return
	}

	// 创建临时 Provider 测试连通性（复用快照中的 ClientPool）
	p, err := provider.NewProvider(target, 0, state.Pool)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}

	rules, err := p.GetRules()
	if err != nil {
		slog.Warn("测试连接失败", "provider", p.Name(), "error", err)
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": fmt.Sprintf("连接成功，当前 %d 条规则", len(rules)),
	})
}

// providerCredentialsReady 判断目标云厂商所需凭据是否已配置。
//
// 未配置时返回面向用户的安全提示；连接测试与资源扫描共用。
// 凭据来自请求开始时取得的运行时快照，而不是重新从数据库零散读取。
func providerCredentialsReady(ct config.CloudType, creds config.Credentials) (bool, string) {
	switch ct {
	case config.CloudTCLighthouse, config.CloudTCCVM:
		if creds.TencentSecretID == "" || creds.TencentSecretKey == "" {
			return false, "腾讯云凭据未配置，请先在全局设置中填写"
		}
	case config.CloudAliSWAS, config.CloudAliECS:
		if creds.AliyunAccessKeyID == "" || creds.AliyunAccessKeySecret == "" {
			return false, "阿里云凭据未配置，请先在全局设置中填写"
		}
	}
	return true, ""
}
