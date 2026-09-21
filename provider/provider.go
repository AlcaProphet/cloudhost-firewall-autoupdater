package provider

import (
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
)

// Provider 多云抽象接口
type Provider interface {
	// Name 返回可读名称，如 "tc_lighthouse(lhins-abc)"
	Name() string
	// CloudType 返回云产品类型
	CloudType() config.CloudType
	// GetRules 查询当前所有规则
	GetRules() ([]config.RuleInfo, error)
	// CreateRules 增量添加规则
	CreateRules(rules []config.RuleAction) error
	// DeleteRules 精确删除规则（传入 RuleInfo 因需要 RuleID/PolicyIndex）
	DeleteRules(rules []config.RuleInfo) error
	// ConvertPorts 统一端口 → 云厂商格式列表
	ConvertPorts(port string) []string
	// TargetIndex 返回目标的数据库 ID
	TargetIndex() int
}

// DiffResult Diff 计算结果
type DiffResult struct {
	ToAdd    []config.RuleAction
	ToDelete []config.RuleInfo
}

// SyncDomainResult 单个域名的同步结果
type SyncDomainResult struct {
	Domain  string
	Target  string
	Added   int
	Deleted int
	Error   error
}

// ResolvedIPs 便捷别名
type ResolvedIPs = []dns.ResolvedIP
