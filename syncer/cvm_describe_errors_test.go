package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	vpc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
)

// 真实 CVM SDK → 正式整目标链；错误消息不带额外重试关键字，不将结果冒称云码白名单。
func TestI8R08CVMTargetDescribeFailure(t *testing.T) {
	for _, stage := range []string{"s0", "quota"} {
		for _, kind := range []string{"unauthorized", "resource-not-found", "rate-limit", "internal", "invalid-json", "http-json", "structure", "network-deadline"} {
			for _, recoverNext := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/recover=%v", stage, kind, recoverNext), func(t *testing.T) {
					cloud := &i8r08TargetCloud{t: t, stage: stage, kind: kind, recoverNext: recoverNext, phase: "s0", oldPresent: true}
					srv := httptest.NewServer(http.HandlerFunc(cloud.serve))
					defer srv.Close()
					cpf := profile.NewClientProfile()
					cpf.HttpProfile.Endpoint = strings.TrimPrefix(srv.URL, "http://")
					cpf.HttpProfile.Scheme = "http"
					cli, err := vpc.NewClient(common.NewCredential("mock", "mock"), "ap-guangzhou", cpf)
					if err != nil {
						t.Fatal(err)
					}
					// 默认单次请求时间与 SDK 重试配置保持；只在网络用例注入 deadline cause。
					transport := http.DefaultTransport.(*http.Transport).Clone()
					defer transport.CloseIdleConnections()
					cli.WithHttpTransport(i8r08TargetTransport{cloud: cloud, base: transport})
					pool := provider.NewClientPool(provider.Credentials{})
					if _, err = pool.GetOrCreate(pool.CacheKey(config.CloudTCCVM, "ap-guangzhou"), func() (any, error) { return cli, nil }); err != nil {
						t.Fatal(err)
					}
					pr, err := provider.NewProvider(config.TargetConfig{CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-mock"}, 1, pool)
					if err != nil {
						t.Fatal(err)
					}
					s := newTargetSyncer(t, []provider.Provider{pr}, []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}, map[string]string{"a.example.com": "1.1.1.1/32"})
					resolves := 0
					baseResolve := s.resolveHostFn
					s.resolveHostFn = func(host string) ([]dns.ResolvedIP, error) { resolves++; return baseResolve(host) }
					s.syncAll()
					sum := s.Status().LastRound
					retryable := kind == "rate-limit" || kind == "internal" || kind == "network-deadline"
					success := retryable && recoverNext
					wantAttempts := 1
					if retryable {
						wantAttempts = 3
						if success {
							wantAttempts = 2
						}
					}
					wantSeq := []string{}
					failedAttempts := wantAttempts
					if success {
						failedAttempts--
					}
					for i := 0; i < failedAttempts; i++ {
						wantSeq = append(wantSeq, "DescribeSecurityGroupPolicies:s0")
						if stage == "quota" {
							wantSeq = append(wantSeq, "DescribeSecurityGroupPolicies:quota")
						}
					}
					if success {
						wantSeq = append(wantSeq, "DescribeSecurityGroupPolicies:s0", "DescribeSecurityGroupPolicies:quota", "CreateSecurityGroupPolicies", "DescribeSecurityGroupPolicies:s1", "DeleteSecurityGroupPolicies", "DescribeSecurityGroupPolicies:s2")
					}
					cloud.mu.Lock()
					seq := append([]string(nil), cloud.seq...)
					attempts, creates, deletes, oldPresent, desiredPresent := cloud.attempts, cloud.creates, cloud.deletes, cloud.oldPresent, cloud.desiredPresent
					cloud.mu.Unlock()
					wantWrites := 0
					if success {
						wantWrites = 1
					}
					if attempts != wantAttempts || resolves != wantAttempts || !reflect.DeepEqual(seq, wantSeq) || creates != wantWrites || deletes != wantWrites {
						t.Fatalf("整目标重试/请求顺序错误：attempts=%d resolves=%d creates=%d deletes=%d seq=%v，期望 attempts=%d writes=%d seq=%v", attempts, resolves, creates, deletes, seq, wantAttempts, wantWrites, wantSeq)
					}
					if sum == nil || sum.Added != wantWrites || sum.Deleted != wantWrites {
						t.Fatalf("结果确认计数错误：%+v", sum)
					}
					if success {
						if sum.Outcome != RoundSuccess || sum.Total != 1 || sum.Failed != 0 || oldPresent || !desiredPresent {
							t.Fatalf("恢复后必须完成新增、覆盖验证和安全清理：%+v old=%v desired=%v", sum, oldPresent, desiredPresent)
						}
					} else if sum.Outcome != RoundFailed || sum.Failed != 1 || !oldPresent || desiredPresent {
						t.Fatalf("失败必须零增删且保留旧权限：%+v old=%v desired=%v", sum, oldPresent, desiredPresent)
					}
				})
			}
		}
	}
}

type i8r08TargetCloud struct {
	t                                    *testing.T
	mu                                   sync.Mutex
	stage, kind, phase                   string
	recoverNext                          bool
	attempts, failures, creates, deletes int
	oldPresent, desiredPresent           bool
	seq                                  []string
}

func (c *i8r08TargetCloud) shouldFail() bool {
	return c.phase == c.stage && (!c.recoverNext || c.failures == 0)
}

func (c *i8r08TargetCloud) recordDescribe() {
	c.seq = append(c.seq, "DescribeSecurityGroupPolicies:"+c.phase)
	if c.phase == "s0" {
		c.attempts++
	}
}

type i8r08TargetTransport struct {
	cloud *i8r08TargetCloud
	base  http.RoundTripper
}

func (rt i8r08TargetTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c := rt.cloud
	c.mu.Lock()
	if c.kind == "network-deadline" && r.Header.Get("X-Tc-Action") == "DescribeSecurityGroupPolicies" && c.shouldFail() {
		c.recordDescribe()
		c.failures++
		c.phase = "s0"
		c.mu.Unlock()
		return nil, context.DeadlineExceeded
	}
	c.mu.Unlock()
	return rt.base.RoundTrip(r)
}

func (c *i8r08TargetCloud) serve(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		SecurityGroupID string `json:"SecurityGroupId"`
		PolicySet       struct {
			Version string `json:"Version"`
			Ingress []struct {
				PolicyIndex *int64 `json:"PolicyIndex"`
			} `json:"Ingress"`
		} `json:"SecurityGroupPolicySet"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.t.Errorf("请求解码失败：%v", err)
		w.WriteHeader(400)
		return
	}
	if req.SecurityGroupID != "sg-mock" {
		c.t.Errorf("目标 ID=%q", req.SecurityGroupID)
	}
	body := map[string]any{"RequestId": "i8-r08-target"}
	switch action := r.Header.Get("X-Tc-Action"); action {
	case "DescribeSecurityGroupPolicies":
		c.recordDescribe()
		if c.shouldFail() {
			c.failures++
			c.phase = "s0"
			switch c.kind {
			case "invalid-json":
				if _, err := io.WriteString(w, `{"Response":`); err != nil {
					c.t.Error(err)
				}
				return
			case "http-json":
				w.WriteHeader(502)
			case "structure":
			default:
				code := map[string]string{"unauthorized": "UnauthorizedOperation", "resource-not-found": "ResourceNotFound", "rate-limit": "RequestLimitExceeded", "internal": "InternalError.ModuleError"}[c.kind]
				if code == "" {
					c.t.Errorf("未知失败类别 %s", c.kind)
				}
				body["Error"] = map[string]string{"Code": code, "Message": "i8-r08 cloud failure"}
			}
		} else {
			ingress := []map[string]any{}
			if c.oldPresent {
				ingress = append(ingress, map[string]any{"PolicyIndex": 0, "Protocol": "TCP", "Port": "9999", "CidrBlock": "192.0.2.5/32", "Action": "ACCEPT", "PolicyDescription": "[auto-dns] old"})
			}
			if c.desiredPresent {
				ingress = append(ingress, map[string]any{"PolicyIndex": 1, "Protocol": "TCP", "Port": "443", "CidrBlock": "1.1.1.1/32", "Action": "ACCEPT", "PolicyDescription": "[auto-dns]"})
			}
			version := fmt.Sprint(10 + c.attempts)
			switch c.phase {
			case "s0":
				c.phase = "quota"
			case "quota":
				version = "999"
				c.phase = "create" // 配额 Version 不得替代 S0。
			case "s1":
				version = fmt.Sprint(20 + c.attempts)
				c.phase = "delete"
			case "s2":
				version = fmt.Sprint(30 + c.attempts)
				c.phase = "done"
			default:
				c.t.Errorf("意外读取阶段：%s", c.phase)
			}
			body["SecurityGroupPolicySet"] = map[string]any{"Version": version, "Ingress": ingress}
		}
	case "CreateSecurityGroupPolicies":
		c.seq = append(c.seq, action)
		c.creates++
		if c.phase != "create" || req.PolicySet.Version != fmt.Sprint(10+c.attempts) {
			c.t.Errorf("Create 未使用当前 S0 Version：phase=%s version=%s", c.phase, req.PolicySet.Version)
		}
		c.desiredPresent = true
		c.phase = "s1"
	case "DeleteSecurityGroupPolicies":
		c.seq = append(c.seq, action)
		c.deletes++
		if c.phase != "delete" || req.PolicySet.Version != fmt.Sprint(20+c.attempts) || len(req.PolicySet.Ingress) != 1 || req.PolicySet.Ingress[0].PolicyIndex == nil || *req.PolicySet.Ingress[0].PolicyIndex != 0 {
			c.t.Errorf("Delete 未使用 S1 Version/旧规则定位：phase=%s req=%+v", c.phase, req)
		}
		c.oldPresent = false
		c.phase = "s2"
	default:
		c.t.Errorf("意外操作：%s", action)
	}
	if err := json.NewEncoder(w).Encode(map[string]any{"Response": body}); err != nil {
		c.t.Error(err)
	}
}
