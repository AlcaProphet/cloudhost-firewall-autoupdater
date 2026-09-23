package config

import "time"

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
	Protocol      string // TCP / UDP / TCP+UDP / ICMP / ICMPv6 / ALL
	Port          string // 归一化为 "port" 或 "start-end" 或 "ALL"
	CidrBlock     string // IPv4 CIDR，如 "1.2.3.4/32"
	Ipv6CidrBlock string // IPv6 CIDR，如 "2001:db8::1/128"
	Action        string // ACCEPT / DROP
	Description   string // 规则描述/备注
	PolicyIndex   string // CVM 安全组删除时需要
	RuleID        string // 阿里云 SWAS/ECS 删除时需要
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

// DomainRule 域名规则配置（RULES 解析结果）
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

// AlertEmailConfig SMTP 邮件告警配置
type AlertEmailConfig struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	FromAddr string `json:"from_addr"`
	ToAddr   string `json:"to_addr"`
}

// AlertWebhookConfig Webhook 告警配置
type AlertWebhookConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Channel string `json:"channel"` // dingtalk / feishu / slack，默认 dingtalk
}

// Config 全局业务配置（唯一来源为 SQLite）。
//
// 监听地址和端口属于部署参数（见 DeploymentConfig），不在这里保存。
type Config struct {
	TCAccessID       string
	TCAccessKey      string
	AliAccessID      string
	AliAccessKey     string
	Targets          []TargetConfig
	DomainRules      []DomainRule
	Tag              string
	Interval         time.Duration
	DNS              string
	DNSTimeout       time.Duration // 默认 10s
	DNSFailThreshold int           // 默认 5
	LogLevel         string        // debug / info / warn / error
	SyncEnabled      bool          // 同步开关：true=开启，false=暂停；默认 true
}
