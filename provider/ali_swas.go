package provider

import (
	"fmt"
	"log/slog"
	"strings"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	swas "github.com/alibabacloud-go/swas-open-20200601/v3/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/portconv"
)

func init() {
	Register(config.CloudAliSWAS, newAliSWAS)
}

// AliSWAS 阿里云轻量应用服务器 Provider
type AliSWAS struct {
	client      *swas.Client
	instanceID  string
	regionID    string
	targetIndex int
}

func newAliSWAS(cfg config.TargetConfig, dbID int, pool *ClientPool) (Provider, error) {
	key := string(config.CloudAliSWAS) + "|" + cfg.Region + "|" + getAliAccessID()

	client, err := pool.GetOrCreate(key, func() (any, error) {
		config := &openapi.Config{
			AccessKeyId:     tea.String(getAliAccessID()),
			AccessKeySecret: tea.String(getAliAccessKey()),
			Endpoint:        tea.String(fmt.Sprintf("swas.%s.aliyuncs.com", cfg.Region)),
		}
		return swas.NewClient(config)
	})
	if err != nil {
		return nil, fmt.Errorf("创建 SWAS Client 失败: %w", err)
	}

	return &AliSWAS{
		client:      client.(*swas.Client),
		instanceID:  cfg.ResourceID,
		regionID:    cfg.Region,
		targetIndex: dbID,
	}, nil
}

func (p *AliSWAS) Name() string {
	return fmt.Sprintf("ali_swas(%s)", p.instanceID)
}

func (p *AliSWAS) CloudType() config.CloudType {
	return config.CloudAliSWAS
}

func (p *AliSWAS) TargetIndex() int {
	return p.targetIndex
}

// GetRules 查询防火墙规则（分页）
func (p *AliSWAS) GetRules() ([]config.RuleInfo, error) {
	var allRules []config.RuleInfo
	pageNumber := int32(1)
	pageSize := int32(100)

	for {
		req := &swas.ListFirewallRulesRequest{
			InstanceId: tea.String(p.instanceID),
			RegionId:   tea.String(p.regionID),
			PageNumber: tea.Int32(pageNumber),
			PageSize:   tea.Int32(pageSize),
		}

		resp, err := p.client.ListFirewallRules(req)
		if err != nil {
			return nil, fmt.Errorf("查询防火墙规则失败: %w", err)
		}

		body := resp.Body
		if body == nil || body.FirewallRules == nil {
			break
		}

		for _, r := range body.FirewallRules {
			info := config.RuleInfo{
				Protocol:    strVal(r.RuleProtocol),
				Port:        normalizeSWASPort(strVal(r.Port)),
				CidrBlock:   strVal(r.SourceCidrIp),
				Action:      strings.ToUpper(strVal(r.Policy)),
				Description: strVal(r.Remark),
				RuleID:      strVal(r.RuleId),
			}
			allRules = append(allRules, info)
		}

		// 分页判断
		if int32(len(body.FirewallRules)) < pageSize {
			break
		}
		pageNumber++
	}

	return allRules, nil
}

// CreateRules 批量创建防火墙规则
func (p *AliSWAS) CreateRules(rules []config.RuleAction) error {
	if len(rules) == 0 {
		return nil
	}

	var fwRules []*swas.CreateFirewallRulesRequestFirewallRules
	for _, r := range rules {
		// DROP 规则不支持：SWAS API 无 Policy 字段，规则均为 accept
		if strings.EqualFold(r.Action, "DROP") {
			slog.Warn("SWAS 不支持 DROP 规则，跳过", "description", r.Description)
			continue
		}

		fwRule := &swas.CreateFirewallRulesRequestFirewallRules{
			Port:         tea.String(r.Port),
			Remark:       tea.String(r.Description),
			RuleProtocol: tea.String(r.Protocol),
			SourceCidrIp: tea.String(r.CidrBlock),
		}
		fwRules = append(fwRules, fwRule)
	}

	if len(fwRules) == 0 {
		return nil
	}

	req := &swas.CreateFirewallRulesRequest{
		InstanceId:    tea.String(p.instanceID),
		RegionId:      tea.String(p.regionID),
		FirewallRules: fwRules,
	}

	_, err := p.client.CreateFirewallRules(req)
	if err != nil {
		return fmt.Errorf("添加防火墙规则失败: %w", err)
	}
	return nil
}

// DeleteRules 批量删除防火墙规则
func (p *AliSWAS) DeleteRules(rules []config.RuleInfo) error {
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

	req := &swas.DeleteFirewallRulesRequest{
		InstanceId: tea.String(p.instanceID),
		RegionId:   tea.String(p.regionID),
		RuleIds:    ruleIDs,
	}

	_, err := p.client.DeleteFirewallRules(req)
	if err != nil {
		return fmt.Errorf("删除防火墙规则失败: %w", err)
	}
	return nil
}

// ConvertPorts 统一端口 → 阿里云斜杠格式
func (p *AliSWAS) ConvertPorts(port string) []string {
	ports := portconv.Parse(port)
	var result []string
	for _, p := range ports {
		result = append(result, portconv.ToSlash(p))
	}
	return result
}

// normalizeSWASPort 将阿里云端口格式归一化
// "80/80" → "80"，"8000/8010" → "8000-8010"，"-1/-1" → "ALL"
func normalizeSWASPort(port string) string {
	if port == "-1/-1" || port == "" {
		return "ALL"
	}
	if strings.Contains(port, "/") {
		parts := strings.SplitN(port, "/", 2)
		if parts[0] == parts[1] {
			return parts[0] // 单端口
		}
		return parts[0] + "-" + parts[1] // 范围
	}
	return port
}
