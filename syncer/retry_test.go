package syncer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
)

// ─── Issue6 A12：常见超时与腾讯 SDK 网络错误必须进入重试 ───

// realHTTPTimeoutError 用真实 http.Client.Timeout 打到「accept 但不回包」的本地
// 服务，返回标准库真实产生的 *url.Error（形如
// `Post "http://…": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`）。
// 这正是 Issue6 A12 指出的、不含小写 `timeout`、会被旧实现判为不可重试的错误形状。
func realHTTPTimeoutError(t *testing.T) error {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动阻塞服务失败: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			if _, err := ln.Accept(); err != nil {
				return
			}
		}
	}()

	client := &http.Client{Timeout: 150 * time.Millisecond}
	resp, err := client.Get("http://" + ln.Addr().String() + "/")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("期望 http.Client.Timeout 触发，实际请求成功")
	}
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		t.Fatalf("期望 *url.Error，实际 %T: %v", err, err)
	}
	// 断言形状：确认这是真实的「awaiting headers」超时而不是其他错误
	if !strings.Contains(err.Error(), "Client.Timeout exceeded while awaiting headers") {
		t.Fatalf("期望 'awaiting headers' 超时形状，实际: %v", err)
	}
	return err
}

// tencentNetworkError 构造腾讯 SDK 真实形状的网络错误：netretry.go 重新包装的
// *TencentCloudSDKError{Code:"ClientError.NetworkError"}，该类型没有 Unwrap。
func tencentNetworkError(cause string) error {
	return sdkerrors.NewTencentCloudSDKError(
		"ClientError.NetworkError",
		"Fail to get response because "+cause,
		"",
	)
}

// TestIsRetryable_RealWorldShapes 表驱动覆盖真实错误形状（判别 A12）。
func TestIsRetryable_RealWorldShapes(t *testing.T) {
	realTimeout := realHTTPTimeoutError(t)

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			// 判别核心：修复前该错误不含小写 "timeout"（只有 "Timeout"），被判不可重试
			name: "真实 http.Client.Timeout 产生的 *url.Error",
			err:  realTimeout,
			want: true,
		},
		{
			name: "context.DeadlineExceeded",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "包装的 context.DeadlineExceeded",
			err:  fmt.Errorf("查询防火墙规则失败: %w", context.DeadlineExceeded),
			want: true,
		},
		{
			name: "net.Error i/o timeout",
			err:  &net.OpError{Op: "read", Err: timeoutErr{}},
			want: true,
		},
		{
			name: "connection refused（大小写不敏感）",
			err:  errors.New("dial tcp 127.0.0.1:1: connect: Connection Refused"),
			want: true,
		},
		{
			name: "InternalError",
			err:  errors.New("[TencentCloudSDKError] Code=InternalError, Message=内部错误"),
			want: true,
		},
		{
			name: "RequestLimitExceeded",
			err:  errors.New("[TencentCloudSDKError] Code=RequestLimitExceeded, Message=请求过于频繁"),
			want: true,
		},
		{
			name: "FirewallBusy",
			err:  errors.New("[TencentCloudSDKError] Code=FirewallBusy, Message=防火墙操作中"),
			want: true,
		},
		{
			// A12 新增：腾讯 SDK 网络类错误码（无 Unwrap，只能靠兜底）
			name: "腾讯 SDK ClientError.NetworkError（含超时正文）",
			err: tencentNetworkError(
				"Post \"https://lighthouse.tencentcloudapi.com\": context deadline exceeded (Client.Timeout exceeded while awaiting headers)"),
			want: true,
		},
		{
			// 有意不可重试：CVM 规则上限（Issue6 A12「必须保持」）
			name: "CVM 安全组规则上限（有意不可重试）",
			err:  errors.New("安全组规则总数将达 101（上限 100），停止新增"),
			want: false,
		},
		{
			name: "未知错误",
			err:  errors.New("some unexpected failure"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryable(tt.err); got != tt.want {
				t.Errorf("isRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// timeoutErr 实现 net.Error 且 Timeout() 为 true 的最小错误。
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// retryProbeProvider 记录调用序列，按调用序号返回预置错误。
type retryProbeProvider struct {
	cloudType   config.CloudType
	targetIndex int
	getRulesNum atomic.Int32
	createNum   atomic.Int32
	// getRulesErrs[i] 是第 i+1 次 GetRules 的返回值（nil 表示成功返回空规则集）
	getRulesErrs []error
}

func (p *retryProbeProvider) Name() string                { return "retry-probe" }
func (p *retryProbeProvider) CloudType() config.CloudType { return p.cloudType }
func (p *retryProbeProvider) TargetIndex() int            { return p.targetIndex }
func (p *retryProbeProvider) GetRules() ([]config.RuleInfo, error) {
	n := int(p.getRulesNum.Add(1)) - 1
	if n < len(p.getRulesErrs) && p.getRulesErrs[n] != nil {
		return nil, p.getRulesErrs[n]
	}
	return nil, nil
}
func (p *retryProbeProvider) CreateRules(rules []config.RuleAction) (provider.CreateResult, error) {
	p.createNum.Add(1)
	return provider.CreateResult{Written: len(rules)}, nil
}
func (p *retryProbeProvider) DeleteRules(rules []config.RuleInfo) error { return nil }
func (p *retryProbeProvider) ConvertPorts(port string) []string {
	return []string{port}
}

// TestRetrySync_RealTimeoutTriggersSecondFullAttempt 判别性端到端：
// 第 1 次 GetRules 返回**真实** http.Client.Timeout 错误时必须重试，
// 从而发生第 2 次完整 Describe → Diff（修复前只尝试一次，getRulesNum 恒为 1）。
func TestRetrySync_RealTimeoutTriggersSecondFullAttempt(t *testing.T) {
	realTimeout := realHTTPTimeoutError(t)
	p := &retryProbeProvider{
		cloudType:    config.CloudTCCVM,
		getRulesErrs: []error{realTimeout, nil},
	}
	s := &Syncer{}

	added, deleted, _, err := s.retrySync(p, config.DomainRule{
		Host: "example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
	}, nil, "auto-dns")
	if err != nil {
		t.Fatalf("重试后应成功，实际错误: %v", err)
	}
	if added != 0 || deleted != 0 {
		t.Errorf("added/deleted = %d/%d, want 0/0", added, deleted)
	}
	if got := p.getRulesNum.Load(); got != 2 {
		t.Fatalf("GetRules 调用次数 = %d, want 2（第 1 次真实超时 + 第 2 次完整 Describe→Diff）", got)
	}
}

// TestRetrySync_TencentNetworkErrorRetries 腾讯 SDK 网络错误（无 Unwrap）也必须重试。
func TestRetrySync_TencentNetworkErrorRetries(t *testing.T) {
	p := &retryProbeProvider{
		cloudType:    config.CloudTCLighthouse,
		getRulesErrs: []error{tencentNetworkError("dial tcp: connect: connection refused"), nil},
	}
	s := &Syncer{}

	if _, _, _, err := s.retrySync(p, config.DomainRule{
		Host: "example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
	}, nil, "auto-dns"); err != nil {
		t.Fatalf("重试后应成功，实际错误: %v", err)
	}
	if got := p.getRulesNum.Load(); got != 2 {
		t.Fatalf("GetRules 调用次数 = %d, want 2", got)
	}
}

// TestRetrySync_NonRetryableStopsImmediately 有意不可重试的错误必须立即返回、不重试。
func TestRetrySync_NonRetryableStopsImmediately(t *testing.T) {
	limitErr := fmt.Errorf("安全组规则总数将达 101（上限 100），停止新增")
	p := &retryProbeProvider{
		cloudType:    config.CloudTCCVM,
		getRulesErrs: []error{limitErr},
	}
	s := &Syncer{}

	_, _, _, err := s.retrySync(p, config.DomainRule{
		Host: "example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
	}, nil, "auto-dns")
	if !errors.Is(err, limitErr) {
		t.Fatalf("应返回原始上限错误，实际: %v", err)
	}
	if got := p.getRulesNum.Load(); got != 1 {
		t.Fatalf("GetRules 调用次数 = %d, want 1（有意不可重试不得重试）", got)
	}
}

// TestRetrySync_ExhaustsThreeAttempts 可重试错误持续出现时上限恒为 3 次。
func TestRetrySync_ExhaustsThreeAttempts(t *testing.T) {
	realTimeout := realHTTPTimeoutError(t)
	p := &retryProbeProvider{
		cloudType: config.CloudTCCVM,
		// 每次都可重试：最后一次仍失败，必须返回错误且恰好尝试 maxRetries 次
		getRulesErrs: []error{realTimeout, realTimeout, realTimeout, realTimeout, realTimeout},
	}
	s := &Syncer{}

	if _, _, _, err := s.retrySync(p, config.DomainRule{
		Host: "example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
	}, nil, "auto-dns"); err == nil {
		t.Fatal("持续可重试失败必须在用尽尝试后返回错误")
	}
	if got := p.getRulesNum.Load(); got != int32(maxRetries) {
		t.Fatalf("GetRules 调用次数 = %d, want %d", got, maxRetries)
	}
}

// TestRetrySync_AddedFollowsProviderWritten A11 判别：added 必须等于 Provider 的
// Written，而不是 len(diff.ToAdd)。本用例强制 firstErr=nil（可重试）并让 Provider
// 报告 Written=0/Skipped=1，构造出「diff 有 1 条待添加但实际写入 0 条」的差异。
func TestRetrySync_AddedFollowsProviderWritten(t *testing.T) {
	p := &writtenProbeProvider{
		cloudType:    config.CloudTCCVM,
		result:       provider.CreateResult{Written: 0, Skipped: 1},
		existingRule: nil,
	}
	s := &Syncer{}

	added, deleted, skipped, err := s.retrySync(p, config.DomainRule{
		Host: "example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
	}, []dns.ResolvedIP{{IP: net.ParseIP("1.2.3.4")}}, "auto-dns")
	if err != nil {
		t.Fatalf("retrySync 失败: %v", err)
	}
	if p.createCalls.Load() == 0 {
		t.Fatal("用例前提：必须真正调用过 CreateRules")
	}
	if added != 0 {
		t.Errorf("added = %d, want 0（必须跟随 Written，而不是 len(diff.ToAdd)）", added)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
}

// writtenProbeProvider 记录 CreateRules 收到的规则条数并返回固定结果。
type writtenProbeProvider struct {
	cloudType    config.CloudType
	result       provider.CreateResult
	existingRule []config.RuleInfo
	createCalls  atomic.Int32
	createdCount atomic.Int32
}

func (p *writtenProbeProvider) Name() string                         { return "written-probe" }
func (p *writtenProbeProvider) CloudType() config.CloudType          { return p.cloudType }
func (p *writtenProbeProvider) TargetIndex() int                     { return 0 }
func (p *writtenProbeProvider) ConvertPorts(port string) []string    { return []string{port} }
func (p *writtenProbeProvider) DeleteRules([]config.RuleInfo) error  { return nil }
func (p *writtenProbeProvider) GetRules() ([]config.RuleInfo, error) { return p.existingRule, nil }
func (p *writtenProbeProvider) CreateRules(rules []config.RuleAction) (provider.CreateResult, error) {
	p.createCalls.Add(1)
	p.createdCount.Add(int32(len(rules)))
	return p.result, nil
}

// TestRetrySyncDetailedUsesSuccessfulAttemptDetails R6-01 判别：失败 attempt 的跳过
// 详情不得与最终成功 attempt 重复累加。
func TestRetrySyncDetailedUsesSuccessfulAttemptDetails(t *testing.T) {
	p := &detailRetryProvider{}
	s := &Syncer{}

	_, _, skipped, details, err := s.retrySyncDetailed(p, config.DomainRule{
		Host: "example.com", Protocol: "TCP", Ports: "443", Action: "DROP",
	}, []dns.ResolvedIP{{IP: net.ParseIP("1.2.3.4")}}, "auto-dns")
	if err != nil {
		t.Fatalf("retrySyncDetailed 失败: %v", err)
	}
	if skipped != 1 || len(details) != 1 {
		t.Fatalf("skipped/details = %d/%d, want 1/1", skipped, len(details))
	}
	if got := p.deleteCalls.Load(); got != 2 {
		t.Fatalf("DeleteRules 调用 = %d, want 2（首轮失败、次轮成功）", got)
	}
}

type detailRetryProvider struct{ deleteCalls atomic.Int32 }

func (p *detailRetryProvider) Name() string                      { return "detail-retry" }
func (p *detailRetryProvider) CloudType() config.CloudType       { return config.CloudAliSWAS }
func (p *detailRetryProvider) TargetIndex() int                  { return 0 }
func (p *detailRetryProvider) ConvertPorts(port string) []string { return []string{port} }
func (p *detailRetryProvider) CreateRules(rules []config.RuleAction) (provider.CreateResult, error) {
	return provider.CreateResult{Written: len(rules)}, nil
}
func (p *detailRetryProvider) GetRules() ([]config.RuleInfo, error) {
	return []config.RuleInfo{{
		Protocol: "TCP", Port: "80", CidrBlock: "9.9.9.9/32", Action: "ACCEPT",
		Description: "[auto-dns]", RuleID: "old",
	}}, nil
}
func (p *detailRetryProvider) DeleteRules([]config.RuleInfo) error {
	if p.deleteCalls.Add(1) == 1 {
		return errors.New("RequestLimitExceeded")
	}
	return nil
}

// progressErrorProvider 用于验证错误返回路径仍累计云端已确认的独立请求进度。
type progressErrorProvider struct {
	getRules       [][]config.RuleInfo
	getErrs        []error
	createResult   provider.CreateResult
	createErr      error
	deleteProgress []int
	deleteErrs     []error
	getCalls       atomic.Int32
	createCalls    atomic.Int32
	deleteCalls    atomic.Int32
}

func (p *progressErrorProvider) Name() string                      { return "progress-error" }
func (p *progressErrorProvider) CloudType() config.CloudType       { return config.CloudTCCVM }
func (p *progressErrorProvider) TargetIndex() int                  { return 0 }
func (p *progressErrorProvider) ConvertPorts(port string) []string { return []string{port} }
func (p *progressErrorProvider) GetRules() ([]config.RuleInfo, error) {
	i := int(p.getCalls.Add(1)) - 1
	if i < len(p.getErrs) && p.getErrs[i] != nil {
		return nil, p.getErrs[i]
	}
	if i < len(p.getRules) {
		return p.getRules[i], nil
	}
	return nil, nil
}
func (p *progressErrorProvider) CreateRules([]config.RuleAction) (provider.CreateResult, error) {
	p.createCalls.Add(1)
	return p.createResult, p.createErr
}
func (p *progressErrorProvider) DeleteRules([]config.RuleInfo) error {
	i := int(p.deleteCalls.Add(1)) - 1
	if i < len(p.deleteErrs) && p.deleteErrs[i] != nil {
		return &provider.PartialDeleteError{Deleted: p.deleteProgress[i], Err: p.deleteErrs[i]}
	}
	return nil
}

func ownedOldRule(id string) config.RuleInfo {
	return config.RuleInfo{
		Protocol: "TCP", Port: "80", CidrBlock: "9.9.9.9/32", Action: "ACCEPT",
		Description: "[auto-dns]", RuleID: id, PolicyIndex: id,
	}
}

// TestRetrySync_NonRetryableErrorKeepsConfirmedDeleteProgress 不可重试错误也必须保留
// 当前请求报错前已经确认成功的独立删除请求数。
func TestRetrySync_NonRetryableErrorKeepsConfirmedDeleteProgress(t *testing.T) {
	p := &progressErrorProvider{
		getRules:       [][]config.RuleInfo{{ownedOldRule("2"), ownedOldRule("1")}},
		deleteProgress: []int{1},
		deleteErrs:     []error{errors.New("permission denied")},
	}
	s := &Syncer{}

	added, deleted, _, err := s.retrySync(p, config.DomainRule{
		Host: "example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
	}, nil, "auto-dns")
	if err == nil {
		t.Fatal("不可重试删除错误必须返回")
	}
	if added != 0 || deleted != 1 {
		t.Errorf("added/deleted = %d/%d, want 0/1", added, deleted)
	}
}

// TestRetrySync_ExhaustedRetriesKeepConfirmedProgress 每轮重新 Describe/Diff 后只累计
// 仍出现的独立请求；耗尽重试后此前确认进度不得丢失或重复。
func TestRetrySync_ExhaustedRetriesKeepConfirmedProgress(t *testing.T) {
	retryErr := errors.New("RequestLimitExceeded")
	p := &progressErrorProvider{
		getRules: [][]config.RuleInfo{
			{ownedOldRule("2"), ownedOldRule("1")},
			{ownedOldRule("1")},
		},
		getErrs:        []error{nil, nil, retryErr},
		deleteProgress: []int{1, 1},
		deleteErrs:     []error{retryErr, retryErr},
	}
	s := &Syncer{}

	_, deleted, _, err := s.retrySync(p, config.DomainRule{
		Host: "example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
	}, nil, "auto-dns")
	if err == nil {
		t.Fatal("耗尽重试必须返回错误")
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2（两轮各确认一条，重新 Diff 后不重复）", deleted)
	}
	if got := p.getCalls.Load(); got != int32(maxRetries) {
		t.Errorf("GetRules 调用 = %d, want %d", got, maxRetries)
	}
}

// TestRetrySync_CreateResultWithErrorKeepsWritten CreateRules 当前子批失败时，返回值中的
// Written 仅代表此前成功子批，retry 必须在判错前累计。
func TestRetrySync_CreateResultWithErrorKeepsWritten(t *testing.T) {
	p := &progressErrorProvider{
		getRules:     [][]config.RuleInfo{{}},
		createResult: provider.CreateResult{Written: 1},
		createErr:    errors.New("permission denied"),
	}
	s := &Syncer{}

	added, _, _, err := s.retrySync(p, config.DomainRule{
		Host: "example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
	}, []dns.ResolvedIP{{IP: net.ParseIP("1.2.3.4")}}, "auto-dns")
	if err == nil {
		t.Fatal("创建失败必须返回错误")
	}
	if p.createCalls.Load() != 1 || added != 1 {
		t.Errorf("CreateRules 调用/added = %d/%d, want 1/1", p.createCalls.Load(), added)
	}
}
