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

// TestPutSettings_RejectsReservedAndUnknownKeys PUT 含 webui_port / sync_enabled / 未知键一律 400
//
// Build6 Step 4 取代了 Step 2 的“静默忽略未知键”行为：固定 DTO + DisallowUnknownFields
// 必须在任何写入前拒绝，且不留下部分写入。
func TestPutSettings_RejectsReservedAndUnknownKeys(t *testing.T) {
	for _, body := range []string{
		`{"webui_port":"61234","tag":"my-tag"}`,
		`{"sync_enabled":"false"}`,
		`{"unknown_key":"x"}`,
	} {
		d, store := newSettingsTestDeps(t)
		w := doJSON(t, d, http.MethodPut, "/api/settings", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body=%s 状态码 = %d, want 400; body=%s", body, w.Code, w.Body.String())
		}
		settings, err := store.GetSettings()
		if err != nil {
			t.Fatalf("GetSettings 失败: %v", err)
		}
		if len(settings) != 0 {
			t.Errorf("body=%s 被拒绝后不应写入任何设置: %+v", body, settings)
		}
	}
}

// TestPutSettings_PartialUpdate PUT 是部分更新：省略字段保持不变，合法字段落库
func TestPutSettings_PartialUpdate(t *testing.T) {
	d, store := newSettingsTestDeps(t)
	if err := store.SetSetting("tag", "keep-tag"); err != nil {
		t.Fatalf("预置设置失败: %v", err)
	}

	w := doJSON(t, d, http.MethodPut, "/api/settings", `{"interval":"7m"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	settings, err := store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if settings["interval"] != "7m" {
		t.Errorf("interval 应被更新: %+v", settings)
	}
	if settings["tag"] != "keep-tag" {
		t.Errorf("省略字段不应被改动: %+v", settings)
	}
}

// TestPutSettings_EmptyPatchRejected 空对象必须 400（至少提交一个字段）
func TestPutSettings_EmptyPatchRejected(t *testing.T) {
	d, _ := newSettingsTestDeps(t)
	w := doJSON(t, d, http.MethodPut, "/api/settings", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestPutSettings_EmptyStringOnlyForCredentials 显式空字符串只对四个云凭据合法
func TestPutSettings_EmptyStringOnlyForCredentials(t *testing.T) {
	d, store := newSettingsTestDeps(t)
	if err := store.SetSetting("tc_access_id", "AKIDold"); err != nil {
		t.Fatalf("预置凭据失败: %v", err)
	}

	// 凭据允许显式空字符串（用于清除旧值）
	w := doJSON(t, d, http.MethodPut, "/api/settings", `{"tc_access_id":""}`)
	if w.Code != http.StatusOK {
		t.Fatalf("凭据清空状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	settings, err := store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if settings["tc_access_id"] != "" {
		t.Errorf("凭据应被清空: %+v", settings)
	}

	// 非凭据字段显式空字符串必须 400，且不落库
	w = doJSON(t, d, http.MethodPut, "/api/settings", `{"tag":""}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("tag 空值状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if settings, err = store.GetSettings(); err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if _, ok := settings["tag"]; ok {
		t.Errorf("被拒绝的 tag 不应落库: %+v", settings)
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
	addTargetForTest(t, store, config.TargetConfig{
		CloudType:  config.CloudTCLighthouse,
		Region:     "ap-guangzhou",
		ResourceID: "lhins-old",
	})

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

// TestConfigExport_OmitsWebuiPort 导出配置包不含 webui_port，且只接受 POST
func TestConfigExport_OmitsWebuiPort(t *testing.T) {
	d, store := newSettingsTestDeps(t)
	if err := store.SetSetting("webui_port", "61234"); err != nil {
		t.Fatalf("预置残留键失败: %v", err)
	}
	if err := store.SetSetting("tag", "exported"); err != nil {
		t.Fatalf("预置 tag 失败: %v", err)
	}

	// 旧 GET 端点必须已删除
	if w := doJSON(t, d, http.MethodGet, "/api/config/export", ""); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET 导出应已删除，状态码 = %d, want 405", w.Code)
	}

	w := doJSON(t, d, http.MethodPost, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", w.Code)
	}
	var got struct {
		Version  int `json:"version"`
		Metadata struct {
			ExportedAt string `json:"exported_at"`
		} `json:"metadata"`
		Settings struct {
			Credentials struct {
				Tencent struct {
					SecretID  string `json:"secret_id"`
					SecretKey string `json:"secret_key"`
				} `json:"tencent"`
				Aliyun struct {
					AccessKeyID     string `json:"access_key_id"`
					AccessKeySecret string `json:"access_key_secret"`
				} `json:"aliyun"`
			} `json:"credentials"`
			Tag              string `json:"tag"`
			Interval         string `json:"interval"`
			DNS              string `json:"dns"`
			DNSTimeout       string `json:"dns_timeout"`
			DNSFailThreshold int    `json:"dns_fail_threshold"`
			LogLevel         string `json:"log_level"`
			SyncEnabled      bool   `json:"sync_enabled"`
			Theme            string `json:"theme"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析导出响应失败: %v; body=%s", err, w.Body.String())
	}
	if got.Version != 2 {
		t.Errorf("version = %d, want 2", got.Version)
	}
	if got.Settings.Tag != "exported" {
		t.Errorf("导出应包含业务设置: %+v", got.Settings)
	}
	// 导出必须补齐全部默认值
	if got.Settings.Interval != "5m" || got.Settings.DNS != "223.5.5.5" || got.Settings.DNSTimeout != "10s" ||
		got.Settings.DNSFailThreshold != 5 || got.Settings.LogLevel != "info" || !got.Settings.SyncEnabled ||
		got.Settings.Theme != "light" {
		t.Errorf("导出未补齐默认值: %+v", got.Settings)
	}
	// webui_port 作为未知设置键不得出现在导出的 settings 之外
	if strings.Contains(w.Body.String(), "webui_port") {
		t.Errorf("导出不应包含 webui_port: %s", w.Body.String())
	}
	// 导出必须包含完整凭据字段（此处为空串）与告警对象
	if !strings.Contains(w.Body.String(), `"secret_id"`) || !strings.Contains(w.Body.String(), `"alerts"`) {
		t.Errorf("导出缺少凭据或告警字段: %s", w.Body.String())
	}
}
