package provider

import (
	"fmt"
	"strconv"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	swas "github.com/alibabacloud-go/swas-open-20200601/v3/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
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
// 凭据需先经 SetCredentials 注入（webui/api 层负责从 Store 读取）。
// 各平台对应 API：
//   - tc_lighthouse: DescribeInstances（查询实例）
//   - tc_cvm:        DescribeSecurityGroups（查询安全组）
//   - ali_swas:      ListInstances（查询实例）
//   - ali_ecs:       DescribeSecurityGroups（查询安全组）
func ScanResources(ct config.CloudType, region string, pool *ClientPool) ([]ScannedCloudResource, error) {
	switch ct {
	case config.CloudTCLighthouse:
		return scanTCLighthouse(region, pool)
	case config.CloudTCCVM:
		return scanTCCVM(region, pool)
	case config.CloudAliSWAS:
		return scanAliSWAS(region, pool)
	case config.CloudAliECS:
		return scanAliECS(region, pool)
	default:
		return nil, fmt.Errorf("不支持的云产品类型: %s", ct)
	}
}

// scanTCLighthouse 扫描腾讯云轻量云实例（DescribeInstances，Offset/Limit 分页）
func scanTCLighthouse(region string, pool *ClientPool) ([]ScannedCloudResource, error) {
	client, err := pool.GetOrCreate(string(config.CloudTCLighthouse)+"|"+region+"|"+getTCAccessID(), func() (any, error) {
		credential := common.NewCredential(getTCAccessID(), getTCAccessKey())
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
		req := lighthouse.NewDescribeInstancesRequest()
		req.Offset = common.Int64Ptr(offset)
		req.Limit = common.Int64Ptr(limit)
		resp, err := lh.DescribeInstances(req)
		if err != nil {
			return nil, fmt.Errorf("查询实例列表失败: %w", err)
		}
		for _, inst := range resp.Response.InstanceSet {
			resources = append(resources, ScannedCloudResource{
				ResourceID: strVal(inst.InstanceId),
				Name:       strVal(inst.InstanceName),
				Region:     region,
			})
		}
		// 返回数量 < limit 表示已到最后一页
		if int64(len(resp.Response.InstanceSet)) < limit {
			break
		}
		offset += limit
	}
	return resources, nil
}

// scanTCCVM 扫描腾讯云 CVM 安全组（DescribeSecurityGroups，Offset/Limit 分页，Offset 为字符串类型）
func scanTCCVM(region string, pool *ClientPool) ([]ScannedCloudResource, error) {
	client, err := pool.GetOrCreate(string(config.CloudTCCVM)+"|"+region+"|"+getTCAccessID(), func() (any, error) {
		credential := common.NewCredential(getTCAccessID(), getTCAccessKey())
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
		req := vpc.NewDescribeSecurityGroupsRequest()
		req.Offset = common.StringPtr(strconv.Itoa(offset))
		req.Limit = common.StringPtr(limit)
		resp, err := v.DescribeSecurityGroups(req)
		if err != nil {
			return nil, fmt.Errorf("查询安全组列表失败: %w", err)
		}
		for _, sg := range resp.Response.SecurityGroupSet {
			resources = append(resources, ScannedCloudResource{
				ResourceID: strVal(sg.SecurityGroupId),
				Name:       strVal(sg.SecurityGroupName),
				Region:     region,
			})
		}
		if int64(len(resp.Response.SecurityGroupSet)) < 100 {
			break
		}
		offset += 100
	}
	return resources, nil
}

// scanAliSWAS 扫描阿里云轻量云实例（ListInstances，PageNumber/PageSize 分页）
func scanAliSWAS(region string, pool *ClientPool) ([]ScannedCloudResource, error) {
	client, err := pool.GetOrCreate(string(config.CloudAliSWAS)+"|"+region+"|"+getAliAccessID(), func() (any, error) {
		cfg := &openapi.Config{
			AccessKeyId:     tea.String(getAliAccessID()),
			AccessKeySecret: tea.String(getAliAccessKey()),
			Endpoint:        tea.String(fmt.Sprintf("swas.%s.aliyuncs.com", region)),
		}
		return swas.NewClient(cfg)
	})
	if err != nil {
		return nil, fmt.Errorf("创建 SWAS Client 失败: %w", err)
	}
	s := client.(*swas.Client)

	var resources []ScannedCloudResource
	pageNumber := int32(1)
	pageSize := int32(100)
	for {
		req := &swas.ListInstancesRequest{
			RegionId:   tea.String(region),
			PageNumber: tea.Int32(pageNumber),
			PageSize:   tea.Int32(pageSize),
		}
		resp, err := s.ListInstances(req)
		if err != nil {
			return nil, fmt.Errorf("查询实例列表失败: %w", err)
		}
		body := resp.Body
		if body == nil || body.Instances == nil {
			break
		}
		for _, inst := range body.Instances {
			resources = append(resources, ScannedCloudResource{
				ResourceID: strVal(inst.InstanceId),
				Name:       strVal(inst.InstanceName),
				Region:     region,
			})
		}
		if int32(len(body.Instances)) < pageSize {
			break
		}
		pageNumber++
	}
	return resources, nil
}

// scanAliECS 扫描阿里云 ECS 安全组（DescribeSecurityGroups，NextToken 分页）
func scanAliECS(region string, pool *ClientPool) ([]ScannedCloudResource, error) {
	client, err := pool.GetOrCreate(string(config.CloudAliECS)+"|"+region+"|"+getAliAccessID(), func() (any, error) {
		cfg := &openapi.Config{
			AccessKeyId:     tea.String(getAliAccessID()),
			AccessKeySecret: tea.String(getAliAccessKey()),
			Endpoint:        tea.String(fmt.Sprintf("ecs.%s.aliyuncs.com", region)),
		}
		return ecs.NewClient(cfg)
	})
	if err != nil {
		return nil, fmt.Errorf("创建 ECS Client 失败: %w", err)
	}
	e := client.(*ecs.Client)

	var resources []ScannedCloudResource
	var nextToken *string
	maxResults := int32(100)
	for {
		req := &ecs.DescribeSecurityGroupsRequest{
			RegionId:   tea.String(region),
			MaxResults: tea.Int32(maxResults),
			NextToken:  nextToken,
		}
		resp, err := e.DescribeSecurityGroups(req)
		if err != nil {
			return nil, fmt.Errorf("查询安全组列表失败: %w", err)
		}
		body := resp.Body
		if body == nil || body.SecurityGroups == nil || body.SecurityGroups.SecurityGroup == nil {
			break
		}
		for _, sg := range body.SecurityGroups.SecurityGroup {
			resources = append(resources, ScannedCloudResource{
				ResourceID: strVal(sg.SecurityGroupId),
				Name:       strVal(sg.SecurityGroupName),
				Region:     region,
			})
		}
		// NextToken 为空表示已到最后一页
		if body.NextToken == nil || *body.NextToken == "" {
			break
		}
		nextToken = body.NextToken
	}
	return resources, nil
}
