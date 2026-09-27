package api

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
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

// TestStoreLogWriter_SkippedAndPartial R6-01 判别：历史日志不扩表，仍须通过
// result + 既有 error 文本表达跳过数量、规则和原因。
func TestStoreLogWriter_SkippedAndPartial(t *testing.T) {
	tests := []struct {
		name    string
		added   int
		deleted int
		result  string
	}{
		{name: "仅跳过", result: "skipped"},
		{name: "有成功增删且有跳过", added: 1, result: "partial"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := config.OpenStore(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatalf("打开数据库失败: %v", err)
			}
			defer store.Close()

			w := &StoreLogWriter{Store: store}
			details := []provider.RuleChange{{
				Protocol: "TCP", Port: "443", Cidr: "1.2.3.4/32", Action: "DROP",
				SkipReason: "云产品不支持 DROP",
			}}
			if err := w.OnEvent(notifier.Event{
				Type:      notifier.EventDomainSyncComplete,
				Timestamp: time.Now(),
				Data: map[string]any{
					"provider": "ali_swas(i-abc)", "domain": "api.example.com",
					"added": tt.added, "deleted": tt.deleted, "skipped": 1,
					"skipped_details": details,
				},
			}); err != nil {
				t.Fatalf("OnEvent 失败: %v", err)
			}

			logs, err := store.GetSyncLogs(10)
			if err != nil || len(logs) != 1 {
				t.Fatalf("GetSyncLogs = %v, err = %v, want 1 条", logs, err)
			}
			got := logs[0]
			if got.Result != tt.result {
				t.Errorf("result = %q, want %q", got.Result, tt.result)
			}
			for _, want := range []string{"跳过 1 条规则", "TCP", "443", "DROP", "1.2.3.4/32", "云产品不支持 DROP"} {
				if !strings.Contains(got.Error, want) {
					t.Errorf("error = %q, want 包含 %q", got.Error, want)
				}
			}
		})
	}
}

// TestStoreLogWriter_ErrorDetail 失败事件 → result=failed + error 落库，同时保留已确认计数
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
		Data:      map[string]any{"provider": "tc_lighthouse(lhins-abc)", "domain": "api.example.com", "error": "请求超时", "added": 2, "deleted": 1},
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
	if l.Added != 2 || l.Deleted != 1 {
		t.Errorf("失败记录计数 = added:%d deleted:%d, want 2/1", l.Added, l.Deleted)
	}
}
