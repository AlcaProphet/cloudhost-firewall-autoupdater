package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// 设置键边界（Build6 Step 2 最小清理）：
//   - settingsEditableKeys 是 GET 返回并可经 PUT /api/settings 落库的键；
//   - settingsReservedKeys 已不属于业务设置或只能由专用端点写入，PUT 一律忽略、导入直接拒绝。
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

func (d *Deps) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := d.Store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 填充默认值（key 不存在或值为空时），使前端能显示当前生效配置
	defaults := map[string]string{
		"tag":                "auto-dns",
		"interval":           "5m",
		"dns":                "223.5.5.5",
		"log_level":          "info",
		"dns_timeout":        "10s",
		"dns_fail_threshold": "5",
	}
	for k, v := range defaults {
		if settings[k] == "" {
			settings[k] = v
		}
	}
	// 不返回保留键（含数据库中的 webui_port 残留）和未知键
	for k := range settings {
		if !settingsEditableKeys[k] {
			delete(settings, k)
		}
	}
	writeJSON(w, http.StatusOK, settings)
}

func (d *Deps) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var settings map[string]string
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// 保留键和未知键不落库：webui_port 是部署参数，sync_enabled 由 pause/resume 或导入管理
	applied := make(map[string]string, len(settings))
	for k, v := range settings {
		if settingsReservedKeys[k] || !settingsEditableKeys[k] {
			continue
		}
		applied[k] = v
	}
	for k, v := range applied {
		if err := d.Store.SetSetting(k, v); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	// 仅业务配置变更时触发重载：theme 键仅前端展示（不参与后端配置加载），单独变更跳过重载
	needsReload := false
	for k := range applied {
		if k != "theme" {
			needsReload = true
			break
		}
	}
	if needsReload {
		d.notifyReload()
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "保存成功"})
}

// handleConfigReset 清空所有数据，重新初始化（清空目标、规则、凭据、日志、告警与扫描结果）
func (d *Deps) handleConfigReset(w http.ResponseWriter, r *http.Request) {
	if err := d.Store.ResetAll(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.notifyReload()
	writeJSON(w, http.StatusOK, map[string]string{"message": "数据已清空，请重新配置"})
}

// configExport 配置导出结构（凭据不导出）
type configExport struct {
	Version  int                   `json:"version"`
	Targets  []config.TargetConfig `json:"targets"`
	Rules    []config.DomainRule   `json:"rules"`
	Settings map[string]string     `json:"settings"`
}

func (d *Deps) handleConfigExport(w http.ResponseWriter, r *http.Request) {
	targets, err := d.Store.GetTargets()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rules, err := d.Store.GetRules()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings, err := d.Store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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

// configImport 配置导入结构
type configImport struct {
	Version  int                   `json:"version"`
	Targets  []config.TargetConfig `json:"targets"`
	Rules    []config.DomainRule   `json:"rules"`
	Settings map[string]string     `json:"settings"`
}

func (d *Deps) handleConfigImport(w http.ResponseWriter, r *http.Request) {
	var imp configImport
	if err := json.NewDecoder(r.Body).Decode(&imp); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式错误")
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

	// 在事务中执行导入，失败自动回滚
	err := d.Store.WithTransaction(func(tx *sql.Tx) error {
		// 清空旧数据
		if err := d.Store.ClearAllTx(tx); err != nil {
			return fmt.Errorf("清空旧配置失败: %w", err)
		}
		// 写入新数据
		if err := d.Store.BatchAddTargetsTx(tx, imp.Targets); err != nil {
			return fmt.Errorf("导入目标失败: %w", err)
		}
		if err := d.Store.BatchAddRulesTx(tx, imp.Rules); err != nil {
			return fmt.Errorf("导入规则失败: %w", err)
		}
		for k, v := range imp.Settings {
			// 跳过凭据字段（不导入）
			if k == "tc_access_id" || k == "tc_access_key" || k == "ali_access_id" || k == "ali_access_key" {
				continue
			}
			if err := d.Store.SetSettingTx(tx, k, v); err != nil {
				return fmt.Errorf("导入设置失败: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	d.notifyReload()
	writeJSON(w, http.StatusOK, map[string]string{"message": "导入成功"})
}
