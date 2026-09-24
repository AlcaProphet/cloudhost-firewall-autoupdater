package dns

import (
	"log/slog"
	"sync"
)

// CircuitBreaker 每个域名独立的熔断器
type CircuitBreaker struct {
	mu        sync.Mutex
	failCount map[string]int // 域名 → 连续失败次数
	threshold int            // 熔断阈值
}

// NewCircuitBreaker 创建熔断器
func NewCircuitBreaker(threshold int) *CircuitBreaker {
	return &CircuitBreaker{
		failCount: make(map[string]int),
		threshold: threshold,
	}
}

// SetThreshold 线程安全地更新熔断阈值，保留既有失败计数（Build6 Step 4）。
//
// 普通配置变更只改阈值、不重置熔断进度；非正数阈值会破坏 IsOpen 语义，
// 因此忽略并记录 WARN（阈值由领域层保证为正整数）。
func (cb *CircuitBreaker) SetThreshold(threshold int) {
	if threshold <= 0 {
		slog.Warn("忽略非正数 DNS 熔断阈值", "threshold", threshold)
		return
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.threshold = threshold
}

// IsOpen 判断域名是否已熔断
func (cb *CircuitBreaker) IsOpen(domain string) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failCount[domain] >= cb.threshold
}

// RecordSuccess 记录成功，解除熔断
func (cb *CircuitBreaker) RecordSuccess(domain string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.failCount[domain] > 0 {
		slog.Info("DNS 熔断解除", "domain", domain)
	}
	cb.failCount[domain] = 0
}

// RecordFailure 记录失败（已熔断时不再递增，半开探测失败维持熔断状态）
func (cb *CircuitBreaker) RecordFailure(domain string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	// 已熔断时跳过递增（符合 Build1.md 12.7 节约定）
	if cb.failCount[domain] >= cb.threshold {
		return
	}
	cb.failCount[domain]++
	if cb.failCount[domain] == cb.threshold {
		slog.Error("DNS 熔断触发", "domain", domain, "连续失败", cb.failCount[domain])
	}
}
