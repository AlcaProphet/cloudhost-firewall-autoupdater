package provider

import (
	"sync"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/tea"
)

// 阿里云 SDK 单次请求超时（毫秒）——Issue6 A1。
//
// 零值语义：darabonba-openapi 把这两个值经 tea/dara 映射为
//   - http.Client.Timeout = Connect + Read（单次请求整体上限）
//   - Transport.ResponseHeaderTimeout = Read（响应头上限）
//   - net.Dialer.Timeout = Connect（拨号上限）
//
// 三者全 0 即完全无界（连接成功后不返回、TLS/响应头挂起或网络黑洞会让同步轮次
// 与 Stop 后的 Wait 长期停滞）。因此四处构造点必须显式设置，且取值完全一致：
// 正式 Provider（ali_swas.go / ali_ecs.go）与两条扫描路径（scan.go）共用同一个
// ClientPool 缓存键（cloud_type + region + 账户），先创建者胜出，取值漂移会让
// 实际生效的超时取决于调用顺序。
const (
	aliDefaultConnectTimeoutMS = 10_000 // 拨号上限 10s
	aliDefaultReadTimeoutMS    = 30_000 // 响应头上限 30s；单次请求整体上限 = Connect + Read = 40s
)

// aliConnectTimeoutMS / aliReadTimeoutMS 是实际生效的超时值（毫秒）。
//
// 非导出变量仅为测试接缝（Issue6 §六.4 F8 用户已裁决）：默认值就等于上面的常量
// （10_000 / 30_000），生产零行为变化；用例可缩短取值以断言 deadline 机制。
// 全仓不使用 t.Parallel，接缝不引入竞态。
var (
	aliConnectTimeoutMS = aliDefaultConnectTimeoutMS
	aliReadTimeoutMS    = aliDefaultReadTimeoutMS
)

// aliResolveEndpoint 把「服务名 + 地域」解析为 (Endpoint, Protocol)。
//
// 默认仍为「<service>.<region>.aliyuncs.com + https」，与修改前的硬编码完全一致；
// 非导出变量仅为测试接缝（Issue6 §六.4 F8），用例据此把四条构造路径指向本地
// 阻塞服务，从而在真实 HTTP 客户端上验证超时，而不是伪造一个不可取消的等待。
var aliResolveEndpoint = func(service, region string) (string, string) {
	return service + "." + region + ".aliyuncs.com", "https"
}

// newAliOpenAPIConfig 构造阿里云 openapi.Config（唯一样本，避免四处超时值漂移）。
//
// 只设置凭据、Endpoint、Protocol 与两个超时；不新增 SQLite 设置或环境变量，
// 也不改腾讯云 SDK 的既有 60s 行为。
func newAliOpenAPIConfig(service, region string, creds Credentials) *openapi.Config {
	endpoint, protocol := aliResolveEndpoint(service, region)
	return &openapi.Config{
		AccessKeyId:     tea.String(creds.AliyunAccessKeyID),
		AccessKeySecret: tea.String(creds.AliyunAccessKeySecret),
		Endpoint:        tea.String(endpoint),
		Protocol:        tea.String(protocol),
		ConnectTimeout:  tea.Int(aliConnectTimeoutMS),
		ReadTimeout:     tea.Int(aliReadTimeoutMS),
	}
}

// Credentials 四个云访问凭据的不可变值类型（Build6 §12.6）。
//
// 用值类型取代进程级可变全局凭据：ClientPool 在创建时接收本值，构造之后
// 不提供任何 setter，Provider 工厂、连接测试与资源扫描都只读同一份凭据，
// 因此并发账户切换不可能发生「同一轮里混用新旧凭据」。
type Credentials struct {
	TencentSecretID       string
	TencentSecretKey      string
	AliyunAccessKeyID     string
	AliyunAccessKeySecret string
}

// AccountKey 返回可用于 client 缓存隔离的账户标识。
//
// 只使用 AccessKey ID / SecretId（不含密钥本身），足以区分账户；
// 空凭据返回空串，仍可与已配置账户的缓存条目区分开。
func (c Credentials) AccountKey() string {
	return c.TencentSecretID + "|" + c.AliyunAccessKeyID
}

// ClientPool SDK Client 复用池。
//
// 构造后凭据不可修改（无 setter），因此 pool 可以被安全地放进不可变
// RuntimeState 并被多轮同步、Dry Run、连接测试与资源扫描共享。
type ClientPool struct {
	mu          sync.Mutex
	credentials Credentials // 构造后不变
	clients     map[string]any
}

// NewClientPool 创建 Client 复用池，凭据在创建时注入且此后不可变更。
//
// 必须传入显式凭据：不再存在无参构造，也不存在任何读取包级全局凭据的路径。
func NewClientPool(credentials Credentials) *ClientPool {
	return &ClientPool{
		credentials: credentials,
		clients:     make(map[string]any),
	}
}

// Credentials 返回 pool 持有的凭据副本（值类型，修改副本不影响 pool）。
func (p *ClientPool) Credentials() Credentials {
	return p.credentials
}

// CacheKey 构造 client 缓存键，至少区分 cloud type、region 与账户标识（Build6 §12.6）。
//
// 因此导入账户 B 之后新建的 pool 不会命中账户 A 留下的 client；
// 旧状态正在执行的同步继续使用旧 pool，自然不会中途切换账户。
func (p *ClientPool) CacheKey(cloudType config.CloudType, region string) string {
	return string(cloudType) + "|" + region + "|" + p.credentials.AccountKey()
}

// GetOrCreate 获取或创建 Client
func (p *ClientPool) GetOrCreate(key string, create func() (any, error)) (any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[key]; ok {
		return c, nil
	}
	c, err := create()
	if err != nil {
		return nil, err
	}
	p.clients[key] = c
	return c, nil
}

// strVal 安全获取字符串指针值
func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// RuleChange 规则变更摘要（供前端直接渲染）
type RuleChange struct {
	Protocol string `json:"protocol"`
	Port     string `json:"port"`
	Action   string `json:"action"`
	Cidr     string `json:"cidr"` // IPv4 或 IPv6 的 CIDR（如 1.2.3.4/32）
	Desc     string `json:"desc"` // 规则描述（含 [TAG]）
	// SkipReason 说明旧逐规则路径为何跳过该项；Issue7 目标级路径统一使用
	// planner.PlanIssue 承载稳定原因码与结构化明细。
	SkipReason string `json:"skip_reason,omitempty"`
}

// RuleChangeFromAction 从期望规则构造摘要（to_add）
func RuleChangeFromAction(a config.RuleAction) RuleChange {
	cidr := a.CidrBlock
	if cidr == "" {
		cidr = a.Ipv6CidrBlock
	}
	return RuleChange{Protocol: a.Protocol, Port: a.Port, Action: a.Action, Cidr: cidr, Desc: a.Description}
}

// RuleChangeFromInfo 从云端规则构造摘要（to_delete）
func RuleChangeFromInfo(r config.RuleInfo) RuleChange {
	cidr := r.CidrBlock
	if cidr == "" {
		cidr = r.Ipv6CidrBlock
	}
	return RuleChange{Protocol: r.Protocol, Port: r.Port, Action: r.Action, Cidr: cidr, Desc: r.Description}
}
