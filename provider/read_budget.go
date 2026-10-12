package provider

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
)

// cloudReadBudget 限制单次完整读取的网络等待，不是整目标或严格墙钟上限。
const cloudReadBudget = 120 * time.Second

// readBudgetError 只在本操作 context 结束时补充快照不完整标记。
func readBudgetError(ctx context.Context, scope string, original error) error {
	if cause := ctx.Err(); cause != nil {
		if original != nil {
			return fmt.Errorf("%w: %s读取操作预算已结束: %w: %w", ErrSnapshotIncomplete, scope, cause, original)
		}
		return fmt.Errorf("%w: %s读取操作预算已结束: %w", ErrSnapshotIncomplete, scope, cause)
	}
	return original
}

// aliReadHTTP 只属于一次串行读取操作，不进入 dara 的全局 HTTP 池。
type aliReadHTTP struct {
	ctx       context.Context
	cancel    context.CancelFunc
	timeout   time.Duration
	client    *http.Client
	transport *http.Transport
	body      *readResponseBody
}

func newAliReadHTTP(parent context.Context, connectMS, readMS int) *aliReadHTTP {
	ctx, cancel := context.WithCancel(parent)
	return &aliReadHTTP{ctx: ctx, cancel: cancel, timeout: time.Duration(connectMS+readMS) * time.Millisecond}
}

func (a *aliReadHTTP) Call(req *http.Request, transport *http.Transport) (*http.Response, error) {
	if err := a.ctx.Err(); err != nil {
		return nil, err
	}
	// SDK 每次调用都会创建 Transport；同一固定端点的操作只复用第一份。
	if a.client == nil {
		a.transport = transport
		a.client = &http.Client{Transport: transport, Timeout: a.timeout}
	}
	if a.body != nil {
		a.body.close()
		a.body = nil
	}
	resp, err := a.client.Do(req.WithContext(a.ctx))
	if resp != nil && resp.Body != nil {
		a.body = &readResponseBody{ReadCloser: resp.Body}
		resp.Body = a.body
	}
	return resp, err
}

func (a *aliReadHTTP) close() {
	a.cancel()
	if a.body != nil {
		a.body.close()
		a.body = nil
	}
	if a.transport != nil {
		a.transport.CloseIdleConnections()
	}
}

// readResponseBody 统一处理 SDK 与操作收尾的关闭，只关闭底层响应体一次。
type readResponseBody struct {
	io.ReadCloser
	once sync.Once
	err  error
}

func (b *readResponseBody) close() {
	b.once.Do(func() {
		b.err = b.ReadCloser.Close()
		if b.err != nil {
			slog.Warn("云读取响应体关闭失败", "stage", "response_close", "error_type", fmt.Sprintf("%T", b.err))
		}
	})
}

func (b *readResponseBody) Close() error {
	b.close()
	return b.err
}

// aliReadConfig 只读取基础客户端的不可变配置，由官方构造器创建独立实例。
// 不复制整个 Client，避免与原池请求的 GetRpcHeaders 清空 Headers 发生竞态。
func aliReadConfig(base *openapi.Client, httpClient *aliReadHTTP) *openapi.Config {
	return &openapi.Config{
		Credential: base.Credential, Endpoint: base.Endpoint, EndpointType: base.EndpointType,
		RegionId: base.RegionId, Protocol: base.Protocol, Method: base.Method, UserAgent: base.UserAgent,
		Network: base.Network, Suffix: base.Suffix, ReadTimeout: base.ReadTimeout, ConnectTimeout: base.ConnectTimeout,
		HttpProxy: base.HttpProxy, HttpsProxy: base.HttpsProxy, NoProxy: base.NoProxy,
		Socks5Proxy: base.Socks5Proxy, Socks5NetWork: base.Socks5NetWork, MaxIdleConns: base.MaxIdleConns,
		SignatureVersion: base.SignatureVersion, SignatureAlgorithm: base.SignatureAlgorithm,
		GlobalParameters: base.GlobalParameters, Key: base.Key, Cert: base.Cert, Ca: base.Ca,
		DisableHttp2: base.DisableHttp2, RetryOptions: base.RetryOptions, TlsMinVersion: base.TlsMinVersion,
		HttpClient: httpClient,
	}
}
