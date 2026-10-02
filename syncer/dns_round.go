package syncer

import (
	"fmt"
	"sync"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
)

// dnsRound 只存活一轮：协调轮初已熔断域名的探测，轮末提交无成功解析的失败计数。
// 成功结果不共享；失败结果不跨轮保存。所有目标结束后才能调用 finish。
type dnsRound struct {
	mu      sync.Mutex
	breaker *dns.CircuitBreaker
	hosts   map[string]*dnsRoundHost
}

type dnsRoundHost struct {
	open      bool // 轮初是否已熔断；成功探测后恢复正常解析
	done      chan struct{}
	err       error // 仅保存失败探测的原始错误
	attempted bool
	success   bool // 本轮成功优先，不受各目标完成顺序影响
}

func newDNSRound(state *RuntimeState) *dnsRound {
	round := &dnsRound{breaker: state.Breaker, hosts: make(map[string]*dnsRoundHost)}
	for _, rule := range state.Config.DomainRules {
		key := dns.DomainKey(rule.Host)
		if _, exists := round.hosts[key]; !exists {
			round.hosts[key] = &dnsRoundHost{open: state.Breaker.IsOpen(key)}
		}
	}
	return round
}

func (r *dnsRound) resolve(host string, resolve func() ([]dns.ResolvedIP, error)) ([]dns.ResolvedIP, error) {
	r.mu.Lock()
	h := r.hosts[dns.DomainKey(host)] // 来源为本轮配置中的适用规则
	probe := false
	if h.open {
		if h.done == nil {
			h.done = make(chan struct{})
			probe = true
		} else {
			done := h.done
			r.mu.Unlock()
			// 生产 Resolver 的整体超时约束探测；等待时不持锁，不新增协程。
			<-done
			r.mu.Lock()
			err := h.err
			r.mu.Unlock()
			if err != nil {
				return nil, err
			}
			// 探测成功后，每个等待者重新解析，不复用探测者的成功 IP。
			return r.run(host, h, false, resolve)
		}
	}
	r.mu.Unlock()
	return r.run(host, h, probe, resolve)
}

func (r *dnsRound) run(host string, h *dnsRoundHost, probe bool, resolve func() ([]dns.ResolvedIP, error)) ([]dns.ResolvedIP, error) {
	ips, err := resolve() // 网络调用不持有协调锁或 breaker 锁
	if err == nil && len(ips) == 0 {
		err = fmt.Errorf("DNS 解析无结果: %s", host)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	h.attempted = true
	if err == nil {
		h.success = true
		r.breaker.RecordSuccess(host)
	}
	if probe {
		h.err = err
		if err == nil {
			h.open = false
		}
		close(h.done)
	}
	return ips, err
}

func (r *dnsRound) finish() {
	// runRound 已等待全部目标完成；只更新捕获的旧 breaker，不回读当前运行时。
	r.mu.Lock()
	defer r.mu.Unlock()
	for host, h := range r.hosts {
		if h.attempted && !h.success {
			r.breaker.RecordFailure(host)
		}
	}
}
