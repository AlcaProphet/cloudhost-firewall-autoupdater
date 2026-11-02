package syncer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	"github.com/alibabacloud-go/tea/tea"
)

// TestI8R26ECSAllPortsRound 走真实 ECS SDK、正式 syncAll、S1/S2 和下一轮收敛。
// 本地服务按官方参数合同拒绝非法端口，模拟返回可归一化的真实线格式，不访问云端。
func TestI8R26ECSAllPortsRound(t *testing.T) {
	for _, protocol := range []string{"TCP", "UDP", "TCP+UDP"} {
		for _, mode := range []string{"new", "owned", "external", "replace", "add_error", "s1_error", "s1_missing", "external_all_protocol"} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				protocols := []string{protocol}
				if protocol == "TCP+UDP" {
					protocols = []string{"TCP", "UDP"}
				}
				var mu sync.Mutex
				actions := []string{}
				describes, creates, deletes := 0, 0, 0
				desiredPresent := mode == "owned" || mode == "external"
				stalePresent := mode == "replace" || mode == "add_error" || mode == "s1_error" || mode == "s1_missing"
				cloudRule := func(id, proto, port, cidr, desc string) map[string]any {
					return map[string]any{"SecurityGroupRuleId": id, "IpProtocol": proto, "PortRange": port, "SourceCidrIp": cidr, "Policy": "accept", "Description": desc}
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					if err := r.ParseForm(); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					body := map[string]any{"RequestId": "local-i8-r26"}
					action := r.Header.Get("X-Acs-Action")
					actions = append(actions, action)
					if r.Form.Get("RegionId") != "cn-hangzhou" || r.Form.Get("SecurityGroupId") != "sg-mock" {
						t.Error("目标参数不符")
					}
					switch action {
					case "DescribeSecurityGroupAttribute":
						describes++
						if r.Form.Get("Direction") != "ingress" {
							t.Error("应只读取入站规则")
						}
						if mode == "s1_error" && describes == 2 {
							w.WriteHeader(400)
							body = map[string]any{"Code": "InvalidParameter", "Message": "S1 unavailable"}
							break
						}
						rows := []any{cloudRule("foreign", "TCP", "22/22", "198.51.100.1/32", "manual")}
						if mode == "external_all_protocol" {
							rows = append(rows, cloudRule("foreign-all", "ALL", "-1/-1", "1.1.1.1/32", "manual"))
						}
						for _, proto := range protocols {
							if stalePresent {
								id := "old-" + proto
								// S0 与 S1 故意返回不同定位；删除必须使用 S1。
								if describes == 1 {
									id = "s0-" + id
								}
								rows = append(rows, cloudRule(id, proto, "1/65535", "192.0.2.9/32", "[auto-dns] old"))
							}
							if desiredPresent && mode != "s1_missing" {
								desc := "[auto-dns] keep"
								if mode == "external" {
									desc = "manual"
								}
								rows = append(rows, cloudRule("desired-"+proto, proto, "1/65535", "1.1.1.1/32", desc))
							}
						}
						body["Permissions"] = map[string]any{"Permission": rows}
					case "AuthorizeSecurityGroup":
						creates++
						validRequest := true
						gotProtocols := map[string]int{}
						for i := 1; i <= len(protocols); i++ {
							prefix := fmt.Sprintf("Permissions.%d.", i)
							proto := r.Form.Get(prefix + "IpProtocol")
							gotProtocols[proto]++
							if r.Form.Get(prefix+"PortRange") != "1/65535" || r.Form.Get(prefix+"SourceCidrIp") != "1.1.1.1/32" || r.Form.Get(prefix+"Policy") != "accept" {
								t.Errorf("非法新增参数: %v", r.Form)
								validRequest = false
							}
						}
						wantProtocols := map[string]int{}
						for _, proto := range protocols {
							wantProtocols[proto]++
						}
						if !reflect.DeepEqual(gotProtocols, wantProtocols) {
							t.Errorf("新增协议=%v want %v", gotProtocols, wantProtocols)
							validRequest = false
						}
						if r.Form.Has(fmt.Sprintf("Permissions.%d.IpProtocol", len(protocols)+1)) {
							t.Error("出现额外新增项")
							validRequest = false
						}
						if mode == "add_error" || !validRequest {
							w.WriteHeader(400)
							body = map[string]any{"Code": "InvalidIpProtocol.Malformed", "Message": "PortRange is not valid"}
						} else {
							desiredPresent = true
						}
					case "RevokeSecurityGroup":
						deletes++
						ids, err := ecsCleanupRPCIDs(r.Form)
						if err != nil {
							t.Error(err)
							w.WriteHeader(400)
							return
						}
						want := []string{}
						for _, proto := range protocols {
							want = append(want, "old-"+proto)
						}
						if !reflect.DeepEqual(ids, want) {
							t.Errorf("删除定位=%v want S1定位 %v", ids, want)
						}
						if !desiredPresent || describes < 2 {
							t.Error("覆盖确认前进入删除")
						}
						stalePresent = false
					default:
						t.Errorf("非预期操作 %s", action)
						w.WriteHeader(400)
					}
					if err := json.NewEncoder(w).Encode(body); err != nil {
						t.Error(err)
					}
				}))
				defer srv.Close()
				client, err := ecs.NewClient(&openapi.Config{AccessKeyId: tea.String("fake"), AccessKeySecret: tea.String("fake"), Endpoint: tea.String(strings.TrimPrefix(srv.URL, "http://")), Protocol: tea.String("http")})
				if err != nil {
					t.Fatal(err)
				}
				pool := provider.NewClientPool(provider.Credentials{})
				if _, err := pool.GetOrCreate(pool.CacheKey(config.CloudAliECS, "cn-hangzhou"), func() (any, error) { return client, nil }); err != nil {
					t.Fatal(err)
				}
				p, err := provider.NewProvider(config.TargetConfig{CloudType: config.CloudAliECS, Region: "cn-hangzhou", ResourceID: "sg-mock"}, 1, pool)
				if err != nil {
					t.Fatal(err)
				}
				s := newTargetSyncer(t, []provider.Provider{p}, []config.DomainRule{staticRule(1, "a.test", protocol, "ALL")}, map[string]string{"a.test": "1.1.1.1/32"})
				s.syncAll()
				failed := mode == "add_error" || mode == "s1_error" || mode == "s1_missing"
				wantAdded := len(protocols)
				if mode == "owned" || mode == "external" || mode == "add_error" {
					wantAdded = 0
				}
				wantDeleted := 0
				if mode == "replace" {
					wantDeleted = len(protocols)
				}
				sum := s.Status().LastRound
				if sum == nil {
					t.Fatal("缺少整轮结论")
				}
				wantOutcome := RoundSuccess
				if failed {
					wantOutcome = RoundFailed
				}
				if sum.Outcome != wantOutcome || sum.Total != 1 || sum.Added != wantAdded || sum.Deleted != wantDeleted || sum.CleanupDeleted != wantDeleted {
					t.Fatalf("整轮结果=%+v want outcome=%s added=%d deleted=%d", sum, wantOutcome, wantAdded, wantDeleted)
				}
				mu.Lock()
				firstActions := append([]string(nil), actions...)
				wantActions := []string{"DescribeSecurityGroupAttribute"}
				if mode != "owned" && mode != "external" {
					wantActions = append(wantActions, "AuthorizeSecurityGroup")
				}
				if mode != "add_error" {
					wantActions = append(wantActions, "DescribeSecurityGroupAttribute")
				}
				if mode == "replace" {
					wantActions = append(wantActions, "RevokeSecurityGroup", "DescribeSecurityGroupAttribute")
				}
				if !reflect.DeepEqual(firstActions, wantActions) {
					t.Errorf("操作顺序=%v want %v", firstActions, wantActions)
				}
				if failed && (deletes != 0 || !stalePresent) {
					t.Error("新增或S1失败必须保留旧权限")
				}
				initialCreates, initialDeletes := creates, deletes
				mu.Unlock()
				if failed {
					return
				}
				// 下一轮必须由实际回读证明已覆盖，不能重复新增或清理任何现存规则。
				s.syncAll()
				sum = s.Status().LastRound
				if sum == nil || sum.Outcome != RoundSuccess || sum.Added != 0 || sum.Deleted != 0 || sum.CleanupCandidates != 0 {
					t.Fatalf("下一轮未收敛: %+v", sum)
				}
				mu.Lock()
				defer mu.Unlock()
				if creates != initialCreates || deletes != initialDeletes || !reflect.DeepEqual(actions[len(firstActions):], []string{"DescribeSecurityGroupAttribute", "DescribeSecurityGroupAttribute"}) {
					t.Fatalf("下一轮请求=%v", actions[len(firstActions):])
				}
			})
		}
	}
}
