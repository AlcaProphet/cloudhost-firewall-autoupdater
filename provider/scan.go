package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	swas "github.com/alibabacloud-go/swas-open-20200601/v3/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	lighthouse "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/lighthouse/v20200324"
	vpc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
)

// ScannedCloudResource 扫描到的云资源（实例或安全组）
type ScannedCloudResource struct {
	ResourceID string // 实例 ID（lhins-xxx / UUID）或安全组 ID（sg-xxx）
	Name       string // 资源名称（实例名/安全组名，可能为空）
	Region     string // 资源所属地域
}

// ScanResources 扫描指定云厂商+地域的资源列表，供前端"扫描资源"按钮调用。
// 凭据只取自传入 pool 持有的不可变 Credentials；调用方必须传请求开始时
// 取得的同一个 RuntimeState.Pool，不得另行读取数据库或全局凭据。
// 各平台对应 API：
//   - tc_lighthouse: DescribeInstances（查询实例）
//   - tc_cvm:        DescribeSecurityGroups（查询安全组）
//   - ali_swas:      ListInstances（查询实例）
//   - ali_ecs:       DescribeSecurityGroups（查询安全组）
func ScanResources(ct config.CloudType, region string, pool *ClientPool) ([]ScannedCloudResource, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cloudReadBudget)
	defer cancel()
	return scanResourcesBounded(ctx, ct, region, pool)
}

func scanResourcesBounded(ctx context.Context, ct config.CloudType, region string, pool *ClientPool) (resources []ScannedCloudResource, err error) {
	defer func() {
		if cause := ctx.Err(); cause != nil {
			resources = nil
			err = readBudgetError(ctx, "资源扫描", err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch ct {
	case config.CloudTCLighthouse:
		return scanTCLighthouse(ctx, region, pool)
	case config.CloudTCCVM:
		return scanTCCVM(ctx, region, pool)
	case config.CloudAliSWAS:
		return scanAliSWAS(ctx, region, pool)
	case config.CloudAliECS:
		return scanAliECS(ctx, region, pool)
	default:
		return nil, fmt.Errorf("不支持的云产品类型: %s", ct)
	}
}

// scanTCLighthouse 扫描腾讯云轻量云实例（DescribeInstances，Offset/Limit 分页）
func scanTCLighthouse(ctx context.Context, region string, pool *ClientPool) ([]ScannedCloudResource, error) {
	creds := pool.Credentials()
	client, err := pool.GetOrCreate(pool.CacheKey(config.CloudTCLighthouse, region), func() (any, error) {
		credential := common.NewCredential(creds.TencentSecretID, creds.TencentSecretKey)
		cpf := profile.NewClientProfile()
		cpf.HttpProfile.Endpoint = "lighthouse.tencentcloudapi.com"
		return lighthouse.NewClient(credential, region, cpf)
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Lighthouse Client 失败: %w", err)
	}
	lh := client.(*lighthouse.Client)

	var resources []ScannedCloudResource
	var offset int64
	limit := int64(100)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req := lighthouse.NewDescribeInstancesRequest()
		req.Offset = common.Int64Ptr(offset)
		req.Limit = common.Int64Ptr(limit)
		resp, err := lh.DescribeInstancesWithContext(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("查询实例列表失败: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := decodeLighthouseScanPage(resp, region)
		if err != nil {
			return nil, err
		}
		resources = append(resources, page...)
		// 返回数量不足一页时结束；结构异常已由页解码拒绝。
		if int64(len(page)) < limit {
			break
		}
		offset += limit
	}
	return resources, nil
}

// scanTCCVM 扫描腾讯云 CVM 安全组（DescribeSecurityGroups，Offset/Limit 分页，Offset 为字符串类型）
func scanTCCVM(ctx context.Context, region string, pool *ClientPool) ([]ScannedCloudResource, error) {
	creds := pool.Credentials()
	client, err := pool.GetOrCreate(pool.CacheKey(config.CloudTCCVM, region), func() (any, error) {
		credential := common.NewCredential(creds.TencentSecretID, creds.TencentSecretKey)
		cpf := profile.NewClientProfile()
		cpf.HttpProfile.Endpoint = "vpc.tencentcloudapi.com"
		return vpc.NewClient(credential, region, cpf)
	})
	if err != nil {
		return nil, fmt.Errorf("创建 VPC Client 失败: %w", err)
	}
	v := client.(*vpc.Client)

	var resources []ScannedCloudResource
	offset := 0
	limit := "100"
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req := vpc.NewDescribeSecurityGroupsRequest()
		req.Offset = common.StringPtr(strconv.Itoa(offset))
		req.Limit = common.StringPtr(limit)
		resp, err := v.DescribeSecurityGroupsWithContext(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("查询安全组列表失败: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := decodeCVMScanPage(resp, region)
		if err != nil {
			return nil, err
		}
		resources = append(resources, page...)
		// 返回数量不足一页时结束；结构异常已由页解码拒绝。
		if len(page) < 100 {
			break
		}
		offset += 100
	}
	return resources, nil
}

// scanAliSWAS 扫描阿里云轻量云实例，按单次调用的总数与唯一 ID 验证分页完整性。
// 总数使用 SDK 解码后的值；不自动重扫，不提供跨页原子快照。
// 单次扫描读取预算由 ScanResources 统一提供。
func scanAliSWAS(ctx context.Context, region string, pool *ClientPool) ([]ScannedCloudResource, error) {
	creds := pool.Credentials()
	client, err := pool.GetOrCreate(pool.CacheKey(config.CloudAliSWAS, region), func() (any, error) {
		// 与正式 SWAS Provider 共用同一缓存键与同一超时样本（Issue6 A1）
		return swas.NewClient(newAliOpenAPIConfig("swas", region, creds))
	})
	if err != nil {
		return nil, fmt.Errorf("创建 SWAS Client 失败: %w", err)
	}
	base := client.(*swas.Client)
	operation := newAliReadHTTP(ctx, tea.IntValue(base.ConnectTimeout), tea.IntValue(base.ReadTimeout))
	defer operation.close()
	s, createErr := swas.NewClient(aliReadConfig(&base.Client, operation))
	if createErr != nil {
		return nil, fmt.Errorf("创建 SWAS 读取 Client 失败: %w", createErr)
	}

	resources := make([]ScannedCloudResource, 0)
	seenIDs := make(map[string]struct{})
	const pageSize int32 = 100
	var totalCount int32
	for pageNumber := int32(1); ; pageNumber++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req := &swas.ListInstancesRequest{
			RegionId:   tea.String(region),
			PageNumber: tea.Int32(pageNumber),
			PageSize:   tea.Int32(pageSize),
		}
		resp, err := s.ListInstances(req)
		if err != nil {
			return nil, fmt.Errorf("查询实例列表失败: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := decodeSWASScanPage(resp, region)
		if err != nil {
			return nil, err
		}
		body := resp.Body
		if body.TotalCount == nil || *body.TotalCount < 0 {
			return nil, fmt.Errorf("%w: SWAS 资源扫描总数缺失或无效", ErrSnapshotIncomplete)
		}
		if pageNumber == 1 {
			// 复制数值作为本次扫描基准，不持有响应字段指针。
			totalCount = *body.TotalCount
		} else if *body.TotalCount != totalCount {
			return nil, fmt.Errorf("%w: SWAS 资源扫描分页总数发生变化", ErrSnapshotIncomplete)
		}
		if body.PageNumber != nil && *body.PageNumber != pageNumber {
			return nil, fmt.Errorf("%w: SWAS 资源扫描返回页码不一致", ErrSnapshotIncomplete)
		}
		if body.PageSize != nil && *body.PageSize != pageSize {
			return nil, fmt.Errorf("%w: SWAS 资源扫描返回页大小不一致", ErrSnapshotIncomplete)
		}
		if len(page) > int(pageSize) {
			return nil, fmt.Errorf("%w: SWAS 资源扫描单页条数超限", ErrSnapshotIncomplete)
		}
		for _, item := range page {
			if _, exists := seenIDs[item.ResourceID]; exists {
				return nil, fmt.Errorf("%w: SWAS 资源扫描存在重复资源标识", ErrSnapshotIncomplete)
			}
			seenIDs[item.ResourceID] = struct{}{}
		}
		count := int64(len(resources)) + int64(len(page))
		if count > int64(totalCount) {
			return nil, fmt.Errorf("%w: SWAS 资源扫描已读条数超出总数", ErrSnapshotIncomplete)
		}
		resources = append(resources, page...)
		// 满页也可直接完成；未达到总数的短页不能解释为完整列表。
		if count == int64(totalCount) {
			return resources, nil
		}
		if len(page) < int(pageSize) {
			return nil, fmt.Errorf("%w: SWAS 资源扫描在达到总数前返回短页", ErrSnapshotIncomplete)
		}
	}
}

// decodeSWASScanPage 校验并转换实例页；缺失集合失败，显式空数组合法。
// TotalCount 为零不豁免结构校验，名称允许为空，资源 ID 与名称保持原值。
func decodeSWASScanPage(resp *swas.ListInstancesResponse, region string) ([]ScannedCloudResource, error) {
	if resp == nil || resp.Body == nil {
		return nil, fmt.Errorf("%w: SWAS 资源扫描响应缺失", ErrSnapshotIncomplete)
	}
	if resp.Body.Instances == nil {
		return nil, fmt.Errorf("%w: SWAS 资源扫描集合缺失", ErrSnapshotIncomplete)
	}
	resources := make([]ScannedCloudResource, 0, len(resp.Body.Instances))
	for _, inst := range resp.Body.Instances {
		if inst == nil || strVal(inst.InstanceId) == "" {
			return nil, fmt.Errorf("%w: SWAS 资源扫描资源标识缺失", ErrSnapshotIncomplete)
		}
		resources = append(resources, ScannedCloudResource{
			ResourceID: strVal(inst.InstanceId),
			Name:       strVal(inst.InstanceName),
			Region:     region,
		})
	}
	return resources, nil
}

// scanAliECS 扫描阿里云 ECS 安全组（DescribeSecurityGroups，NextToken 分页）
func scanAliECS(ctx context.Context, region string, pool *ClientPool) ([]ScannedCloudResource, error) {
	creds := pool.Credentials()
	client, err := pool.GetOrCreate(pool.CacheKey(config.CloudAliECS, region), func() (any, error) {
		// 与正式 ECS Provider 共用同一缓存键与同一超时样本（Issue6 A1）
		return ecs.NewClient(newAliOpenAPIConfig("ecs", region, creds))
	})
	if err != nil {
		return nil, fmt.Errorf("创建 ECS Client 失败: %w", err)
	}
	base := client.(*ecs.Client)
	operation := newAliReadHTTP(ctx, tea.IntValue(base.ConnectTimeout), tea.IntValue(base.ReadTimeout))
	defer operation.close()
	e, createErr := ecs.NewClient(aliReadConfig(&base.Client, operation))
	if createErr != nil {
		return nil, fmt.Errorf("创建 ECS 读取 Client 失败: %w", createErr)
	}

	resources := make([]ScannedCloudResource, 0)
	var nextToken *string
	seenTokens := make(map[string]bool)
	maxResults := int32(100)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req := &ecs.DescribeSecurityGroupsRequest{
			RegionId:   tea.String(region),
			MaxResults: tea.Int32(maxResults),
			NextToken:  nextToken,
		}
		resp, err := e.DescribeSecurityGroups(req)
		if err != nil {
			return nil, fmt.Errorf("查询安全组列表失败: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, token, err := decodeECSScanPage(resp, region)
		if err != nil {
			return nil, err
		}
		resources = append(resources, page...)
		// 只有返回的 token 为空才是末页；显式空数组不提前终止分页。
		if token == "" {
			break
		}
		// token 是不透明字符串，原样传递；历史集合同时识别未推进和环路。
		if seenTokens[token] {
			return nil, fmt.Errorf("%w: ECS 资源扫描分页 token 未推进或重复", ErrSnapshotIncomplete)
		}
		seenTokens[token] = true
		nextToken = tea.String(token)
	}
	return resources, nil
}

// decodeECSScanPage 将 SDK 响应转换为有效资源页；结构异常不得伪装为空资源。
// 缺失/null 集合采用保守失败策略，显式空数组合法；名称允许为空。
func decodeECSScanPage(resp *ecs.DescribeSecurityGroupsResponse, region string) ([]ScannedCloudResource, string, error) {
	if resp == nil || resp.Body == nil {
		return nil, "", fmt.Errorf("%w: ECS 资源扫描响应缺失", ErrSnapshotIncomplete)
	}
	body := resp.Body
	if body.SecurityGroups == nil || body.SecurityGroups.SecurityGroup == nil {
		return nil, "", fmt.Errorf("%w: ECS 资源扫描集合缺失", ErrSnapshotIncomplete)
	}
	resources := make([]ScannedCloudResource, 0, len(body.SecurityGroups.SecurityGroup))
	for _, sg := range body.SecurityGroups.SecurityGroup {
		if sg == nil || strVal(sg.SecurityGroupId) == "" {
			return nil, "", fmt.Errorf("%w: ECS 资源扫描资源标识缺失", ErrSnapshotIncomplete)
		}
		resources = append(resources, ScannedCloudResource{
			ResourceID: strVal(sg.SecurityGroupId),
			Name:       strVal(sg.SecurityGroupName),
			Region:     region,
		})
	}
	return resources, strVal(body.NextToken), nil
}

// decodeLighthouseScanPage 校验并转换资源页；缺失集合失败，显式空数组合法。
func decodeLighthouseScanPage(resp *lighthouse.DescribeInstancesResponse, region string) ([]ScannedCloudResource, error) {
	if resp == nil || resp.Response == nil {
		return nil, fmt.Errorf("%w: Lighthouse 资源扫描响应缺失", ErrSnapshotIncomplete)
	}
	entries := resp.Response.InstanceSet
	if entries == nil {
		return nil, fmt.Errorf("%w: Lighthouse 资源扫描集合缺失", ErrSnapshotIncomplete)
	}
	page := make([]ScannedCloudResource, 0, len(entries))
	for _, item := range entries {
		if item == nil || strVal(item.InstanceId) == "" {
			return nil, fmt.Errorf("%w: Lighthouse 资源扫描资源标识缺失", ErrSnapshotIncomplete)
		}
		page = append(page, ScannedCloudResource{ResourceID: strVal(item.InstanceId), Name: strVal(item.InstanceName), Region: region})
	}
	return page, nil
}

// decodeCVMScanPage 校验并转换资源页；缺失集合失败，显式空数组合法。
func decodeCVMScanPage(resp *vpc.DescribeSecurityGroupsResponse, region string) ([]ScannedCloudResource, error) {
	if resp == nil || resp.Response == nil {
		return nil, fmt.Errorf("%w: CVM 资源扫描响应缺失", ErrSnapshotIncomplete)
	}
	entries := resp.Response.SecurityGroupSet
	if entries == nil {
		return nil, fmt.Errorf("%w: CVM 资源扫描集合缺失", ErrSnapshotIncomplete)
	}
	page := make([]ScannedCloudResource, 0, len(entries))
	for _, item := range entries {
		if item == nil || strVal(item.SecurityGroupId) == "" {
			return nil, fmt.Errorf("%w: CVM 资源扫描资源标识缺失", ErrSnapshotIncomplete)
		}
		page = append(page, ScannedCloudResource{ResourceID: strVal(item.SecurityGroupId), Name: strVal(item.SecurityGroupName), Region: region})
	}
	return page, nil
}
