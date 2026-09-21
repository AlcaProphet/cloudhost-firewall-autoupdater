package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// stubSyncer 测试用模拟 Syncer（实现 api.Syncer 接口）
type stubSyncer struct {
	enabled   bool
	triggered atomic.Bool
	paused    atomic.Bool
	resumed   atomic.Bool
}

func (s *stubSyncer) Status() syncer.SyncStatus {
	return syncer.SyncStatus{Running: true, Enabled: s.enabled}
}
func (s *stubSyncer) TriggerSync() { s.triggered.Store(true) }
func (s *stubSyncer) DryRun() (syncer.DryRunResponse, error) {
	return syncer.DryRunResponse{Results: []syncer.DryRunResult{}}, nil
}
func (s *stubSyncer) Pause()  { s.enabled = false; s.paused.Store(true) }
func (s *stubSyncer) Resume() { s.enabled = true; s.resumed.Store(true) }

func doPost(t *testing.T, d *Deps, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	d.Register(mux)
	mux.ServeHTTP(w, req)
	return w
}

// TestHandleSyncTrigger_Paused 暂停状态下 trigger 返回 409
func TestHandleSyncTrigger_Paused(t *testing.T) {
	d := &Deps{Syncer: &stubSyncer{enabled: false}}
	w := doPost(t, d, "/api/sync/trigger")

	if w.Code != http.StatusConflict {
		t.Errorf("状态码 = %d, want 409", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "同步已暂停，请先开启") {
		t.Errorf("响应体 = %s, want 包含「同步已暂停，请先开启」", body)
	}
}

// TestHandleSyncTrigger_Enabled 开启状态下 trigger 正常触发
func TestHandleSyncTrigger_Enabled(t *testing.T) {
	s := &stubSyncer{enabled: true}
	d := &Deps{Syncer: s}
	w := doPost(t, d, "/api/sync/trigger")

	if w.Code != http.StatusAccepted {
		t.Errorf("状态码 = %d, want 202", w.Code)
	}
	if !s.triggered.Load() {
		t.Error("TriggerSync 应被调用")
	}
}

// TestHandleSyncPauseResume pause/resume 端点：200 + settings 表持久化
func TestHandleSyncPauseResume(t *testing.T) {
	store, err := config.OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenStore 失败: %v", err)
	}
	defer store.Close()
	s := &stubSyncer{enabled: true}
	d := &Deps{Store: store, Syncer: s}

	// 暂停
	w := doPost(t, d, "/api/sync/pause")
	if w.Code != http.StatusOK {
		t.Errorf("pause 状态码 = %d, want 200", w.Code)
	}
	if !s.paused.Load() {
		t.Error("Syncer.Pause 应被调用")
	}
	settings, err := store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if settings["sync_enabled"] != "false" {
		t.Errorf("sync_enabled = %s, want false", settings["sync_enabled"])
	}

	// 恢复
	w = doPost(t, d, "/api/sync/resume")
	if w.Code != http.StatusOK {
		t.Errorf("resume 状态码 = %d, want 200", w.Code)
	}
	if !s.resumed.Load() {
		t.Error("Syncer.Resume 应被调用")
	}
	settings, err = store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if settings["sync_enabled"] != "true" {
		t.Errorf("sync_enabled = %s, want true", settings["sync_enabled"])
	}
}
