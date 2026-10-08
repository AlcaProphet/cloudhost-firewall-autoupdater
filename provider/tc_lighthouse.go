package provider

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
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
	// 凭据只来自 pool 持有的不可变 Credentials，不再读取任何包级全局值
	creds := pool.Credentials()
	key := pool.CacheKey(config.CloudTCLighthouse, cfg.Region)

	client, err := pool.GetOrCreate(key, func() (any, error) {
		credential := common.NewCredential(
			creds.TencentSecretID,
			creds.TencentSecretKey,
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

// 单次完整快照操作的额度，包含分页与版本重读；不代表整个目标或同步轮预算。
const (
	lighthouseSnapshotBudget      = 120 * time.Second
	lighthouseSnapshotMaxRequests = 100
)

// GetSnapshot 查询当前所有防火墙规则（分页）并携带 FirewallVersion。
//
// Issue7 §6.1：FirewallVersion 必须进入快照；分页期间版本变化说明读取不一致，
// 必须先重读整个快照，仍不一致则按快照不完整失败，绝不退化为无版本写入。
func (p *TCLighthouse) GetSnapshot() (RuleSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), lighthouseSnapshotBudget)
	defer cancel()
	return p.getSnapshotBounded(ctx, lighthouseSnapshotMaxRequests)
}

// getSnapshotBounded 复用同一上下文和查询额度；私有参数允许回归缩短预算，生产入口固定使用上述常量。
func (p *TCLighthouse) getSnapshotBounded(ctx context.Context, maxRequests int) (RuleSnapshot, error) {
	// 计数必须位于版本重读循环之外，重读不重新获得额度。
	requests := 0
	budgetError := func() error {
		return fmt.Errorf("%w: Lighthouse 快照读取预算耗尽: %w", ErrSnapshotIncomplete, ctx.Err())
	}
	const maxVersionRetries = 2
	offset := int64(0)
	limit := int64(100)

	for attempt := 0; attempt < maxVersionRetries; attempt++ {
		var allRules []config.RuleInfo
		offset = 0
		firstVersion, lastVersion := "", ""

		for {
			if ctx.Err() != nil {
				return RuleSnapshot{}, budgetError()
			}
			if requests >= maxRequests {
				return RuleSnapshot{}, fmt.Errorf("%w: Lighthouse 快照查询次数达到上限", ErrSnapshotIncomplete)
			}
			requests++
			req := lighthouse.NewDescribeFirewallRulesRequest()
			req.InstanceId = common.StringPtr(p.instanceID)
			req.Offset = common.Int64Ptr(offset)
			req.Limit = common.Int64Ptr(limit)

			resp, err := p.client.DescribeFirewallRulesWithContext(ctx, req)
			// SDK 网络错误不保留 Unwrap；自身预算耗尽时显式保留 deadline 身份，沿用整目标超时重试。
			if ctx.Err() != nil {
				return RuleSnapshot{}, budgetError()
			}
			if err != nil {
				return RuleSnapshot{}, fmt.Errorf("查询防火墙规则失败: %w", err)
			}
			if resp == nil || resp.Response == nil {
				return RuleSnapshot{}, fmt.Errorf("%w: Lighthouse 返回空响应", ErrSnapshotIncomplete)
			}

			if resp.Response.FirewallVersion == nil {
				return RuleSnapshot{}, fmt.Errorf("%w: Lighthouse 未返回 FirewallVersion", ErrSnapshotIncomplete)
			}
			version := strconv.FormatUint(*resp.Response.FirewallVersion, 10)
			if firstVersion == "" {
				firstVersion = version
			}
			lastVersion = version

			for _, r := range resp.Response.FirewallRuleSet {
				info := config.RuleInfo{
					Protocol:      strVal(r.Protocol),
					Port:          strVal(r.Port),
					CidrBlock:     strVal(r.CidrBlock),
					Ipv6CidrBlock: strVal(r.Ipv6CidrBlock),
					Action:        strVal(r.Action),
					Description:   strVal(r.FirewallRuleDescription),
				}
				allRules = append(allRules, info)
			}

			// 分页：返回数量 < limit 表示已到最后一页
			if int64(len(resp.Response.FirewallRuleSet)) < limit {
				break
			}
			offset += limit
		}

		if ctx.Err() != nil {
			return RuleSnapshot{}, budgetError()
		}
		if firstVersion == lastVersion {
			return RuleSnapshot{Rules: allRules, Revision: lastVersion}, nil
		}
		slog.Warn("Lighthouse 分页期间防火墙版本变化，重读整个快照",
			"first", firstVersion, "last", lastVersion, "attempt", attempt+1)
	}
	return RuleSnapshot{}, fmt.Errorf("%w: Lighthouse 分页期间 FirewallVersion 持续变化", ErrSnapshotIncomplete)
}

// GetRules 旧逐规则同步路径的兼容包装（已废弃，见 Provider 接口注释）。
func (p *TCLighthouse) GetRules() ([]config.RuleInfo, error) {
	snapshot, err := p.GetSnapshot()
	if err != nil {
		return nil, err
	}
	return snapshot.Rules, nil
}

// CreateRules 增量添加防火墙规则。
//
// Lighthouse 不支持任何本工具需要跳过的期望规则形态（ICMPv6 由其原生支持），
// 因此成功时恒为 {len(rules), 0}（Issue6 A11）。
func (p *TCLighthouse) CreateRules(snapshot RuleSnapshot, rules []config.RuleAction) (CreateResult, error) {
	if len(rules) == 0 {
		return CreateResult{}, nil
	}

	var fwRules []*lighthouse.FirewallRule
	for _, r := range rules {
		fwRule := &lighthouse.FirewallRule{
			Action:                  common.StringPtr(r.Action),
			FirewallRuleDescription: common.StringPtr(r.Description), // 已由目标 planner 的 RenderDescription 统一渲染与截断
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

	// 版本保护（Issue7 §6.1）：Create 必须携带 S0 的 FirewallVersion；
	// 缺失或不可解析即快照不完整，绝不退化为无版本写入。
	version, err := strconv.ParseUint(strings.TrimSpace(snapshot.Revision), 10, 64)
	if err != nil {
		return CreateResult{}, fmt.Errorf("%w: Lighthouse 写入缺少可用的 FirewallVersion", ErrSnapshotIncomplete)
	}

	req := lighthouse.NewCreateFirewallRulesRequest()
	req.InstanceId = common.StringPtr(p.instanceID)
	req.FirewallRules = fwRules
	req.FirewallVersion = common.Uint64Ptr(version)

	if _, err := p.client.CreateFirewallRules(req); err != nil {
		return CreateResult{}, fmt.Errorf("添加防火墙规则失败: %w", err)
	}
	return CreateResult{Written: len(fwRules), Skipped: 0}, nil
}

// DeleteRules 按完整规则定义精确删除（Lighthouse 无稳定 RuleID）。
//
// Issue7 §6.1：Delete 必须携带 S1 的 FirewallVersion；缺失或不可解析即快照不完整，
// 绝不降级为无版本删除。删除请求不得包含任何 Desired key（由调用方保证）。
func (p *TCLighthouse) DeleteRules(snapshot RuleSnapshot, rules []config.RuleInfo) (DeleteResult, error) {
	if len(rules) == 0 {
		return DeleteResult{}, nil
	}

	version, err := strconv.ParseUint(strings.TrimSpace(snapshot.Revision), 10, 64)
	if err != nil {
		return DeleteResult{}, fmt.Errorf("%w: Lighthouse 删除缺少可用的 FirewallVersion", ErrSnapshotIncomplete)
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
	req.FirewallVersion = common.Uint64Ptr(version)

	if _, err := p.client.DeleteFirewallRules(req); err != nil {
		return DeleteResult{}, fmt.Errorf("删除防火墙规则失败: %w", err)
	}
	return DeleteResult{Deleted: len(fwRules), Resolved: len(fwRules)}, nil
}

// ConvertPorts 统一端口 → Lighthouse 格式（唯一样本见 provider.ExpandPorts）
func (p *TCLighthouse) ConvertPorts(port string) []string {
	return ExpandPorts(config.CloudTCLighthouse, port)
}
