package config

import "time"

// Build7 告警与运行健康的固定默认值与边界（Build7 §4.1、§4.2、§4.5）。
const (
	// DefaultEmailSubject 默认邮件主题
	DefaultEmailSubject = "[FWAlizer] 告警通知"
	// DefaultEmailBody 默认邮件正文
	DefaultEmailBody = "FWAlizer 检测到运行异常，请检查同步日志。"
	// MaxEmailSubjectRunes 邮件主题最大 Unicode 字符数
	MaxEmailSubjectRunes = 200
	// MaxEmailBodyBytes 邮件正文最大字节数（10 KiB）
	MaxEmailBodyBytes = 10 << 10
	// DefaultHealthTimeout 唯一健康超时的默认值
	DefaultHealthTimeout = 10 * time.Minute
	// DefaultPushInterval Uptime Kuma Push 默认发送间隔
	DefaultPushInterval = 60 * time.Second
	// MinPushInterval Uptime Kuma Push 允许的最小发送间隔
	MinPushInterval = 20 * time.Second
	// DefaultAlertPort 默认 SMTP 端口
	DefaultAlertPort = "587"
	// DefaultWebhookChannel 默认 Webhook 渠道
	DefaultWebhookChannel = "dingtalk"
)

// CloudType 云产品类型
type CloudType string

const (
	CloudTCLighthouse CloudType = "tc_lighthouse"
	CloudTCCVM        CloudType = "tc_cvm"
	CloudAliSWAS      CloudType = "ali_swas"
	CloudAliECS       CloudType = "ali_ecs"
)

// RuleInfo 云端查询回来的规则
type RuleInfo struct {
	Protocol      string `json:"protocol"`                  // TCP / UDP / TCP+UDP / ICMP / ICMPv6 / ALL
	Port          string `json:"port"`                      // 归一化为 "port" 或 "start-end" 或 "ALL"
	CidrBlock     string `json:"cidr_block,omitempty"`      // IPv4 CIDR，如 "1.2.3.4/32"
	Ipv6CidrBlock string `json:"ipv6_cidr_block,omitempty"` // IPv6 CIDR，如 "2001:db8::1/128"
	Action        string `json:"action"`                    // ACCEPT / DROP
	Description   string `json:"description"`               // 规则描述/备注
	PolicyIndex   string `json:"policy_index,omitempty"`    // CVM 安全组删除时需要
	RuleID        string `json:"rule_id,omitempty"`         // 阿里云 SWAS/ECS 删除时需要
}

// RuleAction 要写入云端的规则
type RuleAction struct {
	Protocol      string
	Port          string // 已转换为对应云厂商的端口格式
	CidrBlock     string
	Ipv6CidrBlock string
	Action        string
	Description   string
}

// TargetConfig 云资源目标配置
type TargetConfig struct {
	ID         int       `json:"id"`
	CloudType  CloudType `json:"cloud_type"`
	Region     string    `json:"region"`
	ResourceID string    `json:"resource_id"` // InstanceId 或 SecurityGroupId
}

// DomainRule 域名规则配置（SQLite 持久化领域模型）
type DomainRule struct {
	ID         int    `json:"id"`
	Host       string `json:"host"`
	Protocol   string `json:"protocol"` // TCP / UDP / TCP+UDP / ICMP
	Ports      string `json:"ports"`    // 单端口、逗号分隔、范围、ALL
	Action     string `json:"action"`   // ACCEPT / DROP
	Targets    []int  `json:"targets"`  // 目标编号（空 = 全部）
	Comment    string `json:"comment"`
	EnableIPv6 bool   `json:"enable_ipv6"` // 是否解析 AAAA 记录，默认 false
}

// AlertEmailConfig SMTP 邮件告警配置（Build7 起含可编辑纯文本主题与正文）。
type AlertEmailConfig struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	FromAddr string `json:"from_addr"`
	ToAddr   string `json:"to_addr"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
}

// AlertPolicyConfig 告警触发策略：三个触发开关是邮件与 Webhook 共用的全局策略
// （Build7 §4.1），HealthTimeout 是唯一的运行健康超时。
//
// HealthTimeoutText 是已校验的时长文本，用于 SQLite 与 version 3 配置包
// （保留 "10m" 这类可读形态）；HealthTimeout 是它解析后的运行时值，
// 两者始终由 NormalizeAlertPolicy 一并写入，不得单独修改。
type AlertPolicyConfig struct {
	DNSFailedEnabled        bool          `json:"dns_failed_enabled"`
	SyncErrorEnabled        bool          `json:"sync_error_enabled"`
	OperationalErrorEnabled bool          `json:"operational_error_enabled"`
	HealthTimeout           time.Duration `json:"-"`
	HealthTimeoutText       string        `json:"-"`
}

// UptimeKumaPushConfig Uptime Kuma Push 反向心跳配置（独立于普通 Webhook 渠道）。
//
// IntervalText 与 Interval 的约定同 AlertPolicyConfig.HealthTimeoutText。
type UptimeKumaPushConfig struct {
	Enabled      bool          `json:"enabled"`
	URL          string        `json:"url"`
	Interval     time.Duration `json:"-"`
	IntervalText string        `json:"-"`
}

// DefaultAlertPolicy 返回固定的默认告警策略：三个触发开关全部关闭 + health_timeout=10m。
func DefaultAlertPolicy() AlertPolicyConfig {
	return AlertPolicyConfig{HealthTimeout: DefaultHealthTimeout, HealthTimeoutText: "10m"}
}

// DefaultUptimeKumaPush 返回固定的默认 Push 配置：关闭、URL 为空、间隔 60s。
func DefaultUptimeKumaPush() UptimeKumaPushConfig {
	return UptimeKumaPushConfig{Interval: DefaultPushInterval, IntervalText: "60s"}
}

// AlertWebhookConfig Webhook 告警配置
type AlertWebhookConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Channel string `json:"channel"` // dingtalk / feishu / slack，默认 dingtalk
}

// Credentials 四个云访问凭据的领域值（BusinessSnapshot 的组成部分）。
//
// 与 provider.Credentials 字段一一对应但**刻意不互相依赖**：config 保持零 provider
// 依赖，由 syncer 在构造 RuntimeState 时做一次显式映射（Build6 §12.3、§12.6）。
// 空字符串是合法值，表示凭据未配置或已被显式清除。
type Credentials struct {
	TencentSecretID       string
	TencentSecretKey      string
	AliyunAccessKeyID     string
	AliyunAccessKeySecret string
}

// Config 启动期业务配置（唯一来源为 SQLite）。
//
// 监听地址和端口属于部署参数（见 DeploymentConfig），不在这里保存。
// 业务配置的运行时形态是 RuntimeConfig（见 config/runtime.go）；本类型保留给
// 需要一次性读取完整配置的调用方（测试与诊断），凭据只通过 Credentials 暴露，
// 不再保留 `TCAccessID` 这类与 provider 旧全局变量同名的冗余字段。
type Config struct {
	Credentials      Credentials
	Targets          []TargetConfig
	DomainRules      []DomainRule
	Tag              string
	Interval         time.Duration
	DNS              string
	DNSTimeout       time.Duration // 默认 10s
	DNSFailThreshold int           // 默认 5
	LogLevel         string        // debug / info / warn / error
	SyncEnabled      bool          // 同步开关：true=开启，false=暂停；默认 true
	Theme            string        // light / dark（业务配置但不参与同步器）
}
