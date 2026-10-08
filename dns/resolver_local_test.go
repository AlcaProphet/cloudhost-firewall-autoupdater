package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// 本地替身只服务本用例，不转发查询；随机回环端口避免固定端口争用。
type localDNS struct {
	conn    net.PacketConn
	done    chan struct{}
	seen    chan struct{}
	once    sync.Once
	mu      sync.Mutex
	queries []dnsmessage.Question
	fault   error
}

func newLocalDNS(t *testing.T, mode string) *localDNS {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &localDNS{conn: conn, done: make(chan struct{}), seen: make(chan struct{})}
	go func() {
		defer close(s.done)
		buf := make([]byte, 4096)
		for {
			n, peer, err := conn.ReadFrom(buf)
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					s.recordFault(err)
				}
				return
			}
			var req dnsmessage.Message
			if err := req.Unpack(buf[:n]); err != nil {
				s.recordFault(err)
				return
			}
			if len(req.Questions) != 1 {
				s.recordFault(fmt.Errorf("问题数=%d", len(req.Questions)))
				return
			}
			q := req.Questions[0]
			s.mu.Lock()
			s.queries = append(s.queries, q)
			s.mu.Unlock()
			s.once.Do(func() { close(s.seen) })
			if mode == "silent" {
				continue
			}
			reply := dnsmessage.Message{Header: dnsmessage.Header{ID: req.ID, Response: true, Authoritative: true, RecursionDesired: req.RecursionDesired, RecursionAvailable: true}, Questions: req.Questions}
			if mode == "nxdomain" {
				reply.RCode = dnsmessage.RCodeNameError
			}
			if mode == "servfail" {
				reply.RCode = dnsmessage.RCodeServerFailure
			}
			if q.Type == dnsmessage.TypeA && (mode == "dual" || mode == "v4") {
				reply.Answers = append(reply.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: q.Name, Type: q.Type, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{192, 0, 2, 23}}})
			}
			if q.Type == dnsmessage.TypeAAAA && (mode == "dual" || mode == "v6") {
				var a [16]byte
				copy(a[:], net.ParseIP("2001:db8::23").To16())
				reply.Answers = append(reply.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: q.Name, Type: q.Type, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AAAAResource{AAAA: a}})
			}
			packed, err := reply.Pack()
			if err != nil {
				s.recordFault(err)
				return
			}
			if _, err = conn.WriteTo(packed, peer); err != nil {
				s.recordFault(err)
				return
			}
		}
	}()
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
			t.Error("DNS 替身未有界退出")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.fault != nil {
			t.Errorf("DNS 替身错误: %v", s.fault)
		}
	})
	return s
}
func (s *localDNS) recordFault(err error) { s.mu.Lock(); defer s.mu.Unlock(); s.fault = err }
func (s *localDNS) checkQueries(t *testing.T, host string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[dnsmessage.Type]int{}
	for _, q := range s.queries {
		if q.Name.String() != host || q.Class != dnsmessage.ClassINET {
			t.Errorf("意外查询: %v", q)
		}
		seen[q.Type]++
	}
	if seen[dnsmessage.TypeA] < 1 || seen[dnsmessage.TypeAAAA] < 1 {
		t.Errorf("必须实际发出 A 与 AAAA 查询: %v", seen)
	}
	// 不固定重试/并发顺序，标准库和主机 DNS 配置可能改变这些细节。
	for typ := range seen {
		if typ != dnsmessage.TypeA && typ != dnsmessage.TypeAAAA {
			t.Errorf("意外类型: %v", typ)
		}
	}
}

// TestI804LocalAnswers 验证真实解析器发出 A/AAAA 并保留返回地址、地址族和 CIDR。
func TestI804LocalAnswers(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want []string
	}{
		{"dual", []string{"192.0.2.23|false|192.0.2.23/32", "2001:db8::23|true|2001:db8::23/128"}},
		{"v4", []string{"192.0.2.23|false|192.0.2.23/32"}},
		{"v6", []string{"2001:db8::23|true|2001:db8::23/128"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			s := newLocalDNS(t, tc.mode)
			r := NewResolver(s.conn.LocalAddr().String(), 5*time.Second)
			got, err := r.Resolve(context.Background(), "i804-answer.example.test.")
			if err != nil {
				t.Fatal(err)
			}
			items := make([]string, 0, len(got))
			for _, ip := range got {
				items = append(items, fmt.Sprintf("%s|%t|%s", ip.IP, ip.IsIPv6, ip.CIDR()))
			}
			sort.Strings(items)
			if !reflect.DeepEqual(items, tc.want) {
				t.Fatalf("结果=%v 期望=%v", items, tc.want)
			}
			s.checkQueries(t, "i804-answer.example.test.")
		})
	}
}

// TestI804LocalErrors 错误响应必须失败，保留可识别 DNS 错误链。
func TestI804LocalErrors(t *testing.T) {
	for _, mode := range []string{"nxdomain", "empty", "servfail"} {
		t.Run(mode, func(t *testing.T) {
			s := newLocalDNS(t, mode)
			r := NewResolver(s.conn.LocalAddr().String(), 5*time.Second)
			got, err := r.Resolve(context.Background(), "i804-error.example.test.")
			if err == nil || len(got) != 0 {
				t.Fatalf("错误响应不应成功: %v %v", got, err)
			}
			var de *net.DNSError
			if !errors.As(err, &de) {
				t.Fatalf("错误链丢失 DNS 类型: %T", err)
			}
			if (mode == "nxdomain" || mode == "empty") && !de.IsNotFound {
				t.Errorf("无记录错误缺少 IsNotFound: %v", de)
			}
			s.checkQueries(t, "i804-error.example.test.")
		})
	}
}
func boundedResolve(t *testing.T, r *Resolver, ctx context.Context, host string) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		got, err := r.Resolve(ctx, host)
		if len(got) != 0 {
			err = fmt.Errorf("失败路径返回了地址: %v", got)
		}
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("解析未在三秒保护上限内结束")
		return nil
	}
}

// TestI804LocalTimeout 静默上游受解析器整体时限限制。
func TestI804LocalTimeout(t *testing.T) {
	s := newLocalDNS(t, "silent")
	r := NewResolver(s.conn.LocalAddr().String(), 100*time.Millisecond)
	start := time.Now()
	err := boundedResolve(t, r, context.Background(), "i804-timeout.example.test.")
	var de *net.DNSError
	if !errors.As(err, &de) || !de.IsTimeout {
		t.Fatalf("必须返回可识别超时: %v", err)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Error("未等待已配置解析时限")
	}
	s.checkQueries(t, "i804-timeout.example.test.")
}

// TestI804LocalCallerDeadline 调用方期限应早于解析器较长的时限生效。
func TestI804LocalCallerDeadline(t *testing.T) {
	s := newLocalDNS(t, "silent")
	r := NewResolver(s.conn.LocalAddr().String(), 10*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := boundedResolve(t, r, ctx, "i804-deadline.example.test.")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("调用者期限未传播: %v", err)
	}
	s.checkQueries(t, "i804-deadline.example.test.")
}

// TestI804LocalCancelInFlight 报文已发出后取消调用方，不使用固定 sleep 猜测在途状态。
func TestI804LocalCancelInFlight(t *testing.T) {
	s := newLocalDNS(t, "silent")
	r := NewResolver(s.conn.LocalAddr().String(), 10*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := r.Resolve(ctx, "i804-cancel.example.test."); done <- err }()
	select {
	case <-s.seen:
	case <-time.After(3 * time.Second):
		t.Fatal("未开始 DNS 查询")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("取消未传播: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("取消未有界返回")
	}
}
