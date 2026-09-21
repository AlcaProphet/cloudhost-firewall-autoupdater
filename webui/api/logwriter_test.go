package api

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// TestStoreLogWriter_Counts 成功事件携带计数 → 落库 added/deleted 正确
func TestStoreLogWriter_Counts(t *testing.T) {
	store, err := config.OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer store.Close()

	w := &StoreLogWriter{Store: store}
	if err := w.OnEvent(notifier.Event{
		Type:      notifier.EventDomainSyncComplete,
		Timestamp: time.Now(),
		Data:      map[string]any{"provider": "tc_lighthouse(lhins-abc)", "domain": "api.example.com", "added": 2, "deleted": 1},
	}); err != nil {
		t.Fatalf("OnEvent 失败: %v", err)
	}

	logs, err := store.GetSyncLogs(10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("GetSyncLogs = %v, err = %v, want 1 条", logs, err)
	}
	l := logs[0]
	if l.Result != "success" || l.Added != 2 || l.Deleted != 1 {
		t.Errorf("日志 = result:%s added:%d deleted:%d, want success/2/1", l.Result, l.Added, l.Deleted)
	}
}

// TestStoreLogWriter_ErrorDetail 失败事件 → result=failed + error 落库，计数为 0
func TestStoreLogWriter_ErrorDetail(t *testing.T) {
	store, err := config.OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer store.Close()

	w := &StoreLogWriter{Store: store}
	if err := w.OnEvent(notifier.Event{
		Type:      notifier.EventSyncError,
		Timestamp: time.Now(),
		Data:      map[string]any{"provider": "tc_lighthouse(lhins-abc)", "domain": "api.example.com", "error": "请求超时"},
	}); err != nil {
		t.Fatalf("OnEvent 失败: %v", err)
	}

	logs, err := store.GetSyncLogs(10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("GetSyncLogs = %v, err = %v, want 1 条", logs, err)
	}
	l := logs[0]
	if l.Result != "failed" || l.Error != "请求超时" {
		t.Errorf("日志 = result:%s error:%s, want failed/请求超时", l.Result, l.Error)
	}
	if l.Added != 0 || l.Deleted != 0 {
		t.Errorf("失败记录计数应为 0, got added:%d deleted:%d", l.Added, l.Deleted)
	}
}
