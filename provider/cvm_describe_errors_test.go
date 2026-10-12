package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"

	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
)

// 真实腾讯 SDK → CVM：普通调用错误保留原链，成功响应缺少安全依据才标记不完整。
func TestI8R08CVMDescribeClassification(t *testing.T) {
	cases := []struct {
		name, body, code, requestID string
		status                      int
		incomplete                  bool
		onlyEntry                   string
	}{
		{name: "unauthorized", code: "UnauthorizedOperation"},
		{name: "resource-not-found", code: "ResourceNotFound"},
		{name: "rate-limit", code: "RequestLimitExceeded"},
		{name: "internal", code: "InternalError.ModuleError"},
		{name: "invalid-parameter", code: "InvalidParameterValue.Malformed"},
		{name: "invalid-json", body: `{"Response":`, code: "ClientError.ParseJsonError", status: 200},
		// SDK 先解析错误体；非法 JSON 的 502 与合法 JSON 的 502 错误码不同。
		{name: "http-invalid-json", body: `bad gateway`, code: "ClientError.ParseJsonError", status: 502},
		{name: "http-json", body: `{"Response":{}}`, code: "ClientError.HttpStatusCodeError", status: 502},
		{name: "missing-response", body: `{}`, status: 200, incomplete: true},
		{name: "null-response", body: `{"Response":null}`, status: 200, incomplete: true},
		{name: "missing-policy-set", body: `{"Response":{}}`, status: 200, incomplete: true},
		{name: "null-policy-set", body: `{"Response":{"SecurityGroupPolicySet":null}}`, status: 200, incomplete: true},
		{name: "missing-count", body: `{"Response":{"SecurityGroupPolicySet":{"Version":"7"}}}`, status: 200, incomplete: true, onlyEntry: "quota"},
		{name: "partial-statistics-no-array", body: `{"Response":{"SecurityGroupPolicySet":{"PolicyStatistics":{"IngressIPv4TotalCount":0}}}}`, status: 200, incomplete: true, onlyEntry: "quota"},
		{name: "missing-version", body: `{"Response":{"SecurityGroupPolicySet":{"Ingress":[]}}}`, status: 200, incomplete: true, onlyEntry: "snapshot"},
	}
	for _, entry := range []string{"snapshot", "quota"} {
		for _, tc := range cases {
			if tc.onlyEntry != "" && tc.onlyEntry != entry {
				continue
			}
			t.Run(entry+"/"+tc.name, func(t *testing.T) {
				status, body, requestID := tc.status, tc.body, tc.requestID
				if status == 0 {
					status = 200
					requestID = "i8-r08-request"
					body = fmt.Sprintf(`{"Response":{"Error":{"Code":%q,"Message":"i8-r08 cloud failure"},"RequestId":%q}}`, tc.code, requestID)
				}
				m, host := newMockCloudAPI(t)
				m.reply = func(_ int, _ recordedRequest) (int, string) { return status, body }
				p := mockCVM(t, host)
				err := i8r08Describe(t, p, entry)
				if err == nil || errors.Is(err, ErrSnapshotIncomplete) != tc.incomplete {
					t.Fatalf("ErrSnapshotIncomplete=%v，期望%v；err=%v", errors.Is(err, ErrSnapshotIncomplete), tc.incomplete, err)
				}
				var sdkErr *sdkerrors.TencentCloudSDKError
				if tc.code != "" {
					if !errors.As(err, &sdkErr) || sdkErr.GetCode() != tc.code || sdkErr.GetRequestId() != requestID {
						t.Fatalf("SDK 错误码/请求 ID 丢失：%v", err)
					}
				} else if errors.As(err, &sdkErr) {
					t.Fatalf("结构错误误分类为 SDK 错误：%v", err)
				}
				reqs := m.recorded()
				if len(reqs) != 1 || reqs[0].action() != "DescribeSecurityGroupPolicies" {
					t.Fatalf("失败后不应发出写入请求：%+v", reqs)
				}
				if got, _ := strField(bodyJSON(t, reqs[0]), "SecurityGroupId"); got != "sg-mock" {
					t.Fatalf("Describe 目标=%q", got)
				}
			})
		}
	}
}

func i8r08Describe(t *testing.T, p *TCCVM, entry string) error {
	t.Helper()
	if entry == "quota" {
		result, err := p.CreateRules(mockSnapshot(), makeRules(1))
		if result != (CreateResult{}) {
			t.Fatalf("失败必须零 Written/Skipped：%+v", result)
		}
		return err
	}
	snap, err := p.GetSnapshot()
	if snap.Rules != nil || snap.Revision != "" {
		t.Fatalf("失败必须返回空快照：%+v", snap)
	}
	return err
}

type i8r08Transport func(*http.Request) (*http.Response, error)

func (f i8r08Transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// 通过 SDK 的真实网络包装路径注入 cause；不冒称等待生产默认 60 秒超时。
func TestI8R08CVMNetworkCause(t *testing.T) {
	deadline := &net.OpError{Op: "read", Net: "tcp", Err: context.DeadlineExceeded}
	refused := errors.New("i8-r08 connection refused")
	for _, entry := range []string{"snapshot", "quota"} {
		for _, tc := range []struct {
			name     string
			cause    error
			deadline bool
		}{{"deadline", deadline, true}, {"refused", refused, false}} {
			t.Run(entry+"/"+tc.name, func(t *testing.T) {
				calls := 0
				p := mockCVM(t, "unused.invalid")
				p.client.WithHttpTransport(i8r08Transport(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.Header.Get("X-Tc-Action") != "DescribeSecurityGroupPolicies" {
						t.Errorf("不应写入：%s", r.Header.Get("X-Tc-Action"))
					}
					return nil, tc.cause
				}))
				err := i8r08Describe(t, p, entry)
				var sdkErr *sdkerrors.TencentCloudSDKError
				if err == nil || errors.Is(err, ErrSnapshotIncomplete) || !errors.Is(err, tc.cause) || !errors.As(err, &sdkErr) || sdkErr.GetCode() != "ClientError.NetworkError" || calls != 1 {
					t.Fatalf("网络错误链/分类/次数错误：calls=%d err=%v", calls, err)
				}
				if errors.Is(err, context.DeadlineExceeded) != tc.deadline {
					t.Fatalf("deadline 身份错误：%v", err)
				}
				if tc.deadline {
					var opErr *net.OpError
					if !errors.As(err, &opErr) || opErr != deadline || !opErr.Timeout() {
						t.Fatalf("net.OpError 身份丢失：%v", err)
					}
				}
			})
		}
	}
}
