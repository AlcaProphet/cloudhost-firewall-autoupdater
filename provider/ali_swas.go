package provider

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	swas "github.com/alibabacloud-go/swas-open-20200601/v3/client"
	"github.com/alibabacloud-go/tea/tea"
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
	// 凭据只来自 pool 持有的不可变 Credentials，不再读取任何包级全局值
	creds := pool.Credentials()
	key := pool.CacheKey(config.CloudAliSWAS, cfg.Region)

	client, err := pool.GetOrCreate(key, func() (any, error) {
		// 超时由 newAliOpenAPIConfig 统一提供（Issue6 A1）：与扫描路径共用同一
		// ClientPool 缓存键，取值必须完全一致
		return swas.NewClient(newAliOpenAPIConfig("swas", cfg.Region, creds))
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

// GetSnapshot 查询防火墙规则（分页）并证明遍历完整。
//
// Issue7 §6.3：必须完整遍历 PageNumber，页失败即 snapshot 失败；TotalCount 可用时
// 以它作为完整性判据（返回条数少于总数即视为不完整），不可用时退回「本页不足一页」判据。
func (p *AliSWAS) GetSnapshot() (RuleSnapshot, error) {
	var allRules []config.RuleInfo
	pageNumber := int32(1)
	pageSize := int32(100)
	var totalCount *int32

	// 页数硬上限：PageNumber 严格递增，但服务端若持续返回非空重复页会无限翻页
	// （与 P3-25 同类的无界循环风险）。100 页 × 100 条远超单实例合理规则数。
	const maxPages = 100

	for page := 0; page < maxPages; page++ {
		req := &swas.ListFirewallRulesRequest{
			InstanceId: tea.String(p.instanceID),
			RegionId:   tea.String(p.regionID),
			PageNumber: tea.Int32(pageNumber),
			PageSize:   tea.Int32(pageSize),
		}

		resp, err := p.client.ListFirewallRules(req)
		if err != nil {
			return RuleSnapshot{}, fmt.Errorf("查询防火墙规则失败: %w", err)
		}
		if resp == nil || resp.Body == nil {
			return RuleSnapshot{}, fmt.Errorf("%w: SWAS 返回空响应", ErrSnapshotIncomplete)
		}

		body := resp.Body
		if body.TotalCount != nil {
			totalCount = body.TotalCount
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

		// TotalCount 可用时以它为权威判据（服务端可能返回少于 PageSize 的中间页）；
		// 不可用时退回「本页不足一页即最后一页」的既有判据。
		if totalCount != nil {
			if int32(len(allRules)) >= *totalCount {
				break
			}
			if len(body.FirewallRules) == 0 {
				return RuleSnapshot{}, fmt.Errorf("%w: SWAS 分页提前结束（已读 %d 条，云端声明共 %d 条）",
					ErrSnapshotIncomplete, len(allRules), *totalCount)
			}
			pageNumber++
			continue
		}
		if int32(len(body.FirewallRules)) < pageSize {
			break
		}
		pageNumber++
	}

	if totalCount != nil && int32(len(allRules)) < *totalCount {
		return RuleSnapshot{}, fmt.Errorf("%w: SWAS 仅读取 %d 条，云端声明共 %d 条",
			ErrSnapshotIncomplete, len(allRules), *totalCount)
	}

	// 阿里云无规则版本号，Revision 合法为空；删除必须依赖同一快照回读的稳定 RuleId。
	return RuleSnapshot{Rules: allRules}, nil
}

// GetRules 旧逐规则同步路径的兼容包装（已废弃，见 Provider 接口注释）。
func (p *AliSWAS) GetRules() ([]config.RuleInfo, error) {
	snapshot, err := p.GetSnapshot()
	if err != nil {
		return nil, err
	}
	return snapshot.Rules, nil
}

// CreateRules 批量创建防火墙规则。
//
// SWAS 的 CreateFirewallRules 请求参数**没有 Policy 字段**（见
// PlatformAPIDocs/AliyunSWASAPIGuide/CreateFirewallRules.md），因此 DROP 规则永远
// 无法表达：记 WARN 后跳过，并在返回值里如实计入 Skipped（Issue6 A11）。修复前
// 混合批次照常提交、全 DROP 时直接 return nil，调用方无法区分「全部写入成功」
// 与「全部跳过」，导致 added 虚增且每轮重复出现、永不收敛。
func (p *AliSWAS) CreateRules(_ RuleSnapshot, rules []config.RuleAction) (CreateResult, error) {
	if len(rules) == 0 {
		return CreateResult{}, nil
	}

	var fwRules []*swas.CreateFirewallRulesRequestFirewallRules
	skipped := 0
	for _, r := range rules {
		// DROP 规则不支持：SWAS API 无 Policy 字段，规则均为 accept
		if strings.EqualFold(r.Action, "DROP") {
			slog.Warn("SWAS 不支持 DROP 规则，跳过", "description", r.Description)
			skipped++
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
		// 全部被跳过：没有发起任何请求，但必须如实报告跳过条数
		return CreateResult{Written: 0, Skipped: skipped}, nil
	}

	req := &swas.CreateFirewallRulesRequest{
		InstanceId:    tea.String(p.instanceID),
		RegionId:      tea.String(p.regionID),
		FirewallRules: fwRules,
	}

	if _, err := p.client.CreateFirewallRules(req); err != nil {
		return CreateResult{}, fmt.Errorf("添加防火墙规则失败: %w", err)
	}
	return CreateResult{Written: len(fwRules), Skipped: skipped}, nil
}

// DeleteRules 按同一 S1 回读的稳定 RuleId 批量删除。
//
// Issue7 §6.3：cleanup 只使用 S1 的非空 RuleId；任一候选缺 RuleId 时整体拒绝，
// 绝不按规则值降级删除（SWAS 的删除定位只有 RuleId）。API 文档未声明 RuleIds 上限，
// 因此不凭空设定业务分批；遇服务端限制时以真实错误为准。
func (p *AliSWAS) DeleteRules(_ RuleSnapshot, rules []config.RuleInfo) (DeleteResult, error) {
	if len(rules) == 0 {
		return DeleteResult{}, nil
	}

	ruleIDs := make([]*string, 0, len(rules))
	for _, r := range rules {
		if strings.TrimSpace(r.RuleID) == "" {
			return DeleteResult{}, fmt.Errorf("候选缺少可用的 RuleId，拒绝删除以避免误删: %s", describeCloudRule(r))
		}
		ruleIDs = append(ruleIDs, tea.String(r.RuleID))
	}

	req := &swas.DeleteFirewallRulesRequest{
		InstanceId: tea.String(p.instanceID),
		RegionId:   tea.String(p.regionID),
		RuleIds:    ruleIDs,
	}

	if _, err := p.client.DeleteFirewallRules(req); err != nil {
		return DeleteResult{}, fmt.Errorf("删除防火墙规则失败: %w", err)
	}
	return DeleteResult{Deleted: len(ruleIDs), Resolved: len(ruleIDs)}, nil
}

// ConvertPorts 统一端口 → 阿里云斜杠格式（唯一样本见 provider.ExpandPorts）
func (p *AliSWAS) ConvertPorts(port string) []string {
	return ExpandPorts(config.CloudAliSWAS, port)
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
