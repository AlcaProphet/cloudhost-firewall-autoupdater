package syncer

import (
	"net/netip"
	"sort"
)

// 本文件是 Build8 Step 7 的纯预检决策层。
//
// 只做 CIDR 包含比较与准入判定：不读取运行时状态、不写 SQLite、不访问网络、
// 不修改 DNS 熔断器。进程内/SQLite 证据与自动恢复状态在 conflict_state.go。
//
// 两个独立结果：
//   - 输入完整性（DNSComplete / EdgeOneComplete）只描述本轮能否参与判定；
//   - 正向命中（Overlap）来自已取得的地址/网段，任何已证明的重叠优先于数据不足。

// DNSAddress 是一个已解析的域名地址；比较前会执行 Unmap。
type DNSAddress struct {
	Host string
	Addr netip.Addr
}

// EdgeOnePrefix 是一个站点回源网段；Prefix 保留网段长度。
type EdgeOnePrefix struct {
	ZoneID string
	Prefix netip.Prefix
}

// AddressOverlap 是一次已证明的 DNS 地址落入 EdgeOne 网段的证据。
type AddressOverlap struct {
	DNS     DNSAddress
	EdgeOne EdgeOnePrefix
}

// Preflight 是一次双开全局判定的纯输入。
//
// DNS/EdgeOne 只代表本轮已取得的可用样本；完整性与正向命中分开传递。
type Preflight struct {
	DNSEnabled      bool
	EdgeOneEnabled  bool
	DNSComplete     bool
	EdgeOneComplete bool
	KnownConflict   bool
	Overlap         bool
	// DNS/EdgeOne 是本轮已取得且可参与比较的样本；不完整时也可以携带部分样本，
	// 从而让已证明的重叠优先于查询失败。
	DNS     []DNSAddress
	EdgeOne []EdgeOnePrefix
}

// Admission 是数据层准入结果；最终写入还要满足总暂停、到期来源和目标覆盖门。
type Admission struct {
	DNS          bool
	EdgeOne      bool
	KeepConflict bool
}

// FirstOverlap 返回稳定排序后的首个 DNS 地址落入 EdgeOne 网段的证据。
//
// DNS 地址先 Unmap，保证 IPv4-mapped 表示与既有云快照的 IPv4 身份一致；
// EdgeOne 前缀按 Masked 规范化。输入切片不被修改。
func FirstOverlap(dns []DNSAddress, edge []EdgeOnePrefix) (DNSAddress, EdgeOnePrefix, bool) {
	hosts := append([]DNSAddress(nil), dns...)
	sort.SliceStable(hosts, func(i, j int) bool {
		if hosts[i].Host != hosts[j].Host {
			return hosts[i].Host < hosts[j].Host
		}
		return hosts[i].Addr.String() < hosts[j].Addr.String()
	})
	networks := append([]EdgeOnePrefix(nil), edge...)
	sort.SliceStable(networks, func(i, j int) bool {
		if networks[i].ZoneID != networks[j].ZoneID {
			return networks[i].ZoneID < networks[j].ZoneID
		}
		return networks[i].Prefix.String() < networks[j].Prefix.String()
	})

	for _, host := range hosts {
		if !host.Addr.IsValid() {
			continue
		}
		address := host.Addr.Unmap()
		for _, network := range networks {
			if !network.Prefix.IsValid() {
				continue
			}
			prefix := network.Prefix.Masked()
			if prefix.Contains(address) {
				host.Addr = address
				return host, EdgeOnePrefix{ZoneID: network.ZoneID, Prefix: prefix}, true
			}
		}
	}
	return DNSAddress{}, EdgeOnePrefix{}, false
}

// allDNSAddressesValid 判断本轮 DNS 样本是否都是有效地址。
//
// 不完整轮次可以携带部分样本；但只要样本中存在非法地址，就不能把该来源标为完整。
func allDNSAddressesValid(addresses []DNSAddress) bool {
	for _, address := range addresses {
		if !address.Addr.IsValid() {
			return false
		}
	}
	return true
}

// allEdgeOnePrefixesValid 判断本轮 EdgeOne 样本是否都是有效网段。
func allEdgeOnePrefixesValid(prefixes []EdgeOnePrefix) bool {
	for _, prefix := range prefixes {
		if !prefix.Prefix.IsValid() {
			return false
		}
	}
	return true
}

// DecideAdmission 按 Design6 §7.4/§8.3 的决策优先级计算数据层准入。
//
// 单开/双关不做全局判定；双开时任何正向重叠立即阻断两侧并保留冲突，
// 已知冲突在数据不足时继续阻断；只有两侧完整且无重叠才放行（调用方需先提交解除）。
func DecideAdmission(in Preflight) Admission {
	result := Admission{
		DNS:          in.DNSEnabled,
		EdgeOne:      in.EdgeOneEnabled && in.EdgeOneComplete,
		KeepConflict: in.KnownConflict,
	}
	if !in.DNSEnabled || !in.EdgeOneEnabled {
		// 单项启用只停止对应来源；DNS-only 的错误/重试策略继续由现有目标链处理。
		return result
	}
	if in.Overlap || (in.KnownConflict && !(in.DNSComplete && in.EdgeOneComplete)) {
		return Admission{KeepConflict: true}
	}
	return Admission{DNS: in.DNSComplete, EdgeOne: in.EdgeOneComplete}
}

// PreflightState 是供状态展示/日志使用的稳定结论（Design6 §7.6）。
type PreflightState string

const (
	// PreflightStateSingleSource 未双开，或两项均关闭。
	PreflightStateSingleSource PreflightState = "single_source"
	// PreflightStateClear 双开、两侧完整且无重叠。
	PreflightStateClear PreflightState = "clear"
	// PreflightStateOverlap 已证明重叠，或已查实冲突尚未解除。
	PreflightStateOverlap PreflightState = "overlap"
	// PreflightStateDataUnavailable 双开但没有完整输入，且没有已知冲突。
	PreflightStateDataUnavailable PreflightState = "data_unavailable"
)

// PreflightOutcome 是一次正式预检的完整结论。
type PreflightOutcome struct {
	State           PreflightState
	Admission       Admission
	DNSComplete     bool
	EdgeOneComplete bool
	KnownConflict   bool
	Overlap         *AddressOverlap
}
