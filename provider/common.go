package provider

import (
	"strings"
	"sync"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/tag"
)

// OwnedRules 筛选本工具管理的规则（描述以 [TAG] 开头）
// 同时过滤掉 Port 和 CidrBlock 均为空的规则（可能是模板规则，非本工具创建）
func OwnedRules(allRules []config.RuleInfo, tagStr string) []config.RuleInfo {
	var owned []config.RuleInfo
	for _, r := range allRules {
		if tag.HasPrefix(r.Description, tagStr) {
			// 跳过模板规则（Port 和 CidrBlock 均为空）
			if r.Port == "" && r.CidrBlock == "" && r.Ipv6CidrBlock == "" {
				continue
			}
			owned = append(owned, r)
		}
	}
	return owned
}

// ruleKey 用于比较规则是否相同
// port 字段经 normalizePortForCompare 归一化，保证 ICMP 规则两端（desired 云格式 / existing 归一化格式）可正确比较
type ruleKey struct {
	protocol      string
	port          string
	cidrBlock     string
	ipv6CidrBlock string
	action        string
}

// normalizePortForCompare 端口比较归一化：
// 协议为 ICMP/ICMPv6 时，-1/-1、ALL、空串三者等价（desired 侧为云厂商格式、existing 侧为归一化格式，避免 ICMP 规则永不收敛）
// 归一化仅用于比较，不影响 CreateRules/GetRules 的请求/返回格式
func normalizePortForCompare(protocol, port string) string {
	proto := strings.ToUpper(protocol)
	if proto == "ICMP" || proto == "ICMPV6" {
		return "ALL"
	}
	if strings.EqualFold(port, "-1/-1") {
		return "ALL"
	}
	return strings.ToUpper(port)
}

func keyOf(r config.RuleInfo) ruleKey {
	return ruleKey{
		protocol:      strings.ToUpper(r.Protocol),
		port:          normalizePortForCompare(r.Protocol, r.Port),
		cidrBlock:     r.CidrBlock,
		ipv6CidrBlock: r.Ipv6CidrBlock,
		action:        strings.ToUpper(r.Action),
	}
}

func keyOfAction(r config.RuleAction) ruleKey {
	return ruleKey{
		protocol:      strings.ToUpper(r.Protocol),
		port:          normalizePortForCompare(r.Protocol, r.Port),
		cidrBlock:     r.CidrBlock,
		ipv6CidrBlock: r.Ipv6CidrBlock,
		action:        strings.ToUpper(r.Action),
	}
}

// Diff 计算需要添加和删除的规则
// resolved: DNS 解析结果
// rule: 域名规则配置
// desc: 规则描述（已包含 [TAG]）
// existing: 当前云端属于本工具的规则
// p: Provider（用于 ConvertPorts）
func Diff(
	resolved []dns.ResolvedIP,
	rule config.DomainRule,
	desc string,
	existing []config.RuleInfo,
	p Provider,
) DiffResult {
	// 1. 构建期望规则集
	desired := buildDesired(resolved, rule, desc, p)

	// 2. 筛选当前域名的现有规则（仅比较 Description 完全匹配的规则，避免误删其他域名的规则）
	var domainExisting []config.RuleInfo
	for _, r := range existing {
		if r.Description == desc {
			domainExisting = append(domainExisting, r)
		}
	}

	// 3. 构建现有规则索引
	existingKeys := make(map[ruleKey]config.RuleInfo)
	for _, r := range domainExisting {
		existingKeys[keyOf(r)] = r
	}

	// 4. 计算 toAdd：期望中有、现有中无
	var toAdd []config.RuleAction
	desiredKeys := make(map[ruleKey]bool)
	for _, d := range desired {
		k := keyOfAction(d)
		desiredKeys[k] = true
		if _, exists := existingKeys[k]; !exists {
			toAdd = append(toAdd, d)
		}
	}

	// 5. 计算 toDelete：当前域名的现有规则中，期望中无的
	var toDelete []config.RuleInfo
	for _, r := range domainExisting {
		k := keyOf(r)
		if !desiredKeys[k] {
			toDelete = append(toDelete, r)
		}
	}

	return DiffResult{ToAdd: toAdd, ToDelete: toDelete}
}

// buildDesired 根据 DNS 结果和规则配置构建期望规则列表
func buildDesired(
	resolved []dns.ResolvedIP,
	rule config.DomainRule,
	desc string,
	p Provider,
) []config.RuleAction {
	var actions []config.RuleAction
	ports := p.ConvertPorts(rule.Ports)

	// TCP+UDP 协议拆分：仅 SWAS 原生支持，其他云厂商拆为 TCP + UDP 两条
	protocols := []string{rule.Protocol}
	if rule.Protocol == "TCP+UDP" && !supportsTCPUDP(p.CloudType()) {
		protocols = []string{"TCP", "UDP"}
	}

	for _, ip := range resolved {
		// IPv6 过滤：不支持 IPv6 的云厂商跳过
		if ip.IsIPv6 && !supportsIPv6(p.CloudType()) {
			continue
		}
		// ECS 不支持 ICMPv6 入站规则创建（AuthorizeSecurityGroup 无 ICMPv6），跳过
		if ip.IsIPv6 && rule.Protocol == "ICMP" && p.CloudType() == config.CloudAliECS {
			continue
		}
		for _, proto := range protocols {
			for _, port := range ports {
				action := config.RuleAction{
					Protocol:    proto,
					Port:        port,
					Action:      rule.Action,
					Description: desc,
				}
				if ip.IsIPv6 {
					action.Ipv6CidrBlock = ip.CIDR()
				} else {
					action.CidrBlock = ip.CIDR()
				}
				actions = append(actions, action)
			}
		}
	}
	return actions
}

// supportsIPv6 判断云厂商是否支持 IPv6
func supportsIPv6(ct config.CloudType) bool {
	switch ct {
	case config.CloudAliSWAS:
		return false // 阿里云轻量云不支持 IPv6
	default:
		return true
	}
}

// supportsTCPUDP 判断云厂商是否原生支持 TCP+UDP 协议
func supportsTCPUDP(ct config.CloudType) bool {
	return ct == config.CloudAliSWAS // 仅 SWAS 原生支持 TCP+UDP
}

// ClientPool SDK Client 复用池
type ClientPool struct {
	mu      sync.Mutex
	clients map[string]any // key: cloudType|region|accessID
}

// NewClientPool 创建 Client 复用池
func NewClientPool() *ClientPool {
	return &ClientPool{clients: make(map[string]any)}
}

// GetOrCreate 获取或创建 Client
func (p *ClientPool) GetOrCreate(key string, create func() (any, error)) (any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[key]; ok {
		return c, nil
	}
	c, err := create()
	if err != nil {
		return nil, err
	}
	p.clients[key] = c
	return c, nil
}

// strVal 安全获取字符串指针值
func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// RuleChange 规则变更摘要（供前端直接渲染）
type RuleChange struct {
	Protocol string `json:"protocol"`
	Port     string `json:"port"`
	Action   string `json:"action"`
	Cidr     string `json:"cidr"` // IPv4 或 IPv6 的 CIDR（如 1.2.3.4/32）
	Desc     string `json:"desc"` // 规则描述（含 [TAG]）
}

// RuleChangeFromAction 从期望规则构造摘要（to_add）
func RuleChangeFromAction(a config.RuleAction) RuleChange {
	cidr := a.CidrBlock
	if cidr == "" {
		cidr = a.Ipv6CidrBlock
	}
	return RuleChange{Protocol: a.Protocol, Port: a.Port, Action: a.Action, Cidr: cidr, Desc: a.Description}
}

// RuleChangeFromInfo 从云端规则构造摘要（to_delete）
func RuleChangeFromInfo(r config.RuleInfo) RuleChange {
	cidr := r.CidrBlock
	if cidr == "" {
		cidr = r.Ipv6CidrBlock
	}
	return RuleChange{Protocol: r.Protocol, Port: r.Port, Action: r.Action, Cidr: cidr, Desc: r.Description}
}
