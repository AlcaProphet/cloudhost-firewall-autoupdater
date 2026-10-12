package syncer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	"github.com/alibabacloud-go/tea/tea"
)

// 专项入口通过 Go overlay 仅缩短测试构建的预算；真实 SDK 与正式目标链保持。
func TestReadBudgetTargetSafety(t *testing.T) {
	cases := []struct {
		name                             string
		failReads                        map[int32]bool
		wantReads, wantAdds, wantDeletes int32
		failed                           bool
	}{
		{"s0_exhaust", map[int32]bool{1: true, 2: true, 3: true}, 3, 0, 0, true},
		{"s1_exhaust", map[int32]bool{2: true, 4: true, 6: true}, 6, 1, 0, true},
		{"s2_exhaust", map[int32]bool{3: true, 6: true, 9: true}, 9, 1, 3, true},
		{"s0_recovers", map[int32]bool{1: true}, 4, 1, 1, false},
		{"s1_recovers", map[int32]bool{2: true}, 5, 1, 1, false},
		{"s2_recovers", map[int32]bool{3: true}, 5, 1, 1, false},
		{"normal", nil, 3, 1, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reads, adds, deletes atomic.Int32
			release := make(chan struct{})
			old := map[string]any{"SecurityGroupRuleId": "old", "IpProtocol": "tcp", "PortRange": "9999/9999", "SourceCidrIp": "192.0.2.5/32", "Policy": "accept", "Description": "[auto-dns]"}
			desired := map[string]any{"SecurityGroupRuleId": "new", "IpProtocol": "tcp", "PortRange": "443/443", "SourceCidrIp": "1.1.1.1/32", "Policy": "accept", "Description": "[auto-dns]"}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if e := r.ParseForm(); e != nil {
					t.Error(e)
					return
				}
				body := map[string]any{"RequestId": "local"}
				switch r.Header.Get("X-Acs-Action") {
				case "DescribeSecurityGroupAttribute":
					n := reads.Add(1)
					if tc.failReads[n] {
						select {
						case <-r.Context().Done():
						case <-release:
						case <-time.After(500 * time.Millisecond):
						}
						return
					}
					page := []any{old}
					if adds.Load() > 0 {
						page = append(page, desired)
					}
					if deletes.Load() > 0 {
						page = []any{desired}
					}
					if tc.name == "s2_exhaust" {
						// 另一个写入方在每次成功 Delete 后创建新的旧功能 TAG 规则。
						// 每个 attempt 的 S0/S1 仍完整，只有删除后的 S2 始终耗尽。
						stale := make(map[string]any, len(old))
						for key, value := range old {
							stale[key] = value
						}
						stale["SecurityGroupRuleId"] = fmt.Sprintf("old-%d", deletes.Load())
						page = []any{stale}
						if adds.Load() > 0 {
							page = append(page, desired)
						}
					}
					body["Permissions"] = map[string]any{"Permission": page}
				case "AuthorizeSecurityGroup":
					adds.Add(1)
				case "RevokeSecurityGroup":
					deletes.Add(1)
				default:
					t.Errorf("unexpected action %s", r.Header.Get("X-Acs-Action"))
				}
				if e := json.NewEncoder(w).Encode(body); e != nil {
					t.Error(e)
				}
			}))
			defer func() { close(release); srv.CloseClientConnections(); srv.Close() }()
			client, e := ecs.NewClient(&openapi.Config{AccessKeyId: tea.String("fake"), AccessKeySecret: tea.String("fake"), Endpoint: tea.String(strings.TrimPrefix(srv.URL, "http://")), Protocol: tea.String("http"), ConnectTimeout: tea.Int(1000), ReadTimeout: tea.Int(1000)})
			if e != nil {
				t.Fatal(e)
			}
			pool := provider.NewClientPool(provider.Credentials{})
			_, e = pool.GetOrCreate(pool.CacheKey(config.CloudAliECS, "cn-hangzhou"), func() (any, error) { return client, nil })
			if e != nil {
				t.Fatal(e)
			}
			p, e := provider.NewProvider(config.TargetConfig{CloudType: config.CloudAliECS, Region: "cn-hangzhou", ResourceID: "sg-local"}, 1, pool)
			if e != nil {
				t.Fatal(e)
			}
			s := newTargetSyncer(t, []provider.Provider{p}, []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}, map[string]string{"a.example.com": "1.1.1.1/32"})
			s.syncAll()
			sum := s.Status().LastRound
			if reads.Load() != tc.wantReads || adds.Load() != tc.wantAdds || deletes.Load() != tc.wantDeletes {
				t.Fatalf("target budget writes/retries: actual %d/%d/%d want %d/%d/%d", reads.Load(), adds.Load(), deletes.Load(), tc.wantReads, tc.wantAdds, tc.wantDeletes)
			}
			if sum == nil || sum.Added != int(tc.wantAdds) || sum.Deleted != int(tc.wantDeletes) {
				t.Fatalf("confirmed writes: %+v", sum)
			}
			if tc.failed {
				if sum.Outcome != RoundFailed || sum.Failed != 1 {
					t.Fatalf("failure outcome: %+v", sum)
				}
			} else {
				if sum.Outcome != RoundSuccess || sum.CleanupDeferred != 0 {
					t.Fatalf("recovery outcome: %+v", sum)
				}
			}
		})
	}
}
