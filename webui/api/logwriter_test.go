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

// TestStoreLogWriter_TargetLevelCounts 目标级完成事件 → 一条日志，target 写资源 ID，
// domain 写稳定排序后的来源域名，added/deleted 写云端确认数（Issue7 §7.3）。
func TestStoreLogWriter_TargetLevelCounts(t *testing.T) {
	store, err := config.OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer store.Close()

	w := &StoreLogWriter{Store: store}
	if err := w.OnEvent(notifier.Event{
		Type:      notifier.EventTargetSyncComplete,
		Timestamp: time.Now(),
		Data: map[string]any{
			"provider": "tc_lighthouse(lhins-abc)", "target_id": 7,
			"domains": []string{"a.example.com", "b.example.com"},
			"domain":  "a.example.com, b.example.com",
			"outcome": "success", "added": 2, "deleted": 1,
		},
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
	if l.Target != "lhins-abc" {
		t.Errorf("target = %q, want lhins-abc（资源 ID）", l.Target)
	}
	if l.Domain != "a.example.com, b.example.com" {
		t.Errorf("domain = %q, want 稳定排序后的来源域名", l.Domain)
	}
}

// TestStoreLogWriter_PartialAndCleanupDeferred 目标级口径：
// 平台能力限制 → result=partial 且详情写明无法实施；清理延后 → 仍是 success，
// 只在详情追加可读说明（Issue7 §7.3、§5.3）。
func TestStoreLogWriter_PartialAndCleanupDeferred(t *testing.T) {
	tests := []struct {
		name       string
		outcome    string
		added      int
		deleted    int
		skipped    int
		candidates int
		resolved   int
		deferred   int
		result     string
	}{
		{name: "平台能力限制", outcome: "partial", skipped: 1, result: "partial"},
		{name: "清理延后仍为成功", outcome: "success", added: 1, candidates: 2, resolved: 1, deferred: 1, result: "success"},
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
				Type:      notifier.EventTargetSyncComplete,
				Timestamp: time.Now(),
				Data: map[string]any{
					"provider": "ali_swas(i-abc)", "target_id": 3,
					"domains": []string{"api.example.com"}, "domain": "api.example.com",
					"outcome": tt.outcome,
					"added":   tt.added, "deleted": tt.deleted, "skipped": tt.skipped,
					"skipped_details":    details,
					"cleanup_candidates": tt.candidates,
					"cleanup_deleted":    tt.resolved,
					"cleanup_deferred":   tt.deferred,
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
				t.Errorf("result = %q, want %q（cleanup_deferred 不得把 success 改成 partial）", got.Result, tt.result)
			}
			if tt.skipped > 0 {
				for _, want := range []string{"无法实施 1 条期望规则", "TCP", "443", "DROP", "1.2.3.4/32", "云产品不支持 DROP"} {
					if !strings.Contains(got.Error, want) {
						t.Errorf("error = %q, want 包含 %q", got.Error, want)
					}
				}
			}
			if tt.deferred > 0 && !strings.Contains(got.Error, "延后") {
				t.Errorf("清理延后必须写可读详情，实际 %q", got.Error)
			}
		})
	}
}

// TestStoreLogWriter_ReturnsStoreError 写库失败必须直接返回给 EventBus，不得内部吞掉。
func TestStoreLogWriter_ReturnsStoreError(t *testing.T) {
	store, err := config.OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("关闭数据库失败: %v", err)
	}

	w := &StoreLogWriter{Store: store}
	err = w.OnEvent(notifier.Event{
		Type:      notifier.EventTargetSyncComplete,
		Timestamp: time.Now(),
		Data: map[string]any{
			"provider": "tc_lighthouse(lhins-abc)", "target_id": 1,
			"domains": []string{"a.example.com"}, "domain": "a.example.com", "outcome": "success",
		},
	})
	if err == nil {
		t.Fatal("写库失败必须返回错误，由 EventBus 统一 WARN（不得内部 WARN 后返回 nil）")
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
