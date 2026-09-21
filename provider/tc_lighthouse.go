package provider

import (
	"fmt"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/portconv"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	lighthouse "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/lighthouse/v20200324"
)

func init() {
	Register(config.CloudTCLighthouse, newTCLighthouse)
}

// TCLighthouse 腾讯云 Lighthouse 轻量云 Provider
type TCLighthouse struct {
	client      *lighthouse.Client
	instanceID  string
	targetIndex int
}

func newTCLighthouse(cfg config.TargetConfig, dbID int, pool *ClientPool) (Provider, error) {
	// 从环境变量获取凭据（由 app 层传入 Config，此处通过 pool key 隐含）
	// 实际凭据通过 ClientPool 共享
	key := string(config.CloudTCLighthouse) + "|" + cfg.Region + "|" + getTCAccessID()

	client, err := pool.GetOrCreate(key, func() (any, error) {
		// 凭据从全局配置获取（由 app 层在创建 Provider 前设置）
		credential := common.NewCredential(
			getTCAccessID(),
			getTCAccessKey(),
		)
		cpf := profile.NewClientProfile()
		cpf.HttpProfile.Endpoint = "lighthouse.tencentcloudapi.com"
		return lighthouse.NewClient(credential, cfg.Region, cpf)
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Lighthouse Client 失败: %w", err)
	}

	return &TCLighthouse{
		client:      client.(*lighthouse.Client),
		instanceID:  cfg.ResourceID,
		targetIndex: dbID,
	}, nil
}

func (p *TCLighthouse) Name() string {
	return fmt.Sprintf("tc_lighthouse(%s)", p.instanceID)
}

func (p *TCLighthouse) CloudType() config.CloudType {
	return config.CloudTCLighthouse
}

func (p *TCLighthouse) TargetIndex() int {
	return p.targetIndex
}

// GetRules 查询当前所有防火墙规则（分页）
func (p *TCLighthouse) GetRules() ([]config.RuleInfo, error) {
	var allRules []config.RuleInfo
	var offset int64
	limit := int64(100)

	for {
		req := lighthouse.NewDescribeFirewallRulesRequest()
		req.InstanceId = common.StringPtr(p.instanceID)
		req.Offset = common.Int64Ptr(offset)
		req.Limit = common.Int64Ptr(limit)

		resp, err := p.client.DescribeFirewallRules(req)
		if err != nil {
			return nil, fmt.Errorf("查询防火墙规则失败: %w", err)
		}

		for _, r := range resp.Response.FirewallRuleSet {
			info := config.RuleInfo{
				Protocol:    strVal(r.Protocol),
				Port:        strVal(r.Port),
				CidrBlock:   strVal(r.CidrBlock),
				Ipv6CidrBlock: strVal(r.Ipv6CidrBlock),
				Action:      strVal(r.Action),
				Description: strVal(r.FirewallRuleDescription),
			}
			allRules = append(allRules, info)
		}

		// 分页：返回数量 < limit 表示已到最后一页
		if int64(len(resp.Response.FirewallRuleSet)) < limit {
			break
		}
		offset += limit
	}

	return allRules, nil
}

// CreateRules 增量添加防火墙规则
func (p *TCLighthouse) CreateRules(rules []config.RuleAction) error {
	if len(rules) == 0 {
		return nil
	}

	var fwRules []*lighthouse.FirewallRule
	for _, r := range rules {
		fwRule := &lighthouse.FirewallRule{
			Action:                  common.StringPtr(r.Action),
			FirewallRuleDescription: common.StringPtr(r.Description), // 已由 Syncer 层 truncateDesc 截断
		}

		// 协议处理：IPv6 + ICMP 需用 ICMPv6
		proto := r.Protocol
		if r.Ipv6CidrBlock != "" && strings.EqualFold(proto, "ICMP") {
			proto = "ICMPv6"
		}
		fwRule.Protocol = common.StringPtr(proto)

		// 端口处理：ICMP/ICMPv6/ALL 协议时传 ALL
		if strings.EqualFold(proto, "ICMP") || strings.EqualFold(proto, "ICMPv6") || strings.EqualFold(proto, "ALL") {
			fwRule.Port = common.StringPtr("ALL")
		} else {
			fwRule.Port = common.StringPtr(r.Port)
		}

		// IPv4 和 IPv6 互斥
		if r.Ipv6CidrBlock != "" {
			fwRule.Ipv6CidrBlock = common.StringPtr(r.Ipv6CidrBlock)
		} else {
			fwRule.CidrBlock = common.StringPtr(r.CidrBlock)
		}

		fwRules = append(fwRules, fwRule)
	}

	req := lighthouse.NewCreateFirewallRulesRequest()
	req.InstanceId = common.StringPtr(p.instanceID)
	req.FirewallRules = fwRules
	// 不传 FirewallVersion（由云 API 自行管理）

	_, err := p.client.CreateFirewallRules(req)
	if err != nil {
		return fmt.Errorf("添加防火墙规则失败: %w", err)
	}
	return nil
}

// DeleteRules 精确删除防火墙规则
func (p *TCLighthouse) DeleteRules(rules []config.RuleInfo) error {
	if len(rules) == 0 {
		return nil
	}

	var fwRules []*lighthouse.FirewallRule
	for _, r := range rules {
		fwRule := &lighthouse.FirewallRule{
			Protocol: common.StringPtr(r.Protocol),
			Action:   common.StringPtr(r.Action),
		}

		// 端口
		if r.Port != "" {
			fwRule.Port = common.StringPtr(r.Port)
		} else {
			fwRule.Port = common.StringPtr("ALL")
		}

		// CIDR
		if r.Ipv6CidrBlock != "" {
			fwRule.Ipv6CidrBlock = common.StringPtr(r.Ipv6CidrBlock)
		} else if r.CidrBlock != "" {
			fwRule.CidrBlock = common.StringPtr(r.CidrBlock)
		}

		fwRules = append(fwRules, fwRule)
	}

	req := lighthouse.NewDeleteFirewallRulesRequest()
	req.InstanceId = common.StringPtr(p.instanceID)
	req.FirewallRules = fwRules

	_, err := p.client.DeleteFirewallRules(req)
	if err != nil {
		return fmt.Errorf("删除防火墙规则失败: %w", err)
	}
	return nil
}

// ConvertPorts 统一端口 → Lighthouse 格式
// Lighthouse 支持逗号分隔，Port 字段 ≤ 64 字符
// 超限时拆分为多个条目
func (p *TCLighthouse) ConvertPorts(port string) []string {
	ports := portconv.Parse(port)
	if len(ports) == 1 {
		return ports // ALL 或单端口
	}

	// 尝试合并为逗号分隔（总长度 ≤ 64）
	joined := strings.Join(ports, ",")
	if len(joined) <= 64 {
		return []string{joined}
	}

	// 超限：拆分为多个条目，每个 ≤ 64 字符
	var result []string
	current := ""
	for _, p := range ports {
		if current == "" {
			current = p
		} else if len(current+","+p) <= 64 {
			current += "," + p
		} else {
			result = append(result, current)
			current = p
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

