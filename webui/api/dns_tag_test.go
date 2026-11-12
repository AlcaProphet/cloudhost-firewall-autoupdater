package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"

	_ "modernc.org/sqlite"
)

// 本文件验证 Build8 Step 1 的收口判据：同一旧库经启动加载（运行快照）、
// GET /api/settings 与 v4 导出看到的 DNS TAG 相同；新库与 reset 后重启均使用 auto-ddns。

// createLegacySettingsDB 构造只含 settings 表的旧库并写入给定值。
func createLegacySettingsDB(t *testing.T, values map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy-api-dns.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开旧库失败: %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("关闭旧库失败: %v", cerr)
		}
	}()
	if _, err := db.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("建立旧库 settings 表失败: %v", err)
	}
	for k, v := range values {
		if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", k, v); err != nil {
			t.Fatalf("预置旧库设置 %s 失败: %v", k, err)
		}
	}
	return path
}

// exportSettingsTag 从导出 JSON 中只读取 settings.tag，避免绑定具体配置包 DTO 版本。
func exportSettingsTag(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Settings struct {
			Tag string `json:"tag"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("解析导出配置包失败: %v", err)
	}
	return envelope.Settings.Tag
}

// assertDNSTagSources 同时核对运行快照、设置 GET 与导出三个来源。
func assertDNSTagSources(t *testing.T, d *Deps, store *config.Store, want string) {
	t.Helper()
	snapshot, err := store.LoadBusinessSnapshot()
	if err != nil {
		t.Fatalf("LoadBusinessSnapshot 失败: %v", err)
	}
	if got := snapshot.ToRuntimeConfig().Tag; got != want {
		t.Errorf("运行快照 DNS TAG = %q, want %q", got, want)
	}

	w := doJSON(t, d, http.MethodGet, "/api/settings", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/settings 状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var settings map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &settings); err != nil {
		t.Fatalf("解析设置响应失败: %v", err)
	}
	if got := settings["tag"]; got != want {
		t.Errorf("GET 设置 DNS TAG = %q, want %q", got, want)
	}

	w = doJSON(t, d, http.MethodPost, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("导出状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := exportSettingsTag(t, w.Body.Bytes()); got != want {
		t.Errorf("导出 DNS TAG = %q, want %q", got, want)
	}
}

func TestDNSTagSourcesNewDatabase(t *testing.T) {
	e := newTestEnv(t)
	assertDNSTagSources(t, e.deps, e.store, config.DefaultDNSTag)
}

func TestDNSTagSourcesLegacyMissingTag(t *testing.T) {
	path := createLegacySettingsDB(t, map[string]string{"interval": "5m"})
	store, err := config.OpenStore(path)
	if err != nil {
		t.Fatalf("打开旧库失败: %v", err)
	}
	d := &Deps{Store: store}
	assertDNSTagSources(t, d, store, config.LegacyDefaultDNSTag)
	if err := store.Close(); err != nil {
		t.Fatalf("关闭旧库失败: %v", err)
	}
}

func TestDNSTagSourcesLegacyExplicitTagWithWhitespace(t *testing.T) {
	const rawTag = " my-dns "
	path := createLegacySettingsDB(t, map[string]string{"tag": rawTag})
	for _, phase := range []string{"首次打开", "重启"} {
		t.Run(phase, func(t *testing.T) {
			store, err := config.OpenStore(path)
			if err != nil {
				t.Fatalf("打开旧库失败: %v", err)
			}
			defer func() {
				if err := store.Close(); err != nil {
					t.Errorf("关闭旧库失败: %v", err)
				}
			}()
			assertDNSTagSources(t, &Deps{Store: store}, store, "my-dns")
			settings, err := store.GetSettings()
			if err != nil || settings["tag"] != rawTag {
				t.Fatalf("旧库 TAG 应保留原值 %q，实际 %q，err=%v", rawTag, settings["tag"], err)
			}
		})
	}
}

func TestDNSTagSourcesResetAndRestart(t *testing.T) {
	path := createLegacySettingsDB(t, map[string]string{"tag": "legacy-tag"})
	store, err := config.OpenStore(path)
	if err != nil {
		t.Fatalf("打开旧库失败: %v", err)
	}
	d := &Deps{Store: store}
	assertDNSTagSources(t, d, store, "legacy-tag")

	// 显式 reset 清除业务设置，运行快照、GET 与导出都回到新默认值。
	w := doJSON(t, d, http.MethodPost, "/api/config/reset", `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("reset 状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	assertDNSTagSources(t, d, store, config.DefaultDNSTag)
	if err := store.Close(); err != nil {
		t.Fatalf("关闭 Store 失败: %v", err)
	}

	// 重启后迁移标记仍在，reset 后的空设置继续解析为 auto-ddns。
	store, err = config.OpenStore(path)
	if err != nil {
		t.Fatalf("重启打开 Store 失败: %v", err)
	}
	defer func() {
		if cerr := store.Close(); cerr != nil {
			t.Errorf("关闭 Store 失败: %v", cerr)
		}
	}()
	assertDNSTagSources(t, &Deps{Store: store}, store, config.DefaultDNSTag)
}
