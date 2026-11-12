package syncer

import (
	"net/netip"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 本文件是 Build8 Step 7 的纯预检决策回归：只验证输入完整性、正向命中与
// 准入优先级，不访问 SQLite、网络或 DNS 熔断器。

func TestFirstOverlap(t *testing.T) {
	v4 := func(host, addr string) DNSAddress {
		return DNSAddress{Host: host, Addr: netip.MustParseAddr(addr)}
	}
	edge := func(zone, prefix string) EdgeOnePrefix {
		return EdgeOnePrefix{ZoneID: zone, Prefix: netip.MustParsePrefix(prefix)}
	}

	t.Run("IPv4 主机落入 /24", func(t *testing.T) {
		host, network, ok := FirstOverlap(
			[]DNSAddress{v4("home.example", "203.0.113.7")},
			[]EdgeOnePrefix{edge("zone-a", "203.0.113.0/24")},
		)
		if !ok || host.Addr.String() != "203.0.113.7" || network.Prefix.String() != "203.0.113.0/24" || network.ZoneID != "zone-a" {
			t.Fatalf("overlap = %+v %+v %v", host, network, ok)
		}
	})

	t.Run("IPv4-mapped 使用 Unmap", func(t *testing.T) {
		host, network, ok := FirstOverlap(
			[]DNSAddress{v4("mapped.example", "::ffff:203.0.113.7")},
			[]EdgeOnePrefix{edge("zone-a", "203.0.113.0/24")},
		)
		if !ok {
			t.Fatal("mapped IPv4 应命中 IPv4 网段")
		}
		if !host.Addr.Is4() || host.Addr.String() != "203.0.113.7" {
			t.Fatalf("返回地址未 Unmap: %v", host.Addr)
		}
		if network.Prefix.String() != "203.0.113.0/24" {
			t.Fatalf("网段 = %v", network.Prefix)
		}
	})

	t.Run("IPv6 主机落入 /64", func(t *testing.T) {
		host, network, ok := FirstOverlap(
			[]DNSAddress{v4("v6.example", "2001:db8::1")},
			[]EdgeOnePrefix{edge("zone-a", "2001:db8::/64")},
		)
		if !ok || host.Addr.String() != "2001:db8::1" || network.Prefix.String() != "2001:db8::/64" {
			t.Fatalf("overlap = %+v %+v %v", host, network, ok)
		}
	})

	t.Run("地址族不匹配不命中", func(t *testing.T) {
		cases := []struct {
			name string
			host DNSAddress
			edge EdgeOnePrefix
		}{
			{"IPv4 对 IPv6", v4("v4.example", "203.0.113.7"), edge("zone-a", "2001:db8::/64")},
			{"IPv6 对 IPv4", v4("v6.example", "2001:db8::1"), edge("zone-a", "203.0.113.0/24")},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if _, _, ok := FirstOverlap([]DNSAddress{tc.host}, []EdgeOnePrefix{tc.edge}); ok {
					t.Fatal("不同地址族不应命中")
				}
			})
		}
	})

	t.Run("边界与网段外", func(t *testing.T) {
		if _, _, ok := FirstOverlap(
			[]DNSAddress{v4("last.example", "203.0.113.255")},
			[]EdgeOnePrefix{edge("zone-a", "203.0.113.0/24")},
		); !ok {
			t.Fatal("/24 最后地址应命中")
		}
		if _, _, ok := FirstOverlap(
			[]DNSAddress{v4("next.example", "203.0.114.1")},
			[]EdgeOnePrefix{edge("zone-a", "203.0.113.0/24")},
		); ok {
			t.Fatal("网段外不应命中")
		}
	})

	t.Run("稳定排序返回首个证据", func(t *testing.T) {
		host, network, ok := FirstOverlap(
			[]DNSAddress{v4("b.example", "203.0.113.9"), v4("a.example", "203.0.113.8")},
			[]EdgeOnePrefix{edge("zone-b", "203.0.113.0/24"), edge("zone-a", "203.0.113.0/24")},
		)
		if !ok || host.Host != "a.example" || network.ZoneID != "zone-a" {
			t.Fatalf("稳定证据 = %+v %+v %v", host, network, ok)
		}
	})

	t.Run("非法项跳过且不 panic", func(t *testing.T) {
		if _, _, ok := FirstOverlap(
			[]DNSAddress{{Host: "bad", Addr: netip.Addr{}}},
			[]EdgeOnePrefix{{ZoneID: "zone-bad", Prefix: netip.Prefix{}}},
		); ok {
			t.Fatal("非法输入不应命中")
		}
	})

	t.Run("输入切片不被修改", func(t *testing.T) {
		dns := []DNSAddress{v4("b.example", "203.0.113.9"), v4("a.example", "203.0.113.8")}
		edge := []EdgeOnePrefix{edge("zone-b", "203.0.113.0/24"), edge("zone-a", "203.0.113.0/24")}
		_, _, _ = FirstOverlap(dns, edge)
		if dns[0].Host != "b.example" || edge[0].ZoneID != "zone-b" {
			t.Fatalf("输入顺序被修改: %+v %+v", dns, edge)
		}
	})
}

func TestDecideAdmission(t *testing.T) {
	cases := []struct {
		name string
		in   Preflight
		want Admission
	}{
		{
			name: "两项均关闭",
			in:   Preflight{},
			want: Admission{},
		},
		{
			name: "仅 DNS 启用",
			in:   Preflight{DNSEnabled: true},
			want: Admission{DNS: true},
		},
		{
			name: "仅 EdgeOne 启用且完整",
			in:   Preflight{EdgeOneEnabled: true, EdgeOneComplete: true},
			want: Admission{EdgeOne: true},
		},
		{
			name: "仅 EdgeOne 启用但输入不足",
			in:   Preflight{EdgeOneEnabled: true},
			want: Admission{},
		},
		{
			name: "双开正向命中",
			in:   Preflight{DNSEnabled: true, EdgeOneEnabled: true, DNSComplete: true, EdgeOneComplete: true, Overlap: true},
			want: Admission{KeepConflict: true},
		},
		{
			name: "部分成功仍命中",
			in:   Preflight{DNSEnabled: true, EdgeOneEnabled: true, DNSComplete: false, EdgeOneComplete: false, Overlap: true},
			want: Admission{KeepConflict: true},
		},
		{
			name: "已知冲突且数据不足",
			in:   Preflight{DNSEnabled: true, EdgeOneEnabled: true, DNSComplete: true, EdgeOneComplete: false, KnownConflict: true},
			want: Admission{KeepConflict: true},
		},
		{
			name: "已知冲突但完整无重叠",
			in:   Preflight{DNSEnabled: true, EdgeOneEnabled: true, DNSComplete: true, EdgeOneComplete: true, KnownConflict: true},
			want: Admission{DNS: true, EdgeOne: true},
		},
		{
			name: "无已知冲突 DNS 不足",
			in:   Preflight{DNSEnabled: true, EdgeOneEnabled: true, DNSComplete: false, EdgeOneComplete: true},
			want: Admission{EdgeOne: true},
		},
		{
			name: "无已知冲突 EdgeOne 不足",
			in:   Preflight{DNSEnabled: true, EdgeOneEnabled: true, DNSComplete: true, EdgeOneComplete: false},
			want: Admission{DNS: true},
		},
		{
			name: "无已知冲突两侧均不足",
			in:   Preflight{DNSEnabled: true, EdgeOneEnabled: true},
			want: Admission{},
		},
		{
			name: "双开完整无重叠",
			in:   Preflight{DNSEnabled: true, EdgeOneEnabled: true, DNSComplete: true, EdgeOneComplete: true},
			want: Admission{DNS: true, EdgeOne: true},
		},
		{
			name: "单项继续保留已知冲突证据",
			in:   Preflight{DNSEnabled: true, KnownConflict: true},
			want: Admission{DNS: true, KeepConflict: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideAdmission(tc.in)
			if got != tc.want {
				t.Fatalf("Admission = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestFirstOverlapIncludesNextVersionPrefix 只存在于 Next 的被跟踪网段也必须参与全局判定。
func TestFirstOverlapIncludesNextVersionPrefix(t *testing.T) {
	next := provider.OriginACLVersion{
		Version:  "v2",
		Prefixes: []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")},
	}
	snapshot := provider.OriginACLSnapshot{
		ZoneID:  "zone-a",
		Current: provider.OriginACLVersion{Version: "v1", Prefixes: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}},
		Next:    &next,
	}
	edges := make([]EdgeOnePrefix, 0, 2)
	for _, prefix := range snapshot.AllPrefixes() {
		edges = append(edges, EdgeOnePrefix{ZoneID: "zone-a", Prefix: prefix})
	}
	host, network, ok := FirstOverlap(
		[]DNSAddress{{Host: "next.example", Addr: netip.MustParseAddr("198.51.100.7")}},
		edges,
	)
	if !ok || host.Host != "next.example" || network.Prefix.String() != "198.51.100.0/24" {
		t.Fatalf("Next 版本网段未参与判定: %+v %+v %v", host, network, ok)
	}
}
