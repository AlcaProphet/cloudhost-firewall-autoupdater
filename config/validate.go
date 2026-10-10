package config

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ValidationError 领域校验错误。
//
// 只携带字段名与原因，不携带用户提交的原值，可以直接作为 HTTP 400 的文案；
// handler、LoadConfig 与（Step 5 的）配置导入共用同一组校验函数。
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Reason
	}
	return e.Field + " " + e.Reason
}

func invalidField(field, reason string) error {
	return &ValidationError{Field: field, Reason: reason}
}

// maxTagRunes TAG 最大长度（Unicode 字符数，Build6 §二第 5 项、AGENTS.md §9.1）
const maxTagRunes = 48

// 协议与动作枚举（Build6 §4.3）
var (
	validProtocols = map[string]bool{"TCP": true, "UDP": true, "TCP+UDP": true, "ICMP": true}
	validActions   = map[string]bool{"ACCEPT": true, "DROP": true}
)

// NormalizeCloudType 归一化并校验云产品类型：只允许四个已支持枚举。
func NormalizeCloudType(v string) (CloudType, error) {
	switch ct := CloudType(strings.TrimSpace(v)); ct {
	case CloudTCLighthouse, CloudTCCVM, CloudAliSWAS, CloudAliECS:
		return ct, nil
	default:
		return "", invalidField("cloud_type", "必须是 tc_lighthouse / tc_cvm / ali_swas / ali_ecs 之一")
	}
}

// NormalizeRegion 归一化地域：Trim 后必须非空。
//
// 不校验地域是否存在于预填列表，由云 API 自行报错（不过度防御）。
func NormalizeRegion(v string) (string, error) {
	s := strings.TrimSpace(v)
	if s == "" {
		return "", invalidField("region", "不能为空")
	}
	return s, nil
}

// NormalizeResourceID 归一化资源 ID：Trim 后必须非空。
//
// 不猜测各云资源 ID 的完整格式，也不调用云 API。
func NormalizeResourceID(v string) (string, error) {
	s := strings.TrimSpace(v)
	if s == "" {
		return "", invalidField("resource_id", "不能为空")
	}
	return s, nil
}

// NormalizeTarget 归一化并校验云资源目标（普通 CRUD、连接测试与资源扫描共用）。
func NormalizeTarget(t TargetConfig) (TargetConfig, error) {
	ct, err := NormalizeCloudType(string(t.CloudType))
	if err != nil {
		return TargetConfig{}, err
	}
	region, err := NormalizeRegion(t.Region)
	if err != nil {
		return TargetConfig{}, err
	}
	resourceID, err := NormalizeResourceID(t.ResourceID)
	if err != nil {
		return TargetConfig{}, err
	}
	return TargetConfig{ID: t.ID, CloudType: ct, Region: region, ResourceID: resourceID}, nil
}

// NormalizeRule 归一化并校验域名规则本身（不含目标引用的存在性检查）。
//
//   - host Trim 后非空，不增加复杂域名正则；
//   - protocol Trim 并转大写，只允许 TCP / UDP / TCP+UDP / ICMP；
//   - action Trim 并转大写，只允许 ACCEPT / DROP；
//   - ICMP 的 ports 统一为 ALL，非 ICMP 的 ports Trim 后必须非空；
//   - comment 允许为空，按原值保存。
func NormalizeRule(r DomainRule) (DomainRule, error) {
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return DomainRule{}, invalidField("host", "不能为空")
	}

	protocol := strings.ToUpper(strings.TrimSpace(r.Protocol))
	if !validProtocols[protocol] {
		return DomainRule{}, invalidField("protocol", "只允许 TCP / UDP / TCP+UDP / ICMP")
	}

	action := strings.ToUpper(strings.TrimSpace(r.Action))
	if !validActions[action] {
		return DomainRule{}, invalidField("action", "只允许 ACCEPT / DROP")
	}

	ports := strings.TrimSpace(r.Ports)
	if protocol == "ICMP" {
		ports = "ALL"
	} else if ports == "" {
		return DomainRule{}, invalidField("ports", "非 ICMP 协议不能为空")
	}

	r.Host = host
	r.Protocol = protocol
	r.Ports = ports
	r.Action = action
	return r, nil
}

// NormalizeTargetIDs 校验规则引用的目标 ID 数组：
// 每项必须为正数，同一数组内不得重复；空数组保留“适用于全部目标”语义。
//
// 返回新切片，避免调用方继续修改请求 DTO 的底层数组。
func NormalizeTargetIDs(ids []int) ([]int, error) {
	if len(ids) == 0 {
		return []int{}, nil
	}
	out := make([]int, 0, len(ids))
	seen := make(map[int]bool, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, invalidField("targets", "目标 ID 必须为正数")
		}
		if seen[id] {
			return nil, invalidField("targets", "目标 ID 不能重复")
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// NormalizeTag 归一化并校验 TAG：Trim 后非空，禁止方括号与控制字符，最多 48 个 Unicode 字符。
func NormalizeTag(v string) (string, error) {
	s := strings.TrimSpace(v)
	if s == "" {
		return "", invalidField("tag", "不能为空")
	}
	if utf8.RuneCountInString(s) > maxTagRunes {
		return "", invalidField("tag", "最多 48 个字符")
	}
	if strings.ContainsAny(s, "[]") {
		return "", invalidField("tag", "不能包含方括号")
	}
	if hasControlChar(s) {
		return "", invalidField("tag", "不能包含控制字符")
	}
	return s, nil
}

// ParsePositiveDuration 归一化并校验时长设置：Trim 后可被 time.ParseDuration 解析且大于 0。
//
// field 用于生成带键名但不含值的错误；返回归一化后的字符串与解析结果。
func ParsePositiveDuration(field, value string) (string, time.Duration, error) {
	s := strings.TrimSpace(value)
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return "", 0, invalidField(field, "必须是大于 0 的时长（如 30s / 5m / 1h）")
	}
	return s, d, nil
}

// NormalizeDNSFailThreshold 归一化并校验 DNS 失败阈值：必须是正整数。
func NormalizeDNSFailThreshold(v string) (string, int, error) {
	s := strings.TrimSpace(v)
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return "", 0, invalidField("dns_fail_threshold", "必须是正整数")
	}
	return strconv.Itoa(n), n, nil
}

// NormalizeLogLevel 归一化并校验日志级别：只允许 debug / info / warn / error。
func NormalizeLogLevel(v string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case "debug", "info", "warn", "error":
		return s, nil
	default:
		return "", invalidField("log_level", "只允许 debug / info / warn / error")
	}
}

// NormalizeTheme 归一化并校验主题：只允许 light / dark。
func NormalizeTheme(v string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case "light", "dark":
		return s, nil
	default:
		return "", invalidField("theme", "只允许 light / dark")
	}
}

// NormalizeSyncEnabled 解析同步开关（只由 pause/resume 与配置导入写入）。
func NormalizeSyncEnabled(v string) (bool, error) {
	switch strings.TrimSpace(v) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, invalidField("sync_enabled", "只允许 true / false")
	}
}

// NormalizeDNSAddress 归一化并校验 DNS 服务器地址（Build6 §4.4）：
//
//   - 接受 hostname、IPv4，可选 `:port`；
//   - IPv6 必须写成 `[address]` 或 `[address]:port`，未加括号的 IPv6 拒绝；
//   - 端口省略时由 Resolver 补 53，显式端口必须为 1～65535；
//   - 不测试 DNS 是否在线。
func NormalizeDNSAddress(v string) (string, error) {
	s := strings.TrimSpace(v)
	if s == "" {
		return "", invalidField("dns", "不能为空")
	}

	if strings.HasPrefix(s, "[") {
		host, port, err := net.SplitHostPort(s)
		if err != nil {
			if !strings.HasSuffix(s, "]") {
				return "", invalidField("dns", "IPv6 地址必须写成 [地址] 或 [地址]:端口")
			}
			host = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
			port = ""
		}
		ip := net.ParseIP(host)
		if ip == nil || ip.To4() != nil {
			return "", invalidField("dns", "IPv6 地址无效")
		}
		if port != "" {
			if _, err := normalizePort(port); err != nil {
				return "", invalidField("dns", "端口必须是 1～65535")
			}
		}
		return s, nil
	}

	if strings.Contains(s, ":") {
		host, port, err := net.SplitHostPort(s)
		if err != nil {
			// 多个冒号且未加括号：按固定契约拒绝未加括号的 IPv6，也不猜端口
			return "", invalidField("dns", "IPv6 必须使用 [地址] 或 [地址]:端口；端口必须是 1～65535")
		}
		if strings.TrimSpace(host) == "" {
			return "", invalidField("dns", "主机不能为空")
		}
		if _, err := normalizePort(port); err != nil {
			return "", invalidField("dns", "端口必须是 1～65535")
		}
		return s, nil
	}

	// 无冒号：hostname 或 IPv4，仅要求非空
	return s, nil
}

// NormalizeAlertEmail 归一化并校验邮件告警配置（Build6 §4.5、Build7 §4.5）：
//
//   - security 必须为固定三模式（禁用时也要求合法）；
//   - 端口必须是 1～65535 的整数（禁用时也要求类型正确）；
//   - 启用时 host / from_addr / to_addr 必须非空；
//   - username / password 允许为空，作为不透明凭据按原值保存；
//   - subject Trim 后必须非空、禁止换行与控制字符、最多 200 个 Unicode 字符；
//   - body 允许普通换行，最大 10 KiB。
func NormalizeAlertEmail(c AlertEmailConfig) (AlertEmailConfig, error) {
	if !ValidSMTPSecurity(c.Security) {
		return AlertEmailConfig{}, invalidField("email.security", "只允许 auto_starttls / starttls / implicit_tls")
	}
	port, err := normalizePort(c.Port)
	if err != nil {
		return AlertEmailConfig{}, invalidField("email.port", "必须是 1～65535 的整数")
	}
	c.Port = port
	c.Host = strings.TrimSpace(c.Host)
	c.FromAddr = strings.TrimSpace(c.FromAddr)
	c.ToAddr = strings.TrimSpace(c.ToAddr)

	if c.Enabled {
		if c.Host == "" {
			return AlertEmailConfig{}, invalidField("email.host", "启用邮件告警时不能为空")
		}
		if c.FromAddr == "" {
			return AlertEmailConfig{}, invalidField("email.from_addr", "启用邮件告警时不能为空")
		}
		if c.ToAddr == "" {
			return AlertEmailConfig{}, invalidField("email.to_addr", "启用邮件告警时不能为空")
		}
	}

	// 主题/正文校验放在既有 host/port/from/to 之后：保留既有错误优先级不变
	c.Subject = strings.TrimSpace(c.Subject)
	if c.Subject == "" {
		return AlertEmailConfig{}, invalidField("email.subject", "不能为空")
	}
	if utf8.RuneCountInString(c.Subject) > MaxEmailSubjectRunes {
		return AlertEmailConfig{}, invalidField("email.subject", "最多 200 个字符")
	}
	if hasControlChar(c.Subject) {
		return AlertEmailConfig{}, invalidField("email.subject", "不能包含换行或控制字符")
	}
	if len(c.Body) > MaxEmailBodyBytes {
		return AlertEmailConfig{}, invalidField("email.body", "最大 10 KiB")
	}
	return c, nil
}

// NormalizeAlertWebhook 归一化并校验 Webhook 告警配置（Build6 §4.5）：
//
//   - 关闭时 channel 允许空值，启用时必须为 dingtalk / feishu / slack；
//   - 启用时 URL 必须是 host 非空的绝对 http/https URL；
//   - 不主动发起请求。
func NormalizeAlertWebhook(c AlertWebhookConfig) (AlertWebhookConfig, error) {
	channel := strings.ToLower(strings.TrimSpace(c.Channel))
	switch channel {
	case "":
		if c.Enabled {
			return AlertWebhookConfig{}, invalidField("webhook.channel", "启用时必须选择渠道")
		}
	case "dingtalk", "feishu", "slack":
	default:
		return AlertWebhookConfig{}, invalidField("webhook.channel", "只允许 dingtalk / feishu / slack")
	}

	c.Channel = channel
	c.URL = strings.TrimSpace(c.URL)
	if c.Enabled {
		u, err := url.Parse(c.URL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return AlertWebhookConfig{}, invalidField("webhook.url", "启用时必须是以 http/https 开头且包含主机的绝对 URL")
		}
	}
	return c, nil
}

// NormalizeAlertPolicy 归一化并校验告警触发策略（Build7 §4.1、§4.5）：
//
//   - 三个触发开关是布尔值，直接采用；
//   - health_timeout 必须是大于 0 的 Go duration，默认值由调用方以 "10m" 给出；
//   - 同时写回 HealthTimeoutText（持久化/导出文本）与 HealthTimeout（运行时值）。
func NormalizeAlertPolicy(p AlertPolicyConfig) (AlertPolicyConfig, error) {
	text, d, err := ParsePositiveDuration("policy.health_timeout", p.HealthTimeoutText)
	if err != nil {
		return AlertPolicyConfig{}, err
	}
	p.HealthTimeoutText = text
	p.HealthTimeout = d
	return p, nil
}

// NormalizeUptimeKumaPush 归一化并校验 Uptime Kuma Push 配置（Build7 §4.5）：
//
//   - interval 必须可解析且不少于 20s（默认 60s 由调用方给出）；
//   - 启用时 URL 必须是 host 非空的绝对 http/https URL；禁用时允许为空，
//     且不主动发起任何请求。
func NormalizeUptimeKumaPush(p UptimeKumaPushConfig) (UptimeKumaPushConfig, error) {
	text, d, err := ParsePositiveDuration("uptime_kuma_push.interval", p.IntervalText)
	if err != nil {
		return UptimeKumaPushConfig{}, err
	}
	if d < MinPushInterval {
		return UptimeKumaPushConfig{}, invalidField("uptime_kuma_push.interval", "不能小于 20s")
	}
	p.IntervalText = text
	p.Interval = d

	p.URL = strings.TrimSpace(p.URL)
	if p.Enabled {
		u, err := url.Parse(p.URL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return UptimeKumaPushConfig{}, invalidField("uptime_kuma_push.url",
				"启用时必须是以 http/https 开头且包含主机的绝对 URL")
		}
	}
	return p, nil
}

// normalizePort 校验端口字符串并归一化为规范十进制（1～65535）。
func normalizePort(v string) (string, error) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 || n > 65535 {
		return "", invalidField("port", "必须是 1～65535 的整数")
	}
	return strconv.Itoa(n), nil
}

// hasControlChar 判断字符串是否包含控制字符
func hasControlChar(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
