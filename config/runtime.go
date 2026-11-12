package config

import "time"

// settingsKeysV3 是 version 3 配置包的**完整**设置键集合（Build6 §3.1、§12.12）。
//
// 该集合只描述 v3 配置包与 v3 导入 DTO 的既有字段；v4 与当前业务快照使用
// settingsKeysCurrent。固定顺序即写入顺序；导入时显式逐键写入，绝不遍历
// 请求里的任意 map，从而不会把旧数据库的未知键带回新快照。
var settingsKeysV3 = []string{
	"tc_access_id", "tc_access_key", "ali_access_id", "ali_access_key",
	"tag", "interval", "dns", "dns_timeout", "dns_fail_threshold",
	"log_level", "sync_enabled", "theme",
}

// settingsKeysCurrent 是当前 SQLite/运行时快照的完整设置键集合：
// v3 既有 12 键 + Build8 Step 2 新增的四个双功能设置键。
//
// v3 配置包不要求携带新增键，由 v3 转换补入默认值；当前写路径使用本集合。
var settingsKeysCurrent = append(append([]string{}, settingsKeysV3...),
	"dns_enabled", "edgeone_enabled", "edgeone_tag", "edgeone_interval")

// BusinessSnapshot 是一个 SQLite 事务内取到的完整业务配置快照。
//
// 字段口径（Build7 Step 7 核验修正）：
//   - Targets / Rules 由各自 load 函数严格解析（rules.targets 四态口径）；
//   - Settings 经 normalizeSettings 归一化；
//   - Policy / UptimeKumaPush 在读取时解析并校验时长文本；
//   - Email / Webhook 按数据库原值读取，其合法性由 PUT /api/alerts 与配置导入
//     共用的领域校验保证（Build6 §12.7、§12.10）。
//
// 同一事务内一次性取出全部业务表，保证导出快照内部一致；构造 RuntimeState
// 的候选也只依赖本结构，不在 commit 之后再读库。
type BusinessSnapshot struct {
	Targets        []TargetConfig
	Rules          []DomainRule
	EdgeOneSites   []EdgeOneSite
	Settings       map[string]string
	Policy         AlertPolicyConfig
	Email          AlertEmailConfig
	Webhook        AlertWebhookConfig
	UptimeKumaPush UptimeKumaPushConfig
}

// Credentials 从快照设置中取出四个云凭据。
//
// 凭据作为不透明文本按原值保存，允许空字符串（表示未配置或显式清除）。
func (s *BusinessSnapshot) Credentials() Credentials {
	return Credentials{
		TencentSecretID:       s.Settings["tc_access_id"],
		TencentSecretKey:      s.Settings["tc_access_key"],
		AliyunAccessKeyID:     s.Settings["ali_access_id"],
		AliyunAccessKeySecret: s.Settings["ali_access_key"],
	}
}

// DeepCopyRules 深拷贝规则切片（含每条的 Targets 数组），
// 避免把仍可能被修改的切片直接放进不可变运行时状态。
func DeepCopyRules(rules []DomainRule) []DomainRule {
	out := make([]DomainRule, len(rules))
	for i, r := range rules {
		cp := r
		cp.Targets = append([]int(nil), r.Targets...)
		out[i] = cp
	}
	return out
}

// RuntimeConfig 是 SQLite 业务配置的完整已归一化快照（Build6 §12.3）。
//
// 发布到运行时后视为**不可变**：slice 已深拷贝，任何调用方都不得再修改
// 其元素或追加。同步、Dry Run、连接测试与资源扫描各自只取一次本结构。
type RuntimeConfig struct {
	Credentials      Credentials
	Targets          []TargetConfig
	DomainRules      []DomainRule
	EdgeOneSites     []EdgeOneSite
	Tag              string
	Interval         time.Duration
	DNS              string
	DNSTimeout       time.Duration
	DNSFailThreshold int
	LogLevel         string
	SyncEnabled      bool
	DNSEnabled       bool
	EdgeOneEnabled   bool
	EdgeOneTag       string
	EdgeOneInterval  time.Duration
	Theme            string
	// dnsEnabledSet 区分“生产快照显式写入 dns_enabled”与旧测试夹具/零值配置。
	// 未显式设置时沿用 Build8 之前的唯一 DNS 路径默认开启，避免把零值误判为关闭。
	dnsEnabledSet  bool
	Policy         AlertPolicyConfig
	Email          AlertEmailConfig
	Webhook        AlertWebhookConfig
	UptimeKumaPush UptimeKumaPushConfig
}

// DeepCopy 返回 RuntimeConfig 的深拷贝：全部 slice 元素独立可写。
//
// 必须在把候选配置发布到运行时之前调用，避免把仍可被后续代码修改的
// 请求/查询切片直接放进不可变状态（Build6 §12.3 第 4 条）。
func (rc RuntimeConfig) DeepCopy() RuntimeConfig {
	out := rc
	out.Targets = append([]TargetConfig(nil), rc.Targets...)
	out.DomainRules = DeepCopyRules(rc.DomainRules)
	out.EdgeOneSites = DeepCopyEdgeOneSites(rc.EdgeOneSites)
	return out
}

// ToRuntimeConfig 把已归一化的业务快照转换为运行时配置（含深拷贝）。
//
// Settings 必须已经归一化并通过校验；生产调用方通过 LoadBusinessSnapshot
// 或 LoadBusinessSnapshotTx 取得快照。本方法不补默认值或重新校验。
func (s *BusinessSnapshot) ToRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		Credentials:      s.Credentials(),
		Targets:          append([]TargetConfig(nil), s.Targets...),
		DomainRules:      DeepCopyRules(s.Rules),
		EdgeOneSites:     DeepCopyEdgeOneSites(s.EdgeOneSites),
		Tag:              s.Settings["tag"],
		Interval:         mustDuration(s.Settings["interval"]),
		DNS:              s.Settings["dns"],
		DNSTimeout:       mustDuration(s.Settings["dns_timeout"]),
		DNSFailThreshold: mustThreshold(s.Settings["dns_fail_threshold"]),
		LogLevel:         s.Settings["log_level"],
		SyncEnabled:      s.Settings["sync_enabled"] == "true",
		DNSEnabled:       s.Settings["dns_enabled"] == "true",
		dnsEnabledSet:    true,
		EdgeOneEnabled:   s.Settings["edgeone_enabled"] == "true",
		EdgeOneTag:       s.Settings["edgeone_tag"],
		EdgeOneInterval:  mustDuration(s.Settings["edgeone_interval"]),
		Theme:            s.Settings["theme"],
		Policy:           s.Policy,
		Email:            s.Email,
		Webhook:          s.Webhook,
		UptimeKumaPush:   s.UptimeKumaPush,
	}
}

// DNSFeatureEnabled 返回 DNS 来源是否启用。生产快照显式设置；未设置 marker
// 的零值测试夹具按旧唯一 DNS 路径视为启用。
func (rc RuntimeConfig) DNSFeatureEnabled() bool {
	if rc.dnsEnabledSet {
		return rc.DNSEnabled
	}
	return true
}

// SetDNSFeatureEnabled 供测试/构造路径显式覆盖 DNS 开关。
func (rc *RuntimeConfig) SetDNSFeatureEnabled(enabled bool) {
	rc.DNSEnabled = enabled
	rc.dnsEnabledSet = true
}

// mustDuration 解析已完成校验的时长设置。
//
// 生产调用前，LoadBusinessSnapshotTx 已在同一事务内用 ParsePositiveDuration
// 拒绝非正数或格式非法的值。此处解析失败返回 0，并不提供有效默认值；
// 本函数也不独立检查正数，调用方不得绕过快照校验。
func mustDuration(v string) time.Duration {
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0
	}
	return d
}

// mustThreshold 解析已完成校验的正整数阈值设置。
// 生产调用前已通过快照校验；此处失败返回 0，不承担默认阈值的补齐。
func mustThreshold(v string) int {
	_, n, err := NormalizeDNSFailThreshold(v)
	if err != nil {
		return 0
	}
	return n
}
