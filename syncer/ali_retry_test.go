package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	swas "github.com/alibabacloud-go/swas-open-20200601/v3/client"
	"github.com/alibabacloud-go/tea/tea"
)

// i8r12SDKError 通过真实 SDK 读取本地业务失败响应，验证默认转换与单次请求次数。
func i8r12SDKError(t *testing.T, ct config.CloudType, code, msg string, status int) error {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(map[string]any{"Code": code, "Message": msg, "RequestId": "local"}); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	cfg := &openapi.Config{AccessKeyId: tea.String("fake"), AccessKeySecret: tea.String("fake"), Endpoint: tea.String(strings.TrimPrefix(srv.URL, "http://")), Protocol: tea.String("http"), ConnectTimeout: tea.Int(1000), ReadTimeout: tea.Int(1000)}
	var err error
	if ct == config.CloudAliECS {
		c, e := ecs.NewClient(cfg)
		if e != nil {
			t.Fatal(e)
		}
		_, err = c.DescribeSecurityGroupAttribute(&ecs.DescribeSecurityGroupAttributeRequest{RegionId: tea.String("cn-hangzhou"), SecurityGroupId: tea.String("sg-local")})
	} else {
		c, e := swas.NewClient(cfg)
		if e != nil {
			t.Fatal(e)
		}
		_, err = c.ListFirewallRules(&swas.ListFirewallRulesRequest{RegionId: tea.String("cn-hangzhou"), InstanceId: tea.String("i-local")})
	}
	var sdk *tea.SDKError
	if !errors.As(err, &sdk) || (code != "" && tea.StringValue(sdk.Code) != code) || calls.Load() != 1 || tea.IntValue(sdk.StatusCode) != status {
		t.Fatalf("shape=%T calls=%d code=%s", err, calls.Load(), code)
	}
	return fmt.Errorf("provider wrap: %w", err)
}

// TestI8R12SDKClassification 验证 ECS/SWAS 实际 SDK 错误与业务码边界。
func TestI8R12SDKClassification(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudAliECS, config.CloudAliSWAS} {
		for _, x := range []struct {
			code, msg string
			status    int
			want      bool
		}{
			{"Throttling", "busy", 400, true}, {"Throttling.User", "busy", 403, true}, {"Throttling.Api", "busy", 429, true}, {"ServiceUnavailable", "busy", 500, true}, {"ServiceUnavailable", "busy", 503, true}, {"InternalError", "busy", 500, true},
			{"Forbidden.RAM", "timeout InternalError Throttling ServiceUnavailable", 403, false}, {"QuotaExceeded", "timeout", 400, false}, {"Unknown", "InternalError", 500, false}, {"Throttling.Future", "busy", 400, false}, {"NotThrottling", "busy", 400, false}, {"serviceunavailable", "busy", 500, false}, {"", "timeout", 500, false},
		} {
			t.Run(string(ct)+"/"+x.code+fmt.Sprint(x.status), func(t *testing.T) {
				err := i8r12SDKError(t, ct, x.code, x.msg, x.status)
				if got := isRetryable(err); got != x.want {
					t.Fatalf("retry=%v want=%v", got, x.want)
				}
			})
		}
	}
	t.Run("budget-double-identity", func(t *testing.T) {
		if !isRetryable(errors.Join(provider.ErrSnapshotIncomplete, context.DeadlineExceeded)) {
			t.Fatal("legitimate budget retry lost")
		}
		if isRetryable(provider.ErrSnapshotIncomplete) {
			t.Fatal("incomplete alone retried")
		}
	})
}

// TestI8R12Idempotency 证明 SDK 消息不能覆盖幂等和版本分类。
func TestI8R12Idempotency(t *testing.T) {
	for _, x := range []struct {
		code, msg   string
		create, del bool
	}{
		{"FirewallRuleAlreadyExist", "busy", true, false}, {"InvalidSecurityGroupRuleId.NotFound", "busy", false, true},
		{"Throttling", "FirewallRuleAlreadyExist InvalidSecurityGroupRuleId.NotFound VersionMismatch", false, false},
		{"Forbidden.RAM", "FirewallRuleAlreadyExist InvalidInstanceId.NotFound", false, false},
	} {
		t.Run(x.code, func(t *testing.T) {
			err := i8r12SDKError(t, config.CloudAliSWAS, x.code, x.msg, 400)
			if isIdempotentCreate(err) != x.create || isIdempotentDelete(err) != x.del || isVersionMismatch(err) {
				t.Fatal("message altered error code classification")
			}
		})
	}
}

// TestI8R12TargetSDK 贯通真实 SDK、Provider 与正式目标链，只访问回环服务。
// D/A/X 分别表示完整快照读取、新增与删除；每次读取改变陈旧 RuleID 以判别定位来源。
func TestI8R12TargetSDK(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudAliECS, config.CloudAliSWAS} {
		for _, x := range []struct {
			stage, code, msg         string
			exhaust                  bool
			seq                      string
			added, deleted, deferred int
			outcome                  RoundOutcome
			attempts                 int
		}{
			{"add-applied", "FirewallRuleAlreadyExist", "timeout", false, "D,A,D,X,D", 0, 1, 0, RoundSuccess, 1},
			{"add", "FirewallRuleAlreadyExist", "timeout", false, "D,A,D", 0, 0, 1, RoundFailed, 1},
			{"delete-absent", "InvalidParam.SecurityGroupRuleId", "timeout", false, "D,A,D,X,D", 1, 0, 0, RoundSuccess, 1},
			{"delete-absent", "InvalidInstanceId.NotFound", "timeout", false, "D,A,D,X,D", 1, 0, 0, RoundSuccess, 1},
			{"s0", "Throttling", "busy", false, "D,D,A,D,X,D", 1, 1, 0, RoundSuccess, 2},
			{"add", "ServiceUnavailable", "busy", false, "D,A,D,A,D,X,D", 1, 1, 0, RoundSuccess, 2},
			{"add-applied", "ServiceUnavailable", "busy", false, "D,A,D,D,X,D", 0, 1, 0, RoundSuccess, 2},
			{"s1", "Throttling.User", "busy", false, "D,A,D,D,D,X,D", 1, 1, 0, RoundSuccess, 2},
			{"delete", "Throttling.Api", "busy", false, "D,A,D,X,D,D,X,D", 1, 1, 0, RoundSuccess, 2},
			{"s2", "ServiceUnavailable", "busy", false, "D,A,D,X,D,D,D", 1, 1, 0, RoundSuccess, 2},
			{"s0", "Throttling", "busy", true, "D,D,D", 0, 0, 0, RoundFailed, 3},
			{"add", "Throttling", "busy", true, "D,A,D,A,D,A", 0, 0, 0, RoundFailed, 3},
			{"delete", "Throttling", "busy", true, "D,A,D,X,D,D,X,D,D,X", 1, 0, 1, RoundSuccess, 3},
			{"s0", "Forbidden.RAM", "timeout InternalError", false, "D", 0, 0, 0, RoundFailed, 1},
			{"add", "Throttling", "FirewallRuleAlreadyExist", false, "D,A,D,A,D,X,D", 1, 1, 0, RoundSuccess, 2},
			{"delete", "Throttling", "InvalidSecurityGroupRuleId.NotFound", false, "D,A,D,X,D,D,X,D", 1, 1, 0, RoundSuccess, 2},
		} {
			t.Run(string(ct)+"/"+x.stage+"/"+x.code+fmt.Sprint(x.exhaust)+x.msg, func(t *testing.T) {
				var mu sync.Mutex
				var seq []string
				reads, fails := 0, 0
				added, stale := false, true
				latestStaleID := ""
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					w.Header().Set("Content-Type", "application/json")
					if err := r.ParseForm(); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					action := r.Header.Get("X-Acs-Action")
					kind := ""
					switch action {
					case "DescribeSecurityGroupAttribute", "ListFirewallRules":
						kind = "D"
						reads++
					case "AuthorizeSecurityGroup", "CreateFirewallRules":
						kind = "A"
					case "RevokeSecurityGroup", "DeleteFirewallRules":
						kind = "X"
					default:
						t.Errorf("unknown action=%s", action)
					}
					seq = append(seq, kind)
					if len(seq) > 15 {
						w.WriteHeader(400)
						if err := json.NewEncoder(w).Encode(map[string]any{"Code": "FixtureBound"}); err != nil {
							t.Error(err)
						}
						return
					}
					if kind == "X" {
						var ids []string
						if ct == config.CloudAliECS {
							var err error
							ids, err = ecsCleanupRPCIDs(r.Form)
							if err != nil {
								t.Error(err)
							}
						} else {
							ids = strings.Split(r.Form.Get("RuleIds"), ",")
						}
						if !reflect.DeepEqual(ids, []string{latestStaleID}) {
							t.Errorf("删除未使用最新 S1 定位: got=%v want=%s", ids, latestStaleID)
						}
					}
					stageMatch := (x.stage == "s0" && kind == "D") || ((x.stage == "add" || x.stage == "add-applied") && kind == "A") || (x.stage == "s1" && kind == "D" && reads == 2) || (x.stage == "s2" && kind == "D" && reads == 3) || ((x.stage == "delete" || x.stage == "delete-absent") && kind == "X")
					if stageMatch && (fails == 0 || x.exhaust) {
						fails++
						if x.stage == "add-applied" {
							added = true
						}
						if x.stage == "delete-absent" {
							stale = false
						}
						w.WriteHeader(500)
						if err := json.NewEncoder(w).Encode(map[string]any{"Code": x.code, "Message": x.msg}); err != nil {
							t.Error(err)
						}
						return
					}
					body := map[string]any{"RequestId": "local"}
					if kind == "D" {
						latestStaleID = fmt.Sprintf("stale-read-%d", reads)
						rules := []map[string]any{}
						if ct == config.CloudAliECS {
							if added {
								rules = append(rules, map[string]any{"SecurityGroupRuleId": "desired", "IpProtocol": "TCP", "PortRange": "443/443", "SourceCidrIp": "1.1.1.1/32", "Policy": "accept", "Description": "[auto-dns]"})
							}
							if stale {
								rules = append(rules, map[string]any{"SecurityGroupRuleId": latestStaleID, "IpProtocol": "TCP", "PortRange": "22/22", "SourceCidrIp": "192.0.2.1/32", "Policy": "accept", "Description": "[auto-dns]"})
							}
							body["Permissions"] = map[string]any{"Permission": rules}
						} else {
							if added {
								rules = append(rules, map[string]any{"RuleId": "desired", "RuleProtocol": "TCP", "Port": "443", "SourceCidrIp": "1.1.1.1/32", "Policy": "accept", "Remark": "[auto-dns]"})
							}
							if stale {
								rules = append(rules, map[string]any{"RuleId": latestStaleID, "RuleProtocol": "TCP", "Port": "22", "SourceCidrIp": "192.0.2.1/32", "Policy": "accept", "Remark": "[auto-dns]"})
							}
							body["FirewallRules"] = rules
							body["TotalCount"] = len(rules)
						}
					} else if kind == "A" {
						added = true
					} else if kind == "X" {
						stale = false
					}
					if err := json.NewEncoder(w).Encode(body); err != nil {
						t.Error(err)
					}
				}))
				defer srv.Close()
				cfg := &openapi.Config{AccessKeyId: tea.String("fake"), AccessKeySecret: tea.String("fake"), Endpoint: tea.String(strings.TrimPrefix(srv.URL, "http://")), Protocol: tea.String("http"), ConnectTimeout: tea.Int(1000), ReadTimeout: tea.Int(1000)}
				pool := provider.NewClientPool(provider.Credentials{})
				var cli any
				var err error
				if ct == config.CloudAliECS {
					cli, err = ecs.NewClient(cfg)
				} else {
					cli, err = swas.NewClient(cfg)
				}
				if err != nil {
					t.Fatal(err)
				}
				if _, err := pool.GetOrCreate(pool.CacheKey(ct, "cn-hangzhou"), func() (any, error) { return cli, nil }); err != nil {
					t.Fatal(err)
				}
				p, err := provider.NewProvider(config.TargetConfig{CloudType: ct, Region: "cn-hangzhou", ResourceID: "local"}, 1, pool)
				if err != nil {
					t.Fatal(err)
				}
				s := newTargetSyncer(t, []provider.Provider{p}, []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}, map[string]string{"a.example.com": "1.1.1.1/32"})
				resolves := 0
				base := s.resolveHostFn
				s.resolveHostFn = func(host string) (ips []dns.ResolvedIP, err error) { resolves++; return base(host) }
				var backoffs []time.Duration
				s.sleepFn = func(delay time.Duration) { backoffs = append(backoffs, delay) }
				s.syncAll()
				sum := s.Status().LastRound
				mu.Lock()
				got := strings.Join(seq, ",")
				mu.Unlock()
				if got != x.seq || sum.Added != x.added || sum.Deleted != x.deleted || sum.CleanupDeferred != x.deferred || sum.Outcome != x.outcome {
					t.Fatalf("seq=%s sum=%+v want seq=%s added/deleted/deferred=%d/%d/%d outcome=%s", got, sum, x.seq, x.added, x.deleted, x.deferred, x.outcome)
				}
				var wantWaits []time.Duration
				if x.attempts >= 2 {
					wantWaits = append(wantWaits, time.Second)
				}
				if x.attempts == 3 {
					wantWaits = append(wantWaits, 2*time.Second)
				}
				if resolves != x.attempts || !reflect.DeepEqual(backoffs, wantWaits) {
					t.Fatalf("DNS/退避=%d/%v, want %d/%v", resolves, backoffs, x.attempts, wantWaits)
				}
			})
		}
	}
}

// TestI8R12CodeBoundaries 明确缺失码、包装链、幂等及预算优先级。
func TestI8R12CodeBoundaries(t *testing.T) {
	codes := []struct {
		code               string
		retry, create, del bool
	}{
		{"Throttling", true, false, false}, {"Throttling.User", true, false, false}, {"Throttling.Api", true, false, false}, {"ServiceUnavailable", true, false, false}, {"InternalError", true, false, false},
		{"FirewallRulesExist", false, true, false}, {"FirewallRuleAlreadyExist", false, true, false}, {"DuplicatePolicy", false, true, false}, {"FirewallRulesDuplicated", false, true, false},
		{"FirewallRulesNotFound", false, false, true}, {"InvalidParam.SecurityGroupRuleId", false, false, true}, {"InvalidSecurityGroupRuleId.NotFound", false, false, true}, {"InvalidSecurityGroupRule.RuleNotExist", false, false, true}, {"InvalidInstanceId.NotFound", false, false, true},
		{"Forbidden.RAM", false, false, false}, {"FirewallRuleLimitExceed", false, false, false}, {"AuthorizationLimitExceed", false, false, false}, {"InvalidSecurityGroupId.NotFound", false, false, false},
		{"Throttling.Future", false, false, false}, {"InternalError.Timeout", false, false, false}, {"RequestTimeout", false, false, false}, {"RequestLimitExceeded", false, false, false}, {"Unknown", false, false, false}, {"", false, false, false},
		{" Throttling", false, false, false}, {"Throttling ", false, false, false}, {"throttling", false, false, false}, {"NotThrottling", false, false, false}, {"InvalidParam.SecurityGroupRuleId.Other", false, false, false}, {"FirewallRuleAlreadyExist.Other", false, false, false},
	}
	for i, x := range codes {
		for _, wrap := range []string{"direct", "provider-target", "cleanup", "partial"} {
			t.Run(fmt.Sprintf("%02d/%s", i, wrap), func(t *testing.T) {
				sdk := &tea.SDKError{Code: tea.String(x.code), Message: tea.String("timeout Throttling ServiceUnavailable InternalError FirewallRuleAlreadyExist InvalidParam.SecurityGroupRuleId VersionMismatch"), Detail: tea.String("timeout"), Data: tea.String("InternalError")}
				if x.code == "" {
					sdk.Code = nil
				}
				var err error = sdk
				switch wrap {
				case "provider-target":
					err = fmt.Errorf("VersionMismatch: %w", fmt.Errorf("FirewallRuleAlreadyExist: %w", err))
				case "cleanup":
					err = &retryableCleanupError{err: err}
				case "partial":
					err = &provider.PartialDeleteError{Deleted: 1, Err: err}
				}
				if isRetryable(err) != x.retry || isIdempotentCreate(err) != x.create || isIdempotentDelete(err) != x.del || isVersionMismatch(err) {
					t.Fatalf("分类不符: code=%q retry/create/delete/version=%v/%v/%v/%v", x.code, isRetryable(err), isIdempotentCreate(err), isIdempotentDelete(err), isVersionMismatch(err))
				}
			})
		}
	}
	for _, x := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false}, {"pure-incomplete", provider.ErrSnapshotIncomplete, false}, {"canceled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, true}, {"budget-with-business-cause", errors.Join(provider.ErrSnapshotIncomplete, context.DeadlineExceeded, &tea.SDKError{Code: tea.String("Forbidden.RAM")}), true},
		{"plain-throttling-no-new-fallback", errors.New("Throttling"), false}, {"plain-service-unavailable-no-new-fallback", errors.New("ServiceUnavailable"), false},
		{"legacy-timeout", errors.New("I/O TIMEOUT"), true}, {"legacy-version", errors.New("UnsupportedOperation.VersionMismatch"), true},
	} {
		t.Run(x.name, func(t *testing.T) {
			if got := isRetryable(x.err); got != x.want {
				t.Fatalf("retry=%v want=%v", got, x.want)
			}
		})
	}
}

// TestI8R12ReplanChangedDNS 验证重试使用新解析结果，不沿用上一次 ToAdd。
func TestI8R12ReplanChangedDNS(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudAliECS, config.CloudAliSWAS} {
		t.Run(string(ct), func(t *testing.T) {
			p := newProbeProvider(ct, 1)
			p.createErrs = []error{&tea.SDKError{Code: tea.String("ServiceUnavailable"), Message: tea.String("busy")}}
			s := newTargetSyncer(t, []provider.Provider{p}, []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}, nil)
			resolves := 0
			s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
				resolves++
				if resolves == 1 {
					return []dns.ResolvedIP{mockResolved("192.0.2.1/32")}, nil
				}
				return []dns.ResolvedIP{mockResolved("192.0.2.2/32")}, nil
			}
			var waits []time.Duration
			s.sleepFn = func(d time.Duration) { waits = append(waits, d) }
			s.syncAll()
			sum := s.Status().LastRound
			if resolves != 2 || !p.hasRule("192.0.2.2/32", "443/443") || p.hasRule("192.0.2.1/32", "443/443") || sum.Outcome != RoundSuccess || sum.Added != 1 || !reflect.DeepEqual(waits, []time.Duration{time.Second}) {
				t.Fatalf("未重新规划: resolves=%d waits=%v sum=%+v", resolves, waits, sum)
			}
			if !reflect.DeepEqual(p.callSeq(), []string{"snapshot", "create:1", "snapshot", "create:1", "snapshot"}) {
				t.Fatal(p.callSeq())
			}
		})
	}
}
