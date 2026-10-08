package syncer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	"github.com/alibabacloud-go/tea/tea"
)

// TestTargetRound_ECSPaginationSafety 真实 SDK → 正式目标链的本地回归，非真实云验收。
// S0 异常零新增/删除；S1 异常保留已确认新增但零删除；正常对照必须能清理旧规则。
func TestTargetRound_ECSPaginationSafety(t *testing.T) {
	cases := []struct {
		name, stage string
		tokens      []string
		complete    bool
	}{
		{"s0_repeat", "S0", []string{"T1", "T1"}, false},
		{"s0_cycle", "S0", []string{"T1", "T2", "T1"}, false},
		{"s1_repeat", "S1", []string{"T1", "T1"}, false},
		{"s1_cycle", "S1", []string{"T1", "T2", "T1"}, false},
		{"complete_allows_cleanup", "S1", []string{"T1", ""}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var describes, adds, deletes atomic.Int32
			old := map[string]any{
				"SecurityGroupRuleId": "sgr-old", "IpProtocol": "TCP", "PortRange": "9999/9999",
				"SourceCidrIp": "192.0.2.5/32", "Policy": "accept", "Description": "[auto-dns]",
			}
			desired := map[string]any{
				"SecurityGroupRuleId": "sgr-new", "IpProtocol": "TCP", "PortRange": "443/443",
				"SourceCidrIp": "1.1.1.1/32", "Policy": "accept", "Description": "[auto-dns]",
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				body := map[string]any{"RequestId": "mock"}
				switch action := r.Header.Get("X-Acs-Action"); action {
				case "DescribeSecurityGroupAttribute":
					n := int(describes.Add(1)) - 1
					offset := 0
					if tc.stage == "S1" {
						offset = 1 // 首次 S0 完整返回，允许真实 Add。
					}
					page := []any{old}
					if tc.complete && deletes.Load() > 0 {
						page = []any{desired} // 正常清理后 S2 只剩所需功能。
					} else if n >= offset {
						i := n - offset
						if i >= len(tc.tokens) {
							// 异常候选缺少守卫时也有界结束，不靠测试超时判定。
							w.WriteHeader(http.StatusBadRequest)
							body = map[string]any{"Code": "InvalidParameter", "Message": "fixture stop"}
						} else {
							want := ""
							if i > 0 {
								want = tc.tokens[i-1]
							}
							if got := r.Form.Get("NextToken"); got != want {
								t.Errorf("请求 token=%q，预期 %q", got, want)
							}
							body["NextToken"] = tc.tokens[i]
							if tc.complete {
								if i > 0 {
									page = []any{desired}
								}
							} else if tc.stage == "S1" {
								// 半截 S1 也含完整期望与旧候选，证明不能误当成功后删除。
								page = []any{old, desired}
							}
						}
					}
					if _, failed := body["Code"]; !failed {
						body["Permissions"] = map[string]any{"Permission": page}
					}
				case "AuthorizeSecurityGroup":
					adds.Add(1)
				case "RevokeSecurityGroup":
					deletes.Add(1)
				default:
					t.Errorf("非预期 SDK 操作 %q", action)
					w.WriteHeader(http.StatusBadRequest)
					body = map[string]any{"Code": "InvalidParameter", "Message": "unexpected action"}
				}
				if err := json.NewEncoder(w).Encode(body); err != nil {
					t.Error(err)
				}
			}))
			defer srv.Close()

			cli, err := ecs.NewClient(&openapi.Config{
				AccessKeyId: tea.String("fake"), AccessKeySecret: tea.String("fake"),
				Endpoint: tea.String(strings.TrimPrefix(srv.URL, "http://")), Protocol: tea.String("http"),
			})
			if err != nil {
				t.Fatal(err)
			}
			pool := provider.NewClientPool(provider.Credentials{})
			if _, err := pool.GetOrCreate(pool.CacheKey(config.CloudAliECS, "cn-hangzhou"), func() (any, error) { return cli, nil }); err != nil {
				t.Fatal(err)
			}
			p, err := provider.NewProvider(config.TargetConfig{CloudType: config.CloudAliECS, Region: "cn-hangzhou", ResourceID: "sg-mock"}, 1, pool)
			if err != nil {
				t.Fatal(err)
			}
			s := newTargetSyncer(t, []provider.Provider{p}, []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}, map[string]string{"a.example.com": "1.1.1.1/32"})
			s.syncAll()
			sum := s.Status().LastRound
			wantCalls, wantAdds, wantDeletes := len(tc.tokens), int32(0), int32(0)
			if tc.stage == "S1" {
				wantCalls++
				wantAdds = 1
			}
			if tc.complete {
				wantCalls++ // Delete 后强制 S2。
				wantDeletes = 1
			}
			if describes.Load() != int32(wantCalls) || adds.Load() != wantAdds || deletes.Load() != wantDeletes {
				t.Fatalf("实际 Describe/Add/Delete=%d/%d/%d，预期 %d/%d/%d", describes.Load(), adds.Load(), deletes.Load(), wantCalls, wantAdds, wantDeletes)
			}
			if sum == nil || sum.Total != 1 || sum.Added != int(wantAdds) || sum.Deleted != int(wantDeletes) || sum.CleanupDeleted != int(wantDeletes) {
				t.Fatalf("确认增删数与轮次汇总不一致: %+v", sum)
			}
			if tc.complete {
				if sum.Outcome != RoundSuccess || sum.Changed != 1 || sum.Failed != 0 {
					t.Fatalf("正常多页查询应成功并清理旧规则: %+v", sum)
				}
			} else if sum.Outcome != RoundFailed || sum.Failed != 1 || sum.CleanupCandidates != 0 || sum.CleanupDeferred != 0 {
				t.Fatalf("不可信 S0/S1 必须失败且不能建立清理计划: %+v", sum)
			}
		})
	}
}
