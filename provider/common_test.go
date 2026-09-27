package provider

import (
	"net"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/portconv"
)

// mockProvider 用于测试的模拟 Provider
type mockProvider struct {
	cloudType   config.CloudType
	targetIndex int
}

func (m *mockProvider) Name() string                         { return "mock" }
func (m *mockProvider) CloudType() config.CloudType          { return m.cloudType }
func (m *mockProvider) TargetIndex() int                     { return m.targetIndex }
func (m *mockProvider) GetRules() ([]config.RuleInfo, error) { return nil, nil }
func (m *mockProvider) CreateRules(rules []config.RuleAction) (CreateResult, error) {
	return CreateResult{Written: len(rules)}, nil
}
func (m *mockProvider) DeleteRules(rules []config.RuleInfo) error { return nil }
func (m *mockProvider) ConvertPorts(port string) []string {
	return portconv.Parse(port)
}

func TestOwnedRules(t *testing.T) {
	allRules := []config.RuleInfo{
		{Protocol: "TCP", Port: "80", CidrBlock: "1.2.3.4/32", Description: "[auto-dns] 生产API"},
		{Protocol: "TCP", Port: "443", CidrBlock: "1.2.3.4/32", Description: "[auto-dns] 生产API"},
		{Protocol: "TCP", Port: "22", CidrBlock: "0.0.0.0/0", Description: "手动规则"},
		{Protocol: "", Port: "", CidrBlock: "", Description: "[auto-dns] 模板规则"},
	}
	owned := OwnedRules(allRules, "auto-dns")
	if len(owned) != 2 {
		t.Errorf("OwnedRules 数量 = %d, want 2", len(owned))
	}
}

func TestDiff_AddNew(t *testing.T) {
	p := &mockProvider{cloudType: config.CloudTCLighthouse}
	resolved := []dns.ResolvedIP{
		{IP: net.ParseIP("1.2.3.4"), IsIPv6: false},
	}
	rule := config.DomainRule{
		Host:     "api.example.com",
		Protocol: "TCP",
		Ports:    "443",
		Action:   "ACCEPT",
	}
	desc := "[auto-dns] 生产API"

	// 无现有规则 → 应新增
	diff := Diff(resolved, rule, desc, nil, p)
	if len(diff.ToAdd) != 1 {
		t.Fatalf("ToAdd 数量 = %d, want 1", len(diff.ToAdd))
	}
	if diff.ToAdd[0].CidrBlock != "1.2.3.4/32" {
		t.Errorf("ToAdd[0].CidrBlock = %s, want 1.2.3.4/32", diff.ToAdd[0].CidrBlock)
	}
	if len(diff.ToDelete) != 0 {
		t.Errorf("ToDelete 数量 = %d, want 0", len(diff.ToDelete))
	}
}

func TestDiff_NoChange(t *testing.T) {
	p := &mockProvider{cloudType: config.CloudTCLighthouse}
	resolved := []dns.ResolvedIP{
		{IP: net.ParseIP("1.2.3.4"), IsIPv6: false},
	}
	rule := config.DomainRule{
		Host:     "api.example.com",
		Protocol: "TCP",
		Ports:    "443",
		Action:   "ACCEPT",
	}
	desc := "[auto-dns] 生产API"
	existing := []config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "1.2.3.4/32", Action: "ACCEPT", Description: desc},
	}

	diff := Diff(resolved, rule, desc, existing, p)
	if len(diff.ToAdd) != 0 {
		t.Errorf("ToAdd 数量 = %d, want 0", len(diff.ToAdd))
	}
	if len(diff.ToDelete) != 0 {
		t.Errorf("ToDelete 数量 = %d, want 0", len(diff.ToDelete))
	}
}

func TestDiff_DeleteOld(t *testing.T) {
	p := &mockProvider{cloudType: config.CloudTCLighthouse}
	// DNS 解析到新 IP
	resolved := []dns.ResolvedIP{
		{IP: net.ParseIP("5.6.7.8"), IsIPv6: false},
	}
	rule := config.DomainRule{
		Host:     "api.example.com",
		Protocol: "TCP",
		Ports:    "443",
		Action:   "ACCEPT",
	}
	desc := "[auto-dns] 生产API"
	// 现有规则是旧 IP
	existing := []config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "1.2.3.4/32", Action: "ACCEPT", Description: desc, RuleID: "r-123"},
	}

	diff := Diff(resolved, rule, desc, existing, p)
	if len(diff.ToAdd) != 1 {
		t.Fatalf("ToAdd 数量 = %d, want 1", len(diff.ToAdd))
	}
	if diff.ToAdd[0].CidrBlock != "5.6.7.8/32" {
		t.Errorf("ToAdd[0].CidrBlock = %s, want 5.6.7.8/32", diff.ToAdd[0].CidrBlock)
	}
	if len(diff.ToDelete) != 1 {
		t.Fatalf("ToDelete 数量 = %d, want 1", len(diff.ToDelete))
	}
	if diff.ToDelete[0].RuleID != "r-123" {
		t.Errorf("ToDelete[0].RuleID = %s, want r-123", diff.ToDelete[0].RuleID)
	}
}

func TestDiff_TCPUDPSplit(t *testing.T) {
	// Lighthouse 不支持 TCP+UDP，应拆分为两条
	p := &mockProvider{cloudType: config.CloudTCLighthouse}
	resolved := []dns.ResolvedIP{
		{IP: net.ParseIP("1.2.3.4"), IsIPv6: false},
	}
	rule := config.DomainRule{
		Host:     "game.example.com",
		Protocol: "TCP+UDP",
		Ports:    "8000",
		Action:   "ACCEPT",
	}
	desc := "[auto-dns] 游戏"

	diff := Diff(resolved, rule, desc, nil, p)
	if len(diff.ToAdd) != 2 {
		t.Fatalf("TCP+UDP 拆分后 ToAdd 数量 = %d, want 2", len(diff.ToAdd))
	}
	protocols := map[string]bool{}
	for _, a := range diff.ToAdd {
		protocols[a.Protocol] = true
	}
	if !protocols["TCP"] || !protocols["UDP"] {
		t.Errorf("应包含 TCP 和 UDP, got %v", protocols)
	}
}

func TestDiff_SWASNoSplit(t *testing.T) {
	// SWAS 原生支持 TCP+UDP，不拆分
	p := &mockProvider{cloudType: config.CloudAliSWAS}
	resolved := []dns.ResolvedIP{
		{IP: net.ParseIP("1.2.3.4"), IsIPv6: false},
	}
	rule := config.DomainRule{
		Host:     "game.example.com",
		Protocol: "TCP+UDP",
		Ports:    "8000",
		Action:   "ACCEPT",
	}
	desc := "[auto-dns] 游戏"

	diff := Diff(resolved, rule, desc, nil, p)
	if len(diff.ToAdd) != 1 {
		t.Fatalf("SWAS TCP+UDP 不拆分, ToAdd 数量 = %d, want 1", len(diff.ToAdd))
	}
	if diff.ToAdd[0].Protocol != "TCP+UDP" {
		t.Errorf("Protocol = %s, want TCP+UDP", diff.ToAdd[0].Protocol)
	}
}

func TestDiff_SWASSkipsIPv6(t *testing.T) {
	// SWAS 不支持 IPv6
	p := &mockProvider{cloudType: config.CloudAliSWAS}
	resolved := []dns.ResolvedIP{
		{IP: net.ParseIP("1.2.3.4"), IsIPv6: false},
		{IP: net.ParseIP("2001:db8::1"), IsIPv6: true},
	}
	rule := config.DomainRule{
		Host:     "api.example.com",
		Protocol: "TCP",
		Ports:    "443",
		Action:   "ACCEPT",
	}
	desc := "[auto-dns]"

	diff := Diff(resolved, rule, desc, nil, p)
	// 仅 IPv4 生成规则，IPv6 跳过
	if len(diff.ToAdd) != 1 {
		t.Fatalf("SWAS 跳过 IPv6, ToAdd 数量 = %d, want 1", len(diff.ToAdd))
	}
	if diff.ToAdd[0].CidrBlock != "1.2.3.4/32" {
		t.Errorf("CidrBlock = %s, want 1.2.3.4/32", diff.ToAdd[0].CidrBlock)
	}
}

func TestDiff_ECSSkipsICMPv6(t *testing.T) {
	// ECS 不支持 ICMPv6
	p := &mockProvider{cloudType: config.CloudAliECS}
	resolved := []dns.ResolvedIP{
		{IP: net.ParseIP("1.2.3.4"), IsIPv6: false},
		{IP: net.ParseIP("2001:db8::1"), IsIPv6: true},
	}
	rule := config.DomainRule{
		Host:     "ping.example.com",
		Protocol: "ICMP",
		Ports:    "ALL",
		Action:   "ACCEPT",
	}
	desc := "[auto-dns] ping"

	diff := Diff(resolved, rule, desc, nil, p)
	// IPv4 ICMP 生成规则，IPv6 ICMP 跳过
	if len(diff.ToAdd) != 1 {
		t.Fatalf("ECS 跳过 ICMPv6, ToAdd 数量 = %d, want 1", len(diff.ToAdd))
	}
	if diff.ToAdd[0].CidrBlock != "1.2.3.4/32" {
		t.Errorf("CidrBlock = %s, want 1.2.3.4/32", diff.ToAdd[0].CidrBlock)
	}
}

func TestDiff_DomainIsolation(t *testing.T) {
	// 不同域名的规则互不影响
	p := &mockProvider{cloudType: config.CloudTCLighthouse}
	resolved := []dns.ResolvedIP{
		{IP: net.ParseIP("1.2.3.4"), IsIPv6: false},
	}
	rule := config.DomainRule{
		Host:     "api.example.com",
		Protocol: "TCP",
		Ports:    "443",
		Action:   "ACCEPT",
	}
	desc := "[auto-dns] 生产API"
	// 现有规则属于另一个域名
	existing := []config.RuleInfo{
		{Protocol: "TCP", Port: "80", CidrBlock: "5.6.7.8/32", Action: "ACCEPT", Description: "[auto-dns] 其他域名"},
	}

	diff := Diff(resolved, rule, desc, existing, p)
	// 应新增（当前域名无规则），不删除（其他域名的规则不动）
	if len(diff.ToAdd) != 1 {
		t.Errorf("ToAdd 数量 = %d, want 1", len(diff.ToAdd))
	}
	if len(diff.ToDelete) != 0 {
		t.Errorf("ToDelete 数量 = %d, want 0（不应删除其他域名的规则）", len(diff.ToDelete))
	}
}

func TestClientPool(t *testing.T) {
	pool := NewClientPool(Credentials{TencentSecretID: "AKID1", TencentSecretKey: "sk"})
	callCount := 0

	key := pool.CacheKey(config.CloudTCLighthouse, "ap-guangzhou")
	create := func() (any, error) {
		callCount++
		return "client-1", nil
	}

	// 第一次创建
	c1, err := pool.GetOrCreate(key, create)
	if err != nil {
		t.Fatalf("GetOrCreate 失败: %v", err)
	}
	if c1 != "client-1" {
		t.Errorf("client = %v, want client-1", c1)
	}

	// 第二次复用
	c2, err := pool.GetOrCreate(key, create)
	if err != nil {
		t.Fatalf("GetOrCreate 失败: %v", err)
	}
	if c2 != "client-1" {
		t.Errorf("client = %v, want client-1", c2)
	}

	// create 只调用一次
	if callCount != 1 {
		t.Errorf("create 调用次数 = %d, want 1", callCount)
	}
}

// TestClientPoolHoldsImmutableCredentials pool 创建后凭据不可变，
// 且 Credentials() 返回的是值副本（修改副本不影响 pool）。
func TestClientPoolHoldsImmutableCredentials(t *testing.T) {
	pool := NewClientPool(Credentials{TencentSecretID: "AKID-a", TencentSecretKey: "sk-a"})

	got := pool.Credentials()
	if got.TencentSecretID != "AKID-a" || got.TencentSecretKey != "sk-a" {
		t.Fatalf("pool 未持有创建时的凭据: %+v", got)
	}
	got.TencentSecretID = "mutated"
	if pool.Credentials().TencentSecretID != "AKID-a" {
		t.Errorf("修改返回值不应影响 pool 内部凭据: %+v", pool.Credentials())
	}
}

// TestClientPoolCacheKeyDistinguishesAccount 缓存键至少区分 cloud type、region 与账户标识：
// 不同账户的 pool 对同一 cloud type + region 不得命中同一 client（Build6 §12.6）。
func TestClientPoolCacheKeyDistinguishesAccount(t *testing.T) {
	poolA := NewClientPool(Credentials{TencentSecretID: "AKID-a"})
	poolB := NewClientPool(Credentials{TencentSecretID: "AKID-b"})

	keyA := poolA.CacheKey(config.CloudTCLighthouse, "ap-guangzhou")
	keyB := poolB.CacheKey(config.CloudTCLighthouse, "ap-guangzhou")
	if keyA == keyB {
		t.Fatalf("不同账户的缓存键必须不同: %q", keyA)
	}
	if poolA.CacheKey(config.CloudTCLighthouse, "ap-guangzhou") == poolA.CacheKey(config.CloudTCCVM, "ap-guangzhou") {
		t.Errorf("缓存键必须区分 cloud type")
	}
	if poolA.CacheKey(config.CloudTCLighthouse, "ap-guangzhou") == poolA.CacheKey(config.CloudTCLighthouse, "ap-beijing") {
		t.Errorf("缓存键必须区分 region")
	}

	// 账户不同的两个 pool 各自独立创建 client
	calls := 0
	create := func() (any, error) { calls++; return "c", nil }
	if _, err := poolA.GetOrCreate(keyA, create); err != nil {
		t.Fatalf("poolA GetOrCreate 失败: %v", err)
	}
	if _, err := poolB.GetOrCreate(keyB, create); err != nil {
		t.Fatalf("poolB GetOrCreate 失败: %v", err)
	}
	if calls != 2 {
		t.Errorf("不同账户应各自创建 client，create 次数 = %d, want 2", calls)
	}
}

// TestProviderFactoryUsesPoolCredentials 四个 Provider 工厂只从 pool 取凭据：
// 使用不同凭据的两个 pool 创建同一目标，产生的 client 缓存条目必须互相独立。
func TestProviderFactoryUsesPoolCredentials(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudTCLighthouse, config.CloudTCCVM, config.CloudAliSWAS, config.CloudAliECS} {
		t.Run(string(ct), func(t *testing.T) {
			target := config.TargetConfig{CloudType: ct, Region: "ap-guangzhou", ResourceID: "res-1"}
			poolA := NewClientPool(Credentials{TencentSecretID: "A", TencentSecretKey: "a", AliyunAccessKeyID: "A", AliyunAccessKeySecret: "a"})
			poolB := NewClientPool(Credentials{TencentSecretID: "B", TencentSecretKey: "b", AliyunAccessKeyID: "B", AliyunAccessKeySecret: "b"})

			if _, err := NewProvider(target, 1, poolA); err != nil {
				t.Fatalf("poolA 创建 Provider 失败: %v", err)
			}
			if _, err := NewProvider(target, 2, poolB); err != nil {
				t.Fatalf("poolB 创建 Provider 失败: %v", err)
			}
			if poolA.CacheKey(ct, target.Region) == poolB.CacheKey(ct, target.Region) {
				t.Errorf("不同凭据的 pool 不应共享缓存键")
			}
		})
	}
}

// TestDiff_ICMPPortNormalize R16-01：ICMP 规则 desired 端口 -1/-1（SWAS 云格式）与 existing 端口 ALL（归一化格式）应等价，Diff 为空
func TestDiff_ICMPPortNormalize(t *testing.T) {
	p := &mockProvider{cloudType: config.CloudAliSWAS}
	resolved := []dns.ResolvedIP{
		{IP: net.ParseIP("1.2.3.4"), IsIPv6: false},
	}
	rule := config.DomainRule{
		Host:     "ping.example.com",
		Protocol: "ICMP",
		Ports:    "ALL",
		Action:   "ACCEPT",
	}
	desc := "[auto-dns] ping"
	// existing 侧：SWAS GetRules 归一化后端口为 ALL
	existing := []config.RuleInfo{
		{Protocol: "ICMP", Port: "ALL", CidrBlock: "1.2.3.4/32", Action: "ACCEPT", Description: desc, RuleID: "r-1"},
	}

	diff := Diff(resolved, rule, desc, existing, p)
	if len(diff.ToAdd) != 0 {
		t.Errorf("ToAdd 数量 = %d, want 0（ICMP -1/-1 与 ALL 应等价）", len(diff.ToAdd))
	}
	if len(diff.ToDelete) != 0 {
		t.Errorf("ToDelete 数量 = %d, want 0", len(diff.ToDelete))
	}
}

// TestDiff_ICMPCVM R16-01：CVM desired 端口 ALL 与 existing 空串（CVM 云端省略 Port）应等价，Diff 为空
func TestDiff_ICMPCVM(t *testing.T) {
	p := &mockProvider{cloudType: config.CloudTCCVM}
	resolved := []dns.ResolvedIP{
		{IP: net.ParseIP("1.2.3.4"), IsIPv6: false},
	}
	rule := config.DomainRule{
		Host:     "ping.example.com",
		Protocol: "ICMP",
		Ports:    "ALL",
		Action:   "ACCEPT",
	}
	desc := "[auto-dns] ping"
	existing := []config.RuleInfo{
		{Protocol: "ICMP", Port: "", CidrBlock: "1.2.3.4/32", Action: "ACCEPT", Description: desc, PolicyIndex: "10"},
	}

	diff := Diff(resolved, rule, desc, existing, p)
	if len(diff.ToAdd) != 0 {
		t.Errorf("ToAdd 数量 = %d, want 0（CVM ICMP ALL 与空串应等价）", len(diff.ToAdd))
	}
	if len(diff.ToDelete) != 0 {
		t.Errorf("ToDelete 数量 = %d, want 0", len(diff.ToDelete))
	}
}

// TestDiff_NonICMPUnchanged 回归：非 ICMP 规则端口比较行为不变；TCP ALL（SWAS -1/-1）与 ALL 等价
func TestDiff_NonICMPUnchanged(t *testing.T) {
	p := &mockProvider{cloudType: config.CloudAliSWAS}
	resolved := []dns.ResolvedIP{
		{IP: net.ParseIP("1.2.3.4"), IsIPv6: false},
	}

	// TCP 规则：desired 443（ConvertPorts 后为 443/443）对 existing 443 → 空 Diff
	rule := config.DomainRule{
		Host:     "api.example.com",
		Protocol: "TCP",
		Ports:    "443",
		Action:   "ACCEPT",
	}
	desc := "[auto-dns] 生产API"
	existing := []config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "1.2.3.4/32", Action: "ACCEPT", Description: desc, RuleID: "r-1"},
	}
	diff := Diff(resolved, rule, desc, existing, p)
	if len(diff.ToAdd) != 0 || len(diff.ToDelete) != 0 {
		t.Errorf("TCP 443 应无变更, ToAdd=%d ToDelete=%d", len(diff.ToAdd), len(diff.ToDelete))
	}

	// TCP ALL 规则：desired -1/-1（SWAS 云格式）对 existing ALL → 空 Diff（非 ICMP 的 -1/-1 归一化）
	ruleAll := config.DomainRule{
		Host:     "any.example.com",
		Protocol: "TCP",
		Ports:    "ALL",
		Action:   "ACCEPT",
	}
	existingAll := []config.RuleInfo{
		{Protocol: "TCP", Port: "ALL", CidrBlock: "1.2.3.4/32", Action: "ACCEPT", Description: desc, RuleID: "r-2"},
	}
	diffAll := Diff(resolved, ruleAll, desc, existingAll, p)
	if len(diffAll.ToAdd) != 0 || len(diffAll.ToDelete) != 0 {
		t.Errorf("TCP ALL 应无变更, ToAdd=%d ToDelete=%d", len(diffAll.ToAdd), len(diffAll.ToDelete))
	}
}

func TestRuleChangeFromAction(t *testing.T) {
	// IPv4 场景
	a4 := config.RuleAction{
		Protocol:    "TCP",
		Port:        "443",
		CidrBlock:   "1.2.3.4/32",
		Action:      "ACCEPT",
		Description: "[auto-dns] 生产API",
	}
	c4 := RuleChangeFromAction(a4)
	if c4.Cidr != "1.2.3.4/32" {
		t.Errorf("IPv4 Cidr = %s, want 1.2.3.4/32", c4.Cidr)
	}
	if c4.Desc != "[auto-dns] 生产API" {
		t.Errorf("Desc = %s, want [auto-dns] 生产API", c4.Desc)
	}
	// IPv6 场景：CidrBlock 为空时取 Ipv6CidrBlock
	a6 := config.RuleAction{
		Protocol:      "ICMP",
		Port:          "ALL",
		Ipv6CidrBlock: "2001:db8::1/128",
		Action:        "ACCEPT",
		Description:   "[auto-dns] ping",
	}
	c6 := RuleChangeFromAction(a6)
	if c6.Cidr != "2001:db8::1/128" {
		t.Errorf("IPv6 Cidr = %s, want 2001:db8::1/128", c6.Cidr)
	}
	if c6.Protocol != "ICMP" {
		t.Errorf("Protocol = %s, want ICMP", c6.Protocol)
	}
}

func TestRuleChangeFromInfo(t *testing.T) {
	// IPv4 场景
	r4 := config.RuleInfo{
		Protocol:    "TCP",
		Port:        "80",
		CidrBlock:   "5.6.7.8/32",
		Action:      "ACCEPT",
		Description: "[auto-dns] 旧IP",
		RuleID:      "r-1",
	}
	c4 := RuleChangeFromInfo(r4)
	if c4.Cidr != "5.6.7.8/32" {
		t.Errorf("IPv4 Cidr = %s, want 5.6.7.8/32", c4.Cidr)
	}
	if c4.Desc != "[auto-dns] 旧IP" {
		t.Errorf("Desc = %s, want [auto-dns] 旧IP", c4.Desc)
	}
	// IPv6 场景
	r6 := config.RuleInfo{
		Protocol:      "ICMP",
		Port:          "",
		Ipv6CidrBlock: "2001:db8::2/128",
		Action:        "ACCEPT",
		Description:   "[auto-dns] ping6",
		PolicyIndex:   "10",
	}
	c6 := RuleChangeFromInfo(r6)
	if c6.Cidr != "2001:db8::2/128" {
		t.Errorf("IPv6 Cidr = %s, want 2001:db8::2/128", c6.Cidr)
	}
	if c6.Port != "" {
		t.Errorf("Port = %s, want 空串", c6.Port)
	}
}

// ─── Step 7：纯转换补测（不访问真实云 API） ───

// TestDiff_TCPUDPSplitForCVMAndECS 除 Lighthouse 外，CVM/ECS 同样必须拆分 TCP+UDP
// （仅 SWAS 原生支持，见 TestDiff_SWASNoSplit）。
func TestDiff_TCPUDPSplitForCVMAndECS(t *testing.T) {
	resolved := []dns.ResolvedIP{{IP: net.ParseIP("1.2.3.4"), IsIPv6: false}}
	rule := config.DomainRule{Host: "game.example.com", Protocol: "TCP+UDP", Ports: "8000", Action: "ACCEPT"}

	for _, ct := range []config.CloudType{config.CloudTCCVM, config.CloudAliECS} {
		t.Run(string(ct), func(t *testing.T) {
			diff := Diff(resolved, rule, "[auto-dns] 游戏", nil, &mockProvider{cloudType: ct})
			if len(diff.ToAdd) != 2 {
				t.Fatalf("TCP+UDP 拆分后 ToAdd 数量 = %d, want 2", len(diff.ToAdd))
			}
			protocols := map[string]bool{}
			for _, a := range diff.ToAdd {
				protocols[a.Protocol] = true
			}
			if !protocols["TCP"] || !protocols["UDP"] {
				t.Errorf("应同时包含 TCP 与 UDP, got %v", protocols)
			}
		})
	}
}

// TestDiff_TCPUDPSplitMultiPortCartesian 拆分后的协议数 × 端口数必须是完整笛卡尔积。
func TestDiff_TCPUDPSplitMultiPortCartesian(t *testing.T) {
	resolved := []dns.ResolvedIP{
		{IP: net.ParseIP("1.2.3.4"), IsIPv6: false},
		{IP: net.ParseIP("2001:db8::1"), IsIPv6: true},
	}
	rule := config.DomainRule{Host: "game.example.com", Protocol: "TCP+UDP", Ports: "8000,9000", Action: "ACCEPT"}

	diff := Diff(resolved, rule, "[auto-dns] 游戏", nil, &mockProvider{cloudType: config.CloudTCLighthouse})
	// 2 地址族 × 2 协议（TCP/UDP）× 2 端口 = 8
	if len(diff.ToAdd) != 8 {
		t.Fatalf("笛卡尔积数量 = %d, want 8", len(diff.ToAdd))
	}
	seen := map[string]bool{}
	for _, a := range diff.ToAdd {
		cidr := a.CidrBlock
		if cidr == "" {
			cidr = a.Ipv6CidrBlock
		}
		seen[a.Protocol+"|"+a.Port+"|"+cidr] = true
	}
	if len(seen) != 8 {
		t.Errorf("笛卡尔积存在重复组合: %v", seen)
	}
}

// TestBuildDesired_AddressFamilyFieldMapping IPv4 只写 CidrBlock、IPv6 只写 Ipv6CidrBlock（字段互斥）。
func TestBuildDesired_AddressFamilyFieldMapping(t *testing.T) {
	resolved := []dns.ResolvedIP{
		{IP: net.ParseIP("1.2.3.4"), IsIPv6: false},
		{IP: net.ParseIP("2001:db8::1"), IsIPv6: true},
	}
	rule := config.DomainRule{Host: "api.example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT"}

	got := buildDesired(resolved, rule, "[auto-dns] api", &mockProvider{cloudType: config.CloudTCCVM})
	if len(got) != 2 {
		t.Fatalf("期望规则数 = %d, want 2", len(got))
	}

	v4 := got[0]
	if v4.CidrBlock != "1.2.3.4/32" || v4.Ipv6CidrBlock != "" {
		t.Errorf("IPv4 规则字段错误: CidrBlock=%q Ipv6CidrBlock=%q", v4.CidrBlock, v4.Ipv6CidrBlock)
	}
	v6 := got[1]
	if v6.Ipv6CidrBlock != "2001:db8::1/128" || v6.CidrBlock != "" {
		t.Errorf("IPv6 规则字段错误: CidrBlock=%q Ipv6CidrBlock=%q", v6.CidrBlock, v6.Ipv6CidrBlock)
	}
	if v4.Port != "443" || v6.Port != "443" {
		t.Errorf("端口必须保持一致: v4=%q v6=%q", v4.Port, v6.Port)
	}
}
