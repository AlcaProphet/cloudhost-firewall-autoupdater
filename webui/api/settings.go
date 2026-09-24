package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// 设置键边界（Build6 Step 2 最小清理 + Step 4 固定 DTO）：
// settingsEditableKeys 是 GET 返回并可经 PUT /api/settings 落库的 11 个键；
// sync_enabled 只由 pause/resume 端点或 version 2 配置导入写入，
// webui_port 已改为部署参数 WEBUI_PORT，二者都不属于本 DTO。
var settingsEditableKeys = map[string]bool{
	"tc_access_id": true, "tc_access_key": true,
	"ali_access_id": true, "ali_access_key": true,
	"tag": true, "interval": true, "dns": true, "dns_timeout": true,
	"dns_fail_threshold": true, "log_level": true, "theme": true,
}

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
