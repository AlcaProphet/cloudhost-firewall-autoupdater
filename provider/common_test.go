package provider

import (
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"testing"
)

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
