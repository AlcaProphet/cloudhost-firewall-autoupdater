// API 响应类型定义

export interface TargetConfig {
  id: number
  cloud_type: string
  region: string
  resource_id: string
}

// ZoneRegion 地域及其可用区（GET /api/zones，地域自动补全数据源）
export interface ZoneRegion {
  id: string
  name: string
  zones: string[]
}

// ScannedResource 扫描到的云资源（GET /api/scanned-resources，资源 ID 自动补全数据源）
export interface ScannedResource {
  id: number
  cloud_type: string
  region: string
  resource_id: string
  resource_name: string
}

export interface DomainRule {
  id: number
  host: string
  protocol: string
  ports: string
  action: string
  targets: number[]
  comment: string
  enable_ipv6: boolean
}

// RoundSummary 一轮同步的整轮汇总（Issue7 §5.4：统计单元为**有适用规则的目标**）
//
// 不变量：total === ok + changed + failed + skipped
//   - ok      目标 success 且零增删（允许存在 cleanup_deferred）
//   - changed 目标 success 且确认发生增删
//   - failed  目标最终 failed
//   - skipped 目标未失败但存在 unsupported（部分实施目标数，字段名保留以兼容）
export interface RoundSummary {
  finished_at: string
  total: number
  ok: number
  changed: number
  failed: number
  skipped: number
  added: number
  deleted: number
  // 清理可观测性：候选数 / 实际确认清理数 / 最终残留候选数
  cleanup_candidates: number
  cleanup_deleted: number
  cleanup_deferred: number
  duration_ms: number
  outcome: 'success' | 'failed' | 'partial' | 'idle'
}

export interface SyncStatus {
  running: boolean
  last_sync: string | null
  enabled: boolean // 同步开关（Step 11 起后端必返回）
  // 同步健康（Issue6 A3/A18，向后兼容追加）：
  // 两者都是内存态，后端重启后为 null（不得表述为「从未成功」）
  last_success: string | null
  last_round: RoundSummary | null
  // Build7 Step 4：唯一运行健康计算源使用的内存态时间戳
  // round_started_at 无在途轮次时为 null；process_started_at 为 Syncer 构造时间
  round_started_at: string | null
  process_started_at: string
}

// RuleChange 规则变更摘要（Dry Run 明细化）
export interface RuleChange {
  protocol: string
  port: string
  action: string
  cidr: string // IPv4 或 IPv6 的 CIDR
  desc: string // 规则描述（含 [TAG]）
  // skip_reason 只在 skipped 列表中出现（Issue6 A11，向后兼容追加）
  skip_reason?: string
}

// FunctionalKey 功能身份：address-family + canonical CIDR + 协议 + 端口 + action
export interface FunctionalKey {
  family: string
  cidr: string
  protocol: string
  port: string
  action: string
}

// PlannedRule 目标级期望功能的一项
export interface PlannedRule {
  key: FunctionalKey
  comment: string
  description: string
  rule_ids: number[]
  domains: string[]
  implementable: boolean
}

// CloudRule 云端规则摘要（匹配结果里回显实际命中的规则）
export interface CloudRule {
  protocol: string
  port: string
  cidr_block?: string
  ipv6_cidr_block?: string
  action: string
  description: string
  policy_index?: string
  rule_id?: string
}

// PlanMatch 一条期望功能被云端规则精确满足
export interface PlanMatch {
  key: FunctionalKey
  description: string
  rules: CloudRule[]
}

// PlanIssue 稳定、可分类、可展示的规划问题
export interface PlanIssue {
  code: string
  message: string
  key?: FunctionalKey
  domain?: string
  rule_id?: number
}

// DryRunResult 目标级试运行结果（Issue7 §7.1）：每目标一项，数组恒为 []，绝不为 null
export interface DryRunResult {
  target_id: number
  provider: string
  domains: string[]
  desired: PlannedRule[]
  satisfied_by_owned: PlanMatch[]
  satisfied_by_external: PlanMatch[]
  to_add: RuleChange[]
  // cleanup_candidates 只是预览：正式流程还要在 S1 上重新过一遍安全门才可能删除
  cleanup_candidates: RuleChange[]
  cleanup_deferred: PlanIssue[]
  dns_errors: PlanIssue[]
  unsupported: PlanIssue[]
  conflicts: PlanIssue[]
  coverage_ready: boolean
  error: string
}

// DryRunResponse Dry Run 响应包装（空状态语义化）
export interface DryRunResponse {
  results: DryRunResult[]
  warnings: string[]
}

// TestConnectionResult 连接测试结果
export interface TestConnectionResult {
  success: boolean
  message?: string
  error?: string
}

export interface SyncLogEntry {
  timestamp: string
  target: string
  domain: string
  result: string
  added: number
  deleted: number
  error?: string // 失败原因，或 skipped/partial 的同步详情（复用既有持久化列）
}

export interface SyncEvent {
  type: string
  timestamp: string
  data: Record<string, unknown>
}

// AlertPolicyConfig 告警触发策略（Build7 §4.6）：三个触发开关共用，health_timeout 为时长文本
export interface AlertPolicyConfig {
  dns_failed_enabled: boolean
  sync_error_enabled: boolean
  operational_error_enabled: boolean
  health_timeout: string
}

export interface AlertEmailConfig {
  enabled: boolean
  host: string
  port: string
  username: string
  password: string
  from_addr: string
  to_addr: string
  // Build7：可编辑纯文本主题与正文
  subject: string
  body: string
}

// TestEmailPayload 测试邮件请求体（Build7 §5.1）：固定 8 个发送字段。
//
// 刻意不复用 AlertEmailConfig：后者多一个 enabled，而 /api/alerts/test-email 的
// 请求 DTO（webui/api/test_email.go 的 testEmailRequest）只有这 8 个字段且使用
// 严格解码（拒绝未知字段），多带 enabled 会被 HTTP 400 拒绝。
export interface TestEmailPayload {
  host: string
  port: string
  username: string
  password: string
  from_addr: string
  to_addr: string
  subject: string
  body: string
}

export interface AlertWebhookConfig {
  enabled: boolean
  url: string
  channel?: string
}

// AlertUptimeKumaPushConfig 外部运行监控（Build7 §4.6）：独立于普通 Webhook 渠道
export interface AlertUptimeKumaPushConfig {
  enabled: boolean
  url: string
  interval: string
}
