package provider

import (
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/portconv"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	vpc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
)

func init() {
	Register(config.CloudTCCVM, newTCCVM)
}

// TCCVM 腾讯云 CVM 安全组 Provider
type TCCVM struct {
	client          *vpc.Client
	securityGroupID string
	targetIndex     int
}

func newTCCVM(cfg config.TargetConfig, dbID int, pool *ClientPool) (Provider, error) {
	key := string(config.CloudTCCVM) + "|" + cfg.Region + "|" + getTCAccessID()

	client, err := pool.GetOrCreate(key, func() (any, error) {
		credential := common.NewCredential(
			getTCAccessID(),
			getTCAccessKey(),
		)
		cpf := profile.NewClientProfile()
		cpf.HttpProfile.Endpoint = "vpc.tencentcloudapi.com"
		return vpc.NewClient(credential, cfg.Region, cpf)
	})
	if err != nil {
		return nil, fmt.Errorf("创建 CVM Client 失败: %w", err)
	}

	return &TCCVM{
		client:          client.(*vpc.Client),
		securityGroupID: cfg.ResourceID,
		targetIndex:     dbID,
	}, nil
}

func (p *TCCVM) Name() string {
	return fmt.Sprintf("tc_cvm(%s)", p.securityGroupID)
}

func (p *TCCVM) CloudType() config.CloudType {
	return config.CloudTCCVM
}

func (p *TCCVM) TargetIndex() int {
	return p.targetIndex
}

// GetRules 查询安全组入站规则
func (p *TCCVM) GetRules() ([]config.RuleInfo, error) {
	req := vpc.NewDescribeSecurityGroupPoliciesRequest()
	req.SecurityGroupId = common.StringPtr(p.securityGroupID)

	resp, err := p.client.DescribeSecurityGroupPolicies(req)
	if err != nil {
		return nil, fmt.Errorf("查询安全组规则失败: %w", err)
	}

	var rules []config.RuleInfo
	policySet := resp.Response.SecurityGroupPolicySet
	if policySet == nil {
		return rules, nil
	}

	// 只取 Ingress（入站）规则
	// PolicyIndex 是安全组全方向全局索引（Ingress+Egress 共用编号空间），与 Ingress 数组索引不一致；
	// 缺失时跳过该规则（不参与本工具删除），避免按错误索引定位误删 Egress 或其他规则
	for _, r := range policySet.Ingress {
		if r.PolicyIndex == nil {
			slog.Warn("CVM 规则缺少 PolicyIndex，跳过该规则（避免误删）", "description", strVal(r.PolicyDescription))
			continue
		}
		info := config.RuleInfo{
			Protocol:      strings.ToUpper(strVal(r.Protocol)),
			Port:          strVal(r.Port),
			CidrBlock:     strVal(r.CidrBlock),
			Ipv6CidrBlock: strVal(r.Ipv6CidrBlock),
			Action:        strings.ToUpper(strVal(r.Action)),
			Description:   strVal(r.PolicyDescription),
			PolicyIndex:   strconv.FormatInt(*r.PolicyIndex, 10),
		}
		rules = append(rules, info)
	}

	return rules, nil
}

// CreateRules 增量添加入站规则
func (p *TCCVM) CreateRules(rules []config.RuleAction) error {
	if len(rules) == 0 {
		return nil
	}

	// 检查规则总数是否接近上限（100 条）
	if err := p.checkRuleLimit(len(rules)); err != nil {
		return err
	}

	var policies []*vpc.SecurityGroupPolicy
	for _, r := range rules {
		policy := &vpc.SecurityGroupPolicy{
			// CVM Action 使用小写
			Action:            common.StringPtr(strings.ToLower(r.Action)),
			PolicyDescription: common.StringPtr(r.Description),
		}

		// 协议处理：IPv6 + ICMP 需用 ICMPV6
		proto := r.Protocol
		if r.Ipv6CidrBlock != "" && strings.EqualFold(proto, "ICMP") {
			proto = "ICMPV6"
		}
		policy.Protocol = common.StringPtr(proto)

		// 端口处理：仅 TCP/UDP 设置 Port，ICMP/ICMPV6/ALL 省略
		if strings.EqualFold(proto, "TCP") || strings.EqualFold(proto, "UDP") {
			policy.Port = common.StringPtr(r.Port)
		}

		// IPv4 和 IPv6 互斥
		if r.Ipv6CidrBlock != "" {
			policy.Ipv6CidrBlock = common.StringPtr(r.Ipv6CidrBlock)
		} else {
			policy.CidrBlock = common.StringPtr(r.CidrBlock)
		}

		policies = append(policies, policy)
	}

	req := vpc.NewCreateSecurityGroupPoliciesRequest()
	req.SecurityGroupId = common.StringPtr(p.securityGroupID)
	req.SecurityGroupPolicySet = &vpc.SecurityGroupPolicySet{
		Ingress: policies,
	}

	_, err := p.client.CreateSecurityGroupPolicies(req)
	if err != nil {
		return fmt.Errorf("添加安全组规则失败: %w", err)
	}
	return nil
}

// DeleteRules 按 PolicyIndex 降序逐条删除入站规则
func (p *TCCVM) DeleteRules(rules []config.RuleInfo) error {
	if len(rules) == 0 {
		return nil
	}

	// 按 PolicyIndex 降序排列（避免索引偏移）
	sorted := make([]config.RuleInfo, len(rules))
	copy(sorted, rules)
	sort.Slice(sorted, func(i, j int) bool {
		pi, _ := strconv.Atoi(sorted[i].PolicyIndex)
		pj, _ := strconv.Atoi(sorted[j].PolicyIndex)
		return pi > pj
	})

	// 逐条删除
	for _, r := range sorted {
		idx, err := strconv.ParseInt(r.PolicyIndex, 10, 64)
		if err != nil {
			slog.Warn("无效的 PolicyIndex，跳过", "index", r.PolicyIndex)
			continue
		}

		req := vpc.NewDeleteSecurityGroupPoliciesRequest()
		req.SecurityGroupId = common.StringPtr(p.securityGroupID)
		req.SecurityGroupPolicySet = &vpc.SecurityGroupPolicySet{
			Ingress: []*vpc.SecurityGroupPolicy{
				{PolicyIndex: common.Int64Ptr(idx)},
			},
		}

		_, err = p.client.DeleteSecurityGroupPolicies(req)
		if err != nil {
			// ResourceNotFound 视为成功（幂等）
			if strings.Contains(err.Error(), "ResourceNotFound") {
				slog.Warn("规则已不存在，跳过", "index", r.PolicyIndex)
				continue
			}
			return fmt.Errorf("删除安全组规则失败 (index=%s): %w", r.PolicyIndex, err)
		}
	}
	return nil
}

// ConvertPorts CVM 不支持逗号分隔，拆分为多条
func (p *TCCVM) ConvertPorts(port string) []string {
	return portconv.Parse(port)
}

// checkRuleLimit 检查安全组规则总数是否接近上限
func (p *TCCVM) checkRuleLimit(toAdd int) error {
	req := vpc.NewDescribeSecurityGroupPoliciesRequest()
	req.SecurityGroupId = common.StringPtr(p.securityGroupID)

	resp, err := p.client.DescribeSecurityGroupPolicies(req)
	if err != nil {
		return fmt.Errorf("查询规则数量失败: %w", err)
	}

	ps := resp.Response.SecurityGroupPolicySet
	if ps == nil {
		return nil
	}

	// 计算总规则数（优先使用 PolicyStatistics 精确计数，fallback 到手动计数）
	var total int
	if ps.PolicyStatistics != nil {
		stats := ps.PolicyStatistics
		total = int(uint64Val(stats.IngressIPv4TotalCount) + uint64Val(stats.IngressIPv6TotalCount) +
			uint64Val(stats.EgressIPv4TotalCount) + uint64Val(stats.EgressIPv6TotalCount))
	} else {
		total = len(ps.Ingress) + len(ps.Egress)
	}
	if total+toAdd > 100 {
		return fmt.Errorf("安全组规则总数将达 %d（上限 100），停止新增", total+toAdd)
	}
	if total+toAdd > 90 {
		slog.Warn("安全组规则接近上限", "当前", total, "新增", toAdd, "上限", 100)
	}
	return nil
}

// uint64Val 安全获取 uint64 指针值
func uint64Val(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}
