package provider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tcerr "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	teo "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/teo/v20220901"
)

// 本文件是 Build8 Step 5 的 TEO 只读适配层。
//
// 固定边界：
//   - 只做 DescribeOriginACL；确认（ConfirmOriginACLUpdate）留给 Step 10；
//   - 构造函数只做本地初始化，零网络请求；
//   - 只返回已校验、已 Mask、已去重的快照；任何字段缺失、畸形地址族、
//     整体空集合都返回错误，绝不退化为可授权的空集合；
//   - 同一站点的查询结果是一个纯值，调用方应复用它服务于全部绑定目标。

const (
	// teoDefaultEndpoint 腾讯云 EdgeOne 中国站 API Endpoint。
	teoDefaultEndpoint = "teo.tencentcloudapi.com"
	// teoDefaultScheme 生产固定 HTTPS；测试可注入 HTTP 以使用 httptest。
	teoDefaultScheme = "HTTPS"
	// teoDefaultRequestBudget 单次 Describe 的本地 context 预算。
	// 与既有腾讯云 SDK 60 秒 HTTP 超时分离，作为查询层的更短上限。
	teoDefaultRequestBudget = 30 * time.Second
	// teoDefaultMinInterval 查询/确认共享的站点级最小请求间隔（20 次/秒）。
	teoDefaultMinInterval = 50 * time.Millisecond
)

// TEOErrorKind 是 TEO 只读查询的安全错误分类。
//
// 分类用于调度与状态展示，不改变云防火墙 Provider 的错误/重试合同。
type TEOErrorKind string

const (
	// TEOErrorPermission CAM 权限不足或凭据不可用
	TEOErrorPermission TEOErrorKind = "permission"
	// TEOErrorThrottled 腾讯云限流
	TEOErrorThrottled TEOErrorKind = "throttled"
	// TEOErrorTimeout 请求预算、调用方 context 或网络超时
	TEOErrorTimeout TEOErrorKind = "timeout"
	// TEOErrorCanceled 调用方主动取消
	TEOErrorCanceled TEOErrorKind = "canceled"
	// TEOErrorNetwork 其他网络/协议错误
	TEOErrorNetwork TEOErrorKind = "network"
	// TEOErrorIncomplete 成功响应缺少必要字段，无法证明完整
	TEOErrorIncomplete TEOErrorKind = "incomplete"
	// TEOErrorMalformed CIDR、地址族或计划时间畸形
	TEOErrorMalformed TEOErrorKind = "malformed"
	// TEOErrorEmpty 整体没有可用网段（两个地址族都有效为空）
	TEOErrorEmpty TEOErrorKind = "empty"
	// TEOErrorDisabled 源站防护未启用或状态不可用
	TEOErrorDisabled TEOErrorKind = "disabled"
)

// TEOError 是 TEO 查询的安全错误：只保留分类、云错误码、RequestId 与固定文案。
//
// 不复制 SDK 原始 Message、请求体、签名或凭据；底层错误仅通过 Unwrap 保留，
// 便于使用 errors.Is/As 判断超时。Error() 输出可直接进日志与状态。
type TEOError struct {
	Kind      TEOErrorKind
	ZoneID    string
	Code      string
	RequestID string
	Message   string
	cause     error
}

func (e *TEOError) Error() string {
	var b strings.Builder
	b.WriteString("TEO 查询失败")
	if e.ZoneID != "" {
		b.WriteString(" zone=")
		b.WriteString(e.ZoneID)
	}
	b.WriteString(" kind=")
	b.WriteString(string(e.Kind))
	if e.Code != "" {
		b.WriteString(" code=")
		b.WriteString(e.Code)
	}
	if e.RequestID != "" {
		b.WriteString(" request_id=")
		b.WriteString(e.RequestID)
	}
	if e.Message != "" {
		b.WriteString("；")
		b.WriteString(e.Message)
	}
	return b.String()
}

// Unwrap 保留超时/取消等底层错误；不向调用方暴露原始 SDK Message。
func (e *TEOError) Unwrap() error { return e.cause }

// TEOErrorKindOf 提取安全错误分类；非 TEO 错误返回空串。
func TEOErrorKindOf(err error) TEOErrorKind {
	var teoErr *TEOError
	if errors.As(err, &teoErr) {
		return teoErr.Kind
	}
	return ""
}

// TEOErrorRequestID 提取安全 RequestId；非 TEO 错误返回空串。
func TEOErrorRequestID(err error) string {
	var teoErr *TEOError
	if errors.As(err, &teoErr) {
		return teoErr.RequestID
	}
	return ""
}

// OriginACLVersion 是一个已规范化的回源网段版本。
//
// Prefix 已 Mask，地址族与来源字段一致；切片按地址族与字符串稳定排序。
type OriginACLVersion struct {
	Version  string         `json:"version"`
	Prefixes []netip.Prefix `json:"prefixes"`
}

// OriginACLSnapshot 是一次 DescribeOriginACL 的只读结果。
//
// 该值可被同一站点的多个绑定目标复用。失败查询不返回部分快照。
type OriginACLSnapshot struct {
	ZoneID          string
	Status          string
	Current         OriginACLVersion
	Next            *OriginACLVersion
	PlannedActiveAt *time.Time
	ObservedAt      time.Time
	RequestID       string
}

// AllPrefixes 返回当前版与待更新版的并集（去重、稳定排序）。
//
// 规划依据使用完整列表；AddedAddresses/RemovedAddresses 只用于展示，不进入本结果。
func (s OriginACLSnapshot) AllPrefixes() []netip.Prefix {
	all := make([]netip.Prefix, 0, len(s.Current.Prefixes))
	all = append(all, s.Current.Prefixes...)
	if s.Next != nil {
		all = append(all, s.Next.Prefixes...)
	}
	return normalizePrefixes(all)
}

// PrefixesForFamily 按站点配置的地址族返回并集：
// ipv4 / ipv6 / dual；未知地址族返回空切片。
func (s OriginACLSnapshot) PrefixesForFamily(family string) []netip.Prefix {
	all := s.AllPrefixes()
	out := make([]netip.Prefix, 0, len(all))
	for _, prefix := range all {
		switch family {
		case config.EdgeOneAddressFamilyIPv4:
			if prefix.Addr().Is4() {
				out = append(out, prefix)
			}
		case config.EdgeOneAddressFamilyIPv6:
			if prefix.Addr().Is6() && !prefix.Addr().Is4() {
				out = append(out, prefix)
			}
		case config.EdgeOneAddressFamilyDual:
			out = append(out, prefix)
		}
	}
	return normalizePrefixes(out)
}

// TEOClient 是 EdgeOne 只读查询客户端。
//
// 构造时只创建本地 SDK client；查询与确认前共用 rate limiter。当前阶段只暴露
// DescribeOriginACL；确认入口由 Step 10 接入。
type TEOClient struct {
	client   *teo.Client
	endpoint string
	budget   time.Duration

	mu          sync.Mutex
	minInterval time.Duration
	next        time.Time
}

// teoClientOptions 仅测试和装配层使用；生产默认值见上方常量。
type teoClientOptions struct {
	Endpoint      string
	Scheme        string
	RequestBudget time.Duration
	MinInterval   time.Duration
}

func defaultTEOClientOptions() teoClientOptions {
	return teoClientOptions{
		Endpoint:      teoDefaultEndpoint,
		Scheme:        teoDefaultScheme,
		RequestBudget: teoDefaultRequestBudget,
		MinInterval:   teoDefaultMinInterval,
	}
}

// NewTEOClient 从既有 ClientPool 复用腾讯云凭据构造 TEO client。
//
// 构造只做本地初始化，不发任何网络请求；凭据仍由 pool 不可变持有。
func NewTEOClient(pool *ClientPool) (*TEOClient, error) {
	return newTEOClient(pool, defaultTEOClientOptions())
}

// newTEOClient 是带 endpoint/profile 注入口的内部构造，供 httptest 使用。
func newTEOClient(pool *ClientPool, opts teoClientOptions) (*TEOClient, error) {
	if pool == nil {
		return nil, fmt.Errorf("创建 TEO Client 失败: 缺少客户端池")
	}
	if opts.Endpoint == "" {
		return nil, fmt.Errorf("创建 TEO Client 失败: endpoint 为空")
	}
	if opts.Scheme == "" {
		opts.Scheme = teoDefaultScheme
	}
	creds := pool.Credentials()
	key := "teo|" + opts.Endpoint + "|" + opts.Scheme + "|" + creds.AccountKey()

	cached, err := pool.GetOrCreate(key, func() (any, error) {
		credential := common.NewCredential(creds.TencentSecretID, creds.TencentSecretKey)
		cpf := profile.NewClientProfile()
		cpf.HttpProfile.Endpoint = opts.Endpoint
		cpf.HttpProfile.Scheme = opts.Scheme
		// SDK 的 ReqTimeout 以秒为单位；小于 1 秒的预算由 context 控制。
		seconds := int((opts.RequestBudget + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		cpf.HttpProfile.ReqTimeout = seconds
		// 不使用 SDK 内部网络/限流重试：本项目所有重试均由目标链显式控制。
		cpf.NetworkFailureMaxRetries = 0
		cpf.RateLimitExceededMaxRetries = 0
		sdkClient, err := teo.NewClient(credential, "", cpf)
		if err != nil {
			return nil, err
		}
		// 缓存包装对象而不是裸 SDK client，使同一 pool+endpoint 的查询与
		// 未来确认共享同一个 rate limiter（Step 10 复用）。
		return &TEOClient{
			client:      sdkClient,
			endpoint:    opts.Endpoint,
			budget:      opts.RequestBudget,
			minInterval: opts.MinInterval,
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("创建 TEO Client 失败: %w", err)
	}
	return cached.(*TEOClient), nil
}

// Endpoint 返回只读 endpoint，用于安全诊断（不含凭据）。
func (c *TEOClient) Endpoint() string { return c.endpoint }

// wait 为查询/确认预留一个站点级请求槽位，并在需要时等待。
//
// 预留发生在等待之前，因此并发调用会按顺序排队而不是同时唤醒；等待不持锁。
func (c *TEOClient) wait(ctx context.Context) error {
	if c.minInterval <= 0 {
		return nil
	}
	c.mu.Lock()
	now := time.Now()
	slot := c.next
	if slot.Before(now) {
		slot = now
	}
	c.next = slot.Add(c.minInterval)
	c.mu.Unlock()

	wait := time.Until(slot)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// DescribeOriginACL 查询一个站点的回源网段，返回已校验快照。
//
// 请求使用调用方 context；本方法再叠加本地预算和限速。任何失败都不返回部分
// 快照，调用方必须把错误视为“站点输入不完整”，不得解释为空集合或清理授权。
func (c *TEOClient) DescribeOriginACL(ctx context.Context, zoneID string) (OriginACLSnapshot, error) {
	zoneID = strings.TrimSpace(zoneID)
	if zoneID == "" {
		return OriginACLSnapshot{}, &TEOError{
			Kind:    TEOErrorMalformed,
			Message: "ZoneId 为空",
		}
	}

	queryCtx := ctx
	if c.budget > 0 {
		var cancel context.CancelFunc
		queryCtx, cancel = context.WithTimeout(ctx, c.budget)
		defer cancel()
	}

	if err := c.wait(queryCtx); err != nil {
		return OriginACLSnapshot{}, classifyTEOContextError(zoneID, err)
	}

	req := teo.NewDescribeOriginACLRequest()
	req.ZoneId = common.StringPtr(zoneID)
	resp, err := c.client.DescribeOriginACLWithContext(queryCtx, req)
	if err != nil {
		classified := classifyTEOError(zoneID, err)
		// TEOError.Error() 只含分类、云错误码、RequestId 与固定文案。
		slog.Warn("TEO 查询失败", "error", classified)
		return OriginACLSnapshot{}, classified
	}
	snapshot, err := convertOriginACLResponse(zoneID, resp, time.Now().UTC())
	if err != nil {
		slog.Warn("TEO 响应不可用", "error", err)
		return OriginACLSnapshot{}, err
	}
	currentVersion, nextVersion := snapshot.Current.Version, ""
	if snapshot.Next != nil {
		nextVersion = snapshot.Next.Version
	}
	slog.Debug("TEO 查询完成", "zone", snapshot.ZoneID, "status", snapshot.Status,
		"current", currentVersion, "next", nextVersion, "request_id", snapshot.RequestID)
	return snapshot, nil
}

// classifyTEOContextError 把等待/预算错误转换为安全分类。
func classifyTEOContextError(zoneID string, err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return &TEOError{Kind: TEOErrorCanceled, ZoneID: zoneID, Message: "查询被取消", cause: err}
	case errors.Is(err, context.DeadlineExceeded):
		return &TEOError{Kind: TEOErrorTimeout, ZoneID: zoneID, Message: "查询预算耗尽", cause: err}
	default:
		return &TEOError{Kind: TEOErrorNetwork, ZoneID: zoneID, Message: "请求未发出", cause: err}
	}
}

// classifyTEOError 把 SDK/网络错误转换为安全分类，并保留 RequestId。
func classifyTEOError(zoneID string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &TEOError{Kind: TEOErrorTimeout, ZoneID: zoneID, Message: "查询预算耗尽", cause: err}
	}
	if errors.Is(err, context.Canceled) {
		return &TEOError{Kind: TEOErrorCanceled, ZoneID: zoneID, Message: "查询被取消", cause: err}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &TEOError{Kind: TEOErrorTimeout, ZoneID: zoneID, Message: "网络超时", cause: err}
	}

	var sdkErr *tcerr.TencentCloudSDKError
	if errors.As(err, &sdkErr) {
		code := strings.TrimSpace(sdkErr.GetCode())
		requestID := strings.TrimSpace(sdkErr.GetRequestId())
		kind := TEOErrorNetwork
		message := "腾讯云返回错误"
		switch {
		case isTEOPermissionCode(code):
			kind = TEOErrorPermission
			message = "CAM 权限不足或凭据不可用"
		case isTEOThrottleCode(code):
			kind = TEOErrorThrottled
			message = "腾讯云接口限流"
		case strings.Contains(code, "Timeout") || strings.Contains(code, "RequestTimeout"):
			kind = TEOErrorTimeout
			message = "腾讯云请求超时"
		}
		return &TEOError{
			Kind: kind, ZoneID: zoneID, Code: code, RequestID: requestID,
			Message: message, cause: err,
		}
	}
	return &TEOError{Kind: TEOErrorNetwork, ZoneID: zoneID, Message: "网络或协议错误", cause: err}
}

func isTEOPermissionCode(code string) bool {
	switch {
	case strings.HasPrefix(code, "UnauthorizedOperation"),
		strings.HasPrefix(code, "AuthFailure"),
		code == "InvalidCredential",
		code == "InvalidCredential.SecretIdNotFound":
		return true
	default:
		return false
	}
}

func isTEOThrottleCode(code string) bool {
	switch {
	case strings.HasPrefix(code, "RequestLimitExceeded"),
		strings.HasPrefix(code, "LimitExceeded"):
		return true
	default:
		return false
	}
}

// convertOriginACLResponse 把 SDK 响应转换为内部快照。
//
// 校验顺序：响应/OriginACLInfo/状态 → 当前版 → 待更新版。任一必要字段缺失
// 返回 incomplete；地址族/地址畸形返回 malformed；整体无网段返回 empty。
func convertOriginACLResponse(zoneID string, resp *teo.DescribeOriginACLResponse, now time.Time) (OriginACLSnapshot, error) {
	if resp == nil || resp.Response == nil {
		return OriginACLSnapshot{}, &TEOError{
			Kind: TEOErrorIncomplete, ZoneID: zoneID, Message: "响应为空",
		}
	}
	requestID := strings.TrimSpace(strVal(resp.Response.RequestId))
	info := resp.Response.OriginACLInfo
	if info == nil {
		return OriginACLSnapshot{}, &TEOError{
			Kind: TEOErrorIncomplete, ZoneID: zoneID, RequestID: requestID,
			Message: "缺少 OriginACLInfo",
		}
	}
	status := strings.ToLower(strings.TrimSpace(strVal(info.Status)))
	if status == "" {
		return OriginACLSnapshot{}, &TEOError{
			Kind: TEOErrorIncomplete, ZoneID: zoneID, RequestID: requestID,
			Message: "缺少源站防护状态",
		}
	}
	if status != "online" && status != "updating" {
		return OriginACLSnapshot{}, &TEOError{
			Kind: TEOErrorDisabled, ZoneID: zoneID, RequestID: requestID,
			Message: "源站防护未启用（" + status + "）",
		}
	}

	current, err := convertOriginACLVersion(zoneID, requestID, "当前版", info.CurrentOriginACL)
	if err != nil {
		return OriginACLSnapshot{}, err
	}
	snapshot := OriginACLSnapshot{
		ZoneID:     zoneID,
		Status:     status,
		Current:    current,
		ObservedAt: now,
		RequestID:  requestID,
	}
	if info.NextOriginACL != nil {
		next, plannedAt, err := convertNextOriginACL(zoneID, requestID, info.NextOriginACL)
		if err != nil {
			return OriginACLSnapshot{}, err
		}
		snapshot.Next = next
		snapshot.PlannedActiveAt = plannedAt
	}
	return snapshot, nil
}

// convertOriginACLVersion 校验当前版。
func convertOriginACLVersion(zoneID, requestID, label string, version *teo.CurrentOriginACL) (OriginACLVersion, error) {
	if version == nil {
		return OriginACLVersion{}, &TEOError{
			Kind: TEOErrorIncomplete, ZoneID: zoneID, RequestID: requestID,
			Message: label + "缺失",
		}
	}
	versionText := strings.TrimSpace(strVal(version.Version))
	if versionText == "" {
		return OriginACLVersion{}, &TEOError{
			Kind: TEOErrorIncomplete, ZoneID: zoneID, RequestID: requestID,
			Message: label + "缺少版本号",
		}
	}
	prefixes, err := convertTEOAddresses(zoneID, requestID, label, version.EntireAddresses)
	if err != nil {
		return OriginACLVersion{}, err
	}
	return OriginACLVersion{Version: versionText, Prefixes: prefixes}, nil
}

// convertNextOriginACL 校验待更新版与计划生效时间。
func convertNextOriginACL(zoneID, requestID string, version *teo.NextOriginACL) (*OriginACLVersion, *time.Time, error) {
	versionText := strings.TrimSpace(strVal(version.Version))
	if versionText == "" {
		return nil, nil, &TEOError{
			Kind: TEOErrorIncomplete, ZoneID: zoneID, RequestID: requestID,
			Message: "待更新版缺少版本号",
		}
	}
	plannedAt, ok := parseTEOPlanTime(strVal(version.PlannedActiveTime))
	if !ok {
		return nil, nil, &TEOError{
			Kind: TEOErrorIncomplete, ZoneID: zoneID, RequestID: requestID,
			Message: "待更新版缺少有效的计划生效时间",
		}
	}
	prefixes, err := convertTEOAddresses(zoneID, requestID, "待更新版", version.EntireAddresses)
	if err != nil {
		return nil, nil, err
	}
	planned := plannedAt.UTC()
	return &OriginACLVersion{Version: versionText, Prefixes: prefixes}, &planned, nil
}

// convertTEOAddresses 校验并规范化一个版本的两类 CIDR。
//
//   - 地址族字段整体缺失 → incomplete；
//   - 两个字段都显式为空（或解析后无任何前缀）→ empty（不是可用空集）；
//   - 单个地址族为空、另一个有效 → 合法；
//   - 任一 CIDR 非法/地址族错配 → malformed，不返回部分结果。
func convertTEOAddresses(zoneID, requestID, label string, addrs *teo.Addresses) ([]netip.Prefix, error) {
	if addrs == nil {
		return nil, &TEOError{
			Kind: TEOErrorIncomplete, ZoneID: zoneID, RequestID: requestID,
			Message: label + "缺少 EntireAddresses",
		}
	}
	if addrs.IPv4 == nil && addrs.IPv6 == nil {
		return nil, &TEOError{
			Kind: TEOErrorIncomplete, ZoneID: zoneID, RequestID: requestID,
			Message: label + "缺少地址族列表",
		}
	}

	v4, err := parseTEOAddressList(zoneID, requestID, label+" IPv4", addrs.IPv4, false)
	if err != nil {
		return nil, err
	}
	v6, err := parseTEOAddressList(zoneID, requestID, label+" IPv6", addrs.IPv6, true)
	if err != nil {
		return nil, err
	}
	if len(v4) == 0 && len(v6) == 0 {
		return nil, &TEOError{
			Kind: TEOErrorEmpty, ZoneID: zoneID, RequestID: requestID,
			Message: label + "没有可用网段",
		}
	}
	return normalizePrefixes(append(v4, v6...)), nil
}

// parseTEOAddressList 解析单个地址族的 CIDR/单 IP，要求地址族与字段一致。
func parseTEOAddressList(zoneID, requestID, label string, values []*string, ipv6 bool) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(strVal(raw))
		if value == "" {
			return nil, &TEOError{
				Kind: TEOErrorMalformed, ZoneID: zoneID, RequestID: requestID,
				Message: label + "包含空地址",
			}
		}
		prefix, ok := canonicalIPPrefix(value)
		if !ok {
			return nil, &TEOError{
				Kind: TEOErrorMalformed, ZoneID: zoneID, RequestID: requestID,
				Message: label + "包含非法 CIDR",
			}
		}
		if ipv6 {
			if prefix.Addr().Is4() {
				return nil, &TEOError{
					Kind: TEOErrorMalformed, ZoneID: zoneID, RequestID: requestID,
					Message: label + "包含 IPv4 地址（地址族错配）",
				}
			}
		} else if !prefix.Addr().Is4() {
			return nil, &TEOError{
				Kind: TEOErrorMalformed, ZoneID: zoneID, RequestID: requestID,
				Message: label + "包含 IPv6 地址（地址族错配）",
			}
		}
		out = append(out, prefix)
	}
	return out, nil
}

// parseTEOPlanTime 解析腾讯云 ISO 8601 时间；无时区按北京时间（UTC+8）解释。
func parseTEOPlanTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, true
	}
	cst := time.FixedZone("CST", 8*60*60)
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, value, cst); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// normalizePrefixes 去重并稳定排序（IPv4 在前，再按字符串）。
func normalizePrefixes(prefixes []netip.Prefix) []netip.Prefix {
	seen := make(map[netip.Prefix]bool, len(prefixes))
	out := make([]netip.Prefix, 0, len(prefixes))
	for _, prefix := range prefixes {
		prefix = prefix.Masked()
		if seen[prefix] {
			continue
		}
		seen[prefix] = true
		out = append(out, prefix)
	}
	sort.SliceStable(out, func(i, j int) bool {
		i4, j4 := out[i].Addr().Is4(), out[j].Addr().Is4()
		if i4 != j4 {
			return i4
		}
		return out[i].String() < out[j].String()
	})
	return out
}
