package syncer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	swas "github.com/alibabacloud-go/swas-open-20200601/v3/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	lighthouse "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/lighthouse/v20200324"
	vpc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
)

// I8-R27：真实 SDK→NewProvider→syncAll→S1 删除定位→S2→下一轮收敛。
// 所有请求只访问本地 httptest 服务，不构成真实云删除验收。
func TestI8R27BareIPRealSDKRound(t *testing.T) {
	modes := []string{"owned", "external", "replace", "invalid_manual", "opposite_action", "owned_twins", "external_twin", "add_error", "s1_error", "s1_missing", "s2_error", "delete_error", "dns_failure"}
	for _, ct := range []config.CloudType{config.CloudTCLighthouse, config.CloudTCCVM, config.CloudAliECS, config.CloudAliSWAS} {
		for _, mode := range modes {
			t.Run(string(ct)+"/v4/"+mode, func(t *testing.T) { runBareIPSDKRound(t, ct, mode, false) })
		}
		if ct == config.CloudTCLighthouse || ct == config.CloudTCCVM {
			t.Run(string(ct)+"/v4/version_retry", func(t *testing.T) { runBareIPSDKRound(t, ct, "version_retry", false) })
		}
		if ct != config.CloudTCLighthouse {
			t.Run(string(ct)+"/v4/missing_locator", func(t *testing.T) { runBareIPSDKRound(t, ct, "missing_locator", false) })
		}
		if ct != config.CloudAliSWAS {
			for _, mode := range []string{"owned", "external", "replace", "s1_missing"} {
				t.Run(string(ct)+"/v6/"+mode, func(t *testing.T) { runBareIPSDKRound(t, ct, mode, true) })
			}
		}
	}
}
func runBareIPSDKRound(t *testing.T, ct config.CloudType, mode string, ipv6 bool) {
	t.Helper()
	var mu sync.Mutex
	reads, creates, deletes := 0, 0, 0
	stalePresent := true
	desiredPresent := mode != "replace" && mode != "add_error" && mode != "s1_error" && mode != "s1_missing" && mode != "s2_error"
	actions := []string{}
	desiredIP, oldIP, manualIP, mask := "203.0.113.10", "192.0.2.20", "198.51.100.9", "/32"
	if ipv6 {
		desiredIP, oldIP, manualIP, mask = "2001:db8::10", "2001:0DB8::20", "2001:db8:ff::9", "/128"
	}
	readAction, createAction, deleteAction := "DescribeFirewallRules", "CreateFirewallRules", "DeleteFirewallRules"
	switch ct {
	case config.CloudTCCVM:
		readAction, createAction, deleteAction = "DescribeSecurityGroupPolicies", "CreateSecurityGroupPolicies", "DeleteSecurityGroupPolicies"
	case config.CloudAliECS:
		readAction, createAction, deleteAction = "DescribeSecurityGroupAttribute", "AuthorizeSecurityGroup", "RevokeSecurityGroup"
	case config.CloudAliSWAS:
		readAction = "ListFirewallRules"
	}
	tc := ct == config.CloudTCLighthouse || ct == config.CloudTCCVM
	row := func(ip, desc, id, action string, index int) map[string]any {
		switch ct {
		case config.CloudTCLighthouse:
			r := map[string]any{"Protocol": "TCP", "Port": "443", "Action": action, "FirewallRuleDescription": desc}
			if ipv6 {
				r["Ipv6CidrBlock"] = ip
			} else {
				r["CidrBlock"] = ip
			}
			return r
		case config.CloudTCCVM:
			r := map[string]any{"Protocol": "TCP", "Port": "443", "Action": action, "PolicyDescription": desc, "PolicyIndex": index}
			if ipv6 {
				r["Ipv6CidrBlock"] = ip
			} else {
				r["CidrBlock"] = ip
			}
			return r
		case config.CloudAliECS:
			r := map[string]any{"IpProtocol": "TCP", "PortRange": "443/443", "Policy": strings.ToLower(action), "Description": desc, "SecurityGroupRuleId": id}
			if ipv6 {
				r["Ipv6SourceCidrIp"] = ip
			} else {
				r["SourceCidrIp"] = ip
			}
			return r
		default:
			return map[string]any{"RuleProtocol": "TCP", "Port": "443/443", "Policy": strings.ToLower(action), "Remark": desc, "RuleId": id, "SourceCidrIp": ip}
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		action := r.Header.Get("X-TC-Action")
		if !tc {
			action = r.Header.Get("X-Acs-Action")
		}
		actions = append(actions, action)
		payload := map[string]any{}
		if tc {
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
		} else {
			if err := r.ParseForm(); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
		}
		body := map[string]any{"RequestId": "local-bare-ip"}
		fail := func() {
			if tc {
				body = map[string]any{"Error": map[string]any{"Code": "InvalidParameter", "Message": "local injected error"}}
			} else {
				w.WriteHeader(400)
				body = map[string]any{"Code": "InvalidParameter", "Message": "local injected error", "RequestId": "mock"}
			}
		}
		switch action {
		case readAction:
			reads++
			if (mode == "s1_error" && creates > 0) || (mode == "s2_error" && deletes > 0) {
				fail()
				break
			}
			oldRaw := oldIP
			oldID := "old-s1"
			index := 3
			if reads == 1 {
				oldRaw = oldIP + mask
				oldID = "old-s0"
				index = 8
			}
			rows := []any{row(manualIP, "manual", "foreign", "ACCEPT", 0)}
			if mode == "invalid_manual" {
				rows = append(rows, row("not-an-ip", "manual-invalid", "invalid", "ACCEPT", 9))
			}
			if stalePresent {
				oldRow := row(oldRaw, "[auto-dns] old", oldID, "ACCEPT", index)
				if mode == "missing_locator" && reads > 1 {
					delete(oldRow, "PolicyIndex")
					delete(oldRow, "SecurityGroupRuleId")
					delete(oldRow, "RuleId")
				}
				rows = append(rows, oldRow)
				if mode == "owned_twins" {
					rows = append(rows, row(oldIP+mask, "[auto-dns] twin", "old2-s1", "ACCEPT", 4))
				}
				if mode == "external_twin" {
					rows = append(rows, row(oldIP+mask, "manual-twin", "foreign-twin", "ACCEPT", 5))
				}
				if mode == "opposite_action" {
					rows = append(rows, row(oldIP+mask, "manual-drop", "foreign-drop", "DROP", 5))
				}
			}
			if desiredPresent {
				desc := "[auto-dns] keep"
				if mode == "external" {
					desc = "manual-current"
				}
				rows = append(rows, row(desiredIP, desc, "current", "ACCEPT", 1))
			}
			switch ct {
			case config.CloudTCLighthouse:
				body["FirewallRuleSet"], body["FirewallVersion"], body["TotalCount"] = rows, 100+reads, len(rows)
			case config.CloudTCCVM:
				body["SecurityGroupPolicySet"] = map[string]any{"Version": strconv.Itoa(100 + reads), "Ingress": rows}
			case config.CloudAliECS:
				body["Permissions"] = map[string]any{"Permission": rows}
			case config.CloudAliSWAS:
				body["FirewallRules"], body["TotalCount"] = rows, len(rows)
			}
		case createAction:
			creates++
			v4Key, v6Key := "CidrBlock", "Ipv6CidrBlock"
			rules := []any{}
			switch ct {
			case config.CloudTCLighthouse:
				if payload["FirewallVersion"] != float64(101) {
					t.Errorf("Create must retain S0 version: %+v", payload)
				}
				rules, _ = payload["FirewallRules"].([]any)
			case config.CloudTCCVM:
				ps, _ := payload["SecurityGroupPolicySet"].(map[string]any)
				if ps["Version"] != "101" {
					t.Errorf("Create must retain S0 version, not quota-read: %+v", ps)
				}
				rules, _ = ps["Ingress"].([]any)
			case config.CloudAliECS:
				key := "Permissions.1.SourceCidrIp"
				if ipv6 {
					key = "Permissions.1.Ipv6SourceCidrIp"
				}
				if r.Form.Get(key) != strings.ToLower(desiredIP)+mask || r.Form.Get("Permissions.1.PortRange") != "443/443" {
					t.Errorf("bad create parameters %v", r.Form)
				}
			case config.CloudAliSWAS:
				if err := json.Unmarshal([]byte(r.Form.Get("FirewallRules")), &rules); err != nil {
					t.Error(err)
				}
				v4Key = "SourceCidrIp"
			}
			if ct != config.CloudAliECS {
				if len(rules) != 1 {
					t.Errorf("create count %d want 1", len(rules))
				} else {
					a, _ := rules[0].(map[string]any)
					key, opposite := v4Key, v6Key
					if ipv6 {
						key, opposite = v6Key, v4Key
					}
					if a[key] != strings.ToLower(desiredIP)+mask {
						t.Errorf("create must remain canonical: %+v", a)
					}
					if v, ok := a[opposite]; ok && v != "" {
						t.Errorf("both address families sent: %+v", a)
					}
				}
			}
			if mode == "add_error" {
				fail()
			} else if mode != "s1_missing" {
				desiredPresent = true
			}
		case deleteAction:
			deletes++
			if !desiredPresent || reads < 2 {
				t.Error("delete before coverage confirmation")
			}
			n := 1
			if mode == "owned_twins" {
				n = 2
			}
			switch ct {
			case config.CloudTCLighthouse:
				if payload["FirewallVersion"] != float64(100+reads) {
					t.Errorf("delete version not S1: %+v", payload)
				}
				rr, _ := payload["FirewallRules"].([]any)
				if len(rr) != n {
					t.Errorf("delete count=%d want=%d", len(rr), n)
				}
				if len(rr) > 0 {
					a, _ := rr[0].(map[string]any)
					key := "CidrBlock"
					if ipv6 {
						key = "Ipv6CidrBlock"
					}
					if a[key] != oldIP {
						t.Errorf("delete lost raw S1 address: %+v want %q", a, oldIP)
					}
				}
			case config.CloudTCCVM:
				ps, _ := payload["SecurityGroupPolicySet"].(map[string]any)
				if ps["Version"] != strconv.Itoa(100+reads) {
					t.Errorf("delete version not S1: %+v", ps)
				}
				rr, _ := ps["Ingress"].([]any)
				if len(rr) != n {
					t.Errorf("delete count=%d want=%d", len(rr), n)
				}
				for i, x := range rr {
					a, _ := x.(map[string]any)
					if a["PolicyIndex"] != float64(3+i) || len(a) != 1 {
						t.Errorf("delete index not S1 or extra selector: %+v", a)
					}
				}
			case config.CloudAliECS:
				ids, err := ecsCleanupRPCIDs(r.Form)
				if err != nil {
					t.Error(err)
				}
				want := []string{"old-s1"}
				if n == 2 {
					want = append(want, "old2-s1")
				}
				if !reflect.DeepEqual(ids, want) {
					t.Errorf("delete IDs=%v want S1 %v", ids, want)
				}
			case config.CloudAliSWAS:
				want := "old-s1"
				if n == 2 {
					want += ",old2-s1"
				}
				if r.Form.Get("RuleIds") != want {
					t.Errorf("delete IDs=%s want S1 %s", r.Form.Get("RuleIds"), want)
				}
			}
			if mode == "version_retry" && deletes == 1 {
				code := "UnsupportedOperation.VersionMismatch"
				if ct == config.CloudTCLighthouse {
					code = "UnsupportedOperation.FirewallVersionMismatch"
				}
				body = map[string]any{"Error": map[string]any{"Code": code, "Message": "local concurrent version"}}
			} else if mode == "delete_error" {
				fail()
			} else {
				stalePresent = false
			}
		default:
			t.Errorf("unexpected cloud action %q", action)
			fail()
		}
		if tc {
			body = map[string]any{"Response": body}
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	endpoint := strings.TrimPrefix(srv.URL, "http://")
	region := "cn-hangzhou"
	resource := "mock-resource"
	var client any
	var err error
	if tc {
		region = "ap-guangzhou"
		cpf := profile.NewClientProfile()
		cpf.HttpProfile.Endpoint = endpoint
		cpf.HttpProfile.Scheme = "http"
		if ct == config.CloudTCLighthouse {
			client, err = lighthouse.NewClient(common.NewCredential("fake", "fake"), region, cpf)
		} else {
			client, err = vpc.NewClient(common.NewCredential("fake", "fake"), region, cpf)
		}
	} else {
		ac := &openapi.Config{AccessKeyId: tea.String("fake"), AccessKeySecret: tea.String("fake"), Endpoint: tea.String(endpoint), Protocol: tea.String("http")}
		if ct == config.CloudAliECS {
			client, err = ecs.NewClient(ac)
		} else {
			client, err = swas.NewClient(ac)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	pool := provider.NewClientPool(provider.Credentials{})
	if _, err = pool.GetOrCreate(pool.CacheKey(ct, region), func() (any, error) { return client, nil }); err != nil {
		t.Fatal(err)
	}
	p, err := provider.NewProvider(config.TargetConfig{CloudType: ct, Region: region, ResourceID: resource}, 1, pool)
	if err != nil {
		t.Fatal(err)
	}
	localRule := staticRule(1, "a.test", "TCP", "443")
	localRule.EnableIPv6 = ipv6
	resolved := map[string]string{"a.test": strings.ToLower(desiredIP) + mask}
	if mode == "dns_failure" {
		resolved = nil
	}
	s := newTargetSyncer(t, []provider.Provider{p}, []config.DomainRule{localRule}, resolved)
	s.syncAll()
	sum := s.Status().LastRound
	failed := mode == "dns_failure" || mode == "add_error" || mode == "s1_error" || mode == "s1_missing" || mode == "s2_error"
	wantOutcome := RoundSuccess
	if failed {
		wantOutcome = RoundFailed
	}
	added := 0
	if mode == "replace" || mode == "s1_error" || mode == "s1_missing" || mode == "s2_error" {
		added = 1
	}
	blocked := mode == "dns_failure" || mode == "missing_locator" || mode == "invalid_manual" || mode == "opposite_action" || mode == "add_error" || mode == "s1_error" || mode == "s1_missing" || mode == "delete_error" || (ct == config.CloudTCLighthouse && (mode == "owned_twins" || mode == "external_twin"))
	deleted := 1
	if blocked {
		deleted = 0
	}
	if mode == "owned_twins" && !blocked {
		deleted = 2
	}
	if sum == nil || sum.Outcome != wantOutcome || sum.Added != added || sum.Deleted != deleted || sum.CleanupDeleted != deleted {
		t.Fatalf("round=%+v want outcome=%s added=%d deleted=%d", sum, wantOutcome, added, deleted)
	}
	mu.Lock()
	firstCreates, firstDeletes := creates, deletes
	firstActions := append([]string(nil), actions...)
	if mode == "version_retry" && (deletes != 2 || !reflect.DeepEqual(firstActions, []string{readAction, readAction, deleteAction, readAction, readAction, deleteAction, readAction})) {
		t.Errorf("version retry must reread and replan whole target: %v", firstActions)
	}
	if blocked && mode != "delete_error" && deletes != 0 {
		t.Error("safety gate allowed delete request")
	}
	if mode == "delete_error" && (!stalePresent || sum.CleanupDeferred != 1) {
		t.Errorf("failed delete lost deferred old rule: %+v", sum)
	}
	if deleted > 0 && stalePresent {
		t.Error("confirmed deletion did not remove old rules")
	}
	if deleted > 0 && firstActions[len(firstActions)-1] != readAction {
		t.Errorf("missing S2 read: %v", firstActions)
	}
	mu.Unlock()
	if failed || blocked {
		return
	}
	s.syncAll()
	sum = s.Status().LastRound
	if sum == nil || sum.Outcome != RoundSuccess || sum.Added != 0 || sum.Deleted != 0 || sum.CleanupCandidates != 0 {
		t.Fatalf("next round did not converge: %+v", sum)
	}
	mu.Lock()
	defer mu.Unlock()
	if creates != firstCreates || deletes != firstDeletes || !reflect.DeepEqual(actions[len(firstActions):], []string{readAction, readAction}) {
		t.Errorf("next round repeated mutation: %v", actions[len(firstActions):])
	}
}
