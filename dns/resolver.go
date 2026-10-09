package dns

import (
	"context"
	"fmt"
	"net"
	"time"
)

// ResolvedIP 解析结果
type ResolvedIP struct {
	IP     net.IP
	IsIPv6 bool
}

// CIDR 返回带掩码的 CIDR 格式
func (r ResolvedIP) CIDR() string {
	if r.IsIPv6 {
		return r.IP.String() + "/128"
	}
	return r.IP.String() + "/32"
}

// Resolver 自定义 DNS 解析器
type Resolver struct {
	resolver *net.Resolver
	timeout  time.Duration
}

// NewResolver 创建解析器
// dnsAddr 格式: "8.8.8.8:53" 或 "8.8.8.8"（默认补 :53）
// IPv6 使用 "[::1]" 或 "[::1]:53"；调用方通过配置校验保证输入格式合法。
func NewResolver(dnsAddr string, timeout time.Duration) *Resolver {
	if !hasPort(dnsAddr) {
		dnsAddr = dnsAddr + ":53"
	}
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: timeout}
			return d.DialContext(ctx, "udp", dnsAddr)
		},
	}
	return &Resolver{resolver: r, timeout: timeout}
}

// Resolve 同时解析 A 和 AAAA 记录
func (r *Resolver) Resolve(ctx context.Context, host string) ([]ResolvedIP, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var results []ResolvedIP

	// A + AAAA 记录（LookupIPAddr 同时返回 IPv4 和 IPv6）
	addrs, err := r.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("DNS 解析失败 %s: %w", host, err)
	}
	for _, addr := range addrs {
		isV6 := addr.IP.To4() == nil
		results = append(results, ResolvedIP{IP: addr.IP, IsIPv6: isV6})
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("DNS 解析无结果: %s", host)
	}
	return results, nil
}

func hasPort(addr string) bool {
	_, _, err := net.SplitHostPort(addr)
	return err == nil
}
