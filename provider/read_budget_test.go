package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	"github.com/alibabacloud-go/tea/tea"
)

func readBudgetPage(t *testing.T, path string, n int, more bool) string {
	t.Helper()
	if path == "ecs_snapshot" {
		token := ""
		if more {
			token = fmt.Sprint("token-", n)
		}
		return fmt.Sprintf(`{"Permissions":{"Permission":[{"SecurityGroupRuleId":"rule-%d","IpProtocol":"tcp","PortRange":"443/443","SourceCidrIp":"1.1.1.1/32","Policy":"accept"}]},"NextToken":"%s"}`, n, token)
	}
	if path == "ali_ecs" {
		token := ""
		if more {
			token = fmt.Sprint("token-", n)
		}
		return fmt.Sprintf(`{"SecurityGroups":{"SecurityGroup":[{"SecurityGroupId":"sg-%d"}]},"NextToken":"%s"}`, n, token)
	}
	list, id := "Instances", "InstanceId"
	if path == "tc_lighthouse" {
		list = "InstanceSet"
	}
	if path == "tc_cvm" {
		list = "SecurityGroupSet"
		id = "SecurityGroupId"
	}
	size := 1
	if more {
		size = 100
	}
	items := make([]map[string]string, size)
	for i := range items {
		items[i] = map[string]string{id: fmt.Sprintf("item-%d-%d", n, i)}
	}
	b, err := json.Marshal(items)
	if err != nil {
		t.Error(err)
		return ""
	}
	total := 1
	if more {
		total = 101
	}
	result := fmt.Sprintf(`{"%s":%s,"TotalCount":%d}`, list, b, total)
	if strings.HasPrefix(path, "tc_") {
		result = `{"Response":` + result + `}`
	}
	return result
}

func readBudgetReader(t *testing.T, path, host string) func(context.Context) (int, bool, error) {
	t.Helper()
	// 阿里云单请求自然超时为 20s，必须晚于下面的 3s 测试保护。
	pool := NewClientPool(Credentials{})
	var c any
	ct := config.CloudType(path)
	switch path {
	case "ecs_snapshot", "ali_ecs":
		p := mockECS(t, host)
		p.client.ConnectTimeout = tea.Int(10000)
		p.client.ReadTimeout = tea.Int(10000)
		c = p.client
		if path == "ecs_snapshot" {
			return func(ctx context.Context) (int, bool, error) {
				snap, e := p.getSnapshotBounded(ctx)
				return len(snap.Rules), len(snap.Rules) == 0, e
			}
		}
	case "ali_swas":
		p := mockSWAS(t, host)
		p.client.ConnectTimeout = tea.Int(10000)
		p.client.ReadTimeout = tea.Int(10000)
		c = p.client
	case "tc_lighthouse":
		c = mockLighthouse(t, host).client
	case "tc_cvm":
		c = mockCVM(t, host).client
	}
	_, e := pool.GetOrCreate(pool.CacheKey(ct, "region"), func() (any, error) { return c, nil })
	if e != nil {
		t.Fatal(e)
	}
	return func(ctx context.Context) (int, bool, error) {
		result, e := scanResourcesBounded(ctx, ct, "region", pool)
		return len(result), result == nil, e
	}
}

// 异步读取使用缓冲结果和独立完成信号，失败清理后也必须等待读取退出。
type readBudgetResult struct {
	count int
	empty bool
	err   error
}

func startReadBudget(t *testing.T, ctx context.Context, read func(context.Context) (int, bool, error)) <-chan readBudgetResult {
	t.Helper()
	result := make(chan readBudgetResult, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		n, empty, err := read(ctx)
		result <- readBudgetResult{n, empty, err}
	}()
	t.Cleanup(func() {
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Error("读取协程未在夹具清理后退出")
		}
	})
	return result
}

func waitReadBudget(t *testing.T, result <-chan readBudgetResult) readBudgetResult {
	t.Helper()
	select {
	case r := <-result:
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("读取未响应取消或预算到期")
		return readBudgetResult{}
	}
}

func TestReadBudgetCancellation(t *testing.T) {
	for _, path := range []string{"ecs_snapshot", "ali_ecs", "ali_swas", "tc_lighthouse", "tc_cvm"} {
		for _, phase := range []string{"headers", "body", "later_headers", "later_body"} {
			t.Run(path+"/"+phase, func(t *testing.T) {
				var calls atomic.Int32
				reached := make(chan struct{}, 1)
				release := make(chan struct{})
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !readBudgetDrainRequest(t, r) {
						return
					}
					n := int(calls.Add(1))
					if strings.HasPrefix(phase, "later_") && n == 1 {
						readBudgetWriteFixture(t, w, r, readBudgetPage(t, path, n, true))
						return
					}
					if strings.HasSuffix(phase, "body") {
						readBudgetWriteFixture(t, w, r, `{`)
						w.(http.Flusher).Flush()
					}
					reached <- struct{}{}
					// 不定时补回响应，读取只能通过取消或测试清理退出。
					select {
					case <-release:
					case <-r.Context().Done():
					}
				}))
				defer func() { close(release); srv.CloseClientConnections(); srv.Close() }()
				read := readBudgetReader(t, path, strings.TrimPrefix(srv.URL, "http://"))
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				result := startReadBudget(t, ctx, read)
				// 先证明请求到达所需页/响应阶段，再触发标准 context 取消。
				select {
				case <-reached:
				case r := <-result:
					t.Fatalf("读取在到达指定阶段前返回: %+v", r)
				case <-time.After(3 * time.Second):
					t.Fatal("请求未到达指定阶段")
				}
				cancel()
				r := waitReadBudget(t, result)
				if r.count != 0 || !r.empty || !errors.Is(r.err, ErrSnapshotIncomplete) || !errors.Is(r.err, context.Canceled) || errors.Is(r.err, context.DeadlineExceeded) {
					t.Fatalf("partial-result/cancellation guard: count=%d empty=%v error=%v", r.count, r.empty, r.err)
				}
				want := int32(1)
				if strings.HasPrefix(phase, "later_") {
					want = 2
				}
				if calls.Load() != want {
					t.Fatalf("calls=%d want=%d", calls.Load(), want)
				}
			})
		}
	}
}

// 自动到期的错误身份单独验证，不要求在短预算内到达特定请求阶段。
// 在途取消和后页丢弃部分结果由上面的阶段同步矩阵证明。
func TestReadBudgetDeadline(t *testing.T) {
	for _, path := range []string{"ecs_snapshot", "ali_ecs", "ali_swas", "tc_lighthouse", "tc_cvm"} {
		t.Run(path, func(t *testing.T) {
			release := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !readBudgetDrainRequest(t, r) {
					return
				}
				select {
				case <-release:
				case <-r.Context().Done():
				}
			}))
			defer func() { close(release); srv.CloseClientConnections(); srv.Close() }()
			read := readBudgetReader(t, path, strings.TrimPrefix(srv.URL, "http://"))
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			r := waitReadBudget(t, startReadBudget(t, ctx, read))
			if r.count != 0 || !r.empty || !errors.Is(r.err, ErrSnapshotIncomplete) || !errors.Is(r.err, context.DeadlineExceeded) || errors.Is(r.err, context.Canceled) {
				t.Fatalf("real deadline guard: count=%d empty=%v error=%v", r.count, r.empty, r.err)
			}
		})
	}
}

func TestReadBudgetSharedAcrossPages(t *testing.T) {
	for _, path := range []string{"ecs_snapshot", "ali_ecs"} {
		t.Run(path, func(t *testing.T) {
			var calls atomic.Int32
			release := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !readBudgetDrainRequest(t, r) {
					return
				}
				n := calls.Add(1)
				select {
				case <-time.After(12 * time.Millisecond):
				case <-r.Context().Done():
					return
				case <-release:
					return
				}
				readBudgetWriteFixture(t, w, r, readBudgetPage(t, path, int(n), true))
			}))
			defer func() { close(release); srv.CloseClientConnections(); srv.Close() }()
			// 全部分页仍共用真实 deadline；裕量不再要求两页在 65ms 内到达。
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			read := readBudgetReader(t, path, strings.TrimPrefix(srv.URL, "http://"))
			r := waitReadBudget(t, startReadBudget(t, ctx, read))
			if r.count != 0 || !r.empty || !errors.Is(r.err, context.DeadlineExceeded) || !errors.Is(r.err, ErrSnapshotIncomplete) || calls.Load() < 2 {
				t.Fatalf("shared budget guard: count=%d empty=%v calls=%d error=%v", r.count, r.empty, calls.Load(), r.err)
			}
		})
	}
}

func TestReadBudgetSuccessAndExpired(t *testing.T) {
	for _, path := range []string{"ecs_snapshot", "ali_ecs", "ali_swas", "tc_lighthouse", "tc_cvm"} {
		t.Run(path, func(t *testing.T) {
			m, h := newMockCloudAPI(t)
			m.reply = func(i int, r recordedRequest) (int, string) { return 200, readBudgetPage(t, path, i, false) }
			read := readBudgetReader(t, path, h)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			n, _, e := read(ctx)
			if e != nil || n != 1 {
				t.Fatalf("normal read failed: count=%d error=%v", n, e)
			}
			expired, cancel2 := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel2()
			before := len(m.recorded())
			n, empty, e := read(expired)
			if n != 0 || !empty || !errors.Is(e, ErrSnapshotIncomplete) || !errors.Is(e, context.DeadlineExceeded) || len(m.recorded()) != before {
				t.Fatalf("expired admission guard: count=%d empty=%v error=%v", n, empty, e)
			}
		})
	}
}

func TestReadBudgetSinglePageTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer func() { close(release); srv.CloseClientConnections(); srv.Close() }()
	p := mockECS(t, strings.TrimPrefix(srv.URL, "http://"))
	p.client.ConnectTimeout = tea.Int(10)
	p.client.ReadTimeout = tea.Int(20)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, e := p.getSnapshotBounded(ctx)
	var ne net.Error
	if e == nil || ctx.Err() != nil || errors.Is(e, ErrSnapshotIncomplete) || !errors.As(e, &ne) || !ne.Timeout() {
		t.Fatalf("ordinary single-page timeout changed: ctx=%v error=%v", ctx.Err(), e)
	}
}

type readBudgetCloseBody struct {
	io.Reader
	closes int
	err    error
}

func (b *readBudgetCloseBody) Close() error { b.closes++; return b.err }
func TestReadBudgetBodyCleanup(t *testing.T) {
	for _, mode := range []string{"sdk_closes", "sdk_skips_close", "close_error"} {
		t.Run(mode, func(t *testing.T) {
			orig := slog.Default()
			var logs bytes.Buffer
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			defer slog.SetDefault(orig)
			body := &readBudgetCloseBody{Reader: strings.NewReader(`{}`)}
			if mode == "close_error" {
				body.err = errors.New("SECRET-URL-RESPONSE")
			}
			a := newAliReadHTTP(context.Background(), 10, 30)
			a.body = &readResponseBody{ReadCloser: body}
			if mode != "sdk_skips_close" {
				if err := a.body.Close(); !errors.Is(err, body.err) {
					t.Fatalf("关闭错误未保留: %T", err)
				}
			}
			a.close()
			a.close()
			if body.closes != 1 {
				t.Fatalf("close ownership guard: actual closes=%d", body.closes)
			}
			if strings.Contains(logs.String(), "SECRET") || strings.Contains(logs.String(), "URL-RESPONSE") {
				t.Fatal("cleanup logged error payload")
			}
			if mode == "close_error" && strings.Count(logs.String(), "response_close") != 1 {
				t.Fatalf("close failure logging guard: %s", logs.String())
			}
		})
	}
}

func TestReadBudgetAgainstPooledRequest(t *testing.T) {
	m, host := newMockCloudAPI(t)
	m.reply = func(i int, r recordedRequest) (int, string) { return 200, readBudgetPage(t, "ecs_snapshot", i, false) }
	p := mockECS(t, host)
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(ready)
		for i := 0; i < 100; i++ {
			_, e := p.client.DescribeSecurityGroupAttribute(&ecs.DescribeSecurityGroupAttributeRequest{})
			if e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()
	<-ready
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, e := p.getSnapshotBounded(ctx)
		cancel()
		if e != nil {
			t.Fatal(e)
		}
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}

type readBudgetRoundTrip func(*http.Request) (*http.Response, error)

func (f readBudgetRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestReadBudgetSDKBodyOwnership(t *testing.T) {
	for _, payload := range []string{"", `{}`, `{bad`} {
		t.Run(fmt.Sprintf("payload=%q", payload), func(t *testing.T) {
			b := &readBudgetCloseBody{Reader: strings.NewReader(payload)}
			a := newAliReadHTTP(context.Background(), 100, 300)
			a.client = &http.Client{Transport: readBudgetRoundTrip(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: b, Request: r}, nil
			})}
			base := mockECS(t, "endpoint.invalid")
			local, e := ecs.NewClient(aliReadConfig(&base.client.Client, a))
			if e != nil {
				t.Fatal(e)
			}
			if _, err := local.DescribeSecurityGroups(&ecs.DescribeSecurityGroupsRequest{}); err != nil {
				t.Logf("读取夹具返回错误类型 %T", err)
			}
			a.close()
			if b.closes != 1 {
				t.Fatalf("SDK body ownership guard: closes=%d", b.closes)
			}
		})
	}
}

func TestReadBudgetOperationIsolation(t *testing.T) {
	var created, closed atomic.Int32
	shortStarted, longStarted := make(chan struct{}), make(chan struct{})
	allowLong, release := make(chan struct{}), make(chan struct{})
	allClosed := make(chan struct{})
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !readBudgetDrainRequest(t, r) {
			return
		}
		switch r.URL.Query().Get("NextToken") {
		case "short":
			close(shortStarted)
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		case "long":
			close(longStarted)
			select {
			case <-allowLong:
			case <-r.Context().Done():
				return
			case <-release:
				return
			}
		}
		readBudgetWriteFixture(t, w, r, readBudgetPage(t, "ecs_snapshot", 0, false))
	}))
	srv.Config.ConnState = func(c net.Conn, state http.ConnState) {
		if state == http.StateNew {
			created.Add(1)
		}
		if state == http.StateClosed && closed.Add(1) == 2 {
			close(allClosed)
		}
	}
	srv.Start()
	defer func() { close(release); srv.CloseClientConnections(); srv.Close() }()
	p := mockECS(t, strings.TrimPrefix(srv.URL, "http://"))
	p.client.ConnectTimeout = tea.Int(10000)
	p.client.ReadTimeout = tea.Int(10000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := newAliReadHTTP(ctx, 10000, 10000)
	defer a.close()
	short, e := ecs.NewClient(aliReadConfig(&p.client.Client, a))
	if e != nil {
		t.Fatal(e)
	}
	longCtx, longCancel := context.WithCancel(context.Background())
	defer longCancel()
	b := newAliReadHTTP(longCtx, 10000, 10000)
	defer b.close()
	long, e := ecs.NewClient(aliReadConfig(&p.client.Client, b))
	if e != nil {
		t.Fatal(e)
	}
	shortResult := startReadBudget(t, ctx, func(context.Context) (int, bool, error) {
		_, err := short.DescribeSecurityGroupAttribute(&ecs.DescribeSecurityGroupAttributeRequest{NextToken: tea.String("short")})
		return 0, true, err
	})
	longResult := startReadBudget(t, longCtx, func(context.Context) (int, bool, error) {
		_, err := long.DescribeSecurityGroupAttribute(&ecs.DescribeSecurityGroupAttributeRequest{NextToken: tea.String("long")})
		return 0, true, err
	})
	for _, started := range []<-chan struct{}{shortStarted, longStarted} {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("两个隔离操作未同时进入在途阶段")
		}
	}
	cancel()
	if r := waitReadBudget(t, shortResult); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("short operation error=%v", r.err)
	}
	// 先关闭已取消操作的池，另一个在途操作仍应能完成并复用自己的连接。
	a.close()
	if longCtx.Err() != nil || b.ctx.Err() != nil {
		t.Fatal("other operation context canceled")
	}
	close(allowLong)
	if r := waitReadBudget(t, longResult); r.err != nil {
		t.Fatalf("other operation affected: %v", r.err)
	}
	before := created.Load()
	if before != 2 {
		t.Fatalf("independent operation connections=%d want=2", before)
	}
	for i := 0; i < 3; i++ {
		_, e = long.DescribeSecurityGroups(&ecs.DescribeSecurityGroupsRequest{})
		if e != nil {
			t.Fatal(e)
		}
	}
	if created.Load() != before {
		t.Fatal("operation transport not reused")
	}
	b.close()
	select {
	case <-allClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("operation connections not closed")
	}
	if p.client.HttpClient != nil || tea.IntValue(p.client.ReadTimeout) != 10000 {
		t.Fatal("pool client changed")
	}
}

func TestReadBudgetTransportProfile(t *testing.T) {
	t.Run("tls_ca", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			readBudgetWriteFixture(t, w, r, readBudgetPage(t, "ecs_snapshot", 0, false))
		}))
		defer srv.Close()
		base, e := ecs.NewClient(&openapi.Config{Credential: mockECS(t, "unused.invalid").client.Credential, Endpoint: tea.String(strings.TrimPrefix(srv.URL, "https://")), Protocol: tea.String("https"), Ca: tea.String(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))), ConnectTimeout: tea.Int(1000), ReadTimeout: tea.Int(1000)})
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		a := newAliReadHTTP(ctx, 1000, 1000)
		defer a.close()
		c, e := ecs.NewClient(aliReadConfig(&base.Client, a))
		if e != nil {
			t.Fatal(e)
		}
		_, e = c.DescribeSecurityGroupAttribute(&ecs.DescribeSecurityGroupAttributeRequest{})
		if e != nil {
			t.Fatal(e)
		}
		if a.transport.TLSClientConfig.InsecureSkipVerify {
			t.Fatal("TLS verification disabled")
		}
	})
	t.Run("http_proxy", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Host != "ecs.invalid" {
				t.Errorf("proxy URL=%s", r.URL)
			}
			calls.Add(1)
			readBudgetWriteFixture(t, w, r, readBudgetPage(t, "ecs_snapshot", 0, false))
		}))
		defer srv.Close()
		base, e := ecs.NewClient(&openapi.Config{Credential: mockECS(t, "unused.invalid").client.Credential, Endpoint: tea.String("ecs.invalid"), Protocol: tea.String("http"), HttpProxy: tea.String(srv.URL), ConnectTimeout: tea.Int(1000), ReadTimeout: tea.Int(1000)})
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		a := newAliReadHTTP(ctx, 1000, 1000)
		defer a.close()
		c, e := ecs.NewClient(aliReadConfig(&base.Client, a))
		if e != nil {
			t.Fatal(e)
		}
		_, e = c.DescribeSecurityGroupAttribute(&ecs.DescribeSecurityGroupAttributeRequest{})
		if e != nil || calls.Load() != 1 {
			t.Fatalf("proxy calls=%d error=%v", calls.Load(), e)
		}
	})
}

// 请求正文先读完，确保服务端能观察到客户端取消。
func readBudgetDrainRequest(t *testing.T, r *http.Request) bool {
	t.Helper()
	_, readErr := io.Copy(io.Discard, r.Body)
	closeErr := r.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Error(err)
		return false
	}
	return true
}

// 取消后的响应写失败属于夹具预期，其余错误必须使测试失败。
func readBudgetWriteFixture(t *testing.T, w http.ResponseWriter, r *http.Request, text string) {
	t.Helper()
	if _, err := io.WriteString(w, text); err != nil && r.Context().Err() == nil {
		t.Error(err)
	}
}
