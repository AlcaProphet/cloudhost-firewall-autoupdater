package provider

import (
	"fmt"
	"strings"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/portconv"
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
	key := string(config.CloudAliECS) + "|" + cfg.Region + "|" + getAliAccessID()

	client, err := pool.GetOrCreate(key, func() (any, error) {
		config := &openapi.Config{
			AccessKeyId:     tea.String(getAliAccessID()),
			AccessKeySecret: tea.String(getAliAccessKey()),
			Endpoint:        tea.String(fmt.Sprintf("ecs.%s.aliyuncs.com", cfg.Region)),
		}
		return ecs.NewClient(config)
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

// GetRules 查询安全组入站规则（NextToken 分页）
func (p *AliECS) GetRules() ([]config.RuleInfo, error) {
	var allRules []config.RuleInfo
	var nextToken *string
	maxResults := int32(500)

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
			return nil, fmt.Errorf("查询安全组规则失败: %w", err)
		}

		body := resp.Body
		if body == nil || body.Permissions == nil || body.Permissions.Permission == nil {
			break
		}

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

		// NextToken 为空表示已到最后一页
		if body.NextToken == nil || *body.NextToken == "" {
			break
		}
		nextToken = body.NextToken
	}

	return allRules, nil
}

// CreateRules 增量添加入站规则（Permissions 数组，单次最多 100 条）
func (p *AliECS) CreateRules(rules []config.RuleAction) error {
	if len(rules) == 0 {
		return nil
	}

	// 分批提交（单次最多 100 条）
	batches := batchRules(rules, 100)
	for _, batch := range batches {
		if err := p.createBatch(batch); err != nil {
			return err
		}
	}
	return nil
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

// DeleteRules 用 SecurityGroupRuleId 数组删除
func (p *AliECS) DeleteRules(rules []config.RuleInfo) error {
	if len(rules) == 0 {
		return nil
	}

	var ruleIDs []*string
	for _, r := range rules {
		if r.RuleID != "" {
			ruleIDs = append(ruleIDs, tea.String(r.RuleID))
		}
	}

	if len(ruleIDs) == 0 {
		return nil
	}

	req := &ecs.RevokeSecurityGroupRequest{
		SecurityGroupId:   tea.String(p.securityGroupID),
		RegionId:          tea.String(p.regionID),
		SecurityGroupRuleId: ruleIDs,
	}

	_, err := p.client.RevokeSecurityGroup(req)
	if err != nil {
		return fmt.Errorf("删除安全组规则失败: %w", err)
	}
	return nil
}

// ConvertPorts 统一端口 → 阿里云斜杠格式
func (p *AliECS) ConvertPorts(port string) []string {
	ports := portconv.Parse(port)
	var result []string
	for _, p := range ports {
		result = append(result, portconv.ToSlash(p))
	}
	return result
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
