package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 本文件是 Build8 Step 8 的正式调度回归：只使用本地 TEO/Provider 替身与
// synctest 可控时钟，证明来源隔离、到期时间、缓存复用和双开预检零写入。

type step8FakeTEO struct {
	mu        sync.Mutex
	describes int
	confirms  int
	snapshot  provider.OriginACLSnapshot
	err       error
}

func (f *step8FakeTEO) DescribeOriginACL(_ context.Context, zoneID string) (provider.OriginACLSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.describes++
	if f.err != nil {
		return provider.OriginACLSnapshot{}, f.err
	}
	return f.snapshot, nil
}

func (f *step8FakeTEO) ConfirmOriginACLUpdate(_ context.Context, _ string, admit func() bool) (string, error) {
	if !admit() {
		return "", &provider.TEOError{Kind: provider.TEOErrorCanceled, NotSent: true}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.confirms++
	return "req-confirm", nil
}

func (f *step8FakeTEO) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.describes, f.confirms
}

type step8MemConflictStore struct {
	mu       sync.Mutex
	conflict *config.AddressConflict
	saveErr  error
	getErr   error
}

func (m *step8MemConflictStore) GetAddressConflict(context.Context) (*config.AddressConflict, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.conflict == nil {
		return nil, nil
	}
	cp := *m.conflict
	return &cp, nil
}
func (m *step8MemConflictStore) SaveAddressConflict(_ context.Context, c config.AddressConflict) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	m.conflict = &c
	return nil
}
func (m *step8MemConflictStore) ClearAddressConflict(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conflict = nil
	return nil
}

func step8Snapshot(zone string, prefixes ...string) provider.OriginACLSnapshot {
	cur := make([]netip.Prefix, 0, len(prefixes))
	for _, p := range prefixes {
		prefix, err := netip.ParsePrefix(p)
		if err != nil {
			panic(err)
		}
		cur = append(cur, prefix.Masked())
	}
	nextPrefixes := append([]netip.Prefix(nil), cur...)
	return provider.OriginACLSnapshot{
		ZoneID:    zone,
		Status:    "online",
		Current:   provider.OriginACLVersion{Version: "v1", Prefixes: cur},
		Next:      &provider.OriginACLVersion{Version: "v2", Prefixes: nextPrefixes},
		RequestID: "req-describe",
	}
}

func step8State(t *testing.T, dnsEnabled, edgeEnabled bool, dnsInterval, edgeInterval time.Duration, p provider.Provider, sites []config.EdgeOneSite) *RuntimeState {
	t.Helper()
	targets := []config.TargetConfig{}
	if p != nil {
		targets = []config.TargetConfig{{ID: p.TargetIndex(), CloudType: p.CloudType(), Region: "ap-guangzhou", ResourceID: "sg-step8"}}
	}
	rc := config.RuntimeConfig{
		Targets: targets, Tag: "auto-dns", Interval: dnsInterval, DNS: "8.8.8.8", DNSTimeout: time.Second,
		DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: true, Theme: "light",
		DomainRules:    []config.DomainRule{{ID: 1, Host: "home.example", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0}}},
		EdgeOneEnabled: edgeEnabled, EdgeOneTag: "edgeone", EdgeOneInterval: edgeInterval,
		EdgeOneSites: sites,
	}
	rc.SetDNSFeatureEnabled(dnsEnabled)
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatalf("构造运行时状态失败: %v", err)
	}
	if p != nil {
		st.Providers = []provider.Provider{p}
	}
	return st
}

func step8Site(zone string) config.EdgeOneSite {
	return config.EdgeOneSite{ZoneID: zone, TargetIDs: []int{0}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4"}
}

func step8Resolve(*RuntimeState) func(string) ([]dns.ResolvedIP, error) {
	return func(string) ([]dns.ResolvedIP, error) {
		return []dns.ResolvedIP{{IP: net.ParseIP("192.0.2.10")}}, nil
	}
}

func probeSnapshotCalls(p *targetProbeProvider) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapshotCalls
}

func probeTaggedRules(p *targetProbeProvider, tag string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, rule := range p.rules {
		if strings.HasPrefix(rule.Description, "["+tag+"]") {
			n++
		}
	}
	return n
}

func TestStep8OnlyEdgeOneDueDoesNotWriteDNS(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newProbeProvider(config.CloudTCCVM, 0)
		teo := &step8FakeTEO{snapshot: step8Snapshot("zone-a", "203.0.113.0/24")}
		st := step8State(t, true, true, time.Hour, 10*time.Second, p, []config.EdgeOneSite{step8Site("zone-a")})
		st.TEO = teo
		s := New(NewRuntimeManager(st))
		s.SetConflictState(NewAddressConflictState(&step8MemConflictStore{}))
		s.resolveHostFn = step8Resolve(st)
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()
		startupDNSRules := probeTaggedRules(p, "auto-dns")
		startupCreated := p.createdCount()
		_, _, startupDeletes := p.counts()
		startupDescribes, _ := teo.counts()
		if startupDNSRules == 0 || startupDescribes != 1 {
			t.Fatalf("启动轮应写入 DNS 并查询一次 TEO：dnsRules=%d teo=%d", startupDNSRules, startupDescribes)
		}

		synctest.Sleep(10 * time.Second)
		synctest.Wait()
		if got := probeTaggedRules(p, "auto-dns"); got != startupDNSRules {
			t.Fatalf("仅 EdgeOne 到期时 DNS 规则数 = %d，启动轮后为 %d", got, startupDNSRules)
		}
		for _, desc := range p.createdSince(startupCreated) {
			if strings.HasPrefix(desc, "[auto-dns]") {
				t.Fatalf("仅 EdgeOne 到期时仍创建了 DNS TAG 动作: %q", desc)
			}
		}
		if _, _, deletes := p.counts(); deletes != startupDeletes {
			t.Fatalf("仅 EdgeOne 到期时不应发生删除调用: %d → %d", startupDeletes, deletes)
		}
		if got := probeTaggedRules(p, "edgeone"); got == 0 {
			t.Fatal("仅 EdgeOne 到期时应写入 EdgeOne TAG 规则")
		}
		for _, desc := range p.createdSince(startupCreated) {
			if !strings.HasPrefix(desc, "[edgeone]") {
				t.Fatalf("EdgeOne 到期轮只允许 EdgeOne TAG 新增，实际 %q", desc)
			}
		}
		if describes, confirms := teo.counts(); describes != startupDescribes+1 || confirms != 0 {
			t.Fatalf("EdgeOne 查询/确认 = %d/%d，期望 %d/0", describes, confirms, startupDescribes+1)
		}
		status := s.Status()
		if status.DNS.NextDueAt == nil || status.EdgeOne.NextDueAt == nil {
			t.Fatalf("来源 next_due 缺失：%+v", status)
		}
		if status.Preflight.ObservedAt == nil || status.Preflight.State != PreflightStateClear {
			t.Fatalf("双开无重叠预检状态 = %+v", status.Preflight)
		}
		s.Stop()
		s.Wait()
	})
}

func TestStep8OnlyDNSDueReusesEdgeOneCache(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newProbeProvider(config.CloudTCCVM, 0)
		teo := &step8FakeTEO{snapshot: step8Snapshot("zone-a", "203.0.113.0/24")}
		st := step8State(t, true, true, 10*time.Second, time.Hour, p, []config.EdgeOneSite{step8Site("zone-a")})
		st.TEO = teo
		s := New(NewRuntimeManager(st))
		s.SetConflictState(NewAddressConflictState(&step8MemConflictStore{}))
		s.resolveHostFn = step8Resolve(st)
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()
		startupCalls := probeSnapshotCalls(p)
		startupDescribes, _ := teo.counts()
		startupEdgeRules := probeTaggedRules(p, "edgeone")

		synctest.Sleep(10 * time.Second)
		synctest.Wait()
		if got := probeSnapshotCalls(p); got <= startupCalls {
			t.Fatalf("仅 DNS 到期时应继续执行 DNS：%d → %d", startupCalls, got)
		}
		if describes, _ := teo.counts(); describes != startupDescribes {
			t.Fatalf("有效 EdgeOne 缓存下查询次数 = %d，期望保持 %d", describes, startupDescribes)
		}
		if got := probeTaggedRules(p, "edgeone"); got != startupEdgeRules {
			t.Fatalf("仅 DNS 到期时 EdgeOne 规则数 = %d，启动轮后为 %d", got, startupEdgeRules)
		}
		s.Stop()
		s.Wait()
	})
}

func TestStep8OverlapBlocksBothSourcesBeforeWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newProbeProvider(config.CloudTCCVM, 0)
		teo := &step8FakeTEO{snapshot: step8Snapshot("zone-a", "203.0.113.0/24")}
		st := step8State(t, true, true, time.Hour, time.Hour, p, []config.EdgeOneSite{step8Site("zone-a")})
		st.TEO = teo
		s := New(NewRuntimeManager(st))
		s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
			return []dns.ResolvedIP{{IP: net.ParseIP("203.0.113.7")}}, nil
		}
		store := &step8MemConflictStore{}
		conflict := NewAddressConflictState(store)
		s.SetConflictState(conflict)
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()

		if got := probeSnapshotCalls(p); got != 0 {
			t.Fatalf("跨源重叠时 DNS 目标快照/写入 = %d，期望 0", got)
		}
		status := s.Status()
		if status.Preflight.State != PreflightStateOverlap || status.Preflight.Conflict == nil {
			t.Fatalf("预检状态 = %+v，期望 overlap 与冲突证据", status.Preflight)
		}
		if status.Preflight.Conflict.DNSAddress != "203.0.113.7" ||
			status.Preflight.Conflict.EdgeOnePrefix != "203.0.113.0/24" {
			t.Fatalf("冲突摘要 = %+v", status.Preflight.Conflict)
		}
		if store.conflict == nil || store.conflict.ZoneID != "zone-a" {
			t.Fatalf("冲突记录未持久化: %+v", store.conflict)
		}
		s.Stop()
		s.Wait()
	})
}

func TestStep8StatusNilAndPausedSource(t *testing.T) {
	st := step8State(t, true, true, time.Hour, 2*time.Hour, nil, []config.EdgeOneSite{step8Site("zone-a")})
	st.Config.SyncEnabled = false
	s := New(NewRuntimeManager(st))
	status := s.Status()
	if status.Enabled || !status.DNS.Enabled || !status.EdgeOne.Enabled {
		t.Fatalf("source enabled 摘要错误: %+v", status)
	}
	if status.DNS.Effective || status.EdgeOne.Effective {
		t.Fatalf("总暂停时 effective 必须为 false: %+v", status)
	}
}

func TestStep8EdgeOneOnlySkipsDNS(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		teo := &step8FakeTEO{snapshot: step8Snapshot("zone-a", "203.0.113.0/24")}
		st := step8State(t, false, true, time.Hour, time.Hour, nil, []config.EdgeOneSite{step8Site("zone-a")})
		st.TEO = teo
		s := New(NewRuntimeManager(st))
		resolves := 0
		s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
			resolves++
			return []dns.ResolvedIP{{IP: net.ParseIP("192.0.2.10")}}, nil
		}
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()
		if resolves != 0 {
			t.Fatalf("仅 EdgeOne 启用时不应解析 DNS，实际 %d", resolves)
		}
		if describes, _ := teo.counts(); describes != 1 {
			t.Fatalf("仅 EdgeOne 启用时应查询一次，实际 %d", describes)
		}
		status := s.Status()
		if status.DNS.Enabled || !status.EdgeOne.Enabled || !status.EdgeOne.Effective {
			t.Fatalf("来源状态错误: %+v", status)
		}
	})
}

func TestStep8BothDisabledNoSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		teo := &step8FakeTEO{snapshot: step8Snapshot("zone-a", "203.0.113.0/24")}
		st := step8State(t, false, false, time.Hour, time.Hour, nil, nil)
		st.TEO = teo
		s := New(NewRuntimeManager(st))
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()
		synctest.Sleep(2 * time.Hour)
		synctest.Wait()
		if describes, _ := teo.counts(); describes != 0 {
			t.Fatalf("两开关关闭时 TEO 查询 = %d，期望 0", describes)
		}
		status := s.Status()
		if status.DNS.NextDueAt != nil || status.EdgeOne.NextDueAt != nil {
			t.Fatalf("两开关关闭时仍发布 next_due: %+v", status)
		}
	})
}

func TestStep8NewSiteSchedulesEdgeOne(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		teo := &step8FakeTEO{snapshot: step8Snapshot("zone-a", "203.0.113.0/24")}
		st := step8State(t, false, true, time.Hour, time.Hour, nil, nil)
		st.TEO = teo
		s := New(NewRuntimeManager(st))
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()
		if describes, _ := teo.counts(); describes != 0 {
			t.Fatalf("无站点时不应查询 TEO，实际 %d", describes)
		}
		next := step8State(t, false, true, time.Hour, time.Hour, nil, []config.EdgeOneSite{step8Site("zone-a")})
		next.TEO = teo
		s.ApplyState(next)
		synctest.Wait()
		if describes, _ := teo.counts(); describes != 1 {
			t.Fatalf("新增站点后应安排一次 EdgeOne 查询，实际 %d", describes)
		}
	})
}

func TestStep8StatusJSONShape(t *testing.T) {
	st := step8State(t, true, true, time.Hour, 2*time.Hour, nil, []config.EdgeOneSite{step8Site("zone-a")})
	s := New(NewRuntimeManager(st))
	due := time.Now().Add(time.Hour).UTC()
	s.mu.Lock()
	s.dnsScheduled, s.edgeScheduled = true, true
	s.dnsNextDue, s.edgeNextDue = due, due.Add(time.Hour)
	s.preflightSet = true
	s.preflight = PreflightOutcome{State: PreflightStateClear, DNSComplete: true, EdgeOneComplete: true}
	s.preflightAt = due
	s.mu.Unlock()
	raw, err := json.Marshal(s.Status())
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	for _, key := range []string{"running", "enabled", "last_sync", "dns", "edgeone", "address_preflight"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("状态缺少原字段或新字段 %q: %s", key, raw)
		}
	}
	var dns, edge, pre map[string]any
	_ = json.Unmarshal(decoded["dns"], &dns)
	_ = json.Unmarshal(decoded["edgeone"], &edge)
	_ = json.Unmarshal(decoded["address_preflight"], &pre)
	if dns["next_due_at"] == nil || edge["next_due_at"] == nil || pre["state"] != "clear" {
		t.Fatalf("状态摘要 = %s", raw)
	}
}

func TestStep8EvaluateRefusesStaleState(t *testing.T) {
	old := step8State(t, true, true, time.Hour, time.Hour, nil, []config.EdgeOneSite{step8Site("zone-a")})
	s := New(NewRuntimeManager(old))
	store := &step8MemConflictStore{}
	s.SetConflictState(NewAddressConflictState(store))
	next := *old
	next.Config = old.Config.DeepCopy()
	next.Config.Theme = "dark"
	s.ApplyState(&next)
	_, err := s.evaluatePreflight(old, []DNSAddress{{Host: "home.example", Addr: netip.MustParseAddr("192.0.2.1")}}, true, nil, true)
	if err == nil {
		t.Fatal("旧状态预检必须被稳定区拒绝")
	}
	if store.conflict != nil {
		t.Fatalf("旧状态预检写入了冲突: %+v", store.conflict)
	}
}

func TestStep8ConflictStoreFailureBlocksAndRecovers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newProbeProvider(config.CloudTCCVM, 0)
		teo := &step8FakeTEO{snapshot: step8Snapshot("zone-a", "203.0.113.0/24")}
		st := step8State(t, true, true, time.Hour, time.Hour, p, []config.EdgeOneSite{step8Site("zone-a")})
		st.TEO = teo
		store := &step8MemConflictStore{saveErr: errors.New("fixture save failure")}
		s := New(NewRuntimeManager(st))
		s.SetConflictState(NewAddressConflictState(store))
		overlap := true
		s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
			if overlap {
				return []dns.ResolvedIP{{IP: net.ParseIP("203.0.113.7")}}, nil
			}
			return []dns.ResolvedIP{{IP: net.ParseIP("192.0.2.10")}}, nil
		}
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()
		if got := probeSnapshotCalls(p); got != 0 {
			t.Fatalf("冲突保存失败时目标快照/写入 = %d，期望 0", got)
		}
		status := s.Status()
		if status.Preflight.State != PreflightStateOverlap || !status.Preflight.KnownConflict {
			t.Fatalf("冲突保存失败后状态 = %+v", status.Preflight)
		}

		overlap = false
		store.mu.Lock()
		store.saveErr = nil
		store.mu.Unlock()
		s.TriggerSync()
		synctest.Wait()
		if got := probeSnapshotCalls(p); got == 0 {
			t.Fatal("完整无重叠且保存恢复后应继续目标")
		}
		if s.Status().Preflight.State != PreflightStateClear {
			t.Fatalf("恢复后预检状态 = %+v", s.Status().Preflight)
		}
	})
}
