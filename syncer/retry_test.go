package syncer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

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
	var (
		connMu   sync.Mutex
		conns    []net.Conn
		acceptWG sync.WaitGroup
	)
	acceptWG.Add(1)
	go func() {
		defer acceptWG.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// 必须持有 accepted connection 的强引用，避免 netFD finalizer 在客户端
			// deadline 前提前关闭连接，把真实超时错误退化为 EOF/reset。
			connMu.Lock()
			conns = append(conns, conn)
			connMu.Unlock()
		}
	}()
	defer func() {
		_ = ln.Close()
		acceptWG.Wait()
		connMu.Lock()
		accepted := append([]net.Conn(nil), conns...)
		conns = nil
		connMu.Unlock()
		for _, conn := range accepted {
			_ = conn.Close()
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
			err:  errors.New("安全组入站规则数将达 101（上限 100），停止新增"),
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
