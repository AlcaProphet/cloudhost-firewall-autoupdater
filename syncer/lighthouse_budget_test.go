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
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	lighthouse "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/lighthouse/v20200324"
)

// 真实 Lighthouse SDK → 正式目标链；次数耗尽不可重试，S0/S1 不得授权删除。
func TestI810TargetPaginationSafety(t *testing.T) {
	for _, stage := range []string{"s0", "s1", "s2", "complete"} {
		t.Run(stage, func(t *testing.T) { i810Target(t, stage) })
	}
}
func i810Target(t *testing.T, stage string) {
	var describes, adds, deletes atomic.Int32
	old := map[string]any{"Protocol": "TCP", "Port": "9999", "CidrBlock": "192.0.2.5/32", "Action": "ACCEPT", "FirewallRuleDescription": "[auto-dns]"}
	desired := map[string]any{"Protocol": "TCP", "Port": "443", "CidrBlock": "1.1.1.1/32", "Action": "ACCEPT", "FirewallRuleDescription": "[auto-dns]"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{"RequestId": "mock"}
		switch r.Header.Get("X-Tc-Action") {
		case "DescribeFirewallRules":
			n := describes.Add(1)
			phase := "s0"
			if adds.Load() > 0 {
				phase = "s1"
			}
			if deletes.Load() > 0 {
				phase = "s2"
			}
			abnormal := stage == phase
			page := []any{old}
			if adds.Load() > 0 {
				page = append(page, desired)
			}
			if deletes.Load() > 0 {
				page = []any{desired}
			}
			if abnormal {
				page = make([]any, 100)
				for i := range page {
					page[i] = old
				}
				if phase != "s0" {
					page[99] = desired
				}
				if n > 105 {
					body = map[string]any{"Error": map[string]any{"Code": "FailedOperation", "Message": "fixture stop"}}
				} // 移除守卫后仍有界结束。
			}
			if _, bad := body["Error"]; !bad {
				body["FirewallRuleSet"] = page
				body["FirewallVersion"] = 7
			}
		case "CreateFirewallRules":
			adds.Add(1)
		case "DeleteFirewallRules":
			deletes.Add(1)
		default:
			t.Errorf("unexpected action %s", r.Header.Get("X-Tc-Action"))
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"Response": body}); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = strings.TrimPrefix(srv.URL, "http://")
	cpf.HttpProfile.Scheme = "http"
	cli, err := lighthouse.NewClient(common.NewCredential("fake", "fake"), "ap-guangzhou", cpf)
	if err != nil {
		t.Fatal(err)
	}
	pool := provider.NewClientPool(provider.Credentials{})
	if _, err = pool.GetOrCreate(pool.CacheKey(config.CloudTCLighthouse, "ap-guangzhou"), func() (any, error) { return cli, nil }); err != nil {
		t.Fatal(err)
	}
	pr, err := provider.NewProvider(config.TargetConfig{CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "lhins-mock"}, 1, pool)
	if err != nil {
		t.Fatal(err)
	}
	s := newTargetSyncer(t, []provider.Provider{pr}, []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}, map[string]string{"a.example.com": "1.1.1.1/32"})
	s.syncAll()
	sum := s.Status().LastRound
	wantD, wantA, wantDel := int32(100), int32(0), int32(0)
	if stage == "s1" {
		wantD = 101
		wantA = 1
	}
	if stage == "s2" {
		wantD = 102
		wantA = 1
		wantDel = 1
	}
	if stage == "complete" {
		wantD = 3
		wantA = 1
		wantDel = 1
	}
	if describes.Load() != wantD || adds.Load() != wantA || deletes.Load() != wantDel {
		t.Fatalf("Describe/Add/Delete=%d/%d/%d want=%d/%d/%d", describes.Load(), adds.Load(), deletes.Load(), wantD, wantA, wantDel)
	}
	if sum == nil || sum.Added != int(wantA) || sum.Deleted != int(wantDel) {
		t.Fatalf("sum=%+v", sum)
	}
	if stage == "complete" {
		if sum.Outcome != RoundSuccess || sum.CleanupDeleted != 1 {
			t.Fatalf("sum=%+v", sum)
		}
	} else if sum.Outcome != RoundFailed || sum.Failed != 1 {
		t.Fatalf("sum=%+v", sum)
	}
}
