package config

import "time"

// settingsKeysV3 是 version 3 配置包与业务快照的**完整**设置键集合（Build6 §3.1、§12.12）。
//
// 固定顺序即写入顺序；导入时显式逐键写入，绝不遍历请求里的任意 map，
// 从而不会把旧数据库的未知键带回新快照。sync_enabled 也在集合内：
// 它是业务配置，只是只能由 pause/resume 端点或配置导入写入。
var settingsKeysV3 = []string{
	"tc_access_id", "tc_access_key", "ali_access_id", "ali_access_key",
	"tag", "interval", "dns", "dns_timeout", "dns_fail_threshold",
	"log_level", "sync_enabled", "theme",
}

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
	Tag              string
	Interval         time.Duration
	DNS              string
	DNSTimeout       time.Duration
	DNSFailThreshold int
	LogLevel         string
	SyncEnabled      bool
	Theme            string
	Policy           AlertPolicyConfig
	Email            AlertEmailConfig
	Webhook          AlertWebhookConfig
	UptimeKumaPush   UptimeKumaPushConfig
}

// DeepCopy 返回 RuntimeConfig 的深拷贝：全部 slice 元素独立可写。
//
// 必须在把候选配置发布到运行时之前调用，避免把仍可被后续代码修改的
// 请求/查询切片直接放进不可变状态（Build6 §12.3 第 4 条）。
func (rc RuntimeConfig) DeepCopy() RuntimeConfig {
	out := rc
	out.Targets = append([]TargetConfig(nil), rc.Targets...)
	out.DomainRules = DeepCopyRules(rc.DomainRules)
	return out
}

// ToRuntimeConfig 把已归一化的业务快照转换为运行时配置（含深拷贝）。
func (s *BusinessSnapshot) ToRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		Credentials:      s.Credentials(),
		Targets:          append([]TargetConfig(nil), s.Targets...),
		DomainRules:      DeepCopyRules(s.Rules),
		Tag:              s.Settings["tag"],
		Interval:         mustDuration(s.Settings["interval"]),
		DNS:              s.Settings["dns"],
		DNSTimeout:       mustDuration(s.Settings["dns_timeout"]),
		DNSFailThreshold: mustThreshold(s.Settings["dns_fail_threshold"]),
		LogLevel:         s.Settings["log_level"],
		SyncEnabled:      s.Settings["sync_enabled"] == "true",
		Theme:            s.Settings["theme"],
		Policy:           s.Policy,
		Email:            s.Email,
		Webhook:          s.Webhook,
		UptimeKumaPush:   s.UptimeKumaPush,
	}
}

// ToConfig 把业务快照转换为启动期 Config（过渡兼容，最终由 RuntimeConfig 取代）。
func (s *BusinessSnapshot) ToConfig() *Config {
	rc := s.ToRuntimeConfig()
	return &Config{
		Credentials:      rc.Credentials,
		Targets:          rc.Targets,
		DomainRules:      rc.DomainRules,
		Tag:              rc.Tag,
		Interval:         rc.Interval,
		DNS:              rc.DNS,
		DNSTimeout:       rc.DNSTimeout,
		DNSFailThreshold: rc.DNSFailThreshold,
		LogLevel:         rc.LogLevel,
		SyncEnabled:      rc.SyncEnabled,
		Theme:            rc.Theme,
	}
}

// mustDuration 解析已完成校验的时长设置。
//
// 非法值不可能到达这里：LoadBusinessSnapshotTx 已在同一事务内用
// ParsePositiveDuration 校验并在失败时返回错误；解析失败时返回调用方兜底值。
func mustDuration(v string) time.Duration {
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0
	}
	return d
}

// mustThreshold 解析已完成校验的正整数阈值设置；非法值同样不可能到达这里。
func mustThreshold(v string) int {
	_, n, err := NormalizeDNSFailThreshold(v)
	if err != nil {
		return 0
	}
	return n
}
