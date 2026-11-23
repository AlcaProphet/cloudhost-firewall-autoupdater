package provider

import (
	"context"
	"fmt"
	"github.com/alibabacloud-go/tea/tea"
	tcerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"reflect"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

func TestBuild9S5ValidateCandidateChanges(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudTCLighthouse, config.CloudTCCVM, config.CloudAliSWAS, config.CloudAliECS} {
		t.Run(string(ct), func(t *testing.T) {
			old := s4Rule("[edgeone]", "TCP", "80,443", "192.0.2.0/24", "ACCEPT")
			old.RuleID = "id-old"
			old.PolicyIndex = "1"
			for _, mode := range []string{"unchanged", "comment", "new-row", "absent", "version", "content", "retag", "duplicate", "missing-id", "second-key"} {
				t.Run(mode, func(t *testing.T) {
					snapshot := RuleSnapshot{Revision: "7", Rules: []config.RuleInfo{old}}
					wantErr := false
					wantN := 1
					switch mode {
					case "comment":
						snapshot.Rules[0].Description = "[edgeone] 修改人类备注"
					case "new-row":
						extra := old
						extra.CidrBlock = "198.51.100.0/24"
						extra.RuleID = "new-id"
						extra.PolicyIndex = "2"
						snapshot.Rules = append(snapshot.Rules, extra)
					case "absent":
						snapshot.Rules = nil
						wantN = 0
					case "version":
						snapshot.Revision = "8"
						wantErr = ct == config.CloudTCCVM || ct == config.CloudTCLighthouse
					case "content":
						snapshot.Rules[0].Port = "8080"
						wantErr = ct != config.CloudTCLighthouse
						if !wantErr {
							wantN = 0
						}
					case "retag":
						snapshot.Rules[0].Description = "[edgeone-old]"
						wantErr = true
					case "duplicate":
						snapshot.Rules = append(snapshot.Rules, old)
						wantErr = true
					case "missing-id":
						if ct != config.CloudAliSWAS && ct != config.CloudAliECS {
							return
						}
						snapshot.Rules[0].RuleID = ""
						wantErr = true
					case "second-key":
						if ct != config.CloudTCLighthouse {
							return
						}
						extra := old
						extra.Port = "80"
						extra.Description = "manual"
						snapshot.Rules = append(snapshot.Rules, extra)
						wantErr = true
					}
					rows, absent, err := ValidateFirewallCleanup(ct, snapshot, "edgeone", "7", []config.RuleInfo{old})
					if (err != nil) != wantErr {
						t.Fatalf("rows=%v absent=%d err=%v wantErr=%v", rows, absent, err, wantErr)
					}
					if !wantErr && (len(rows) != wantN || absent != 1-wantN) {
						t.Fatalf("rows=%v absent=%d", rows, absent)
					}
				})
			}
		})
	}
}

// 真 SDK + 回环 HTTP：对同一份授权及执行快照核对实际删除线格式。
func TestBuild9S5FourPlatformDeleteWire(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudTCLighthouse, config.CloudTCCVM, config.CloudAliSWAS, config.CloudAliECS} {
		t.Run(string(ct), func(t *testing.T) {
			mock, host := newMockCloudAPI(t)
			var p Provider
			switch ct {
			case config.CloudTCLighthouse:
				p = mockLighthouse(t, host)
			case config.CloudTCCVM:
				p = mockCVM(t, host)
			case config.CloudAliSWAS:
				p = mockSWAS(t, host)
			case config.CloudAliECS:
				p = mockECS(t, host)
			}
			old := s4Rule("[edgeone]", "TCP", "80,443", "192.0.2.0/24", "ACCEPT")
			old.RuleID = "id-old"
			old.PolicyIndex = "3"
			manual := old
			manual.Description = "manual"
			manual.CidrBlock = "198.51.100.0/24"
			manual.RuleID = "id-manual"
			manual.PolicyIndex = "4"
			snapshot := RuleSnapshot{Revision: "7", Rules: []config.RuleInfo{old, manual}}
			rows, _, err := ValidateFirewallCleanup(ct, snapshot, "edgeone", "7", []config.RuleInfo{old})
			if err != nil {
				t.Fatal(err)
			}
			result, err := p.DeleteRules(snapshot, rows)
			if err != nil || result.Deleted != 1 {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			requests := mock.recorded()
			if len(requests) != 1 {
				t.Fatalf("requests=%v", requests)
			}
			req := requests[0]
			switch ct {
			case config.CloudTCLighthouse:
				body := bodyJSON(t, req)
				rules := objList(t, body, "FirewallRules")
				if req.action() != "DeleteFirewallRules" || body["FirewallVersion"] != float64(7) || len(rules) != 1 || rules[0]["Port"] != "80,443" || rules[0]["CidrBlock"] != old.CidrBlock {
					t.Fatalf("wire=%v", body)
				}
			case config.CloudTCCVM:
				body := bodyJSON(t, req)
				policies := objField(t, body, "SecurityGroupPolicySet")
				ingress := objList(t, policies, "Ingress")
				if req.action() != "DeleteSecurityGroupPolicies" || policies["Version"] != "7" || len(ingress) != 1 || ingress[0]["PolicyIndex"] != float64(3) || policies["Egress"] != nil {
					t.Fatalf("wire=%v", body)
				}
			case config.CloudAliSWAS:
				if req.action() != "DeleteFirewallRules" || !reflect.DeepEqual(rpcCommaList(req, "RuleIds"), []string{"id-old"}) {
					t.Fatalf("wire=%+v", req)
				}
			case config.CloudAliECS:
				if req.action() != "RevokeSecurityGroup" || !reflect.DeepEqual(rpcIndexedList(t, req, "SecurityGroupRuleId"), []string{"id-old"}) {
					t.Fatalf("wire=%+v", req)
				}
			}
		})
	}
}

func TestBuild9S5CleanupErrorInfo(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		id      string
		unknown bool
	}{
		{"timeout", context.DeadlineExceeded, "", true},
		{"wrapped-sdk-network", fmt.Errorf("delete: %w", tcerrors.NewTencentCloudSDKError("ClientError.NetworkError", "sensitive", "request-network")), "request-network", true},
		{"permission", tcerrors.NewTencentCloudSDKError("UnauthorizedOperation", "sensitive", "request-denied"), "request-denied", false},
		{"ali-timeout", &tea.SDKError{Code: tea.String("RequestTimeout"), Data: tea.String(`{"RequestId":"ali-timeout","Message":"sensitive"}`)}, "ali-timeout", true},
		{"ali-denied", &tea.SDKError{Code: tea.String("Forbidden"), Data: tea.String(`{"RequestId":"ali-denied"}`)}, "ali-denied", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, unknown := FirewallCleanupErrorInfo(tc.err)
			if id != tc.id || unknown != tc.unknown {
				t.Fatalf("id=%q unknown=%v", id, unknown)
			}
		})
	}
}
