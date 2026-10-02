package provider

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
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
	// 凭据只来自 pool 持有的不可变 Credentials，不再读取任何包级全局值
	creds := pool.Credentials()
	key := pool.CacheKey(config.CloudTCCVM, cfg.Region)

	client, err := pool.GetOrCreate(key, func() (any, error) {
		credential := common.NewCredential(
			creds.TencentSecretID,
			creds.TencentSecretKey,
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

// GetSnapshot 查询安全组入站规则并携带 Version。
//
// Issue7 §6.2：SecurityGroupPolicySet.Version 必须进入快照；每条 Ingress 保留 API 返回的
// 真实 PolicyIndex。缺少 PolicyIndex 的规则**仍进入快照**（否则会丢失覆盖判断并可能重复创建），
// 但它不会被当作可删除候选——由规划器统一给出 owned_locator_missing 的 cleanup_deferred。
func (p *TCCVM) GetSnapshot() (RuleSnapshot, error) {
	req := vpc.NewDescribeSecurityGroupPoliciesRequest()
	req.SecurityGroupId = common.StringPtr(p.securityGroupID)

	resp, err := p.client.DescribeSecurityGroupPolicies(req)
	if err != nil {
		return RuleSnapshot{}, fmt.Errorf("查询安全组规则失败: %w", err)
	}
	if resp == nil || resp.Response == nil {
		return RuleSnapshot{}, fmt.Errorf("%w: CVM 返回空响应", ErrSnapshotIncomplete)
	}

	policySet := resp.Response.SecurityGroupPolicySet
	if policySet == nil {
		return RuleSnapshot{}, fmt.Errorf("%w: CVM 未返回安全组规则集合", ErrSnapshotIncomplete)
	}
	version := strVal(policySet.Version)
	if version == "" {
		return RuleSnapshot{}, fmt.Errorf("%w: CVM 未返回安全组 Version", ErrSnapshotIncomplete)
	}

	// 只取 Ingress（入站）规则。PolicyIndex 是安全组全方向全局索引
	// （Ingress+Egress 共用编号空间），与 Ingress 数组索引不一致，必须原样保留。
	rules := make([]config.RuleInfo, 0, len(policySet.Ingress))
	missingIndex := 0
	for _, r := range policySet.Ingress {
		policyIndex := ""
		if r.PolicyIndex != nil {
			policyIndex = strconv.FormatInt(*r.PolicyIndex, 10)
		} else {
			missingIndex++
		}
		rules = append(rules, config.RuleInfo{
			Protocol:      strings.ToUpper(strVal(r.Protocol)),
			Port:          strVal(r.Port),
			CidrBlock:     strVal(r.CidrBlock),
			Ipv6CidrBlock: strVal(r.Ipv6CidrBlock),
			Action:        strings.ToUpper(strVal(r.Action)),
			Description:   strVal(r.PolicyDescription),
			PolicyIndex:   policyIndex,
		})
	}
	if missingIndex > 0 {
		slog.Warn("CVM 存在缺少 PolicyIndex 的入站规则，这些规则不会被自动删除", "数量", missingIndex)
	}

	return RuleSnapshot{Rules: rules, Revision: version}, nil
}

// GetRules 旧逐规则同步路径的兼容包装（已废弃，见 Provider 接口注释）。
func (p *TCCVM) GetRules() ([]config.RuleInfo, error) {
	snapshot, err := p.GetSnapshot()
	if err != nil {
		return nil, err
	}
	return snapshot.Rules, nil
}

// CreateRules 增量添加入站规则。
//
// CVM 的 100 条入站规则本地保护上限是**硬错误**（不属 skipped，也不可重试），因此超出上限时
// 返回空结果与错误；成功时恒为 {len(rules), 0}（Issue6 A11）。
func (p *TCCVM) CreateRules(snapshot RuleSnapshot, rules []config.RuleAction) (CreateResult, error) {
	if len(rules) == 0 {
		return CreateResult{}, nil
	}

	// 检查入站规则数是否接近本地保护上限（100 条）
	if err := p.checkRuleLimit(len(rules)); err != nil {
		return CreateResult{}, err
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

	// 版本保护（Issue7 §6.2）：Create 必须携带 S0 的 Version；
	// 缺失即快照不完整，绝不退化为无版本写入。
	version := strings.TrimSpace(snapshot.Revision)
	if version == "" {
		return CreateResult{}, fmt.Errorf("%w: CVM 写入缺少可用的安全组 Version", ErrSnapshotIncomplete)
	}

	req := vpc.NewCreateSecurityGroupPoliciesRequest()
	req.SecurityGroupId = common.StringPtr(p.securityGroupID)
	req.SecurityGroupPolicySet = &vpc.SecurityGroupPolicySet{
		Ingress: policies,
		Version: common.StringPtr(version),
	}

	if _, err := p.client.CreateSecurityGroupPolicies(req); err != nil {
		return CreateResult{}, fmt.Errorf("添加安全组规则失败: %w", err)
	}
	return CreateResult{Written: len(policies), Skipped: 0}, nil
}

// DeleteRules 在**同一个** DeleteSecurityGroupPolicies 请求内按 PolicyIndex 批量删除入站规则。
//
// Issue7 §6.2：
//   - 必须携带同一 S1 的 Version（缺失即快照不完整，绝不无版本删除）；
//   - 绝不逐条复用已经变化的 Version（逐条删除会造成索引漂移）；
//   - 任一候选缺 PolicyIndex、索引不可解析或索引重复映射到不同规则时直接拒绝删除。
func (p *TCCVM) DeleteRules(snapshot RuleSnapshot, rules []config.RuleInfo) (DeleteResult, error) {
	if len(rules) == 0 {
		return DeleteResult{}, nil
	}

	version := strings.TrimSpace(snapshot.Revision)
	if version == "" {
		return DeleteResult{}, fmt.Errorf("%w: CVM 删除缺少可用的安全组 Version", ErrSnapshotIncomplete)
	}

	seen := make(map[int64]string, len(rules))
	policies := make([]*vpc.SecurityGroupPolicy, 0, len(rules))
	for _, r := range rules {
		idx, err := strconv.ParseInt(strings.TrimSpace(r.PolicyIndex), 10, 64)
		if err != nil {
			return DeleteResult{}, fmt.Errorf("候选缺少可用的 PolicyIndex（%q），拒绝删除以避免误删", r.PolicyIndex)
		}
		if prev, dup := seen[idx]; dup {
			return DeleteResult{}, fmt.Errorf("候选存在重复 PolicyIndex=%d（%q 与 %q），无法唯一定位，拒绝删除", idx, prev, r.Description)
		}
		seen[idx] = r.Description
		policies = append(policies, &vpc.SecurityGroupPolicy{PolicyIndex: common.Int64Ptr(idx)})
	}

	req := vpc.NewDeleteSecurityGroupPoliciesRequest()
	req.SecurityGroupId = common.StringPtr(p.securityGroupID)
	req.SecurityGroupPolicySet = &vpc.SecurityGroupPolicySet{
		Ingress: policies,
		Version: common.StringPtr(version),
	}

	if _, err := p.client.DeleteSecurityGroupPolicies(req); err != nil {
		// ResourceNotFound 视为成功（幂等）：规则已不存在，不计入 Deleted
		if strings.Contains(err.Error(), "ResourceNotFound") {
			slog.Warn("安全组规则已不存在，按幂等处理", "provider", p.Name(), "数量", len(policies))
			return DeleteResult{Resolved: len(policies)}, nil
		}
		return DeleteResult{}, fmt.Errorf("删除安全组规则失败: %w", err)
	}
	return DeleteResult{Deleted: len(policies), Resolved: len(policies)}, nil
}

// ConvertPorts CVM 不支持逗号分隔，拆分为多条（唯一样本见 provider.ExpandPorts）
func (p *TCCVM) ConvertPorts(port string) []string {
	return ExpandPorts(config.CloudTCCVM, port)
}

// checkRuleLimit 检查安全组入站规则数；额外 Describe 仅用于配额保护。
func (p *TCCVM) checkRuleLimit(toAdd int) error {
	req := vpc.NewDescribeSecurityGroupPoliciesRequest()
	req.SecurityGroupId = common.StringPtr(p.securityGroupID)

	resp, err := p.client.DescribeSecurityGroupPolicies(req)
	if err != nil {
		return fmt.Errorf("查询规则数量失败: %w", err)
	}

	if resp == nil || resp.Response == nil || resp.Response.SecurityGroupPolicySet == nil {
		return fmt.Errorf("%w: CVM 配额检查未返回安全组规则集合", ErrSnapshotIncomplete)
	}
	ps := resp.Response.SecurityGroupPolicySet

	// 只检查入站；两个入站统计必须都存在，不能将缺失字段视作零。
	stats := ps.PolicyStatistics
	completeStats := stats != nil && stats.IngressIPv4TotalCount != nil && stats.IngressIPv6TotalCount != nil
	if !completeStats && ps.Ingress == nil {
		return fmt.Errorf("%w: CVM 配额检查缺少可用的入站计数", ErrSnapshotIncomplete)
	}
	// 入站数组条目数作为下界，避免地址族统计遗漏模板等条目。
	total := len(ps.Ingress)
	if completeStats {
		total = max(total, int(*stats.IngressIPv4TotalCount+*stats.IngressIPv6TotalCount))
	}
	if total+toAdd > 100 {
		return fmt.Errorf("安全组入站规则数将达 %d（上限 100），停止新增", total+toAdd)
	}
	if total+toAdd > 90 {
		slog.Warn("安全组入站规则接近上限", "当前", total, "新增", toAdd, "上限", 100)
	}
	return nil
}
