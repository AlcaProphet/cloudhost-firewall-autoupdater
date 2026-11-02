package provider

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
)

// I8-R26：贯通正式 planner 与真实阿里云 SDK 的本地请求，不访问真实云端。
func TestI8R26AllPortWire(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudAliECS, config.CloudAliSWAS} {
		for _, proto := range []string{"TCP", "UDP", "TCP+UDP", "ICMP"} {
			t.Run(string(ct)+"/"+proto, func(t *testing.T) {
				m, host := newMockCloudAPI(t)
				var p Provider
				action, objects, portKey, protoKey := "AuthorizeSecurityGroup", "Permissions", "PortRange", "IpProtocol"
				if ct == config.CloudAliECS {
					p = mockECS(t, host)
				} else {
					p = mockSWAS(t, host)
					action, objects, portKey, protoKey = "CreateFirewallRules", "FirewallRules", "Port", "RuleProtocol"
				}
				rr := []config.DomainRule{rule(1, "a.test", proto, "ALL", "ACCEPT", "")}
				plan := planFor(t, ct, rr, map[int][]dns.ResolvedIP{1: {v4("192.0.2.1")}}, nil)
				n := 1
				if ct == config.CloudAliECS && proto == "TCP+UDP" {
					n = 2
				}
				if len(plan.ToAdd) != n {
					t.Fatalf("ToAdd=%d want %d", len(plan.ToAdd), n)
				}
				res, err := p.CreateRules(mockSnapshot(), plan.ToAdd)
				if err != nil {
					t.Fatal(err)
				}
				if res.Written != n || res.Skipped != 0 {
					t.Fatalf("CreateResult=%+v want Written=%d Skipped=0", res, n)
				}
				reqs := requestsWithAction(t, m.recorded(), action)
				if len(reqs) != 1 {
					t.Fatalf("requests=%d", len(reqs))
				}
				fields := map[int]map[string]string{}
				if ct == config.CloudAliECS {
					fields = rpcIndexedObjects(t, reqs[0], objects)
				} else {
					rows := decodeJSONArrayText(t, reqs[0].query("FirewallRules"))
					for i, row := range rows {
						port, _ := strField(row, "Port")
						proto, _ := strField(row, "RuleProtocol")
						fields[i+1] = map[string]string{"Port": port, "RuleProtocol": proto}
					}
				}
				if len(fields) != n {
					t.Fatalf("fields=%d", len(fields))
				}
				want := "1/65535"
				if proto == "ICMP" {
					want = "-1/-1"
				}
				gotProtocols := map[string]int{}
				for i := 1; i <= n; i++ {
					gotProtocols[fields[i][protoKey]]++
					if fields[i][portKey] != want {
						t.Errorf("wire %s=%s %s=%q want %q", protoKey, fields[i][protoKey], portKey, fields[i][portKey], want)
					}
				}
				wantProtocols := map[string]int{proto: 1}
				if ct == config.CloudAliECS && proto == "TCP+UDP" {
					wantProtocols = map[string]int{"TCP": 1, "UDP": 1}
				}
				if !reflect.DeepEqual(gotProtocols, wantProtocols) {
					t.Fatalf("protocols=%v want %v", gotProtocols, wantProtocols)
				}
			})
		}
	}
}

func TestI8R26AllPortConvergence(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudAliECS, config.CloudAliSWAS} {
		for _, proto := range []string{"TCP", "UDP", "TCP+UDP"} {
			if ct == config.CloudAliECS && proto == "TCP+UDP" {
				continue
			}
			for _, port := range []string{"1/65535", "1-65535"} {
				for _, desc := range []string{"[auto-dns] keep", "manual"} {
					t.Run(fmt.Sprintf("%s/%s/%s/%s", ct, proto, port, desc), func(t *testing.T) {
						rr := []config.DomainRule{rule(1, "a.test", proto, "ALL", "ACCEPT", "")}
						snap := []config.RuleInfo{{Protocol: proto, Port: port, CidrBlock: "192.0.2.1/32", Action: "ACCEPT", Description: desc, RuleID: "keep"}}
						plan := planFor(t, ct, rr, map[int][]dns.ResolvedIP{1: {v4("192.0.2.1")}}, snap)
						if desc == "manual" {
							if len(plan.SatisfiedByExternal) != 1 || len(plan.SatisfiedByOwned) != 0 {
								t.Fatalf("external match=%+v", plan)
							}
						} else if len(plan.SatisfiedByOwned) != 1 || len(plan.SatisfiedByExternal) != 0 {
							t.Fatalf("owned match=%+v", plan)
						}
						if len(plan.ToAdd) != 0 || len(plan.CleanupCandidates) != 0 || !plan.CoverageReady {
							t.Fatalf("ToAdd=%d Cleanup=%d Coverage=%v desired=%+v", len(plan.ToAdd), len(plan.CleanupCandidates), plan.CoverageReady, plan.Desired)
						}
					})
				}
			}
		}
	}
}

func TestI8R26AllPortDedup(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudAliECS, config.CloudAliSWAS} {
		t.Run(string(ct), func(t *testing.T) {
			rr := []config.DomainRule{rule(1, "a.test", "TCP", "ALL", "ACCEPT", ""), rule(2, "b.test", "TCP", "1-65535", "ACCEPT", "")}
			plan := planFor(t, ct, rr, map[int][]dns.ResolvedIP{1: {v4("192.0.2.1")}, 2: {v4("192.0.2.1")}}, nil)
			if len(plan.Desired) != 1 || len(plan.ToAdd) != 1 {
				t.Fatalf("Desired=%d ToAdd=%d", len(plan.Desired), len(plan.ToAdd))
			}
		})
	}
}

func TestI8R26AllPortControls(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudAliECS, config.CloudAliSWAS, config.CloudTCCVM, config.CloudTCLighthouse} {
		for _, input := range []string{"443", "8000-8010"} {
			t.Run(string(ct)+"/"+input, func(t *testing.T) {
				plan := planFor(t, ct, []config.DomainRule{rule(1, "a.test", "TCP", input, "ACCEPT", "")}, map[int][]dns.ResolvedIP{1: {v4("192.0.2.1")}}, nil)
				want := input
				if ct == config.CloudAliECS || ct == config.CloudAliSWAS {
					if input == "443" {
						want = "443/443"
					} else {
						want = "8000/8010"
					}
				}
				if len(plan.ToAdd) != 1 || plan.ToAdd[0].Port != want {
					t.Fatalf("ToAdd=%+v want %s", plan.ToAdd, want)
				}
			})
		}
	}
	// 腾讯全端口仍保留 ALL，不全局折叠为阿里云范围。
	for _, ct := range []config.CloudType{config.CloudTCCVM, config.CloudTCLighthouse} {
		plan := planFor(t, ct, []config.DomainRule{rule(1, "a.test", "TCP", "ALL", "ACCEPT", "")}, map[int][]dns.ResolvedIP{1: {v4("192.0.2.1")}}, nil)
		if len(plan.ToAdd) != 1 || plan.ToAdd[0].Port != "ALL" {
			t.Fatalf("Tencent ToAdd=%+v", plan.ToAdd)
		}
	}
	// ECS IPv6 TCP 可发，IPv6 ICMP 继续按现有能力矩阵跳过。
	for _, proto := range []string{"TCP", "ICMP"} {
		plan := planFor(t, config.CloudAliECS, []config.DomainRule{rule(1, "a.test", proto, "ALL", "ACCEPT", "")}, map[int][]dns.ResolvedIP{1: {v6("2001:db8::1")}}, nil)
		if proto == "TCP" {
			if len(plan.ToAdd) != 1 || plan.ToAdd[0].Port != "1/65535" {
				t.Fatalf("IPv6 TCP %+v", plan.ToAdd)
			}
		} else if len(plan.ToAdd) != 0 || len(plan.Unsupported) != 1 {
			t.Fatalf("IPv6 ICMP %+v", plan)
		}
	}
}
