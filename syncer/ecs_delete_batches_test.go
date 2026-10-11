package syncer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	"github.com/alibabacloud-go/tea/tea"
)

// TestTargetRound_ECSDeleteBatchesThroughSDK 贯通实际 SDK、正式目标链、S2 和事件汇总。
// 全部请求只访问本地 HTTP 夹具，不构成真实云验收。
func TestTargetRound_ECSDeleteBatchesThroughSDK(t *testing.T) {
	for _, tc := range []struct {
		mode              string
		sizes             []int
		deleted, deferred int
		outcome           RoundOutcome
	}{
		{"success", []int{100, 100, 50}, 250, 0, RoundSuccess},
		{"second_fail", []int{100, 100}, 100, 150, RoundSuccess},
		{"second_fail_external_removes7", []int{100, 100}, 100, 143, RoundSuccess},
		{"second_fail_applied", []int{100, 100}, 100, 50, RoundSuccess},
		{"second_fail_s2_error", []int{100, 100}, 100, 150, RoundFailed},
		{"second_fail_s2_loses_desired", []int{100, 100}, 100, 150, RoundFailed},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			mode := tc.mode
			var mu sync.Mutex
			stale := make([]map[string]any, 250)
			wantIDs := make([]string, 250)
			remaining := map[string]bool{}
			for i := range stale {
				id := fmt.Sprintf("sgr-%04d", 250-i)
				wantIDs[i] = id
				remaining[id] = true
				stale[i] = map[string]any{"SecurityGroupRuleId": id, "IpProtocol": "TCP", "PortRange": fmt.Sprintf("%d/%d", 20000+i, 20000+i), "SourceCidrIp": "192.0.2.5/32", "Policy": "accept", "Description": "[auto-dns]"}
			}
			desired := map[string]any{"SecurityGroupRuleId": "desired", "IpProtocol": "TCP", "PortRange": "443/443", "SourceCidrIp": "1.1.1.1/32", "Policy": "accept", "Description": "[auto-dns]"}
			foreign := map[string]any{"SecurityGroupRuleId": "foreign", "IpProtocol": "TCP", "PortRange": "22/22", "SourceCidrIp": "192.0.2.6/32", "Policy": "accept", "Description": "[other]"}
			added := false
			describes := 0
			batches := [][]string{}
			actions := []string{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				body := map[string]any{"RequestId": "mock"}
				action := r.Header.Get("X-Acs-Action")
				actions = append(actions, action)
				if r.Form.Get("RegionId") != "cn-hangzhou" || r.Form.Get("SecurityGroupId") != "sg-mock" {
					t.Error("请求目标不符")
				}
				if len(actions) > 10 {
					w.WriteHeader(400)
					if err := json.NewEncoder(w).Encode(map[string]any{"Code": "InvalidParameter", "Message": "fixture bound"}); err != nil {
						t.Error(err)
					}
					return
				}
				switch action {
				case "DescribeSecurityGroupAttribute":
					describes++
					page := []any{foreign}
					for _, x := range stale {
						if remaining[x["SecurityGroupRuleId"].(string)] {
							cloudRule := map[string]any{}
							for k, v := range x {
								cloudRule[k] = v
							}
							// S0 与 S1 功能相同但定位不同，防止误用旧定位也碰巧通过。
							if describes == 1 {
								cloudRule["SecurityGroupRuleId"] = "s0-" + x["SecurityGroupRuleId"].(string)
							}
							page = append(page, cloudRule)
						}
					}
					if added && !(describes >= 3 && mode == "second_fail_s2_loses_desired") {
						page = append(page, desired)
					}
					if describes >= 3 && mode == "second_fail_s2_error" {
						w.WriteHeader(400)
						body = map[string]any{"Code": "InvalidParameter", "Message": "S2 unavailable"}
					} else {
						body["Permissions"] = map[string]any{"Permission": page}
					}
				case "AuthorizeSecurityGroup":
					added = true
				case "RevokeSecurityGroup":
					ids, decodeErr := ecsCleanupRPCIDs(r.Form)
					if decodeErr != nil {
						t.Error(decodeErr)
						w.WriteHeader(400)
						return
					}
					for key := range r.Form {
						if strings.HasPrefix(key, "Permissions") {
							t.Errorf("不得按规则内容降级删除: %s", key)
						}
					}
					batches = append(batches, ids)
					if len(ids) < 1 || len(ids) > 100 {
						t.Errorf("删除批大小非法: %d", len(ids))
					}
					for _, id := range ids {
						if id == "foreign" || id == "desired" {
							t.Error("期望或非当前TAG规则被提交删除")
						}
					}
					if mode != "success" && len(batches) == 2 {
						// 失败响应不保证未生效；本场景实际应用第二批后再返回错误。
						if mode == "second_fail_applied" {
							for _, id := range ids {
								delete(remaining, id)
							}
						}
						if mode == "second_fail_external_removes7" {
							for _, id := range wantIDs[100:107] {
								delete(remaining, id)
							}
						}
						w.WriteHeader(500)
						body = map[string]any{"Code": "InternalError", "Message": "batch2 failed"}
					} else {
						for _, id := range ids {
							delete(remaining, id)
						}
					}
				default:
					t.Errorf("非预期 SDK 动作 %s", action)
					w.WriteHeader(400)
				}
				if err := json.NewEncoder(w).Encode(body); err != nil {
					t.Error(err)
				}
			}))
			defer srv.Close()
			cli, err := ecs.NewClient(&openapi.Config{AccessKeyId: tea.String("fake"), AccessKeySecret: tea.String("fake"), Endpoint: tea.String(strings.TrimPrefix(srv.URL, "http://")), Protocol: tea.String("http")})
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
			targetType := notifier.EventTargetSyncComplete
			if tc.outcome == RoundFailed {
				targetType = notifier.EventSyncError
			}
			targetEvents := make(chan notifier.Event, 2)
			roundEvents := make(chan notifier.Event, 2)
			s.EventBus().Subscribe(targetType, roundEventSink{ch: targetEvents})
			s.EventBus().Subscribe(notifier.EventSyncComplete, roundEventSink{ch: roundEvents})
			s.syncAll()
			sum := s.Status().LastRound
			var targetEvent, roundEvent notifier.Event
			select {
			case targetEvent = <-targetEvents:
			case <-time.After(time.Second):
				t.Fatal("未收到目标完成/失败事件")
			}
			select {
			case roundEvent = <-roundEvents:
			case <-time.After(time.Second):
				t.Fatal("未收到整轮完成事件")
			}
			mu.Lock()
			defer mu.Unlock()
			calls, deleted, deferred, outcome := len(tc.sizes), tc.deleted, tc.deferred, tc.outcome
			if len(batches) != calls || describes != 3 {
				t.Fatalf("删除请求=%d，快照=%d，预期 %d/3", len(batches), describes, calls)
			}
			offset := 0
			for i, ids := range batches {
				end := offset + tc.sizes[i]
				if !reflect.DeepEqual(ids, wantIDs[offset:end]) {
					t.Fatalf("第%d批 ID 数量或顺序不符", i+1)
				}
				offset = end
			}
			wantActions := []string{"DescribeSecurityGroupAttribute", "AuthorizeSecurityGroup", "DescribeSecurityGroupAttribute"}
			for range calls {
				wantActions = append(wantActions, "RevokeSecurityGroup")
			}
			wantActions = append(wantActions, "DescribeSecurityGroupAttribute")
			if !reflect.DeepEqual(actions, wantActions) {
				t.Fatalf("实际 SDK 动作顺序=%v，预期 %v", actions, wantActions)
			}
			if sum == nil || sum.Total != 1 || sum.Added != 1 || sum.Deleted != deleted || sum.CleanupCandidates != 250 || sum.CleanupDeleted != deleted || sum.CleanupDeferred != deferred || sum.Outcome != outcome {
				t.Fatalf("整轮汇总=%+v，预期 outcome=%s deleted/deferred=%d/%d", sum, outcome, deleted, deferred)
			}
			estimated := outcome == RoundFailed
			wantFailed, wantChanged := 0, 1
			if estimated {
				wantFailed, wantChanged = 1, 0
			}
			if sum.Failed != wantFailed || sum.Changed != wantChanged || sum.OK != 0 || sum.Skipped != 0 {
				t.Fatalf("整轮目标分类不符: %+v", sum)
			}
			wantSummary := CleanupObservationSummary{ObservedTargets: 1, ObservedCandidates: 250, ObservedDeferred: deferred, Complete: true}
			if estimated {
				wantSummary = CleanupObservationSummary{EstimatedTargets: 1}
			}
			if sum.CleanupObservationSummary != wantSummary {
				t.Fatalf("观察分类不符: got=%+v, want=%+v", sum.CleanupObservationSummary, wantSummary)
			}
			if roundEvent.Data["cleanup_observation_summary"] != wantSummary {
				t.Fatalf("整轮事件观察与状态汇总不符: %+v", roundEvent.Data)
			}
			wantBasis := "s2"
			if estimated {
				wantBasis = "delete_progress"
			}
			observation, ok := targetEvent.Data["cleanup_observation"].(*CleanupObservation)
			if !ok || observation.Basis != wantBasis || observation.Deferred != deferred || observation.Candidates != 250 || observation.Attempt != 1 || observation.Historical || !observation.DesiredComplete {
				t.Fatalf("目标观察=%+v，预期 %s/%d", observation, wantBasis, deferred)
			}
			if targetEvent.Data["attempts"] != 1 {
				t.Fatalf("部分成功后不应整目标重试: %v", targetEvent.Data["attempts"])
			}
			for _, ev := range []notifier.Event{targetEvent, roundEvent} {
				if ev.Data["added"] != 1 || ev.Data["deleted"] != deleted || ev.Data["cleanup_deleted"] != deleted || ev.Data["cleanup_candidates"] != 250 || ev.Data["cleanup_deferred"] != deferred || ev.Data["outcome"] != string(outcome) {
					t.Fatalf("事件计数或结论不符: %+v", ev.Data)
				}
			}
		})
	}
}

// ecsCleanupRPCIDs 校验真实 RPC 数组的连续索引和单值，拒绝重复/跳号/非法索引。
func ecsCleanupRPCIDs(values url.Values) ([]string, error) {
	indexed := make(map[int]string)
	for key, vals := range values {
		if key == "SecurityGroupRuleId" {
			return nil, fmt.Errorf("缺少数组索引")
		}
		if !strings.HasPrefix(key, "SecurityGroupRuleId.") {
			continue
		}
		suffix := strings.TrimPrefix(key, "SecurityGroupRuleId.")
		n, err := strconv.Atoi(suffix)
		if err != nil || n < 1 || strconv.Itoa(n) != suffix || len(vals) != 1 || vals[0] == "" {
			return nil, fmt.Errorf("非法 RPC ID 字段 %q: %v", key, vals)
		}
		indexed[n] = vals[0]
	}
	ids := make([]string, len(indexed))
	for i := range ids {
		id, ok := indexed[i+1]
		if !ok {
			return nil, fmt.Errorf("RPC ID 索引 %d 缺失", i+1)
		}
		ids[i] = id
	}
	return ids, nil
}

func TestTargetRound_ECSDeleteRPCIDsNegativeControls(t *testing.T) {
	for i, values := range []url.Values{
		{"SecurityGroupRuleId.1": {"a"}, "SecurityGroupRuleId.3": {"b"}},
		{"SecurityGroupRuleId.1": {"a", "b"}}, {"SecurityGroupRuleId.0": {"a"}},
		{"SecurityGroupRuleId.01": {"a"}}, {"SecurityGroupRuleId.x": {"a"}},
		{"SecurityGroupRuleId.1": {""}}, {"SecurityGroupRuleId": {"a"}},
	} {
		if _, err := ecsCleanupRPCIDs(values); err == nil {
			t.Fatalf("非法 RPC 参数场景 %d 被接受", i)
		}
	}
	ids, err := ecsCleanupRPCIDs(url.Values{"SecurityGroupRuleId.2": {"a"}, "SecurityGroupRuleId.1": {"z"}})
	if err != nil || !reflect.DeepEqual(ids, []string{"z", "a"}) {
		t.Fatalf("合法乱序参数名应按索引还原: %v, %v", ids, err)
	}
}
