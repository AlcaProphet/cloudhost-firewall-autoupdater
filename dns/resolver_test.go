package dns

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestResolve_IPLiteral 验证地址字面量不触发 DNS 报文，也不依赖本机 hosts。
func TestResolve_IPLiteral(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1"} {
		t.Run(host, func(t *testing.T) {
			s := newLocalDNS(t, "silent")
			r := NewResolver(s.conn.LocalAddr().String(), time.Second)
			got, err := r.Resolve(context.Background(), host)
			if err != nil || len(got) != 1 {
				t.Fatalf("地址字面量解析结果=%v 错误=%v", got, err)
			}
			if !got[0].IP.Equal(net.ParseIP(host)) || got[0].IsIPv6 != (host == "::1") {
				t.Fatalf("地址或地址族错误: %v", got)
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if len(s.queries) != 0 {
				t.Fatalf("地址字面量不应发出查询: %v", s.queries)
			}
		})
	}
}

// TestNewResolver_PortAppend 观察实际 Dial 上游，只建立回环 UDP socket，不发送报文。
func TestNewResolver_PortAppend(t *testing.T) {
	for _, addr := range []string{"127.0.0.1", "127.0.0.1:5353"} {
		r := NewResolver(addr, time.Second)
		c, err := r.resolver.Dial(context.Background(), "udp", "ignored.example:53")
		if err != nil {
			t.Fatal(err)
		}
		got := c.RemoteAddr().String()
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		want := addr
		if addr == "127.0.0.1" {
			want = "127.0.0.1:53"
		}
		if got != want {
			t.Errorf("Dial 上游=%s 期望=%s", got, want)
		}
	}
}

func TestResolvedIP_CIDR(t *testing.T) {
	tests := []struct {
		name string
		ip   ResolvedIP
		want string
	}{
		{"IPv4", ResolvedIP{IP: []byte{1, 2, 3, 4}, IsIPv6: false}, "1.2.3.4/32"},
		{"IPv6", ResolvedIP{IP: []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, IsIPv6: true}, "2001:db8::1/128"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ip.CIDR()
			if got != tt.want {
				t.Errorf("CIDR() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHasPort(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"223.5.5.5:53", true},
		{"223.5.5.5", false},
		{"[::1]:53", true},
		{"::1", false},
	}
	for _, tt := range tests {
		got := hasPort(tt.addr)
		if got != tt.want {
			t.Errorf("hasPort(%q) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}
