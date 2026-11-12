package provider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// 本文件是 Build8 Step 5 的 TEO 只读查询回归。
//
// 所有请求经过真实腾讯云 SDK → 本地 httptest 服务；只使用虚构凭据，
// 断言请求方法/Action/API 版本/ZoneId/TC3 签名，以及响应转换与安全错误分类。

const (
	teoTestSecretID  = "AKID-FAKE-TEO-TEST"
	teoTestSecretKey = "FAKE-TEO-SECRET-KEY"
)

// teoCaptureServer 捕获请求并返回固定响应。
type teoCaptureServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests int
	method   string
	path     string
	host     string
	header   http.Header
	body     []byte
}

func newTEOCaptureServer(t *testing.T, status int, response string) *teoCaptureServer {
	t.Helper()
	capture := &teoCaptureServer{}
	capture.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("读取请求体失败: %v", err)
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}
		capture.mu.Lock()
		capture.requests++
		capture.method = r.Method
		capture.path = r.URL.Path
		capture.host = r.Host
		capture.header = r.Header.Clone()
		capture.body = append([]byte(nil), body...)
		capture.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(capture.Server.Close)
	return capture
}

func (s *teoCaptureServer) snapshot() (requests int, method, path, host string, header http.Header, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests, s.method, s.path, s.host, s.header.Clone(), append([]byte(nil), s.body...)
}

func teoTestClient(t *testing.T, serverURL string, budget, interval time.Duration) *TEOClient {
	t.Helper()
	pool := NewClientPool(Credentials{
		TencentSecretID:  teoTestSecretID,
		TencentSecretKey: teoTestSecretKey,
	})
	client, err := newTEOClient(pool, teoClientOptions{
		Endpoint:      strings.TrimPrefix(serverURL, "http://"),
		Scheme:        "HTTP",
		RequestBudget: budget,
		MinInterval:   interval,
	})
	if err != nil {
		t.Fatalf("构造 TEO client 失败: %v", err)
	}
	return client
}

func TestTEOClientConstructionMakesNoRequest(t *testing.T) {
	server := newTEOCaptureServer(t, 0, teoSuccessResponse())
	pool := NewClientPool(Credentials{
		TencentSecretID:  teoTestSecretID,
		TencentSecretKey: teoTestSecretKey,
	})
	client, err := newTEOClient(pool, teoClientOptions{
		Endpoint:      strings.TrimPrefix(server.URL, "http://"),
		Scheme:        "HTTP",
		RequestBudget: time.Second,
		MinInterval:   0,
	})
	if err != nil {
		t.Fatalf("构造 TEO client 失败: %v", err)
	}
	if requests, _, _, _, _, _ := server.snapshot(); requests != 0 {
		t.Fatalf("构造客户端不应发出请求，实际 %d 次", requests)
	}
	if client.Endpoint() == "" || strings.Contains(client.Endpoint(), "secret") {
		t.Fatalf("Endpoint 诊断值不安全: %q", client.Endpoint())
	}

	// 复用同一 pool+endpoint 的第二个 client 也不应触发网络。
	if _, err := newTEOClient(pool, teoClientOptions{
		Endpoint:      strings.TrimPrefix(server.URL, "http://"),
		Scheme:        "HTTP",
		RequestBudget: time.Second,
		MinInterval:   0,
	}); err != nil {
		t.Fatalf("第二次构造 TEO client 失败: %v", err)
	}
	if requests, _, _, _, _, _ := server.snapshot(); requests != 0 {
		t.Fatalf("第二次构造仍不应发出请求，实际 %d 次", requests)
	}

	if _, err := client.DescribeOriginACL(context.Background(), "zone-test"); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if requests, _, _, _, _, _ := server.snapshot(); requests != 1 {
		t.Fatalf("实际查询请求数 = %d, want 1", requests)
	}
}

func TestTEODescribeOriginACLPostsSignedRequest(t *testing.T) {
	server := newTEOCaptureServer(t, 0, teoSuccessResponse())
	client := teoTestClient(t, server.URL, 2*time.Second, 0)

	snapshot, err := client.DescribeOriginACL(context.Background(), "zone-test")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	requests, method, path, host, header, body := server.snapshot()
	if requests != 1 {
		t.Fatalf("请求数 = %d, want 1", requests)
	}
	if method != http.MethodPost {
		t.Errorf("HTTP 方法 = %s, want POST", method)
	}
	if path != "/" {
		t.Errorf("请求路径 = %q, want /", path)
	}
	if host != strings.TrimPrefix(server.URL, "http://") {
		t.Errorf("Host = %q, want %q", host, strings.TrimPrefix(server.URL, "http://"))
	}
	if got := header.Get("X-TC-Action"); got != "DescribeOriginACL" {
		t.Errorf("X-TC-Action = %q", got)
	}
	if got := header.Get("X-TC-Version"); got != "2022-09-01" {
		t.Errorf("X-TC-Version = %q", got)
	}
	if got := header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := header.Get("X-TC-Region"); got != "" {
		t.Errorf("不应发送 Region，实际 %q", got)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("请求体不是 JSON: %v", err)
	}
	if got := payload["ZoneId"]; got != "zone-test" {
		t.Errorf("ZoneId = %v, want zone-test", got)
	}
	if len(payload) != 1 {
		t.Errorf("请求体字段 = %v, 只应包含 ZoneId", payload)
	}

	verifyTC3Authorization(t, header, host, body, teoTestSecretID, teoTestSecretKey)

	if snapshot.ZoneID != "zone-test" || snapshot.Status != "online" {
		t.Errorf("快照身份 = %+v", snapshot)
	}
	if snapshot.Current.Version != "2026-01" {
		t.Errorf("当前版本 = %q", snapshot.Current.Version)
	}
	wantCurrent := []string{"203.0.113.0/24", "2001:db8::/64"}
	assertPrefixStrings(t, snapshot.Current.Prefixes, wantCurrent)
	if snapshot.Next == nil || snapshot.Next.Version != "2026-02" {
		t.Fatalf("待更新版 = %+v", snapshot.Next)
	}
	assertPrefixStrings(t, snapshot.Next.Prefixes, []string{"198.51.100.0/24"})
	if snapshot.PlannedActiveAt == nil || !snapshot.PlannedActiveAt.Equal(time.Date(2026, 11, 30, 16, 0, 0, 0, time.UTC)) {
		t.Errorf("计划生效时间 = %v", snapshot.PlannedActiveAt)
	}
	if snapshot.RequestID != "req-success" {
		t.Errorf("RequestId = %q", snapshot.RequestID)
	}
	if snapshot.ObservedAt.IsZero() {
		t.Error("ObservedAt 不应为零值")
	}
	assertPrefixStrings(t, snapshot.AllPrefixes(), []string{
		"198.51.100.0/24", "203.0.113.0/24", "2001:db8::/64",
	})
}

func TestTEOResponseConversionMatrix(t *testing.T) {
	cases := []struct {
		name     string
		response string
		wantKind TEOErrorKind
		check    func(t *testing.T, snapshot OriginACLSnapshot)
	}{
		{
			name:     "无下一版是有效状态",
			response: teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"v1","EntireAddresses":{"IPv4":["203.0.113.0/24"],"IPv6":[]}}}`),
			check: func(t *testing.T, snapshot OriginACLSnapshot) {
				if snapshot.Next != nil || snapshot.PlannedActiveAt != nil {
					t.Errorf("无下一版时 Next/PlannedActiveAt 应为空: %+v", snapshot)
				}
				assertPrefixStrings(t, snapshot.Current.Prefixes, []string{"203.0.113.0/24"})
			},
		},
		{
			name:     "IPv4 合法为空 IPv6 有效",
			response: teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"v1","EntireAddresses":{"IPv4":[],"IPv6":["2001:db8::/64"]}}}`),
			check: func(t *testing.T, snapshot OriginACLSnapshot) {
				assertPrefixStrings(t, snapshot.Current.Prefixes, []string{"2001:db8::/64"})
			},
		},
		{
			name:     "IPv6 合法为空 IPv4 有效",
			response: teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"v1","EntireAddresses":{"IPv4":["203.0.113.1"],"IPv6":[]}}}`),
			check: func(t *testing.T, snapshot OriginACLSnapshot) {
				assertPrefixStrings(t, snapshot.Current.Prefixes, []string{"203.0.113.1/32"})
			},
		},
		{
			name:     "两个地址族整体为空",
			response: teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"v1","EntireAddresses":{"IPv4":[],"IPv6":[]}}}`),
			wantKind: TEOErrorEmpty,
		},
		{
			name:     "地址族字段整体缺失",
			response: teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"v1","EntireAddresses":{}}}`),
			wantKind: TEOErrorIncomplete,
		},
		{
			name:     "EntireAddresses 为 null",
			response: teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"v1","EntireAddresses":null}}`),
			wantKind: TEOErrorIncomplete,
		},
		{
			name:     "非法 CIDR",
			response: teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"v1","EntireAddresses":{"IPv4":["not-a-cidr"]}}}`),
			wantKind: TEOErrorMalformed,
		},
		{
			name:     "IPv4 字段放入 IPv6",
			response: teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"v1","EntireAddresses":{"IPv4":["2001:db8::/64"]}}}`),
			wantKind: TEOErrorMalformed,
		},
		{
			name:     "IPv6 字段放入 IPv4",
			response: teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"v1","EntireAddresses":{"IPv6":["203.0.113.0/24"]}}}`),
			wantKind: TEOErrorMalformed,
		},
		{
			name:     "空地址项",
			response: teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"v1","EntireAddresses":{"IPv4":[""]}}}`),
			wantKind: TEOErrorMalformed,
		},
		{
			name:     "OriginACLInfo 缺失",
			response: `{"Response":{"RequestId":"req-missing"}}`,
			wantKind: TEOErrorIncomplete,
		},
		{
			name:     "响应为空",
			response: `{}`,
			wantKind: TEOErrorIncomplete,
		},
		{
			name:     "源站防护未启用",
			response: teoResponse(`{"Status":"offline","CurrentOriginACL":null}`),
			wantKind: TEOErrorDisabled,
		},
		{
			name:     "状态缺失",
			response: teoResponse(`{"CurrentOriginACL":{"Version":"v1","EntireAddresses":{"IPv4":["203.0.113.0/24"]}}}`),
			wantKind: TEOErrorIncomplete,
		},
		{
			name:     "当前版缺失",
			response: teoResponse(`{"Status":"online"}`),
			wantKind: TEOErrorIncomplete,
		},
		{
			name:     "当前版版本号缺失",
			response: teoResponse(`{"Status":"online","CurrentOriginACL":{"EntireAddresses":{"IPv4":["203.0.113.0/24"]}}}`),
			wantKind: TEOErrorIncomplete,
		},
		{
			name:     "待更新版版本号缺失",
			response: teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"v1","EntireAddresses":{"IPv4":["203.0.113.0/24"]}},"NextOriginACL":{"PlannedActiveTime":"2026-12-01T00:00:00+08:00","EntireAddresses":{"IPv4":["198.51.100.0/24"]}}}`),
			wantKind: TEOErrorIncomplete,
		},
		{
			name:     "待更新版计划时间缺失",
			response: teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"v1","EntireAddresses":{"IPv4":["203.0.113.0/24"]}},"NextOriginACL":{"Version":"v2","EntireAddresses":{"IPv4":["198.51.100.0/24"]}}}`),
			wantKind: TEOErrorIncomplete,
		},
		{
			name:     "待更新版两个地址族整体为空",
			response: teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"v1","EntireAddresses":{"IPv4":["203.0.113.0/24"]}},"NextOriginACL":{"Version":"v2","PlannedActiveTime":"2026-12-01T00:00:00+08:00","EntireAddresses":{"IPv4":[],"IPv6":[]}}}`),
			wantKind: TEOErrorEmpty,
		},
		{
			name:     "当前版主机位规范化且去重",
			response: teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"v1","EntireAddresses":{"IPv4":["203.0.113.7/24","203.0.113.0/24","203.0.113.9/24"]}}}`),
			check: func(t *testing.T, snapshot OriginACLSnapshot) {
				assertPrefixStrings(t, snapshot.Current.Prefixes, []string{"203.0.113.0/24"})
			},
		},
		{
			name:     "待更新版有效时保留并集来源",
			response: teoResponse(`{"Status":"updating","CurrentOriginACL":{"Version":"v1","EntireAddresses":{"IPv4":["203.0.113.0/24"]}},"NextOriginACL":{"Version":"v2","PlannedActiveTime":"2026-12-01T00:00:00+08:00","EntireAddresses":{"IPv4":["203.0.113.0/24","198.51.100.0/24"]}}}`),
			check: func(t *testing.T, snapshot OriginACLSnapshot) {
				if snapshot.Status != "updating" {
					t.Errorf("状态 = %q", snapshot.Status)
				}
				assertPrefixStrings(t, snapshot.AllPrefixes(), []string{"198.51.100.0/24", "203.0.113.0/24"})
				assertPrefixStrings(t, snapshot.PrefixesForFamily("ipv4"), []string{"198.51.100.0/24", "203.0.113.0/24"})
				assertPrefixStrings(t, snapshot.PrefixesForFamily("ipv6"), nil)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newTEOCaptureServer(t, 0, tc.response)
			client := teoTestClient(t, server.URL, time.Second, 0)
			snapshot, err := client.DescribeOriginACL(context.Background(), "zone-test")
			if tc.wantKind != "" {
				if err == nil {
					t.Fatalf("期望错误 %s，实际成功: %+v", tc.wantKind, snapshot)
				}
				if got := TEOErrorKindOf(err); got != tc.wantKind {
					t.Fatalf("错误分类 = %q, want %q; err=%v", got, tc.wantKind, err)
				}
				if snapshot.ZoneID != "" || snapshot.Current.Version != "" || snapshot.Next != nil {
					t.Errorf("失败查询不得返回部分快照: %+v", snapshot)
				}
				return
			}
			if err != nil {
				t.Fatalf("期望成功，实际错误: %v", err)
			}
			if tc.check != nil {
				tc.check(t, snapshot)
			}
		})
	}
}

func TestTEOErrorClassificationAndNoSDKRetry(t *testing.T) {
	t.Run("权限错误", func(t *testing.T) {
		server := newTEOCaptureServer(t, 0,
			`{"Response":{"Error":{"Code":"UnauthorizedOperation.NoPermission","Message":"fake sensitive message"},"RequestId":"req-permission"}}`)
		client := teoTestClient(t, server.URL, time.Second, 0)
		snapshot, err := client.DescribeOriginACL(context.Background(), "zone-test")
		if err == nil {
			t.Fatal("期望权限错误")
		}
		if got := TEOErrorKindOf(err); got != TEOErrorPermission {
			t.Fatalf("错误分类 = %q, want permission", got)
		}
		if !strings.Contains(err.Error(), "req-permission") || !strings.Contains(err.Error(), "UnauthorizedOperation.NoPermission") {
			t.Errorf("安全错误应保留 RequestId 与 Code: %v", err)
		}
		if strings.Contains(err.Error(), "fake sensitive message") || strings.Contains(err.Error(), teoTestSecretKey) {
			t.Errorf("安全错误不应回显原始 Message 或凭据: %v", err)
		}
		if TEOErrorRequestID(err) != "req-permission" {
			t.Errorf("RequestId = %q", TEOErrorRequestID(err))
		}
		if snapshot.ZoneID != "" {
			t.Errorf("失败不得返回快照: %+v", snapshot)
		}
		if requests, _, _, _, _, _ := server.snapshot(); requests != 1 {
			t.Errorf("SDK 不应内部重试，请求数 = %d", requests)
		}
	})

	t.Run("限流", func(t *testing.T) {
		server := newTEOCaptureServer(t, 0,
			`{"Response":{"Error":{"Code":"RequestLimitExceeded","Message":"rate limited"},"RequestId":"req-throttle"}}`)
		client := teoTestClient(t, server.URL, time.Second, 0)
		_, err := client.DescribeOriginACL(context.Background(), "zone-test")
		if err == nil {
			t.Fatal("期望限流错误")
		}
		if got := TEOErrorKindOf(err); got != TEOErrorThrottled {
			t.Fatalf("错误分类 = %q, want throttled", got)
		}
		if requests, _, _, _, _, _ := server.snapshot(); requests != 1 {
			t.Errorf("SDK 不应内部重试，请求数 = %d", requests)
		}
	})

	t.Run("请求预算超时", func(t *testing.T) {
		started := make(chan struct{}, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case started <- struct{}{}:
			default:
			}
			// 故意晚于客户端预算返回，验证 context 超时；有界等待避免 Close 挂起。
			time.Sleep(200 * time.Millisecond)
		}))
		defer server.Close()
		client := teoTestClient(t, server.URL, 50*time.Millisecond, 0)
		start := time.Now()
		_, err := client.DescribeOriginACL(context.Background(), "zone-test")
		if err == nil {
			t.Fatal("期望超时错误")
		}
		if got := TEOErrorKindOf(err); got != TEOErrorTimeout {
			t.Fatalf("错误分类 = %q, want timeout; err=%v", got, err)
		}
		select {
		case <-started:
		default:
			t.Error("请求应已到达服务端")
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("超时耗时异常: %v", elapsed)
		}
	})
}

func TestTEOInvalidZoneNeverBecomesEmptySnapshot(t *testing.T) {
	for _, zone := range []string{"", "   "} {
		server := newTEOCaptureServer(t, 0, teoSuccessResponse())
		client := teoTestClient(t, server.URL, time.Second, 0)
		snapshot, err := client.DescribeOriginACL(context.Background(), zone)
		if err == nil {
			t.Fatalf("ZoneId=%q 应返回错误", zone)
		}
		if got := TEOErrorKindOf(err); got != TEOErrorMalformed {
			t.Fatalf("错误分类 = %q", got)
		}
		if snapshot.ZoneID != "" || len(snapshot.Current.Prefixes) != 0 || snapshot.Next != nil {
			t.Errorf("无效输入不得变成空快照: %+v", snapshot)
		}
		if requests, _, _, _, _, _ := server.snapshot(); requests != 0 {
			t.Errorf("无效输入不应发请求，实际 %d 次", requests)
		}
	}
}

func TestTEORateLimitAndContextCancel(t *testing.T) {
	server := newTEOCaptureServer(t, 0, teoSuccessResponse())
	client := teoTestClient(t, server.URL, time.Second, 25*time.Millisecond)

	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := client.DescribeOriginACL(context.Background(), "zone-test"); err != nil {
			t.Fatalf("第 %d 次查询失败: %v", i+1, err)
		}
	}
	elapsed := time.Since(start)
	if elapsed < 40*time.Millisecond {
		t.Errorf("限速未生效，3 次查询耗时 %v", elapsed)
	}
	if requests, _, _, _, _, _ := server.snapshot(); requests != 3 {
		t.Errorf("请求数 = %d, want 3", requests)
	}

	// 预留一个远期槽位，确认等待会被调用方 context 取消且不发请求。
	client.mu.Lock()
	client.next = time.Now().Add(time.Hour)
	client.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	before := time.Now()
	_, err := client.DescribeOriginACL(ctx, "zone-test")
	if err == nil {
		t.Fatal("期望等待被取消")
	}
	if got := TEOErrorKindOf(err); got != TEOErrorTimeout {
		t.Fatalf("错误分类 = %q, want timeout", got)
	}
	if time.Since(before) > time.Second {
		t.Errorf("取消等待耗时异常: %v", time.Since(before))
	}
	if requests, _, _, _, _, _ := server.snapshot(); requests != 3 {
		t.Errorf("取消等待不应新增请求，实际 %d", requests)
	}
}

func TestTEOOneSnapshotSharedByMultipleTargets(t *testing.T) {
	server := newTEOCaptureServer(t, 0, teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"v1","EntireAddresses":{"IPv4":["203.0.113.0/24"],"IPv6":[]}}}`))
	client := teoTestClient(t, server.URL, time.Second, 0)
	snapshot, err := client.DescribeOriginACL(context.Background(), "zone-test")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	// 两个绑定目标复用同一份值；不得为第二个目标再次查询。
	for target := 1; target <= 2; target++ {
		prefixes := snapshot.PrefixesForFamily("ipv4")
		if len(prefixes) != 1 || prefixes[0].String() != "203.0.113.0/24" {
			t.Fatalf("目标 %d 使用快照得到 %v", target, prefixes)
		}
	}
	if requests, _, _, _, _, _ := server.snapshot(); requests != 1 {
		t.Errorf("两个目标共享快照时请求数 = %d, want 1", requests)
	}
}

func TestOriginACLSnapshotPrefixesForFamily(t *testing.T) {
	v4 := netip.MustParsePrefix("203.0.113.0/24")
	v6 := netip.MustParsePrefix("2001:db8::/64")
	nextV4 := netip.MustParsePrefix("198.51.100.0/24")
	snapshot := OriginACLSnapshot{
		Current: OriginACLVersion{Prefixes: []netip.Prefix{v6, v4}},
		Next:    &OriginACLVersion{Prefixes: []netip.Prefix{nextV4, v4}},
	}
	assertPrefixStrings(t, snapshot.PrefixesForFamily("ipv4"), []string{"198.51.100.0/24", "203.0.113.0/24"})
	assertPrefixStrings(t, snapshot.PrefixesForFamily("ipv6"), []string{"2001:db8::/64"})
	assertPrefixStrings(t, snapshot.PrefixesForFamily("dual"), []string{"198.51.100.0/24", "203.0.113.0/24", "2001:db8::/64"})
	assertPrefixStrings(t, snapshot.PrefixesForFamily("unknown"), nil)
	assertPrefixStrings(t, snapshot.AllPrefixes(), []string{"198.51.100.0/24", "203.0.113.0/24", "2001:db8::/64"})
}

func TestParseTEOPlanTime(t *testing.T) {
	cases := []struct {
		value string
		want  time.Time
		ok    bool
	}{
		{"2026-12-01T00:00:00+08:00", time.Date(2026, 11, 30, 16, 0, 0, 0, time.UTC), true},
		{"2026-12-01T00:00:00Z", time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), true},
		{"2026-12-01T00:00:00", time.Date(2026, 11, 30, 16, 0, 0, 0, time.UTC), true},
		{"2026-12-01 00:00:00", time.Date(2026, 11, 30, 16, 0, 0, 0, time.UTC), true},
		{"", time.Time{}, false},
		{"not-a-time", time.Time{}, false},
	}
	for _, tc := range cases {
		got, ok := parseTEOPlanTime(tc.value)
		if ok != tc.ok || (ok && !got.Equal(tc.want)) {
			t.Errorf("parseTEOPlanTime(%q) = (%v,%v), want (%v,%v)", tc.value, got, ok, tc.want, tc.ok)
		}
	}
}

// teoResponse 构造成功响应；requestId 固定为 req-success。
func teoResponse(info string) string {
	return `{"Response":{"OriginACLInfo":` + info + `,"RequestId":"req-success"}}`
}

// teoSuccessResponse 是签名与复用测试使用的完整响应。
func teoSuccessResponse() string {
	return teoResponse(`{"Status":"online","CurrentOriginACL":{"Version":"2026-01","EntireAddresses":{"IPv4":["203.0.113.7/24","203.0.113.0/24"],"IPv6":["2001:db8::1/64"]}},"NextOriginACL":{"Version":"2026-02","PlannedActiveTime":"2026-12-01T00:00:00+08:00","EntireAddresses":{"IPv4":["198.51.100.0/24"],"IPv6":[]}}}`)
}

func assertPrefixStrings(t *testing.T, got []netip.Prefix, want []string) {
	t.Helper()
	gotStrings := make([]string, len(got))
	for i, prefix := range got {
		gotStrings[i] = prefix.String()
	}
	if strings.Join(gotStrings, ",") != strings.Join(want, ",") {
		t.Errorf("前缀 = %v, want %v", gotStrings, want)
	}
}

// verifyTC3Authorization 用虚构密钥重算 TC3-HMAC-SHA256 签名，验证签名接线。
//
// 只断言请求头，不回显签名或密钥。
func verifyTC3Authorization(t *testing.T, header http.Header, host string, body []byte, secretID, secretKey string) {
	t.Helper()
	auth := header.Get("Authorization")
	const prefix = "TC3-HMAC-SHA256 Credential="
	if !strings.HasPrefix(auth, prefix) {
		t.Fatalf("Authorization 前缀不正确: %q", auth)
	}
	rest := strings.TrimPrefix(auth, prefix)
	parts := strings.Split(rest, ", ")
	if len(parts) != 3 {
		t.Fatalf("Authorization 结构不正确: %q", auth)
	}
	credential := parts[0]
	if !strings.HasPrefix(parts[1], "SignedHeaders=") || !strings.HasPrefix(parts[2], "Signature=") {
		t.Fatalf("Authorization 字段不正确: %q", auth)
	}
	signedHeaders := strings.TrimPrefix(parts[1], "SignedHeaders=")
	signature := strings.TrimPrefix(parts[2], "Signature=")
	if signedHeaders != "content-type;host" {
		t.Errorf("SignedHeaders = %q", signedHeaders)
	}

	timestampText := header.Get("X-TC-Timestamp")
	timestamp, err := strconv.ParseInt(timestampText, 10, 64)
	if err != nil || timestamp <= 0 {
		t.Fatalf("X-TC-Timestamp 无效: %q", timestampText)
	}
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	wantCredential := secretID + "/" + date + "/teo/tc3_request"
	if credential != wantCredential {
		t.Errorf("Credential scope 不正确")
	}

	canonicalRequest := "POST\n/\n\ncontent-type:application/json\nhost:" + host + "\n\ncontent-type;host\n" + sha256Hex(body)
	stringToSign := "TC3-HMAC-SHA256\n" + timestampText + "\n" + date + "/teo/tc3_request\n" + sha256Hex([]byte(canonicalRequest))
	secretDate := hmacSHA256([]byte("TC3"+secretKey), date)
	secretService := hmacSHA256(secretDate, "teo")
	secretSigning := hmacSHA256(secretService, "tc3_request")
	wantSignature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))
	if signature != wantSignature {
		t.Errorf("TC3 签名不匹配（签名接线错误）")
	}
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, message string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	return mac.Sum(nil)
}

func TestTEOProductionDefaults(t *testing.T) {
	if teoDefaultEndpoint != "teo.tencentcloudapi.com" {
		t.Fatalf("默认 endpoint = %q", teoDefaultEndpoint)
	}
	if teoDefaultScheme != "HTTPS" {
		t.Fatalf("默认 scheme = %q", teoDefaultScheme)
	}
	if teoDefaultRequestBudget != 30*time.Second || teoDefaultMinInterval != 50*time.Millisecond {
		t.Fatalf("默认预算/限速 = %v / %v", teoDefaultRequestBudget, teoDefaultMinInterval)
	}
	pool := NewClientPool(Credentials{
		TencentSecretID:  teoTestSecretID,
		TencentSecretKey: teoTestSecretKey,
	})
	client, err := NewTEOClient(pool)
	if err != nil {
		t.Fatalf("构造生产默认 TEO client 失败: %v", err)
	}
	if client.Endpoint() != "teo.tencentcloudapi.com" {
		t.Fatalf("生产 endpoint = %q", client.Endpoint())
	}
}
