package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// newSettingsTestDeps 创建带临时 SQLite 的 Deps
func newSettingsTestDeps(t *testing.T) (*Deps, *config.Store) {
	t.Helper()
	store, err := config.OpenStore(filepath.Join(t.TempDir(), "settings-test.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("关闭测试数据库失败: %v", err)
		}
	})
	return &Deps{Store: store}, store
}

// doJSON 向 handler 发送指定 method/body 请求并返回响应
func doJSON(t *testing.T, d *Deps, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	d.Register(mux)
	mux.ServeHTTP(w, req)
	return w
}

// TestGetSettings_NoWebuiPort GET /api/settings 不返回 webui_port（含数据库残留键）
func TestGetSettings_NoWebuiPort(t *testing.T) {
	d, store := newSettingsTestDeps(t)
	if err := store.SetSetting("webui_port", "61234"); err != nil {
		t.Fatalf("预置残留键失败: %v", err)
	}
	if err := store.SetSetting("unknown_key", "x"); err != nil {
		t.Fatalf("预置未知键失败: %v", err)
	}

	w := doJSON(t, d, http.MethodGet, "/api/settings", "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", w.Code)
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v; body=%s", err, w.Body.String())
	}
	if _, ok := got["webui_port"]; ok {
		t.Errorf("响应不应包含 webui_port；body=%s", w.Body.String())
	}
	if _, ok := got["unknown_key"]; ok {
		t.Errorf("响应不应包含未知键；body=%s", w.Body.String())
	}
	// 业务默认值仍应补齐
	if got["tag"] != "auto-dns" || got["interval"] != "5m" {
		t.Errorf("业务默认值未补齐: %+v", got)
	}
}

// TestPutSettings_DoesNotPersistWebuiPort PUT 含 webui_port（及前端回传的 sync_enabled）时不落库
func TestPutSettings_DoesNotPersistWebuiPort(t *testing.T) {
	d, store := newSettingsTestDeps(t)

	body := `{"webui_port":"61234","sync_enabled":"false","tag":"my-tag","interval":"7m"}`
	w := doJSON(t, d, http.MethodPut, "/api/settings", body)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	settings, err := store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if _, ok := settings["webui_port"]; ok {
		t.Errorf("webui_port 不应被 PUT 写入；settings=%+v", settings)
	}
	if _, ok := settings["sync_enabled"]; ok {
		t.Errorf("sync_enabled 不应经 PUT /api/settings 写入；settings=%+v", settings)
	}
	if settings["tag"] != "my-tag" || settings["interval"] != "7m" {
		t.Errorf("合法业务键应正常保存；settings=%+v", settings)
	}
}

// TestLoadConfig_IgnoresWebuiPortResidue SQLite 残留 webui_port 不改变业务配置
func TestLoadConfig_IgnoresWebuiPortResidue(t *testing.T) {
	_, store := newSettingsTestDeps(t)
	if err := store.SetSetting("webui_port", "61234"); err != nil {
		t.Fatalf("预置残留键失败: %v", err)
	}
	if err := store.SetSetting("tag", "kept"); err != nil {
		t.Fatalf("预置 tag 失败: %v", err)
	}

	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}
	if cfg.Tag != "kept" {
		t.Errorf("业务设置应正常加载: tag = %q, want kept", cfg.Tag)
	}
	// 业务 Config 不应再包含监听配置
	if cfg.Interval == 0 {
		t.Error("业务默认值应补齐（Interval 不应为 0）")
	}
}

// TestConfigImport_RejectsWebuiPort 含 webui_port 的配置包在写入前返回 400，且不改变旧配置
func TestConfigImport_RejectsWebuiPort(t *testing.T) {
	d, store := newSettingsTestDeps(t)
	if err := store.SetSetting("tag", "old-tag"); err != nil {
		t.Fatalf("预置旧设置失败: %v", err)
	}
	if err := store.AddTarget(config.TargetConfig{
		CloudType:  config.CloudTCLighthouse,
		Region:     "ap-guangzhou",
		ResourceID: "lhins-old",
	}); err != nil {
		t.Fatalf("预置旧目标失败: %v", err)
	}

	body := `{"version":1,"targets":[],"rules":[],"settings":{"webui_port":"61234","tag":"new-tag"}}`
	w := doJSON(t, d, http.MethodPost, "/api/config/import", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "webui_port") {
		t.Errorf("错误信息应指出 webui_port；body=%s", w.Body.String())
	}

	// 旧配置必须保持不变（未打开写事务）
	settings, err := store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if settings["tag"] != "old-tag" {
		t.Errorf("拒绝导入后旧设置被改变: %+v", settings)
	}
	targets, err := store.GetTargets()
	if err != nil {
		t.Fatalf("GetTargets 失败: %v", err)
	}
	if len(targets) != 1 || targets[0].ResourceID != "lhins-old" {
		t.Errorf("拒绝导入后旧目标被改变: %+v", targets)
	}
}

// TestConfigExport_OmitsWebuiPort 导出配置包不含 webui_port
func TestConfigExport_OmitsWebuiPort(t *testing.T) {
	d, store := newSettingsTestDeps(t)
	if err := store.SetSetting("webui_port", "61234"); err != nil {
		t.Fatalf("预置残留键失败: %v", err)
	}
	if err := store.SetSetting("tag", "exported"); err != nil {
		t.Fatalf("预置 tag 失败: %v", err)
	}

	w := doJSON(t, d, http.MethodGet, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", w.Code)
	}
	var got struct {
		Version  int               `json:"version"`
		Settings map[string]string `json:"settings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析导出响应失败: %v; body=%s", err, w.Body.String())
	}
	if _, ok := got.Settings["webui_port"]; ok {
		t.Errorf("导出不应包含 webui_port；body=%s", w.Body.String())
	}
	if got.Settings["tag"] != "exported" {
		t.Errorf("导出应包含业务设置: %+v", got.Settings)
	}
}
