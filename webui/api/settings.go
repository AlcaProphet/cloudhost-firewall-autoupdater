package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// 设置键边界（Build6 Step 2 最小清理 + Step 4 固定 DTO）：
//   - settingsEditableKeys 是 GET 返回并可经 PUT /api/settings 落库的 11 个键；
//   - settingsReservedKeys 已不属于业务设置或只能由专用端点写入，PUT 一律不落库。
var (
	settingsEditableKeys = map[string]bool{
		"tc_access_id": true, "tc_access_key": true,
		"ali_access_id": true, "ali_access_key": true,
		"tag": true, "interval": true, "dns": true, "dns_timeout": true,
		"dns_fail_threshold": true, "log_level": true, "theme": true,
	}
	settingsReservedKeys = map[string]bool{
		"webui_port":   true, // 已改为部署参数 WEBUI_PORT
		"sync_enabled": true, // 只由 pause/resume 端点或配置导入写入
	}
)

// settingsDefaults 缺失或空白时的默认值（Build6 §3.1、§12.9）
var settingsDefaults = map[string]string{
	"tag":                "auto-dns",
	"interval":           "5m",
	"dns":                "223.5.5.5",
	"dns_timeout":        "10s",
	"dns_fail_threshold": "5",
	"log_level":          "info",
	"theme":              "light",
}

func (d *Deps) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := d.Store.GetSettings()
	if err != nil {
		writeInternalError(w, "读取设置失败", err)
		return
	}
	// 只返回 11 个可编辑键并补齐默认值：数据库中的 webui_port 残留、
	// sync_enabled 与其他未知键都不返回（Build6 §12.9）
	out := make(map[string]string, len(settingsEditableKeys))
	for k := range settingsEditableKeys {
		out[k] = settings[k]
	}
	for k, v := range settingsDefaults {
		if out[k] == "" {
			out[k] = v
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// settingsPatch PUT /api/settings 的部分更新请求体（11 个 pointer 字段，Build6 §12.9）。
//
// 省略字段保持不变；只有四个云凭据允许显式空字符串（用于清除旧值）。
type settingsPatch struct {
	TCAccessID       *string `json:"tc_access_id"`
	TCAccessKey      *string `json:"tc_access_key"`
	AliAccessID      *string `json:"ali_access_id"`
	AliAccessKey     *string `json:"ali_access_key"`
	Tag              *string `json:"tag"`
	Interval         *string `json:"interval"`
	DNS              *string `json:"dns"`
	DNSTimeout       *string `json:"dns_timeout"`
	DNSFailThreshold *string `json:"dns_fail_threshold"`
	LogLevel         *string `json:"log_level"`
	Theme            *string `json:"theme"`
}

// values 归一化并校验本次提交的字段，返回“设置键 → 应落库的值”。
func (p settingsPatch) values() (map[string]string, error) {
	updates := make(map[string]string, len(settingsEditableKeys))

	// 云凭据作为不透明文本按原值保存，允许显式空字符串
	for key, v := range map[string]*string{
		"tc_access_id":   p.TCAccessID,
		"tc_access_key":  p.TCAccessKey,
		"ali_access_id":  p.AliAccessID,
		"ali_access_key": p.AliAccessKey,
	} {
		if v != nil {
			updates[key] = *v
		}
	}

	if p.Tag != nil {
		v, err := config.NormalizeTag(*p.Tag)
		if err != nil {
			return nil, err
		}
		updates["tag"] = v
	}
	if p.Interval != nil {
		v, _, err := config.ParsePositiveDuration("interval", *p.Interval)
		if err != nil {
			return nil, err
		}
		updates["interval"] = v
	}
	if p.DNS != nil {
		v, err := config.NormalizeDNSAddress(*p.DNS)
		if err != nil {
			return nil, err
		}
		updates["dns"] = v
	}
	if p.DNSTimeout != nil {
		v, _, err := config.ParsePositiveDuration("dns_timeout", *p.DNSTimeout)
		if err != nil {
			return nil, err
		}
		updates["dns_timeout"] = v
	}
	if p.DNSFailThreshold != nil {
		v, _, err := config.NormalizeDNSFailThreshold(*p.DNSFailThreshold)
		if err != nil {
			return nil, err
		}
		updates["dns_fail_threshold"] = v
	}
	if p.LogLevel != nil {
		v, err := config.NormalizeLogLevel(*p.LogLevel)
		if err != nil {
			return nil, err
		}
		updates["log_level"] = v
	}
	if p.Theme != nil {
		v, err := config.NormalizeTheme(*p.Theme)
		if err != nil {
			return nil, err
		}
		updates["theme"] = v
	}
	return updates, nil
}

func (d *Deps) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var patch settingsPatch
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &patch); err != nil {
		writeRequestError(w, err)
		return
	}
	updates, err := patch.values()
	if err != nil {
		writeRequestError(w, err)
		return
	}
	if len(updates) == 0 {
		writeRequestError(w, badRequest("至少需要提交一个设置字段"))
		return
	}

	// 多键更新使用一个事务，任一键失败时不得留下部分更新
	err = d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		for k, v := range updates {
			if serr := d.Store.SetSettingTx(ctx, tx, k, v); serr != nil {
				return serr
			}
		}
		return nil
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "保存成功"})
}

// handleConfigReset 清空所有数据，重新初始化（清空目标、规则、凭据、日志、告警与扫描结果）。
//
// 只接受单一空对象 `{}`；未知字段或非对象返回 400（Build6 §12.9）。
func (d *Deps) handleConfigReset(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &req); err != nil {
		writeRequestError(w, err)
		return
	}

	err := d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		return d.Store.ResetAllTx(ctx, tx)
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "数据已清空，请重新配置"})
}

// configExport 配置导出结构（version 1，凭据不导出；version 2 属 Step 5）
type configExport struct {
	Version  int                   `json:"version"`
	Targets  []config.TargetConfig `json:"targets"`
	Rules    []config.DomainRule   `json:"rules"`
	Settings map[string]string     `json:"settings"`
}

func (d *Deps) handleConfigExport(w http.ResponseWriter, r *http.Request) {
	targets, err := d.Store.GetTargets()
	if err != nil {
		writeInternalError(w, "导出配置失败", err)
		return
	}
	rules, err := d.Store.GetRules()
	if err != nil {
		writeInternalError(w, "导出配置失败", err)
		return
	}
	settings, err := d.Store.GetSettings()
	if err != nil {
		writeInternalError(w, "导出配置失败", err)
		return
	}

	// 凭据字段不导出（安全考虑）
	delete(settings, "tc_access_id")
	delete(settings, "tc_access_key")
	delete(settings, "ali_access_id")
	delete(settings, "ali_access_key")
	// 部署参数与运行状态不属于业务配置，不进入配置包
	for k := range settingsReservedKeys {
		delete(settings, k)
	}

	export := configExport{
		Version:  1,
		Targets:  targets,
		Rules:    rules,
		Settings: settings,
	}

	w.Header().Set("Content-Disposition", "attachment; filename=fwalizer-config.json")
	writeJSON(w, http.StatusOK, export)
}

// configImport 配置导入结构（version 1；完整敏感快照 version 2 属 Step 5）
type configImport struct {
	Version  int                   `json:"version"`
	Targets  []config.TargetConfig `json:"targets"`
	Rules    []config.DomainRule   `json:"rules"`
	Settings map[string]string     `json:"settings"`
}

// validateImport 在打开写事务前完成配置包的校验与归一化（Build6 §4.2～§4.5、§12.10）。
//
// 复用与普通 API 完全相同的领域校验函数，不为导入另写一套宽松规则。
// 目标引用存在性校验不在本 Step：version 1 没有 export_id → 新数据库 ID 映射，
// 引用具体目标的旧配置包无法解析，该映射属 Step 5 的协议升级范围。
func validateImport(imp *configImport) error {
	targets := make([]config.TargetConfig, 0, len(imp.Targets))
	for i, t := range imp.Targets {
		normalized, err := config.NormalizeTarget(t)
		if err != nil {
			return importFieldError("targets", i, err)
		}
		targets = append(targets, normalized)
	}

	rules := make([]config.DomainRule, 0, len(imp.Rules))
	for i, r := range imp.Rules {
		ids, err := config.NormalizeTargetIDs(r.Targets)
		if err != nil {
			return importFieldError("rules", i, err)
		}
		r.Targets = ids
		normalized, err := config.NormalizeRule(r)
		if err != nil {
			return importFieldError("rules", i, err)
		}
		rules = append(rules, normalized)
	}

	settings := make(map[string]string, len(imp.Settings))
	for k, v := range imp.Settings {
		normalized, err := normalizeImportSetting(k, v)
		if err != nil {
			return err
		}
		settings[k] = normalized
	}

	imp.Targets = targets
	imp.Rules = rules
	imp.Settings = settings
	return nil
}

// normalizeImportSetting 对配置包中的已知设置键复用同一组校验。
//
// 未知键保持原值：LoadConfig 与 GET /api/settings 都会忽略它们，
// 其严格键集合属于 Step 5 的 version 2 协议。
func normalizeImportSetting(key, value string) (string, error) {
	switch key {
	case "tc_access_id", "tc_access_key", "ali_access_id", "ali_access_key":
		// 凭据在 version 1 导入中被跳过（不导入），这里只保持原值
		return value, nil
	case "tag":
		return config.NormalizeTag(value)
	case "interval":
		v, _, err := config.ParsePositiveDuration("interval", value)
		return v, err
	case "dns":
		return config.NormalizeDNSAddress(value)
	case "dns_timeout":
		v, _, err := config.ParsePositiveDuration("dns_timeout", value)
		return v, err
	case "dns_fail_threshold":
		v, _, err := config.NormalizeDNSFailThreshold(value)
		return v, err
	case "log_level":
		return config.NormalizeLogLevel(value)
	case "theme":
		return config.NormalizeTheme(value)
	case "sync_enabled":
		enabled, err := config.NormalizeSyncEnabled(value)
		if err != nil {
			return "", err
		}
		if enabled {
			return "true", nil
		}
		return "false", nil
	default:
		return value, nil
	}
}

// importFieldError 为数组元素校验错误补上位置信息，便于定位配置包中的问题项
func importFieldError(section string, index int, err error) error {
	var ve *config.ValidationError
	if errors.As(err, &ve) {
		return &config.ValidationError{
			Field:  fmt.Sprintf("%s[%d].%s", section, index, ve.Field),
			Reason: ve.Reason,
		}
	}
	return err
}

func (d *Deps) handleConfigImport(w http.ResponseWriter, r *http.Request) {
	// 与普通 API 共用同一严格解码语义（拒绝未知字段、尾随 JSON、多个顶层值），
	// 只有大小上限不同：导入 10 MiB（Build6 §12.8、§3.2）
	var imp configImport
	if err := decodeJSONStrict(w, r, maxImportBodyBytes, &imp); err != nil {
		writeRequestError(w, err)
		return
	}
	if imp.Version != 1 {
		writeError(w, http.StatusBadRequest, "不支持的配置版本")
		return
	}
	// webui_port 已改为部署参数 WEBUI_PORT：含该键的配置包在打开写事务前直接拒绝
	if _, ok := imp.Settings["webui_port"]; ok {
		writeError(w, http.StatusBadRequest, "配置包不支持 webui_port：监听端口请通过 WEBUI_PORT 部署参数设置")
		return
	}
	// 全部字段校验必须在打开写事务之前完成（非法配置包零写入、零 reload）
	if err := validateImport(&imp); err != nil {
		writeRequestError(w, err)
		return
	}

	// 接入配置变更协调器：单事务 → 提交 → 一次 apply（不再保留直接 notifyReload 旁路）。
	// version 1 的字段集合、覆盖语义与大小策略在本 Step 保持不变，协议升级属 Step 5。
	err := d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		// 清空旧数据
		if cerr := d.Store.ClearAllTx(ctx, tx); cerr != nil {
			return fmt.Errorf("清空旧配置失败: %w", cerr)
		}
		// 写入新数据
		if _, terr := d.Store.BatchAddTargetsTx(ctx, tx, imp.Targets); terr != nil {
			return fmt.Errorf("导入目标失败: %w", terr)
		}
		if rerr := d.Store.BatchAddRulesTx(ctx, tx, imp.Rules); rerr != nil {
			return fmt.Errorf("导入规则失败: %w", rerr)
		}
		for k, v := range imp.Settings {
			// 跳过凭据字段（version 1 不导入凭据）
			if k == "tc_access_id" || k == "tc_access_key" || k == "ali_access_id" || k == "ali_access_key" {
				continue
			}
			if serr := d.Store.SetSettingTx(ctx, tx, k, v); serr != nil {
				return fmt.Errorf("导入设置失败: %w", serr)
			}
		}
		return nil
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "导入成功"})
}
