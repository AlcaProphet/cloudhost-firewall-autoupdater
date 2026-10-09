package provider

import (
	"fmt"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
)

// CreateResult 一次 CreateRules 调用中「实际提交写入」与「明确跳过」的规则条数
// （Issue6 A11，2026-09-27 用户裁决 §六.4 F3）。
//
// 语义约束：
//   - Written 只统计**已由云 API 确认成功**的期望规则；即使调用最终返回错误，
//     也可表示错误发生前已成功提交的独立子批次，当前失败且提交状态未知的子批次不计；
//   - Skipped 只统计**因云端能力限制而明确未实施**的规则（如阿里云 SWAS 无法表达
//     DROP 规则：其 CreateFirewallRules 请求参数没有 Policy 字段）；
//   - 幂等跳过（云 API 报「规则已存在」）**不计入 Skipped**：它由 Syncer 层的
//     isIdempotentCreate 处理，语义是「已符合期望」而不是「未实施」；
//   - 两者都不包含协议拆分带来的条数变化（TCP+UDP 拆分在目标级 PlanTarget 阶段完成）。
type CreateResult struct {
	Written int // 实际写入的规则条数
	Skipped int // 明确跳过（未实施）的规则条数
}

// DeleteResult 一次 DeleteRules 调用中「云端明确确认」的结果。
//
//   - Deleted  实际确认删除的条数（幂等「已不存在」不计入）；
//   - Resolved 云端确认已不再存在的候选条数（= Deleted + 幂等已不存在），
//     用于把「已处理完的候选」与「仍需 deferred 的残留」区分开。
type DeleteResult struct {
	Deleted  int
	Resolved int
}

// PartialDeleteError 表示 DeleteRules 返回错误前，已有若干独立删除请求得到云端成功确认。
// Deleted 不包含当前失败且提交状态未知的请求，也不包含幂等「已不存在」。
// Syncer 可通过 errors.As 读取确认进度，并通过 Unwrap 继续判定原始错误是否可重试。
type PartialDeleteError struct {
	Deleted int
	Err     error
}

func (e *PartialDeleteError) Error() string {
	return fmt.Sprintf("已删除 %d 条后失败: %v", e.Deleted, e.Err)
}

func (e *PartialDeleteError) Unwrap() error {
	return e.Err
}

// Provider 多云抽象接口
type Provider interface {
	// Name 返回可读名称，如 "tc_lighthouse(lhins-abc)"
	Name() string
	// CloudType 返回云产品类型
	CloudType() config.CloudType
	// GetSnapshot 取得一次完整、不可拆分的云端快照（规则 + 版本）。
	//
	// 完整性由成功返回隐含保证：任一页失败、token 不推进、版本缺失或字段不足以
	// 安全判定时必须返回 error，绝不返回半截 Rules（Issue7 §4.1）。
	GetSnapshot() (RuleSnapshot, error)
	// GetRules 查询当前所有规则。
	//
	// Deprecated: 旧逐规则同步路径的兼容包装，等价于 GetSnapshot().Rules；
	// 新代码必须使用 GetSnapshot，不得依赖丢弃版本的读取（Issue7 Step 1～4 后按 I-09 清理）。
	GetRules() ([]config.RuleInfo, error)
	// CreateRules 增量添加规则，返回实际写入与明确跳过的条数。
	//
	// snapshot 必须是本次 attempt 的 S0：腾讯云（Lighthouse/CVM）用它的 Revision
	// 做版本保护（版本不匹配即失败，禁止退化为无版本写入）；阿里云无版本号，忽略该参数。
	CreateRules(snapshot RuleSnapshot, rules []config.RuleAction) (CreateResult, error)
	// DeleteRules 精确删除规则（传入 RuleInfo 因需要 RuleID/PolicyIndex）。
	//
	// snapshot 必须是本次 attempt 的 S1：腾讯云（Lighthouse/CVM）用它的 Revision
	// 做删除版本保护（版本不匹配即失败，禁止降级为无版本删除）；阿里云无版本号，忽略该参数。
	// 返回 DeleteResult 与错误；部分成功时同时返回 *PartialDeleteError 以便调用方读取进度。
	DeleteRules(snapshot RuleSnapshot, rules []config.RuleInfo) (DeleteResult, error)
	// ConvertPorts 统一端口 → 云厂商格式列表
	ConvertPorts(port string) []string
	// TargetIndex 返回目标的数据库 ID
	TargetIndex() int
}

// ResolvedIPs 便捷别名
type ResolvedIPs = []dns.ResolvedIP
