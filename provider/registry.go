package provider

import (
	"fmt"
	"sync"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// Factory 创建 Provider 的工厂函数
type Factory func(cfg config.TargetConfig, dbID int, pool *ClientPool) (Provider, error)

var (
	mu       sync.RWMutex
	registry = map[config.CloudType]Factory{}
)

// Register 注册 Provider 工厂（在各 Provider 的 init() 中调用）
func Register(ct config.CloudType, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	registry[ct] = f
}

// NewProvider 创建 Provider 实例
func NewProvider(cfg config.TargetConfig, dbID int, pool *ClientPool) (Provider, error) {
	mu.RLock()
	f, ok := registry[cfg.CloudType]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("不支持的云产品类型: %s", cfg.CloudType)
	}
	return f(cfg, dbID, pool)
}
