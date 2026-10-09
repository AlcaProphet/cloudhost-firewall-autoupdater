package provider

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/portconv"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/tag"
)

// 本文件是 Issue7 Step 1 的目标级纯规划器与 canonical 功能身份层。
//
// 固定约束（Issue7 §2、§4）：
//   - 全仓只有这一个 canonical key 层，RuleInfo（云端回读）与 RuleAction（期望写入）
//     必须走同一入口，禁止各写一套归一化；
//   - 功能 key 固定为 address-family + canonical CIDR + canonical protocol +
//     canonical port expression + action；TAG/comment/description/域名/本地规则 ID/
//     云端 RuleID/PolicyIndex/快照版本/Provider 名称一律不进入 key；
//   - 规划器是纯函数：不访问网络、数据库、时钟、日志或事件总线；快照由调用方取得后传入。

// ErrSnapshotIncomplete 云端快照无法证明完整（分页失败、token 不推进、字段不足以安全判定）。
//
// 调用方必须把该错误视为「本 attempt 零删除」：不完整快照绝不产生删除计划。
var ErrSnapshotIncomplete = errors.New("云端快照不完整")

// AddressFamily 地址族（功能 key 的第一维）。
type AddressFamily string

const (
	AddressIPv4 AddressFamily = "ipv4"
	AddressIPv6 AddressFamily = "ipv6"
)

// FunctionalKey 功能身份：目标级期望与云端回读比较的唯一依据。
type FunctionalKey struct {
	Family   AddressFamily `json:"family"`
	CIDR     string        `json:"cidr"`
	Protocol string        `json:"protocol"`
	Port     string        `json:"port"`
	Action   string        `json:"action"`
}

// String 返回稳定可比的字符串形态（用于日志与稳定排序，不用于展示）。
func (k FunctionalKey) String() string {
	return string(k.Family) + "|" + k.CIDR + "|" + k.Protocol + "|" + k.Port + "|" + k.Action
}

// 规划器稳定原因码（Issue7 §4.3）。
const (
	// IssueDNSFailed 适用域名解析失败
	IssueDNSFailed = "dns_failed"
	// IssueDNSEmpty 适用域名解析结果为空
	IssueDNSEmpty = "dns_empty"
	// IssueSnapshotIncomplete 快照无法证明完整
	IssueSnapshotIncomplete = "snapshot_incomplete"
	// IssueUnsupportedIPv6 平台不支持 IPv6（SWAS）
	IssueUnsupportedIPv6 = "unsupported_ipv6"
	// IssueUnsupportedICMPv6 平台不支持 ICMPv6（ECS）
	IssueUnsupportedICMPv6 = "unsupported_icmpv6"
	// IssueUnsupportedAction 平台不支持该 action（SWAS 不支持 DROP）
	IssueUnsupportedAction = "unsupported_action"
	// IssueOwnedLocatorMissing 陈旧 Owned 规则缺稳定删除定位
	IssueOwnedLocatorMissing = "owned_locator_missing"
	// IssueOwnedKeyAmbiguous 同 key 不能唯一定位（Lighthouse 重复/Owned 与 External 并存）
	IssueOwnedKeyAmbiguous = "owned_key_ambiguous"
	// IssueSnapshotRuleInvalid 云端规则缺 family/CIDR/协议/端口/action 等必要字段
	IssueSnapshotRuleInvalid = "snapshot_rule_invalid"
	// IssueOppositeAction 相同 family/CIDR/protocol/port 同时存在 ACCEPT 与 DROP
	IssueOppositeAction = "opposite_action"
	// IssueDesiredEmpty 完整期望集为空：不授权清空 TAG 命名空间
	IssueDesiredEmpty = "desired_empty"
	// IssueCoverageNotReady 新快照未能证明覆盖全部可实施期望
	IssueCoverageNotReady = "coverage_not_ready"
	// IssueCleanupDisabled 本 attempt 未启用自动删除（Step 2 使用；Step 3 起按平台放开）
	IssueCleanupDisabled = "cleanup_disabled"
)

// RuleSnapshot 一次云端读取的不可拆分结果：规则 + 版本。
//
// 完整性由 GetSnapshot 成功返回隐含保证：任一页失败、token 不推进或字段不足以
// 安全判定时必须返回 error，绝不返回半截 Rules。
// Lighthouse = FirewallVersion、CVM = Version；阿里云为空字符串。
type RuleSnapshot struct {
	Rules    []config.RuleInfo
	Revision string
}

// PlannedRule 目标级期望功能的一项（canonical 展开后的最小单元）。
type PlannedRule struct {
	Key         FunctionalKey `json:"key"`
	Comment     string        `json:"comment"`
	Description string        `json:"description"`
	RuleIDs     []int         `json:"rule_ids"`
	Domains     []string      `json:"domains"`
	// Implementable 表示当前平台能否表达该项；false 时同时出现在 Unsupported
	Implementable bool `json:"implementable"`
	// Action 是该项的云端线格式（端口已按平台转换），仅内部使用，不参与 JSON
	Action config.RuleAction `json:"-"`
}

// PlanMatch 一条期望功能被云端规则精确满足的记录。
type PlanMatch struct {
	Key         FunctionalKey     `json:"key"`
	Description string            `json:"description"`
	Rules       []config.RuleInfo `json:"rules"`
}

// PlanIssue 一条稳定、可分类、可展示的规划问题。
type PlanIssue struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Key     *FunctionalKey `json:"key,omitempty"`
	Domain  string         `json:"domain,omitempty"`
	RuleID  int            `json:"rule_id,omitempty"`
}

// TargetPlan 一个目标的完整规划结果；Dry Run 与正式同步必须消费同一份结果。
type TargetPlan struct {
	Desired             []PlannedRule       `json:"desired"`
	SatisfiedByOwned    []PlanMatch         `json:"satisfied_by_owned"`
	SatisfiedByExternal []PlanMatch         `json:"satisfied_by_external"`
	ToAdd               []config.RuleAction `json:"to_add"`
	CleanupCandidates   []config.RuleInfo   `json:"cleanup_candidates"`
	// CleanupDeletable 是本快照下**允许自动删除**的候选：清理安全门全部满足、
	// 且删除定位完整无歧义。CleanupCandidates 始终是全部候选（预览），二者不可混用。
	CleanupDeletable []config.RuleInfo `json:"cleanup_deletable"`
	CleanupDeferred  []PlanIssue       `json:"cleanup_deferred"`
	DNSErrors        []PlanIssue       `json:"dns_errors"`
	Unsupported      []PlanIssue       `json:"unsupported"`
	Conflicts        []PlanIssue       `json:"conflicts"`
	// CoverageReady 表示该快照（S0 或 S1）是否已精确覆盖全部 Implementable 期望；
	// 它不表示清理门已打开。没有任何可实施期望时视为 false（无可证明对象）。
	CoverageReady bool `json:"coverage_ready"`
}

// TargetPlanInput 纯规划器输入。
type TargetPlanInput struct {
	CloudType config.CloudType
	Tag       string
	// Rules 必须已按本地 rule ID 升序排列（规划器内部再排一次以自证）
	Rules []config.DomainRule
	// Resolved 以本地 rule ID 为键；同一 host 的结果由调度层复用
	Resolved map[int][]dns.ResolvedIP
	// DNSErrors 以本地 rule ID 为键，值为安全可展示的稳定错误
	DNSErrors map[int]string
	Snapshot  RuleSnapshot
	// AddStateUnknown 表示调用方尚未确认 Add 提交状态，置 true 时冻结覆盖与清理。
	// 当前正式目标链对普通 Add 错误早退，幂等错误仍须经 S1 覆盖验证，故无需置 true。
	AddStateUnknown bool
}

// Capability 平台能力矩阵：唯一的能力判定源（期望展开与 CreateRules 的防御检查均须遵循本矩阵）。
type Capability struct {
	IPv6   bool // 是否支持 IPv6 地址族
	ICMPv6 bool // 是否支持 IPv6 + ICMP
	TCPUDP bool // 是否原生支持 TCP+UDP 单条规则
	Drop   bool // 是否支持 DROP 动作
}

// Capabilities 返回某云产品的能力矩阵。
//
// 依据（PlatformAPIDocs）：
//   - SWAS：ListFirewallRules 无 IPv6 字段，CreateFirewallRules 无 Policy 字段，原生支持 TCP+UDP；
//   - ECS：AuthorizeSecurityGroup 的 IpProtocol 取值不含 ICMPv6（RevokeSecurityGroup 才支持 ICMPv6）；
//   - Lighthouse/CVM：原生支持 IPv6、ICMPv6 与 DROP，但 TCP+UDP 需拆分为两条。
//
// 未知云类型按最宽松处理：BuildRuntimeState 已拒绝未注册云类型，规划器不会在生产中看到它们。
func Capabilities(ct config.CloudType) Capability {
	switch ct {
	case config.CloudAliSWAS:
		return Capability{IPv6: false, ICMPv6: false, TCPUDP: true, Drop: false}
	case config.CloudAliECS:
		return Capability{IPv6: true, ICMPv6: false, TCPUDP: false, Drop: true}
	default:
		return Capability{IPv6: true, ICMPv6: true, TCPUDP: false, Drop: true}
	}
}

// ExpandPorts 把统一端口表达式展开为该云平台的线格式列表（旧逐规则写入路径的唯一样本）。
//
// 语义与各 Provider 既有 ConvertPorts 完全一致：
//   - Lighthouse：单端口/ALL 原样；多端口在总长 ≤64 时合并为逗号串，超限按 ≤64 拆条；
//   - SWAS/ECS：每个端口转斜杠格式（8000-8010 → 8000/8010，ALL → -1/-1）；
//   - CVM：逗号拆分后原样返回（CVM 不接受逗号）。
//
// 注意粒度：Lighthouse 的「多端口合并成一条逗号规则」只属于**旧逐规则写入路径**。
// 目标级规划器不使用本函数的合并结果，而是用 expandPlannedPorts 对每个端口项单独取
// 线格式，保证 Desired/ToAdd 的最小单元恒为单个端口项（Issue7 §2.5 用户裁决）。
func ExpandPorts(ct config.CloudType, port string) []string {
	ports := portconv.Parse(port)
	switch ct {
	case config.CloudTCLighthouse:
		if len(ports) == 1 {
			return ports
		}
		if joined := strings.Join(ports, ","); len(joined) <= 64 {
			return []string{joined}
		}
		var result []string
		current := ""
		for _, p := range ports {
			switch {
			case current == "":
				current = p
			case len(current)+1+len(p) <= 64:
				current += "," + p
			default:
				result = append(result, current)
				current = p
			}
		}
		if current != "" {
			result = append(result, current)
		}
		return result
	case config.CloudAliSWAS, config.CloudAliECS:
		result := make([]string, 0, len(ports))
		for _, p := range ports {
			result = append(result, portconv.ToSlash(p))
		}
		return result
	default:
		return ports
	}
}

// expandPlannedPorts 把统一端口表达式展开为「每个端口项一条」的线格式列表（规划器专用）。
//
// 与 ExpandPorts 的唯一区别是粒度：ExpandPorts 是旧逐规则写入路径的线格式（Lighthouse 会把
// 多个端口合并成一个逗号串以减少规则条数），而本函数先按 portconv.Parse 的业务语义拆分，
// 再对每个端口项各自取线格式，因此 Lighthouse 的 80,443 会落成 80 与 443 两个独立功能，
// 新建时下发两条单端口规则；已存在的合并规则因展开后多个 key 均被覆盖而不会被重建或删除
// （Issue7 §2.5 的 2026-09-29 用户裁决）。
func expandPlannedPorts(ct config.CloudType, port string) []string {
	items := portconv.Parse(port)
	if len(items) == 0 {
		return []string{port}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		wire := ExpandPorts(ct, item)
		if len(wire) == 0 {
			out = append(out, item)
			continue
		}
		out = append(out, wire...)
	}
	return out
}

// RenderDescription 渲染最终写入云端的描述，并按平台长度上限截断可读部分。
//
// 截断只发生在这一个渲染层，绝不反向影响功能 key（Issue7 §2.3、§6.1）。
func RenderDescription(ct config.CloudType, tagStr, comment string) string {
	return TruncateDescription(ct, tag.Format(tagStr, comment))
}

// TruncateDescription 按云厂商描述字段长度上限截断（全仓唯一实现，按 rune 截断以保护 UTF-8）。
//
// 截断结果始终保持完整 "[TAG]" 授权前缀：若截断点会落在右括号之前，则保留完整前缀
// （宁可让云 API 因超长报错，也绝不截成另一个命名空间）。当前 TAG 上限 48 与平台
// 上限 50/64 不会同时触发该分支。
func TruncateDescription(ct config.CloudType, desc string) string {
	maxLen := 0
	switch ct {
	case config.CloudTCLighthouse:
		maxLen = 64 // FirewallRuleDescription ≤ 64（TencentLighthouseAPIGuide/添加防火墙规则.md）
	case config.CloudAliSWAS:
		maxLen = 50 // Remark ≤ 50：仓内 API 文档查无出处，见 ProdTestList.md PT-B7-09
	default:
		return desc
	}
	runes := []rune(desc)
	if len(runes) <= maxLen {
		return desc
	}
	if idx := strings.Index(desc, "]"); idx >= 0 && len([]rune(desc[:idx+1])) > maxLen {
		return desc[:idx+1]
	}
	return string(runes[:maxLen])
}

// canonicalProtocol 协议归一化：大写；ICMPv6/ICMPV6 与 ICMP 在 key 中统一为 ICMP（由 family 区分）。
func canonicalProtocol(protocol string) string {
	p := strings.ToUpper(strings.TrimSpace(protocol))
	if p == "ICMPV6" {
		return "ICMP"
	}
	return p
}

// canonicalPortExpression 单个端口项的 canonical 形态：
// 斜杠形态与短横形态等价（443/443 → 443、8000/8010 → 8000-8010），
// ICMP/ALL 协议与非协议端口一律折叠为 ALL。
func canonicalPortExpression(protocol, port string) string {
	if protocol == "ICMP" || protocol == "ALL" {
		return "ALL"
	}
	p := strings.ToUpper(strings.TrimSpace(port))
	switch p {
	case "", "ALL", "-1/-1", "-1":
		return "ALL"
	}
	if start, end, ok := strings.Cut(p, "/"); ok {
		if start == end {
			return start
		}
		return start + "-" + end
	}
	return p
}

// CanonicalCIDR 返回掩码后的规范 CIDR 字符串；非法输入返回 ok=false。
func CanonicalCIDR(cidr string) (string, bool) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	if err != nil {
		return "", false
	}
	return prefix.Masked().String(), true
}

// ruleFamilyCIDR 解析云端规则的地址族与 canonical CIDR。
//
// 两个字段同时有值或都为空视为快照冲突（ok=false），该规则不进入可删除集合。
func ruleFamilyCIDR(cidrBlock, ipv6CidrBlock string) (AddressFamily, string, bool) {
	hasV4 := strings.TrimSpace(cidrBlock) != ""
	hasV6 := strings.TrimSpace(ipv6CidrBlock) != ""
	if hasV4 == hasV6 {
		return "", "", false
	}
	if hasV6 {
		cidr, ok := CanonicalCIDR(ipv6CidrBlock)
		return AddressIPv6, cidr, ok
	}
	cidr, ok := CanonicalCIDR(cidrBlock)
	return AddressIPv4, cidr, ok
}

// buildKeys 由地址族/CIDR/协议/端口字段/action 构造 canonical key 集合（唯一入口）。
func buildKeys(family AddressFamily, cidr, protocol, portField, action string) ([]FunctionalKey, bool) {
	proto := canonicalProtocol(protocol)
	if proto == "" {
		return nil, false
	}
	act := strings.ToUpper(strings.TrimSpace(action))
	if act != "ACCEPT" && act != "DROP" {
		return nil, false
	}
	ports := splitPortField(portField)
	if len(ports) == 0 {
		if proto == "ICMP" || proto == "ALL" {
			ports = []string{"ALL"}
		} else {
			return nil, false
		}
	}
	keys := make([]FunctionalKey, 0, len(ports))
	for _, p := range ports {
		keys = append(keys, FunctionalKey{
			Family:   family,
			CIDR:     cidr,
			Protocol: proto,
			Port:     canonicalPortExpression(proto, p),
			Action:   act,
		})
	}
	sortRuleKeys(keys)
	return keys, true
}

// SnapshotRuleKeys 把一条云端规则展开为其 canonical 功能 key 集合。
//
// 一条规则可能对应多个 key（如 Lighthouse 逗号端口 "80,443"）。
// ok=false 表示字段不足以安全判定（地址族字段同时有值或都为空、协议/端口/action 缺失、
// CIDR 非法、TCP/UDP 缺端口），该规则不得进入删除集合，并使目标清理门冻结。
func SnapshotRuleKeys(r config.RuleInfo) ([]FunctionalKey, bool) {
	family, cidr, ok := ruleFamilyCIDR(r.CidrBlock, r.Ipv6CidrBlock)
	if !ok {
		return nil, false
	}
	return buildKeys(family, cidr, r.Protocol, r.Port, r.Action)
}

// PlannedActionKeys 把一条期望规则展开为其 canonical 功能 key 集合（与 SnapshotRuleKeys 同一层）。
func PlannedActionKeys(a config.RuleAction) []FunctionalKey {
	family, cidr, ok := ruleFamilyCIDR(a.CidrBlock, a.Ipv6CidrBlock)
	if !ok {
		return nil
	}
	keys, ok := buildKeys(family, cidr, a.Protocol, a.Port, a.Action)
	if !ok {
		return nil
	}
	return keys
}

// unsupportedKeyReason 返回该期望项在当前平台不可实施的原因码；可实施时返回空串。
//
// 每个展开项只返回一个原因码（family 限制优先于 action 限制），避免同一项重复报因。
func unsupportedKeyReason(caps Capability, family AddressFamily, protocol, action string) string {
	if family == AddressIPv6 && !caps.IPv6 {
		return IssueUnsupportedIPv6
	}
	if family == AddressIPv6 && protocol == "ICMP" && !caps.ICMPv6 {
		return IssueUnsupportedICMPv6
	}
	if action == "DROP" && !caps.Drop {
		return IssueUnsupportedAction
	}
	return ""
}

func unsupportedMessage(code string) string {
	switch code {
	case IssueUnsupportedIPv6:
		return "阿里云轻量云（SWAS）不支持 IPv6 防火墙规则，该期望不会创建"
	case IssueUnsupportedICMPv6:
		return "阿里云 ECS 不支持 ICMPv6 入站规则（AuthorizeSecurityGroup 无 ICMPv6），该期望不会创建"
	case IssueUnsupportedAction:
		return "阿里云轻量云（SWAS）不支持 DROP 规则（CreateFirewallRules 无 Policy 字段），该期望不会创建"
	default:
		return "当前平台无法实施该期望功能"
	}
}

// PlanTarget 目标级纯规划：输入完整期望与一次云端快照，输出完整计划。
//
// 本函数不访问网络、数据库、时钟、日志或事件总线，可被 Dry Run 与正式同步共用。
func PlanTarget(in TargetPlanInput) TargetPlan {
	caps := Capabilities(in.CloudType)
	plan := TargetPlan{
		Desired:             []PlannedRule{},
		SatisfiedByOwned:    []PlanMatch{},
		SatisfiedByExternal: []PlanMatch{},
		ToAdd:               []config.RuleAction{},
		CleanupCandidates:   []config.RuleInfo{},
		CleanupDeletable:    []config.RuleInfo{},
		CleanupDeferred:     []PlanIssue{},
		DNSErrors:           []PlanIssue{},
		Unsupported:         []PlanIssue{},
		Conflicts:           []PlanIssue{},
	}

	// ── 1) 目标级完整 Desired：按本地 rule ID 升序聚合 ──
	rules := append([]config.DomainRule(nil), in.Rules...)
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })

	desiredIndex := make(map[FunctionalKey]int)
	unsupportedSeen := make(map[string]bool)

	for _, r := range rules {
		if msg := strings.TrimSpace(in.DNSErrors[r.ID]); msg != "" {
			plan.DNSErrors = append(plan.DNSErrors, PlanIssue{
				Code: IssueDNSFailed, Message: msg, Domain: r.Host, RuleID: r.ID,
			})
			continue
		}
		resolved := in.Resolved[r.ID]
		if len(resolved) == 0 {
			plan.DNSErrors = append(plan.DNSErrors, PlanIssue{
				Code: IssueDNSEmpty, Message: "域名解析结果为空", Domain: r.Host, RuleID: r.ID,
			})
			continue
		}

		protocols := []string{r.Protocol}
		if strings.EqualFold(r.Protocol, "TCP+UDP") && !caps.TCPUDP {
			protocols = []string{"TCP", "UDP"}
		}
		wirePorts := expandPlannedPorts(in.CloudType, r.Ports)
		action := strings.ToUpper(strings.TrimSpace(r.Action))
		comment := strings.TrimSpace(r.Comment)

		for _, ip := range resolved {
			cidr, ok := CanonicalCIDR(ip.CIDR())
			if !ok {
				plan.DNSErrors = append(plan.DNSErrors, PlanIssue{
					Code: IssueDNSFailed, Message: "解析结果不是合法 CIDR", Domain: r.Host, RuleID: r.ID,
				})
				continue
			}
			family := AddressIPv4
			if ip.IsIPv6 {
				family = AddressIPv6
			}
			for _, proto := range protocols {
				keyProto := canonicalProtocol(proto)
				for _, wirePort := range wirePorts {
					key := FunctionalKey{
						Family:   family,
						CIDR:     cidr,
						Protocol: keyProto,
						Port:     canonicalPortExpression(keyProto, wirePort),
						Action:   action,
					}
					reasonCode := unsupportedKeyReason(caps, family, keyProto, action)
					planned := PlannedRule{
						Key:           key,
						Comment:       comment,
						RuleIDs:       []int{r.ID},
						Domains:       []string{r.Host},
						Implementable: reasonCode == "",
						Action: config.RuleAction{
							Protocol: proto,
							Port:     wirePort,
							Action:   action,
						},
					}
					if family == AddressIPv6 {
						planned.Action.Ipv6CidrBlock = cidr
					} else {
						planned.Action.CidrBlock = cidr
					}

					if idx, exists := desiredIndex[key]; exists {
						cur := &plan.Desired[idx]
						cur.RuleIDs = appendUniqueInt(cur.RuleIDs, r.ID)
						cur.Domains = appendUniqueString(cur.Domains, r.Host)
						// comment：按 rule ID 升序取第一个非空值（rules 已升序）
						if cur.Comment == "" && comment != "" {
							cur.Comment = comment
						}
					} else {
						desiredIndex[key] = len(plan.Desired)
						plan.Desired = append(plan.Desired, planned)
					}

					if reasonCode != "" {
						seenKey := reasonCode + "|" + key.String()
						if !unsupportedSeen[seenKey] {
							unsupportedSeen[seenKey] = true
							k := key
							plan.Unsupported = append(plan.Unsupported, PlanIssue{
								Code:    reasonCode,
								Message: unsupportedMessage(reasonCode),
								Key:     &k,
								Domain:  r.Host,
								RuleID:  r.ID,
							})
						}
					}
				}
			}
		}
	}

	// 渲染 description（唯一渲染/截断层），再按 key 稳定排序，保证输出可复现。
	for i := range plan.Desired {
		plan.Desired[i].Description = RenderDescription(in.CloudType, in.Tag, plan.Desired[i].Comment)
		plan.Desired[i].Action.Description = plan.Desired[i].Description
		sort.Ints(plan.Desired[i].RuleIDs)
		sort.Strings(plan.Desired[i].Domains)
	}
	sort.SliceStable(plan.Desired, func(i, j int) bool {
		return plan.Desired[i].Key.String() < plan.Desired[j].Key.String()
	})

	// ── 2) 云端快照索引：Owned / External 多值结构，禁止静默覆盖重复项 ──
	ownedIndex := make(map[FunctionalKey][]config.RuleInfo)
	externalIndex := make(map[FunctionalKey][]config.RuleInfo)
	type snapshotEntry struct {
		keys  []FunctionalKey
		owned bool
	}
	entries := make([]snapshotEntry, len(in.Snapshot.Rules))
	// tuple（family|CIDR|协议|端口，不含 action）→ 出现过的 action 集合
	tupleActions := make(map[string]map[string]bool)
	tupleText := make(map[string]string)

	for i, r := range in.Snapshot.Rules {
		keys, ok := SnapshotRuleKeys(r)
		if !ok {
			plan.Conflicts = append(plan.Conflicts, PlanIssue{
				Code:    IssueSnapshotRuleInvalid,
				Message: "云端规则字段不足以安全判定（地址族/协议/端口/action），已排除出删除集合：" + describeCloudRule(r),
			})
			continue
		}
		owned := tag.IsOwned(r.Description, in.Tag)
		entries[i] = snapshotEntry{keys: keys, owned: owned}
		for _, k := range keys {
			if owned {
				ownedIndex[k] = append(ownedIndex[k], r)
			} else {
				externalIndex[k] = append(externalIndex[k], r)
			}
			tuple := fmt.Sprintf("%s|%s|%s|%s", k.Family, k.CIDR, k.Protocol, k.Port)
			if tupleActions[tuple] == nil {
				tupleActions[tuple] = make(map[string]bool)
				tupleText[tuple] = fmt.Sprintf("%s %s %s %s", k.Family, k.CIDR, k.Protocol, k.Port)
			}
			tupleActions[tuple][k.Action] = true
		}
	}

	// opposite_action：同一五元组去掉 action 后同时存在 ACCEPT/DROP，只提示、冻结清理，
	// 不做包含或优先级推断（Issue7 §4.3）。
	tuples := make([]string, 0, len(tupleActions))
	for tuple := range tupleActions {
		tuples = append(tuples, tuple)
	}
	sort.Strings(tuples)
	for _, tuple := range tuples {
		actions := tupleActions[tuple]
		if actions["ACCEPT"] && actions["DROP"] {
			plan.Conflicts = append(plan.Conflicts, PlanIssue{
				Code:    IssueOppositeAction,
				Message: "同一功能上同时存在 ACCEPT 与 DROP，无法安全自动裁决，保留全部清理候选：" + tupleText[tuple],
			})
		}
	}

	// 腾讯云写入必须携带快照版本；缺失即视为快照不完整（不得退化为无版本写入）。
	if (in.CloudType == config.CloudTCLighthouse || in.CloudType == config.CloudTCCVM) && strings.TrimSpace(in.Snapshot.Revision) == "" {
		plan.Conflicts = append(plan.Conflicts, PlanIssue{
			Code:    IssueSnapshotIncomplete,
			Message: "腾讯云快照缺少版本号，禁止无版本写入与删除",
		})
	}

	// ── 3) 覆盖计算：Owned 优先，External 只按精确 key 满足 ──
	implementable, covered := 0, 0
	for i := range plan.Desired {
		d := plan.Desired[i]
		if d.Implementable {
			implementable++
		}
		if rules, ok := ownedIndex[d.Key]; ok && len(rules) > 0 {
			plan.SatisfiedByOwned = append(plan.SatisfiedByOwned, PlanMatch{
				Key: d.Key, Description: rules[0].Description, Rules: rules,
			})
			if d.Implementable {
				covered++
			}
			continue
		}
		if rules, ok := externalIndex[d.Key]; ok && len(rules) > 0 {
			plan.SatisfiedByExternal = append(plan.SatisfiedByExternal, PlanMatch{
				Key: d.Key, Description: rules[0].Description, Rules: rules,
			})
			if d.Implementable {
				covered++
			}
			continue
		}
		if d.Implementable {
			plan.ToAdd = append(plan.ToAdd, d.Action)
		}
	}
	plan.CoverageReady = implementable > 0 && covered == implementable && !in.AddStateUnknown

	// ── 4) 清理候选：S0 Owned 中**全部** key 都不在完整 Desired 的规则 ──
	desiredKeys := make(map[FunctionalKey]bool, len(plan.Desired))
	for _, d := range plan.Desired {
		desiredKeys[d.Key] = true
	}
	type candidate struct {
		rule config.RuleInfo
		keys []FunctionalKey
	}
	var candidates []candidate
	for i, r := range in.Snapshot.Rules {
		entry := entries[i]
		if !entry.owned || len(entry.keys) == 0 {
			continue
		}
		allAbsent := true
		for _, k := range entry.keys {
			if desiredKeys[k] {
				allAbsent = false
				break
			}
		}
		if !allAbsent {
			continue
		}
		candidates = append(candidates, candidate{rule: r, keys: entry.keys})
		plan.CleanupCandidates = append(plan.CleanupCandidates, r)
	}

	// ── 5) 清理安全门（Issue7 §4.6）：全部条件满足才允许自动删除 ──
	gateIssues := make([]PlanIssue, 0, 8)
	if len(plan.Desired) == 0 {
		gateIssues = append(gateIssues, PlanIssue{
			Code: IssueDesiredEmpty, Message: "完整期望集为空，不授权清空 TAG 命名空间",
		})
	}
	if len(plan.DNSErrors) > 0 {
		gateIssues = append(gateIssues, PlanIssue{
			Code:    IssueDNSFailed,
			Message: fmt.Sprintf("%d 个适用域名解析失败或为空，保留全部清理候选", len(plan.DNSErrors)),
		})
	}
	for _, code := range distinctIssueCodes(plan.Unsupported) {
		gateIssues = append(gateIssues, PlanIssue{
			Code: code, Message: "存在平台无法实施的期望规则，保留全部清理候选",
		})
	}
	for _, code := range distinctIssueCodes(plan.Conflicts) {
		gateIssues = append(gateIssues, PlanIssue{
			Code: code, Message: "存在无法安全自动裁决的冲突，保留全部清理候选",
		})
	}
	if !plan.CoverageReady {
		gateIssues = append(gateIssues, PlanIssue{
			Code: IssueCoverageNotReady, Message: "当前快照未能证明覆盖全部可实施期望，保留全部清理候选",
		})
	}

	plan.CleanupDeferred = append(plan.CleanupDeferred, gateIssues...)
	// 清理门未全部满足时，全部候选只作预览（deferred），可删除集合为空。
	gateBlocked := len(gateIssues) > 0
	for _, c := range candidates {
		// 定位问题始终登记（Dry Run 需要展示每个候选为何不能删除）；
		// 但只有清理门全部满足时才可能进入可删除集合。
		if issue, bad := cleanupLocatorIssue(in.CloudType, c.rule, c.keys, ownedIndex, externalIndex); bad {
			plan.CleanupDeferred = append(plan.CleanupDeferred, issue)
			continue
		}
		if !gateBlocked {
			plan.CleanupDeletable = append(plan.CleanupDeletable, c.rule)
		}
	}

	// 稳定输出顺序
	sortIssues(plan.DNSErrors)
	sortIssues(plan.Unsupported)
	sortIssues(plan.Conflicts)
	sortIssues(plan.CleanupDeferred)
	return plan
}

// cleanupLocatorIssue 判断候选在**当前快照**中是否具备无歧义的删除定位。
//
//   - Lighthouse：无稳定 RuleID，按完整规则值匹配；只有当候选的每个 key 在该快照的
//     全部规则中恰好出现一次、且该唯一项严格 Owned 时才可删除，否则歧义 → deferred；
//   - CVM：必须使用同一快照的 PolicyIndex；缺失/不可解析 → deferred；候选间索引重复
//     由 Provider 在批量删除前拒绝，返回错误且不发送删除请求；
//   - SWAS/ECS：必须使用同一快照回读的非空 RuleId/SecurityGroupRuleId，缺失 → deferred。
func cleanupLocatorIssue(
	ct config.CloudType,
	rule config.RuleInfo,
	keys []FunctionalKey,
	ownedIndex, externalIndex map[FunctionalKey][]config.RuleInfo,
) (PlanIssue, bool) {
	key := keys[0]
	issue := PlanIssue{Key: &key, Message: "候选缺少无歧义的删除定位，保留为残留：" + describeCloudRule(rule)}

	switch ct {
	case config.CloudTCLighthouse:
		for _, k := range keys {
			if len(ownedIndex[k]) != 1 || len(externalIndex[k]) != 0 {
				issue.Code = IssueOwnedKeyAmbiguous
				issue.Message = "Lighthouse 同功能 key 在快照中不唯一或与外部规则并存，无法唯一定位，保留为残留：" + describeCloudRule(rule)
				return issue, true
			}
		}
		return PlanIssue{}, false
	case config.CloudTCCVM:
		if strings.TrimSpace(rule.PolicyIndex) == "" {
			issue.Code = IssueOwnedLocatorMissing
			return issue, true
		}
		if _, err := strconv.ParseInt(strings.TrimSpace(rule.PolicyIndex), 10, 64); err != nil {
			issue.Code = IssueOwnedLocatorMissing
			return issue, true
		}
		return PlanIssue{}, false
	default:
		if strings.TrimSpace(rule.RuleID) == "" {
			issue.Code = IssueOwnedLocatorMissing
			return issue, true
		}
		return PlanIssue{}, false
	}
}

// describeCloudRule 生成不含敏感信息的云端规则摘要（用于稳定原因文案）。
func describeCloudRule(r config.RuleInfo) string {
	cidr := r.CidrBlock
	if cidr == "" {
		cidr = r.Ipv6CidrBlock
	}
	desc := r.Description
	if len([]rune(desc)) > 40 {
		desc = string([]rune(desc)[:40]) + "…"
	}
	return fmt.Sprintf("protocol=%s port=%s cidr=%s action=%s desc=%q",
		r.Protocol, r.Port, cidr, r.Action, desc)
}

// distinctIssueCodes 返回问题列表中出现的稳定原因码（去重、字典序）。
func distinctIssueCodes(issues []PlanIssue) []string {
	seen := make(map[string]bool, len(issues))
	codes := make([]string, 0, len(issues))
	for _, it := range issues {
		if seen[it.Code] {
			continue
		}
		seen[it.Code] = true
		codes = append(codes, it.Code)
	}
	sort.Strings(codes)
	return codes
}

// sortIssues 稳定排序规划问题（code → key → domain → rule id），保证输出可复现。
func sortIssues(issues []PlanIssue) {
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		ki, kj := "", ""
		if issues[i].Key != nil {
			ki = issues[i].Key.String()
		}
		if issues[j].Key != nil {
			kj = issues[j].Key.String()
		}
		if ki != kj {
			return ki < kj
		}
		if issues[i].Domain != issues[j].Domain {
			return issues[i].Domain < issues[j].Domain
		}
		return issues[i].RuleID < issues[j].RuleID
	})
}

// sortRuleKeys 稳定排序 key 列表（family → CIDR → protocol → port → action）。
func sortRuleKeys(keys []FunctionalKey) {
	sort.SliceStable(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
}

// splitPortField 拆分端口字段为独立端口项（去空、去重、稳定排序）。
func splitPortField(port string) []string {
	parts := strings.Split(port, ",")
	seen := make(map[string]bool, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func appendUniqueInt(list []int, v int) []int {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func appendUniqueString(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
