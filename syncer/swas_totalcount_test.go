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
	swas "github.com/alibabacloud-go/swas-open-20200601/v3/client"
	"github.com/alibabacloud-go/tea/tea"
)

// 真实 SWAS SDK → syncAll → 正式目标链；验证各阶段失败的增删和观察边界。
func TestI811SWASTargetSafety(t *testing.T) {
	for _, stage := range []string{"s0", "s1", "s2"} {
		for _, kind := range []string{"decrease", "decrease_zero", "increase", "zero", "negative", "overshoot"} {
			t.Run(stage+"_"+kind, func(t *testing.T) { i811SWASTarget(t, stage, kind) })
		}
	}
	t.Run("complete", func(t *testing.T) { i811SWASTarget(t, "complete", "") })
}

func i811SWASTarget(t *testing.T, stage, kind string) {
	var describes, adds, deletes, deletedRows atomic.Int32
	rule := func(id, port, cidr string) map[string]any {
		return map[string]any{"RuleId": id, "RuleProtocol": "TCP", "Port": port, "SourceCidrIp": cidr, "Policy": "accept", "Remark": "[auto-dns]"}
	}
	old, desired, extra := rule("old", "9999/9999", "192.0.2.5/32"), rule("desired", "443/443", "1.1.1.1/32"), rule("extra", "8888/8888", "192.0.2.6/32")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{"RequestId": "mock"}
		switch action := r.Header.Get("X-Acs-Action"); action {
		case "ListFirewallRules":
			n := describes.Add(1)
			phase := "s0"
			if adds.Load() > 0 {
				phase = "s1"
			}
			if deletes.Load() > 0 {
				phase = "s2"
			}
			pageNum := r.URL.Query().Get("PageNumber")
			if r.URL.Query().Get("PageSize") != "100" {
				t.Errorf("size != 100")
			}
			rows := []any{old}
			total := 1
			if phase == "s1" {
				rows = []any{old, desired}
				total = 2
			}
			if phase == "s2" {
				rows = []any{desired}
				total = 1
			}
			if phase == stage {
				switch kind {
				case "zero":
					total = 0
				case "negative":
					total = -1
				case "overshoot":
					rows = append(rows, extra)
				case "decrease", "decrease_zero", "increase":
					total = 4
					if pageNum == "2" {
						rows = []any{extra}
						total = 1
						if kind == "decrease_zero" {
							total = 0
						}
						if kind == "increase" {
							total = 5
						}
					}
				}
			}
			if stage == "complete" && phase == "s1" {
				if pageNum == "1" {
					rows = []any{old}
				} else {
					rows = []any{desired}
				}
			}
			if n > 10 {
				w.WriteHeader(400)
				body = map[string]any{"Code": "InvalidParam", "Message": "fixture stop"}
			} else {
				body["TotalCount"] = total
				body["FirewallRules"] = rows
			}
		case "CreateFirewallRules":
			adds.Add(1)
		case "DeleteFirewallRules":
			deletes.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			ids := r.Form.Get("RuleIds")
			if ids != "old" {
				t.Errorf("delete RuleIds=%q want old", ids)
			}
			if ids != "" {
				deletedRows.Add(int32(len(strings.Split(ids, ","))))
			}
		default:
			t.Errorf("unexpected action %q", action)
			w.WriteHeader(400)
			body = map[string]any{"Code": "InvalidParam"}
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	cli, err := swas.NewClient(&openapi.Config{AccessKeyId: tea.String("fake"), AccessKeySecret: tea.String("fake"), Endpoint: tea.String(strings.TrimPrefix(srv.URL, "http://")), Protocol: tea.String("http")})
	if err != nil {
		t.Fatal(err)
	}
	pool := provider.NewClientPool(provider.Credentials{})
	if _, err = pool.GetOrCreate(pool.CacheKey(config.CloudAliSWAS, "cn-hangzhou"), func() (any, error) { return cli, nil }); err != nil {
		t.Fatal(err)
	}
	p, err := provider.NewProvider(config.TargetConfig{CloudType: config.CloudAliSWAS, Region: "cn-hangzhou", ResourceID: "swas-mock"}, 1, pool)
	if err != nil {
		t.Fatal(err)
	}
	s := newTargetSyncer(t, []provider.Provider{p}, []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}, map[string]string{"a.example.com": "1.1.1.1/32"})
	s.syncAll()
	sum := s.Status().LastRound
	wantD, wantA, wantDel := int32(1), int32(0), int32(0)
	if strings.HasPrefix(kind, "decrease") || kind == "increase" {
		wantD = 2
	}
	if stage == "s1" {
		wantD++
		wantA = 1
	}
	if stage == "s2" {
		wantD += 2
		wantA = 1
		wantDel = 1
	}
	if stage == "complete" {
		wantD = 4
		wantA = 1
		wantDel = 1
	}
	if describes.Load() != wantD || adds.Load() != wantA || deletes.Load() != wantDel {
		t.Fatalf("Describe/Add/Delete=%d/%d/%d want %d/%d/%d", describes.Load(), adds.Load(), deletes.Load(), wantD, wantA, wantDel)
	}
	if sum == nil || sum.Added != int(wantA) || sum.Deleted != int(wantDel) || sum.CleanupDeleted != int(wantDel) {
		t.Fatalf("counts=%+v", sum)
	}
	if deletedRows.Load() != wantDel {
		t.Fatalf("deleted IDs=%d want=%d", deletedRows.Load(), wantDel)
	}
	obs := sum.CleanupObservationSummary
	if stage == "s2" {
		if obs.ObservedTargets != 0 || obs.EstimatedTargets != 1 || obs.Complete {
			t.Fatalf("S2 failure must retain estimate: %+v", obs)
		}
	}
	if stage == "s0" || stage == "s1" {
		if obs.UnknownTargets != 1 || obs.Complete {
			t.Fatalf("untrusted S0/S1 observation: %+v", obs)
		}
	}
	if stage == "complete" {
		if obs.ObservedTargets != 1 || !obs.Complete {
			t.Fatalf("complete S2 observation: %+v", obs)
		}
	}
	if stage == "complete" {
		if sum.Outcome != RoundSuccess || sum.Failed != 0 {
			t.Fatalf("sum=%+v", sum)
		}
	} else if sum.Outcome != RoundFailed || sum.Failed != 1 {
		t.Fatalf("sum=%+v", sum)
	}
}
