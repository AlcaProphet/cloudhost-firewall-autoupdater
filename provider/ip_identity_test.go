package provider

import (
	"net/http"
	"net/netip"
	"reflect"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
)

// I8-R27：合法单 IP 与主机前缀必须共用同一功能身份，不能误冻结清理。
func TestI8R27BareIPKeys(t *testing.T) {
	for _, tc := range []struct{ name, v4, v6, want string }{
		{"v4_host", "203.0.113.10", "", "203.0.113.10/32"},
		{"v6_host", "", "2001:db8::10", "2001:db8::10/128"},
		{"v4_prefix", "203.0.113.10/24", "", "203.0.113.0/24"},
		{"v6_prefix", "", "2001:DB8::10/64", "2001:db8::/64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := config.RuleInfo{CidrBlock: tc.v4, Ipv6CidrBlock: tc.v6, Protocol: "TCP", Port: "443", Action: "ACCEPT"}
			keys, ok := SnapshotRuleKeys(r)
			if !ok || len(keys) != 1 || keys[0].CIDR != tc.want {
				t.Fatalf("key=%v ok=%v want CIDR=%s", keys, ok, tc.want)
			}
		})
	}
}
func TestI8R27BareIPInvalid(t *testing.T) {
	for _, tc := range []struct{ name, v4, v6 string }{
		{"empty", "", ""},
		{"both_families", "203.0.113.1", "2001:db8::1"},
		{"invalid", "not-an-ip", ""},
		{"missing_mask", "203.0.113.1/", ""},
		{"v4_mask_overflow", "203.0.113.1/33", ""},
		{"v6_mask_overflow", "", "2001:db8::1/129"},
		{"zone_v6", "", "fe80::1%en0"},
		{"v6_host_in_v4", "2001:db8::1", ""},
		{"v6_prefix_in_v4", "2001:db8::1/128", ""},
		{"v4_host_in_v6", "", "203.0.113.1"},
		{"v4_prefix_in_v6", "", "203.0.113.1/32"},
		{"mapped_in_v4", "::ffff:203.0.113.1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := config.RuleInfo{CidrBlock: tc.v4, Ipv6CidrBlock: tc.v6, Protocol: "TCP", Port: "443", Action: "ACCEPT"}
			if k, ok := SnapshotRuleKeys(r); ok {
				t.Fatalf("invalid address accepted: %+v -> %v", tc, k)
			}
		})
	}
}
func bareIPRuleInfo(ip, desc string) config.RuleInfo {
	return config.RuleInfo{CidrBlock: ip, Protocol: "TCP", Port: "443", Action: "ACCEPT", Description: desc, RuleID: "sgr-" + ip, PolicyIndex: "3"}
}
func TestI8R27BareIPPlan(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudTCLighthouse, config.CloudTCCVM, config.CloudAliECS, config.CloudAliSWAS} {
		for _, mode := range []string{"external_unrelated", "owned_current", "external_current", "owned_stale", "all_protocol_external"} {
			t.Run(string(ct)+"/"+mode, func(t *testing.T) {
				current := bareIPRuleInfo("203.0.113.10/32", "[auto-dns] current")
				stale := bareIPRuleInfo("203.0.113.20/32", "[auto-dns] stale")
				existing := []config.RuleInfo{current, stale}
				switch mode {
				case "external_unrelated":
					existing = append(existing, bareIPRuleInfo("198.51.100.5", "manual"))
				case "owned_current":
					existing[0].CidrBlock = "203.0.113.10"
				case "external_current":
					existing[0].CidrBlock = "203.0.113.10"
					existing[0].Description = "manual"
				case "owned_stale":
					existing[1].CidrBlock = "203.0.113.20"
				case "all_protocol_external":
					existing[0].CidrBlock = "203.0.113.10"
					existing[0].Description = "manual"
					existing[0].Protocol = "ALL"
					existing[0].Port = "ALL"
				}
				original := append([]config.RuleInfo(nil), existing...)
				plan := planFor(t, ct, []config.DomainRule{rule(1, "test.example", "TCP", "443", "ACCEPT", "")}, map[int][]dns.ResolvedIP{1: {v4("203.0.113.10")}}, existing)
				if !reflect.DeepEqual(existing, original) {
					t.Fatal("planner modified raw snapshot")
				}
				if len(plan.Conflicts) != 0 {
					t.Fatalf("unexpected conflicts: %+v", plan.Conflicts)
				}
				if mode == "all_protocol_external" {
					if len(plan.SatisfiedByExternal) != 0 || len(plan.ToAdd) != 1 || len(plan.CleanupDeletable) != 0 {
						t.Fatalf("protocol containment was inferred: %+v", plan)
					}
					return
				}
				if !plan.CoverageReady || len(plan.ToAdd) != 0 || len(plan.CleanupDeletable) != 1 {
					t.Fatalf("coverage=%v toAdd=%d candidates=%d deletable=%d deferred=%+v", plan.CoverageReady, len(plan.ToAdd), len(plan.CleanupCandidates), len(plan.CleanupDeletable), plan.CleanupDeferred)
				}
				if !reflect.DeepEqual(plan.CleanupDeletable[0], existing[1]) {
					t.Fatal("delete locator/raw rule changed")
				}
				if mode == "external_current" && len(plan.SatisfiedByExternal) != 1 {
					t.Fatal("external host not matched")
				}
			})
		}
	}
}
func TestI8R27BareIPLighthouseAmbiguity(t *testing.T) {
	current := bareIPRuleInfo("203.0.113.10/32", "[auto-dns]")
	stale := bareIPRuleInfo("203.0.113.20/32", "[auto-dns]")
	external := bareIPRuleInfo("203.0.113.20", "manual")
	plan := planFor(t, config.CloudTCLighthouse, []config.DomainRule{rule(1, "test.example", "TCP", "443", "ACCEPT", "")}, map[int][]dns.ResolvedIP{1: {v4("203.0.113.10")}}, []config.RuleInfo{current, stale, external})
	if hasIssue(plan.Conflicts, IssueSnapshotRuleInvalid) || !hasIssue(plan.CleanupDeferred, IssueOwnedKeyAmbiguous) || len(plan.CleanupDeletable) != 0 {
		t.Fatalf("ambiguity guard: %+v", plan)
	}
}
func TestI8R27BareIPSDK(t *testing.T) {
	for _, kind := range []string{"lighthouse", "ecs"} {
		t.Run(kind, func(t *testing.T) {
			mock, host := newMockCloudAPI(t)
			mock.reply = func(_ int, _ recordedRequest) (int, string) {
				if kind == "lighthouse" {
					return http.StatusOK, lighthousePage([]string{lighthouseRuleJSON("203.0.113.10")}, 7)
				}
				return http.StatusOK, ecsRulePage("sgr-host", "203.0.113.10", "")
			}
			var p Provider
			if kind == "lighthouse" {
				p = mockLighthouse(t, host)
			} else {
				p = mockECS(t, host)
			}
			snap, err := p.GetSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			if len(snap.Rules) != 1 || snap.Rules[0].CidrBlock != "203.0.113.10" {
				t.Fatalf("raw SDK snapshot changed: %+v", snap)
			}
			keys, ok := SnapshotRuleKeys(snap.Rules[0])
			if !ok || len(keys) != 1 || keys[0].CIDR != "203.0.113.10/32" {
				t.Fatalf("SDK -> planner failed: %v %v", keys, ok)
			}
		})
	}
}

// 地址表示不应改变功能身份，也不应扩大成其他协议或网段的包含关系。
func TestI8R27BareIPEquivalenceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, v4, v6, want string
		family             AddressFamily
	}{
		{"v4_trim", " 203.0.113.10 ", "", "203.0.113.10/32", AddressIPv4},
		{"v6_case", "", "2001:0DB8:0:0::10", "2001:db8::10/128", AddressIPv6},
		{"mapped_prefix_preserved", "", "::ffff:203.0.113.10/128", "::ffff:203.0.113.10/128", AddressIPv6},
		{"mapped_host_no_unmap", "", "::ffff:203.0.113.10", "::ffff:203.0.113.10/128", AddressIPv6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, ok := SnapshotRuleKeys(config.RuleInfo{CidrBlock: tc.v4, Ipv6CidrBlock: tc.v6, Protocol: "TCP", Port: "443", Action: "ACCEPT"})
			if !ok || len(k) != 1 || k[0].CIDR != tc.want || k[0].Family != tc.family {
				t.Fatalf("keys=%+v ok=%v", k, ok)
			}
		})
	}
	for _, raw := range []string{"fe80::1%en0", "fe80::1%en0/128", "203.0.113.10:443", "[2001:db8::1]", "203.0.113.10/32/32", "203.0.113.01", "203.0.113.10/32 trailing"} {
		t.Run("reject_"+raw, func(t *testing.T) {
			if k, ok := CanonicalCIDR(raw); ok {
				t.Fatalf("accepted non-address %q as %q", raw, k)
			}
		})
	}
	host, _ := SnapshotRuleKeys(bareIPRuleInfo("203.0.113.10", "manual"))
	subnet, _ := SnapshotRuleKeys(bareIPRuleInfo("203.0.113.0/24", "manual"))
	if len(host) != 1 || len(subnet) != 1 || host[0] == subnet[0] {
		t.Fatal("host normalized into broader network")
	}
	// 对多个真实地址形态检查裸地址和单主机前缀始终精确等价。
	for i := 0; i < 256; i++ {
		addresses := []netip.Addr{netip.AddrFrom4([4]byte{192, 0, 2, byte(i)}), netip.AddrFrom16([16]byte{0x20, 1, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, byte(i), 7})}
		for _, a := range addresses {
			raw, prefix := a.String(), netip.PrefixFrom(a, a.BitLen()).String()
			host, hok := CanonicalCIDR(raw)
			cidr, cok := CanonicalCIDR(prefix)
			if !hok || !cok || host != cidr {
				t.Fatalf("equivalence lost: %q %q -> %q %q", raw, prefix, host, cidr)
			}
		}
	}
}

func TestI8R27BareIPAliasSafety(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudTCLighthouse, config.CloudTCCVM, config.CloudAliECS, config.CloudAliSWAS} {
		for _, mode := range []string{"keep_aliases", "opposite_action", "tag_spoof", "broader_network", "wrong_family_bare", "wrong_family_prefix"} {
			t.Run(string(ct)+"/"+mode, func(t *testing.T) {
				keep := bareIPRuleInfo("203.0.113.10", "[auto-dns] keep")
				old := bareIPRuleInfo("192.0.2.20", "[auto-dns] old")
				rows := []config.RuleInfo{keep, old}
				switch mode {
				case "keep_aliases":
					rows = append(rows, bareIPRuleInfo("203.0.113.10/32", "[auto-dns] alias"))
				case "opposite_action":
					drop := bareIPRuleInfo("192.0.2.20/32", "manual")
					drop.Action = "DROP"
					rows = append(rows, drop)
				case "tag_spoof":
					rows = append(rows, bareIPRuleInfo("192.0.2.99", "[auto-dns]foo"))
				case "broader_network":
					rows[0] = bareIPRuleInfo("203.0.113.0/24", "manual")
				case "wrong_family_bare":
					rows = append(rows, bareIPRuleInfo("2001:db8::99", "manual"))
				case "wrong_family_prefix":
					rows = append(rows, bareIPRuleInfo("2001:db8::99/128", "manual"))
				}
				p := planFor(t, ct, []config.DomainRule{rule(1, "a.test", "TCP", "443", "ACCEPT", "")}, map[int][]dns.ResolvedIP{1: {v4("203.0.113.10")}}, rows)
				switch mode {
				case "opposite_action":
					if !hasIssue(p.Conflicts, IssueOppositeAction) || len(p.CleanupDeletable) != 0 {
						t.Fatalf("opposite action not frozen: %+v", p)
					}
				case "wrong_family_bare", "wrong_family_prefix":
					if !hasIssue(p.Conflicts, IssueSnapshotRuleInvalid) || len(p.CleanupDeletable) != 0 {
						t.Fatalf("wrong family not frozen: %+v", p)
					}
				case "broader_network":
					if len(p.ToAdd) != 1 || len(p.SatisfiedByExternal) != 0 || len(p.CleanupDeletable) != 0 {
						t.Fatalf("containment inferred: %+v", p)
					}
				default:
					if len(p.ToAdd) != 0 || len(p.CleanupCandidates) != 1 || len(p.CleanupDeletable) != 1 || p.CleanupDeletable[0].CidrBlock != "192.0.2.20" {
						t.Fatalf("alias or spoof unsafe cleanup: %+v", p)
					}
				}
			})
		}
	}
	for _, tc := range []struct{ raw, want string }{{"0.0.0.0", "0.0.0.0/32"}, {"::", "::/128"}, {"0.0.0.0/0", "0.0.0.0/0"}, {"::/0", "::/0"}} {
		t.Run(tc.raw, func(t *testing.T) {
			got, ok := CanonicalCIDR(tc.raw)
			if !ok || got != tc.want {
				t.Fatalf("host widened to wildcard: %q %v", got, ok)
			}
		})
	}
}
