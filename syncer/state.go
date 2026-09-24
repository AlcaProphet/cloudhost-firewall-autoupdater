package syncer

import (
	"errors"
	"fmt"
	"sync"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// ErrUnknownCloudType 候选运行时状态构造失败：目标引用了未注册的云产品类型。
//
// 由协调器判定为请求侧（400）错误：配置包/请求本身给出了不受支持的值，
// 而不是数据库或内部故障。
var ErrUnknownCloudType = errors.New("不支持的云产品类型")

// BreakerPolicy 候选运行时状态的 DNS 熔断策略（Build6 §12.3 第 7 条）。
type BreakerPolicy int

const (
	// BreakerPreserve 普通配置变更：复制当前 breaker，保留既有失败计数。
	BreakerPreserve BreakerPolicy = iota
	// BreakerReset 完整配置导入：新建 breaker，允许清空原失败计数。
	BreakerReset
)

// RuntimeState 一次构建、一次发布的完整运行时状态（Build6 §12.3）。
//
// **发布后不可修改**：字段全部在构造期确定，Config 内的 slice 已深拷贝，
// ClientPool 的凭据不可变。同步轮次、Dry Run、连接测试与资源扫描各自只取
// 一次本结构指针，并在该操作全程使用它，禁止再读 Store、旧 cfg 字段或全局凭据。
type RuntimeState struct {
	Config    config.RuntimeConfig
	Pool      *provider.ClientPool
	Providers []provider.Provider
	Resolver  *dns.Resolver
	Breaker   *dns.CircuitBreaker
}

// BuildRuntimeState 由已校验的业务快照构造候选运行时状态。
//
// 只构造本地对象与 SDK client，**不访问云 API、DNS 上游、SMTP、Webhook**
// 或任何其他外部网络（Build6 §12.4）：SDK 的 NewClient 只做本地初始化。
// 任一失败的构造（未注册云类型）都返回错误，由调用方回滚事务。
//
// previous 为当前正在生效的状态，可为 nil（启动时）。policy 决定 breaker 语义。
func BuildRuntimeState(
	previous *RuntimeState,
	rc config.RuntimeConfig,
	policy BreakerPolicy,
) (*RuntimeState, error) {
	creds := provider.Credentials{
		TencentSecretID:       rc.Credentials.TencentSecretID,
		TencentSecretKey:      rc.Credentials.TencentSecretKey,
		AliyunAccessKeyID:     rc.Credentials.AliyunAccessKeyID,
		AliyunAccessKeySecret: rc.Credentials.AliyunAccessKeySecret,
	}

	// 深拷贝：发布之后的配置不得再被调用方修改（Build6 §12.3 第 4 条）
	published := rc.DeepCopy()

	pool := provider.NewClientPool(creds)
	providers := make([]provider.Provider, 0, len(published.Targets))
	for _, t := range published.Targets {
		p, err := provider.NewProvider(t, t.ID, pool)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrUnknownCloudType, t.CloudType)
		}
		providers = append(providers, p)
	}

	var breaker *dns.CircuitBreaker
	switch {
	case policy == BreakerPreserve && previous != nil && previous.Breaker != nil:
		// 普通变更保留既有失败计数，只让新阈值线程安全地进入新状态
		breaker = previous.Breaker.Clone()
		breaker.SetThreshold(published.DNSFailThreshold)
	default:
		// 完整导入或首次启动：新建 breaker（导入允许清空原计数）
		breaker = dns.NewCircuitBreaker(published.DNSFailThreshold)
	}

	return &RuntimeState{
		Config:    published,
		Pool:      pool,
		Providers: providers,
		Resolver:  dns.NewResolver(published.DNS, published.DNSTimeout),
		Breaker:   breaker,
	}, nil
}

// RuntimeManager 持有当前运行时状态，提供单锁边界的读取与替换（Build6 §12.5）。
//
// Snapshot 返回共享指针（状态发布后不可变，允许共享）；Apply 是一次指针替换，
// 不返回 error、不访问网络、不做可能失败的操作。
type RuntimeManager struct {
	mu    sync.RWMutex
	state *RuntimeState
}

// NewRuntimeManager 创建运行时状态管理器；初始状态可为 nil（尚未发布）。
func NewRuntimeManager(initial *RuntimeState) *RuntimeManager {
	return &RuntimeManager{state: initial}
}

// Snapshot 返回当前状态的共享指针；状态发布后不可修改，调用方不得写入其内容。
func (m *RuntimeManager) Snapshot() *RuntimeState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// Apply 在单锁边界内一次性替换完整运行时状态。
//
// 这是无失败操作：调用它之前候选状态必须已经构造成功（Build6 §12.4）。
func (m *RuntimeManager) Apply(next *RuntimeState) {
	m.mu.Lock()
	m.state = next
	m.mu.Unlock()
}
