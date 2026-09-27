package provider

import (
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
)

// CreateResult 一次 CreateRules 调用中「实际提交写入」与「明确跳过」的规则条数
// （Issue6 A11，2026-09-27 用户裁决 §六.4 F3）。
//
// 语义约束：
//   - Written 只统计**真正提交给云 API 的期望规则**（批次内全部提交成功才会返回）；
//   - Skipped 只统计**因云端能力限制而明确未实施**的规则（如阿里云 SWAS 无法表达
//     DROP 规则：其 CreateFirewallRules 请求参数没有 Policy 字段）；
//   - 幂等跳过（云 API 报「规则已存在」）**不计入 Skipped**：它由 Syncer 层的
//     isIdempotentCreate 处理，语义是「已符合期望」而不是「未实施」；
//   - 两者都不包含协议拆分带来的条数变化（TCP+UDP 拆分在 Diff 阶段完成）。
type CreateResult struct {
	Written int // 实际写入的规则条数
	Skipped int // 明确跳过（未实施）的规则条数
}

// Provider 多云抽象接口
type Provider interface {
	// Name 返回可读名称，如 "tc_lighthouse(lhins-abc)"
	Name() string
	// CloudType 返回云产品类型
	CloudType() config.CloudType
	// GetRules 查询当前所有规则
	GetRules() ([]config.RuleInfo, error)
	// CreateRules 增量添加规则，返回实际写入与明确跳过的条数
	CreateRules(rules []config.RuleAction) (CreateResult, error)
	// DeleteRules 精确删除规则（传入 RuleInfo 因需要 RuleID/PolicyIndex）
	DeleteRules(rules []config.RuleInfo) error
	// ConvertPorts 统一端口 → 云厂商格式列表
	ConvertPorts(port string) []string
	// TargetIndex 返回目标的数据库 ID
	TargetIndex() int
}

// SkippedRule 一条**无法实施**的期望规则及其原因（Issue6 A11）。
//
// 与 Provider.CreateRules 的 Skipped 计数不同：这里在 Diff 阶段就识别出
// 云端能力限制，因此 Dry Run 也能如实展示（不再把它误列为普通 to_add）。
type SkippedRule struct {
	Action config.RuleAction
	Reason string
}

// DiffResult Diff 计算结果
type DiffResult struct {
	ToAdd    []config.RuleAction
	ToDelete []config.RuleInfo
	// Skipped 云端能力限制导致无法实施的期望规则（只追加字段，Issue6 A11）
	Skipped []SkippedRule
}

// ResolvedIPs 便捷别名
type ResolvedIPs = []dns.ResolvedIP
