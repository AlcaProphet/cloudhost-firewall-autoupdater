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

// RoundSummary 一轮同步的整轮汇总（Issue6 A18；字段口径按 F4 裁决）
//
// 不变量：total === ok + changed + failed + skipped
//   - ok      成功且无变更的单元
//   - changed 成功且发生增删的单元
//   - failed  DNS 或云调用失败的单元
//   - skipped Provider 明确未实施操作的单元（如 SWAS 无法表达 DROP）
export interface RoundSummary {
  finished_at: string
  total: number
  ok: number
  changed: number
  failed: number
  skipped: number
  added: number
  deleted: number
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

// DryRunResult 试运行结果（to_add/to_delete 为规则明细数组）
export interface DryRunResult {
  provider: string
  domain: string
  to_add: RuleChange[]
  to_delete: RuleChange[]
  error?: string
  // skipped 无法实施的期望规则（如阿里云轻量云不支持 DROP；
  // Issue6 A11，只追加字段，to_add/to_delete 的名称与结构不变）
  skipped?: RuleChange[]
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
