package api

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// logWriterChainProvider 构造 R7-02 的真实生产链场景：Desired 已存在，另有两条
// 陈旧 Owned 规则；删除只确认一条，S2 仍保留另一条。
type logWriterChainProvider struct {
	mu       sync.Mutex
	rules    []config.RuleInfo
	notFound bool
}

func (p *logWriterChainProvider) Name() string                { return "ali_ecs(sg-r702)" }
func (p *logWriterChainProvider) CloudType() config.CloudType { return config.CloudAliECS }
func (p *logWriterChainProvider) TargetIndex() int            { return 1 }
func (p *logWriterChainProvider) GetRules() ([]config.RuleInfo, error) {
	snapshot, err := p.GetSnapshot()
	return snapshot.Rules, err
}
func (p *logWriterChainProvider) GetSnapshot() (provider.RuleSnapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return provider.RuleSnapshot{Rules: append([]config.RuleInfo(nil), p.rules...), Revision: "r1"}, nil
}
func (p *logWriterChainProvider) CreateRules(provider.RuleSnapshot, []config.RuleAction) (provider.CreateResult, error) {
	return provider.CreateResult{}, errors.New("整链测试不应新增规则")
}
func (p *logWriterChainProvider) DeleteRules(_ provider.RuleSnapshot, rules []config.RuleInfo) (provider.DeleteResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.notFound {
		if len(rules) != 1 {
			return provider.DeleteResult{}, errors.New("NotFound 整链测试应收到一条清理候选")
		}
		// 模拟候选已被并发者删除，但本次调用只收到幂等 NotFound，不能虚增 Deleted。
		for i, current := range p.rules {
			if current.RuleID == rules[0].RuleID {
				p.rules = append(p.rules[:i], p.rules[i+1:]...)
				break
			}
		}
		return provider.DeleteResult{}, errors.New("ResourceNotFound.FirewallRulesNotFound")
	}
	if len(rules) != 2 {
		return provider.DeleteResult{}, errors.New("整链测试应收到两条清理候选")
	}
	for i, current := range p.rules {
		if current.RuleID == rules[0].RuleID {
			p.rules = append(p.rules[:i], p.rules[i+1:]...)
			break
		}
	}
	return provider.DeleteResult{Deleted: 1, Resolved: 1}, &provider.PartialDeleteError{
		Deleted: 1,
		Err:     errors.New("模拟第二条删除失败"),
	}
}
func (p *logWriterChainProvider) ConvertPorts(port string) []string {
	return provider.ExpandPorts(config.CloudAliECS, port)
}

type eventCapture struct{ ch chan notifier.Event }

func (c eventCapture) OnEvent(event notifier.Event) error {
	select {
	case c.ch <- event:
	default:
	}
	return nil
}

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
			details := []provider.PlanIssue{{
				Code: provider.IssueUnsupportedAction, Message: "云产品不支持 DROP",
				Key: &provider.FunctionalKey{
					Family: provider.AddressIPv4, Protocol: "TCP", Port: "443",
					CIDR: "1.2.3.4/32", Action: "DROP",
				},
			}}
			if err := w.OnEvent(notifier.Event{
				Type:      notifier.EventTargetSyncComplete,
				Timestamp: time.Now(),
				Data: map[string]any{
					"provider": "ali_swas(i-abc)", "target_id": 3,
					"domains": []string{"api.example.com"}, "domain": "api.example.com",
					"outcome":            tt.outcome,
					"added":              tt.added,
					"deleted":            tt.deleted,
					"skipped":            tt.skipped,
					"unsupported":        details,
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

// TestStoreLogWriter_ProductionChainPersistsCleanupCounts 覆盖真实生产链：
// Syncer.Run → syncTarget → publisher → EventBus → StoreLogWriter → SQLite。
// 旧实现会因 publisher 缺少 cleanup_deleted，把“已清理 1 条”落成 0（R7-02）。
func TestStoreLogWriter_ProductionChainPersistsCleanupCounts(t *testing.T) {
	store, err := config.OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer store.Close()

	p := &logWriterChainProvider{rules: []config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "127.0.0.1/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "desired"},
		{Protocol: "TCP", Port: "9998", CidrBlock: "10.9.9.8/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "stale-1"},
		{Protocol: "TCP", Port: "9999", CidrBlock: "10.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "stale-2"},
	}}
	runtimeState, err := syncer.BuildRuntimeState(nil, config.RuntimeConfig{
		Tag: "auto-dns", Interval: time.Hour, DNS: "8.8.8.8", DNSTimeout: 2 * time.Second,
		DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: true, Theme: "light",
		DomainRules: []config.DomainRule{{
			ID: 1, Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{1},
		}},
	}, syncer.BreakerReset)
	if err != nil {
		t.Fatalf("构造运行时状态失败: %v", err)
	}
	runtimeState.Providers = []provider.Provider{p}
	s := syncer.New(syncer.NewRuntimeManager(runtimeState))

	writer := &StoreLogWriter{Store: store}
	s.EventBus().Subscribe(notifier.EventTargetSyncComplete, writer)
	events := make(chan notifier.Event, 1)
	s.EventBus().Subscribe(notifier.EventTargetSyncComplete, eventCapture{ch: events})

	go s.Run()
	t.Cleanup(func() {
		s.Stop()
		done := make(chan struct{})
		go func() {
			s.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("Syncer 未在限期内停止")
		}
	})

	var event notifier.Event
	select {
	case event = <-events:
	case <-time.After(3 * time.Second):
		t.Fatal("未收到目标完成事件")
	}
	if event.Data["cleanup_candidates"] != 2 || event.Data["cleanup_deleted"] != 1 || event.Data["cleanup_deferred"] != 1 {
		t.Fatalf("事件清理计数 = candidates:%v deleted:%v deferred:%v, want 2/1/1",
			event.Data["cleanup_candidates"], event.Data["cleanup_deleted"], event.Data["cleanup_deferred"])
	}
	if got, ok := event.Data["duration_ms"].(int64); !ok || got < 0 {
		t.Fatalf("duration_ms = %#v, want 非负 int64", event.Data["duration_ms"])
	}
	unsupported, ok := event.Data["unsupported"].([]provider.PlanIssue)
	if !ok || unsupported == nil || len(unsupported) != 0 {
		t.Fatalf("unsupported = %#v, want 非 nil 空 []provider.PlanIssue", event.Data["unsupported"])
	}

	deadline := time.Now().Add(3 * time.Second)
	var logs []config.SyncLog
	for {
		logs, err = store.GetSyncLogs(10)
		if err != nil {
			t.Fatalf("读取同步日志失败: %v", err)
		}
		if len(logs) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("目标事件已发布，但 SQLite 未在限期内出现同步日志")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(logs) != 1 {
		t.Fatalf("同步日志数量 = %d, want 1", len(logs))
	}
	got := logs[0]
	if got.Target != "sg-r702" || got.Domain != "localhost" || got.Result != "success" || got.Deleted != 1 {
		t.Errorf("日志主字段 = target:%q domain:%q result:%q deleted:%d, want sg-r702/localhost/success/1",
			got.Target, got.Domain, got.Result, got.Deleted)
	}
	for _, want := range []string{"清理候选 2 条", "已确认清理 1 条", "延后 1 条"} {
		if !strings.Contains(got.Error, want) {
			t.Errorf("日志详情 = %q, want 包含 %q", got.Error, want)
		}
	}
}

// TestStoreLogWriter_ProductionChainPersistsNotFoundS2Convergence 覆盖 R7-04 的真实生产链：
// S1 有一条候选，Delete 返回幂等 NotFound，S2 已证明候选消失；目标事件、整轮汇总与
// SQLite 必须一致记录 1/0/0，且不得把“已不存在”虚报为本进程实际删除。
func TestStoreLogWriter_ProductionChainPersistsNotFoundS2Convergence(t *testing.T) {
	store, err := config.OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer store.Close()

	p := &logWriterChainProvider{notFound: true, rules: []config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "127.0.0.1/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "desired"},
		{Protocol: "TCP", Port: "9999", CidrBlock: "10.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "stale"},
	}}
	runtimeState, err := syncer.BuildRuntimeState(nil, config.RuntimeConfig{
		Tag: "auto-dns", Interval: time.Hour, DNS: "8.8.8.8", DNSTimeout: 2 * time.Second,
		DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: true, Theme: "light",
		DomainRules: []config.DomainRule{{
			ID: 1, Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{1},
		}},
	}, syncer.BreakerReset)
	if err != nil {
		t.Fatalf("构造运行时状态失败: %v", err)
	}
	runtimeState.Providers = []provider.Provider{p}
	s := syncer.New(syncer.NewRuntimeManager(runtimeState))

	writer := &StoreLogWriter{Store: store}
	s.EventBus().Subscribe(notifier.EventTargetSyncComplete, writer)
	targetEvents := make(chan notifier.Event, 1)
	roundEvents := make(chan notifier.Event, 1)
	s.EventBus().Subscribe(notifier.EventTargetSyncComplete, eventCapture{ch: targetEvents})
	s.EventBus().Subscribe(notifier.EventSyncComplete, eventCapture{ch: roundEvents})

	go s.Run()
	t.Cleanup(func() {
		s.Stop()
		done := make(chan struct{})
		go func() {
			s.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("Syncer 未在限期内停止")
		}
	})

	assertCounts := func(label string, event notifier.Event) {
		t.Helper()
		if event.Data["cleanup_candidates"] != 1 || event.Data["cleanup_deleted"] != 0 ||
			event.Data["cleanup_deferred"] != 0 || event.Data["deleted"] != 0 {
			t.Fatalf("%s 清理计数 = candidates:%v cleanup_deleted:%v deferred:%v deleted:%v, want 1/0/0/0",
				label, event.Data["cleanup_candidates"], event.Data["cleanup_deleted"],
				event.Data["cleanup_deferred"], event.Data["deleted"])
		}
	}
	select {
	case event := <-targetEvents:
		assertCounts("目标事件", event)
	case <-time.After(3 * time.Second):
		t.Fatal("未收到目标完成事件")
	}
	select {
	case event := <-roundEvents:
		assertCounts("整轮事件", event)
	case <-time.After(3 * time.Second):
		t.Fatal("未收到整轮完成事件")
	}

	deadline := time.Now().Add(3 * time.Second)
	var logs []config.SyncLog
	for {
		logs, err = store.GetSyncLogs(10)
		if err != nil {
			t.Fatalf("读取同步日志失败: %v", err)
		}
		if len(logs) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("目标事件已发布，但 SQLite 未在限期内出现同步日志")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(logs) != 1 {
		t.Fatalf("同步日志数量 = %d, want 1", len(logs))
	}
	got := logs[0]
	if got.Result != "success" || got.Deleted != 0 {
		t.Fatalf("日志主字段 = result:%q deleted:%d, want success/0", got.Result, got.Deleted)
	}
	for _, want := range []string{"清理候选 1 条", "已确认清理 0 条", "延后 0 条"} {
		if !strings.Contains(got.Error, want) {
			t.Errorf("日志详情 = %q, want 包含 %q", got.Error, want)
		}
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
	if l.Result != "failed" || !strings.HasPrefix(l.Error, "请求超时") {
		t.Errorf("日志 = result:%s error:%s, want failed/请求超时", l.Result, l.Error)
	}
	if l.Added != 2 || l.Deleted != 1 {
		t.Errorf("失败记录计数 = added:%d deleted:%d, want 2/1", l.Added, l.Deleted)
	}
}

// 使用正式 Run/EventBus/SQLite 链验证历史保留、未知和恢复，Provider 只在本地模拟。
type i802ChainProvider struct {
	*logWriterChainProvider
	snapshots int
	mode      string
}

func (p *i802ChainProvider) GetSnapshot() (provider.RuleSnapshot, error) {
	p.snapshots++
	if p.mode == "unknown" {
		return provider.RuleSnapshot{}, errors.New("permission denied")
	}
	if p.snapshots == 3 || (p.mode == "historical" && p.snapshots > 3) {
		if p.mode == "estimated" {
			return provider.RuleSnapshot{}, errors.New("permission denied")
		}
		return provider.RuleSnapshot{}, errors.New("RequestLimitExceeded")
	}
	return p.logWriterChainProvider.GetSnapshot()
}
func (p *i802ChainProvider) DeleteRules(snap provider.RuleSnapshot, rules []config.RuleInfo) (provider.DeleteResult, error) {
	if len(rules) == 1 {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.rules = p.rules[:1]
		return provider.DeleteResult{Deleted: 1, Resolved: 1}, nil
	}
	return p.logWriterChainProvider.DeleteRules(snap, rules)
}
func TestI802_ProductionLogChain(t *testing.T) {
	for _, mode := range []string{"historical", "recover", "estimated", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEnv(t)
			p := &i802ChainProvider{mode: mode, logWriterChainProvider: &logWriterChainProvider{rules: []config.RuleInfo{
				{Protocol: "TCP", Port: "443", CidrBlock: "127.0.0.1/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "desired"},
				{Protocol: "TCP", Port: "80", CidrBlock: "10.0.0.1/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "old1"},
				{Protocol: "TCP", Port: "81", CidrBlock: "10.0.0.2/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "old2"},
			}}}
			state, err := syncer.BuildRuntimeState(nil, config.RuntimeConfig{Tag: "auto-dns", Interval: time.Hour, DNS: "8.8.8.8", DNSTimeout: time.Second, DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: true, Theme: "light", DomainRules: []config.DomainRule{{ID: 1, Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT"}}}, syncer.BreakerReset)
			if err != nil {
				t.Fatal(err)
			}
			state.Providers = []provider.Provider{p}
			s := syncer.New(syncer.NewRuntimeManager(state))
			targetEvents := make(chan notifier.Event, 1)
			roundEvents := make(chan notifier.Event, 1)
			kind := notifier.EventSyncError
			if mode == "recover" {
				kind = notifier.EventTargetSyncComplete
			}
			s.EventBus().Subscribe(kind, &StoreLogWriter{Store: e.store})
			s.EventBus().Subscribe(kind, eventCapture{ch: targetEvents})
			s.EventBus().Subscribe(notifier.EventSyncComplete, eventCapture{ch: roundEvents})
			go s.Run()
			t.Cleanup(func() { s.Stop(); s.Wait() })
			var ev notifier.Event
			select {
			case ev = <-targetEvents:
			case <-time.After(6 * time.Second):
				t.Fatal("目标事件超时")
			}
			var round notifier.Event
			select {
			case round = <-roundEvents:
			case <-time.After(3 * time.Second):
				t.Fatal("整轮事件超时")
			}
			// EventBus 独立投递订阅者；目标/整轮事件到达不代表异步写库已完成。
			deadline := time.Now().Add(3 * time.Second)
			var logs []config.SyncLog
			for {
				logs, err = e.store.GetSyncLogs(10)
				if err != nil {
					t.Fatalf("读取同步日志失败: %v", err)
				}
				if len(logs) > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("目标事件已发布，但 SQLite 未在限期内出现同步日志")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if len(logs) != 1 {
				t.Fatalf("同步日志数量 = %d, want 1", len(logs))
			}
			sum := round.Data["cleanup_observation_summary"].(syncer.CleanupObservationSummary)
			o := ev.Data["cleanup_observation"].(*syncer.CleanupObservation)
			text := logs[0].Error
			if strings.Contains(text, "条：已确认清理") {
				t.Fatalf("仍把快照与累计量拼成数量关系: %s", text)
			}
			switch mode {
			case "unknown":
				if o != nil || sum.UnknownTargets != 1 || logs[0].Deleted != 0 || !strings.Contains(text, "清理观察未知") {
					t.Fatalf("未知: %+v / %s", sum, text)
				}
			case "historical":
				if o == nil || !o.Historical || o.Attempt != 1 || ev.Data["attempts"] != 3 || o.Deferred != 1 || sum.HistoricalTargets != 1 || logs[0].Deleted != 1 || !strings.Contains(text, "历史观察；当前残留未知") || !strings.Contains(text, "删除进度估计") {
					t.Fatalf("历史: %+v %+v / %s", o, sum, text)
				}
			case "estimated":
				if o == nil || o.Historical || o.Basis != "delete_progress" || sum.EstimatedTargets != 1 || logs[0].Deleted != 1 || !strings.Contains(text, "当前残留未确认") {
					t.Fatalf("估计: %+v / %s", sum, text)
				}
			case "recover":
				if o == nil || o.Historical || o.Basis != "s2" || o.Attempt != 2 || o.Candidates != 1 || o.Deferred != 0 || logs[0].Deleted != 2 || !sum.Complete || !strings.Contains(text, "S2 已观察残留：延后 0 条") || !strings.Contains(text, "累计已确认新增 0 条、已确认清理 2 条") {
					t.Fatalf("恢复: %+v / %s", sum, text)
				}
			}
			if s.Status().LastRound.CleanupObservationSummary != sum {
				t.Fatal("状态与整轮事件不同")
			}
			if logs[0].Deleted != toInt(ev.Data["cleanup_deleted"]) {
				t.Fatal("落库确认数丢失")
			}
		})
	}
}

func TestI802_LogSeparateUnsupportedScopes(t *testing.T) {
	e := newTestEnv(t)
	now := time.Now()
	ev := notifier.Event{Type: notifier.EventSyncError, Timestamp: now, Data: map[string]any{
		"provider": "ali_ecs(sg-test)", "error": "DNS failed", "added": 1, "cleanup_deleted": 0,
		"unsupported_observation": &syncer.UnsupportedObservation{
			Latest:       &syncer.PlanObservation{Attempt: 2, Stage: "s1", ObservedAt: now, Complete: false, Issues: []provider.PlanIssue{}},
			LastComplete: &syncer.PlanObservation{Attempt: 1, Stage: "s0", ObservedAt: now.Add(-time.Second), Complete: true, Historical: true, Issues: []provider.PlanIssue{{Code: "unsupported", Message: "不支持 IPv6"}}},
		},
	}}
	if err := (&StoreLogWriter{Store: e.store}).OnEvent(ev); err != nil {
		t.Fatal(err)
	}
	logs, err := e.store.GetSyncLogs(10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("日志 %v %v", logs, err)
	}
	for _, want := range []string{"最新规划：第 2 次尝试 s1", "范围不完整，仅已知部分", "本规划范围内无法实施项 0 条", "最近完整规划：第 1 次尝试 s0", "历史观察", "不支持 IPv6"} {
		if !strings.Contains(logs[0].Error, want) {
			t.Fatalf("缺少 %s: %s", want, logs[0].Error)
		}
	}
}
