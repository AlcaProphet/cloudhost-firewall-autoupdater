package provider

import (
	"fmt"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	"github.com/alibabacloud-go/tea/tea"
)

func init() {
	Register(config.CloudAliECS, newAliECS)
}

// AliECS 阿里云 ECS 安全组 Provider
type AliECS struct {
	client          *ecs.Client
	securityGroupID string
	regionID        string
	targetIndex     int
}

func newAliECS(cfg config.TargetConfig, dbID int, pool *ClientPool) (Provider, error) {
	// 凭据只来自 pool 持有的不可变 Credentials，不再读取任何包级全局值
	creds := pool.Credentials()
	key := pool.CacheKey(config.CloudAliECS, cfg.Region)

	client, err := pool.GetOrCreate(key, func() (any, error) {
		// 超时由 newAliOpenAPIConfig 统一提供（Issue6 A1）：与扫描路径共用同一
		// ClientPool 缓存键，取值必须完全一致
		return ecs.NewClient(newAliOpenAPIConfig("ecs", cfg.Region, creds))
	})
	if err != nil {
		return nil, fmt.Errorf("创建 ECS Client 失败: %w", err)
	}

	return &AliECS{
		client:          client.(*ecs.Client),
		securityGroupID: cfg.ResourceID,
		regionID:        cfg.Region,
		targetIndex:     dbID,
	}, nil
}

func (p *AliECS) Name() string {
	return fmt.Sprintf("ali_ecs(%s)", p.securityGroupID)
}

func (p *AliECS) CloudType() config.CloudType {
	return config.CloudAliECS
}

func (p *AliECS) TargetIndex() int {
	return p.targetIndex
}

// GetSnapshot 查询安全组入站规则（NextToken 分页）并携带 SecurityGroupRuleId。
//
// Issue7 §6.4：必须完整推进 NextToken；若非空 token 与本次请求 token 相同，
// 或任一已见 token 再次出现，立即按快照不完整失败（ErrSnapshotIncomplete），
// 绝不返回半截规则，也绝不让调用方基于不完整快照产生删除计划（P3-25）。
func (p *AliECS) GetSnapshot() (RuleSnapshot, error) {
	var allRules []config.RuleInfo
	var nextToken *string
	maxResults := int32(500)
	seenTokens := make(map[string]bool)

	for {
		req := &ecs.DescribeSecurityGroupAttributeRequest{
			SecurityGroupId: tea.String(p.securityGroupID),
			RegionId:        tea.String(p.regionID),
			Direction:       tea.String("ingress"),
			MaxResults:      tea.Int32(maxResults),
			NextToken:       nextToken,
		}

		resp, err := p.client.DescribeSecurityGroupAttribute(req)
		if err != nil {
			return RuleSnapshot{}, fmt.Errorf("查询安全组规则失败: %w", err)
		}
		if resp == nil || resp.Body == nil {
			return RuleSnapshot{}, fmt.Errorf("%w: ECS 返回空响应", ErrSnapshotIncomplete)
		}

		body := resp.Body
		if body.Permissions != nil && body.Permissions.Permission != nil {
			for _, r := range body.Permissions.Permission {
				info := config.RuleInfo{
					Protocol:      strings.ToUpper(strVal(r.IpProtocol)),
					Port:          normalizeECSPort(strVal(r.PortRange)),
					CidrBlock:     strVal(r.SourceCidrIp),
					Ipv6CidrBlock: strVal(r.Ipv6SourceCidrIp),
					Action:        strings.ToUpper(strVal(r.Policy)),
					Description:   strVal(r.Description),
					RuleID:        strVal(r.SecurityGroupRuleId),
				}
				allRules = append(allRules, info)
			}
		}

		// NextToken 为空表示已到最后一页
		if body.NextToken == nil || *body.NextToken == "" {
			break
		}
		// token 进度保护：相同 token（或已见 token 重现）说明分页无法推进
		if nextToken != nil && *body.NextToken == *nextToken {
			return RuleSnapshot{}, fmt.Errorf("%w: ECS NextToken 未推进（%s）", ErrSnapshotIncomplete, *body.NextToken)
		}
		if seenTokens[*body.NextToken] {
			return RuleSnapshot{}, fmt.Errorf("%w: ECS NextToken 重复出现（%s）", ErrSnapshotIncomplete, *body.NextToken)
		}
		seenTokens[*body.NextToken] = true
		nextToken = body.NextToken
	}

	// 阿里云无规则版本号，Revision 合法为空；删除必须依赖同一快照回读的稳定 SecurityGroupRuleId。
	return RuleSnapshot{Rules: allRules}, nil
}

// GetRules 旧逐规则同步路径的兼容包装（已废弃，见 Provider 接口注释）。
func (p *AliECS) GetRules() ([]config.RuleInfo, error) {
	snapshot, err := p.GetSnapshot()
	if err != nil {
		return nil, err
	}
	return snapshot.Rules, nil
}

// CreateRules 增量添加入站规则（Permissions 数组，单次最多 100 条）。
//
// 正式新增集合已由 PlanTarget 排除 ICMPv6 unsupported 项；本层不额外跳过规则，
// 因此成功时恒为 {len(rules), 0}（Issue6 A11）。
func (p *AliECS) CreateRules(_ RuleSnapshot, rules []config.RuleAction) (CreateResult, error) {
	if len(rules) == 0 {
		return CreateResult{}, nil
	}

	// 分批提交（单次最多 100 条）
	batches := batchRules(rules, 100)
	written := 0
	for _, batch := range batches {
		if err := p.createBatch(batch); err != nil {
			return CreateResult{Written: written}, err
		}
		written += len(batch)
	}
	return CreateResult{Written: written, Skipped: 0}, nil
}

func (p *AliECS) createBatch(rules []config.RuleAction) error {
	var permissions []*ecs.AuthorizeSecurityGroupRequestPermissions
	for _, r := range rules {
		perm := &ecs.AuthorizeSecurityGroupRequestPermissions{
			IpProtocol:  tea.String(r.Protocol),
			PortRange:   tea.String(r.Port),
			Policy:      tea.String(strings.ToLower(r.Action)),
			Priority:    tea.String("1"),
			Description: tea.String(r.Description),
		}

		// IPv4 和 IPv6 互斥
		if r.Ipv6CidrBlock != "" {
			perm.Ipv6SourceCidrIp = tea.String(r.Ipv6CidrBlock)
		} else {
			perm.SourceCidrIp = tea.String(r.CidrBlock)
		}

		permissions = append(permissions, perm)
	}

	req := &ecs.AuthorizeSecurityGroupRequest{
		SecurityGroupId: tea.String(p.securityGroupID),
		RegionId:        tea.String(p.regionID),
		Permissions:     permissions,
	}

	_, err := p.client.AuthorizeSecurityGroup(req)
	if err != nil {
		return fmt.Errorf("添加安全组规则失败: %w", err)
	}
	return nil
}

// DeleteRules 用同一 S1 回读的 SecurityGroupRuleId 删除，每批最多 100 条。
//
// Issue7 §6.4（P2-02）：
//   - 只用 S1 的非空 SecurityGroupRuleId；任一候选缺定位即整体拒绝；
//   - 每批最多 100（PlatformAPIDocs/AliyunECSAPIGuide/RevokeSecurityGroup.md：
//     SecurityGroupRuleId 数组长度 0~100），150 条固定拆为 100 + 50；
//   - 任一批成功后确认计数如实累计；后续批次失败时返回 *PartialDeleteError，
//     已确认批次保留、剩余候选由调用方计入 cleanup_deferred。
func (p *AliECS) DeleteRules(_ RuleSnapshot, rules []config.RuleInfo) (DeleteResult, error) {
	if len(rules) == 0 {
		return DeleteResult{}, nil
	}

	ruleIDs := make([]*string, 0, len(rules))
	for _, r := range rules {
		if strings.TrimSpace(r.RuleID) == "" {
			return DeleteResult{}, fmt.Errorf("候选缺少可用的 SecurityGroupRuleId，拒绝删除以避免误删: %s", describeCloudRule(r))
		}
		ruleIDs = append(ruleIDs, tea.String(r.RuleID))
	}

	batches := batchStrings(ruleIDs, ecsDeleteBatchSize)
	deleted, resolved := 0, 0
	for i, batch := range batches {
		req := &ecs.RevokeSecurityGroupRequest{
			SecurityGroupId:     tea.String(p.securityGroupID),
			RegionId:            tea.String(p.regionID),
			SecurityGroupRuleId: batch,
		}
		if _, err := p.client.RevokeSecurityGroup(req); err != nil {
			delErr := fmt.Errorf("删除安全组规则失败（第 %d/%d 批，本批 %d 条）: %w", i+1, len(batches), len(batch), err)
			if deleted > 0 {
				return DeleteResult{Deleted: deleted, Resolved: resolved}, &PartialDeleteError{Deleted: deleted, Err: delErr}
			}
			return DeleteResult{Deleted: deleted, Resolved: resolved}, delErr
		}
		deleted += len(batch)
		resolved += len(batch)
	}
	return DeleteResult{Deleted: deleted, Resolved: resolved}, nil
}

// ecsDeleteBatchSize 是 RevokeSecurityGroup 单次请求的 SecurityGroupRuleId 上限。
const ecsDeleteBatchSize = 100

// batchStrings 按固定大小切分字符串指针切片（稳定顺序）。
func batchStrings(items []*string, size int) [][]*string {
	var batches [][]*string
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}
	return batches
}

// ConvertPorts 统一端口 → 阿里云斜杠格式（唯一样本见 provider.ExpandPorts）
func (p *AliECS) ConvertPorts(port string) []string {
	return ExpandPorts(config.CloudAliECS, port)
}

// normalizeECSPort 将 ECS 端口格式归一化
// "80/80" → "80"，"8000/8010" → "8000-8010"，"-1/-1" → "ALL"
func normalizeECSPort(port string) string {
	if port == "-1/-1" || port == "" {
		return "ALL"
	}
	if strings.Contains(port, "/") {
		parts := strings.SplitN(port, "/", 2)
		if parts[0] == parts[1] {
			return parts[0]
		}
		return parts[0] + "-" + parts[1]
	}
	return port
}

// batchRules 将规则分批（每批最多 n 条）
func batchRules(rules []config.RuleAction, n int) [][]config.RuleAction {
	var batches [][]config.RuleAction
	for i := 0; i < len(rules); i += n {
		end := i + n
		if end > len(rules) {
			end = len(rules)
		}
		batches = append(batches, rules[i:end])
	}
	return batches
}
