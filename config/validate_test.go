package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// requireInvalid 断言返回 ValidationError 且错误文本包含键名
func requireInvalid(t *testing.T, err error, field string) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望校验失败，实际通过")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("错误类型应为 *ValidationError，实际 %T: %v", err, err)
	}
	if ve.Field != field {
		t.Errorf("错误字段 = %q, want %q（err=%v）", ve.Field, field, err)
	}
}

// TestNormalizeCloudType 云产品枚举：Trim 后必须命中四个固定值
func TestNormalizeCloudType(t *testing.T) {
	for _, in := range []string{"tc_lighthouse", "tc_cvm", "ali_swas", "ali_ecs", "  tc_cvm  "} {
		if _, err := NormalizeCloudType(in); err != nil {
			t.Errorf("NormalizeCloudType(%q) 应通过: %v", in, err)
		}
	}
	for _, in := range []string{"", "   ", "TC_CVM", "aws_ec2", "tc_lighthousex", "tc-cvm"} {
		if _, err := NormalizeCloudType(in); err == nil {
			t.Errorf("NormalizeCloudType(%q) 应失败", in)
		}
	}
}

// TestNormalizeTarget Trim 与空白校验
func TestNormalizeTarget(t *testing.T) {
	got, err := NormalizeTarget(TargetConfig{CloudType: " tc_lighthouse ", Region: " ap-guangzhou ", ResourceID: " lhins-abc "})
	if err != nil {
		t.Fatalf("应通过: %v", err)
	}
	if got.CloudType != CloudTCLighthouse || got.Region != "ap-guangzhou" || got.ResourceID != "lhins-abc" {
		t.Errorf("归一化结果错误: %+v", got)
	}

	requireInvalid(t, mustErr(NormalizeTarget(TargetConfig{CloudType: CloudTCLighthouse, Region: "  ", ResourceID: "x"})), "region")
	requireInvalid(t, mustErr(NormalizeTarget(TargetConfig{CloudType: CloudTCLighthouse, Region: "gz", ResourceID: "\t"})), "resource_id")
	requireInvalid(t, mustErr(NormalizeTarget(TargetConfig{CloudType: "aws", Region: "gz", ResourceID: "x"})), "cloud_type")
}

// TestNormalizeRule 协议/动作归一化、ICMP 端口与空白校验
func TestNormalizeRule(t *testing.T) {
	got, err := NormalizeRule(DomainRule{Host: " api.example.com ", Protocol: " tcp ", Ports: " 443 ", Action: "accept"})
	if err != nil {
		t.Fatalf("应通过: %v", err)
	}
	if got.Host != "api.example.com" || got.Protocol != "TCP" || got.Ports != "443" || got.Action != "ACCEPT" {
		t.Errorf("归一化结果错误: %+v", got)
	}

	// ICMP 端口统一为 ALL
	got, err = NormalizeRule(DomainRule{Host: "a.example.com", Protocol: "icmp", Ports: "80", Action: "drop"})
	if err != nil {
		t.Fatalf("ICMP 应通过: %v", err)
	}
	if got.Ports != "ALL" || got.Protocol != "ICMP" || got.Action != "DROP" {
		t.Errorf("ICMP 归一化错误: %+v", got)
	}

	// TCP+UDP 保留
	if got, err = NormalizeRule(DomainRule{Host: "a", Protocol: "TCP+UDP", Ports: "1,2", Action: "ACCEPT"}); err != nil || got.Protocol != "TCP+UDP" {
		t.Errorf("TCP+UDP 应通过: got=%+v err=%v", got, err)
	}

	requireInvalid(t, mustErr(NormalizeRule(DomainRule{Host: "  ", Protocol: "TCP", Ports: "80", Action: "ACCEPT"})), "host")
	requireInvalid(t, mustErr(NormalizeRule(DomainRule{Host: "a", Protocol: "SCTP", Ports: "80", Action: "ACCEPT"})), "protocol")
	requireInvalid(t, mustErr(NormalizeRule(DomainRule{Host: "a", Protocol: "TCP", Ports: "80", Action: "ALLOW"})), "action")
	requireInvalid(t, mustErr(NormalizeRule(DomainRule{Host: "a", Protocol: "TCP", Ports: "   ", Action: "ACCEPT"})), "ports")
}

// TestNormalizeTargetIDs 目标引用：正数、去重、空数组语义
func TestNormalizeTargetIDs(t *testing.T) {
	in := []int{3, 1, 2}
	got, err := NormalizeTargetIDs(in)
	if err != nil || len(got) != 3 {
		t.Fatalf("合法引用应通过: %v %v", got, err)
	}
	// 返回新切片：修改结果不得影响调用方传入的请求 DTO
	got[0] = 99
	if in[0] != 3 {
		t.Errorf("不应共享底层数组: in=%v", in)
	}

	for _, empty := range [][]int{nil, {}} {
		out, err := NormalizeTargetIDs(empty)
		if err != nil || len(out) != 0 {
			t.Errorf("空数组应表示全部目标: %v %v", out, err)
		}
	}

	requireInvalid(t, mustErr(NormalizeTargetIDs([]int{1, 1})), "targets")
	requireInvalid(t, mustErr(NormalizeTargetIDs([]int{0})), "targets")
	requireInvalid(t, mustErr(NormalizeTargetIDs([]int{-2})), "targets")
	requireInvalid(t, mustErr(NormalizeTargetIDs([]int{2, 0})), "targets")
}

// TestNormalizeTag TAG 边界：Trim、48/49 字符、方括号、控制字符
func TestNormalizeTag(t *testing.T) {
	if got, err := NormalizeTag("  auto-dns  "); err != nil || got != "auto-dns" {
		t.Errorf("Trim 失败: %q %v", got, err)
	}
	// 48 个 ASCII 与 48 个多字节字符都应按 Unicode 字符数通过
	if _, err := NormalizeTag(strings.Repeat("a", 48)); err != nil {
		t.Errorf("48 字符应通过: %v", err)
	}
	if _, err := NormalizeTag(strings.Repeat("标", 48)); err != nil {
		t.Errorf("48 个多字节字符应通过: %v", err)
	}
	requireInvalid(t, mustErr(NormalizeTag(strings.Repeat("a", 49))), "tag")
	requireInvalid(t, mustErr(NormalizeTag(strings.Repeat("标", 49))), "tag")
	requireInvalid(t, mustErr(NormalizeTag("a[b")), "tag")
	requireInvalid(t, mustErr(NormalizeTag("a]b")), "tag")
	requireInvalid(t, mustErr(NormalizeTag("a\nb")), "tag")
	requireInvalid(t, mustErr(NormalizeTag("a\tb")), "tag")
	requireInvalid(t, mustErr(NormalizeTag("   ")), "tag")
	requireInvalid(t, mustErr(NormalizeTag("")), "tag")
}

// TestParsePositiveDuration 时长：可解析且大于 0
func TestParsePositiveDuration(t *testing.T) {
	if got, d, err := ParsePositiveDuration("interval", " 5m "); err != nil || got != "5m" || d != 5*time.Minute {
		t.Errorf("应通过: %q %v %v", got, d, err)
	}
	requireInvalid(t, mustErr2(ParsePositiveDuration("interval", "abc")), "interval")
	requireInvalid(t, mustErr2(ParsePositiveDuration("dns_timeout", "0s")), "dns_timeout")
	requireInvalid(t, mustErr2(ParsePositiveDuration("dns_timeout", "-5s")), "dns_timeout")
	requireInvalid(t, mustErr2(ParsePositiveDuration("interval", "")), "interval")
}

// TestNormalizeDNSFailThreshold 阈值：正整数并归一化为规范十进制
func TestNormalizeDNSFailThreshold(t *testing.T) {
	if got, n, err := NormalizeDNSFailThreshold(" 007 "); err != nil || got != "7" || n != 7 {
		t.Errorf("应归一化为 7: %q %d %v", got, n, err)
	}
	requireInvalid(t, mustErr2(NormalizeDNSFailThreshold("0")), "dns_fail_threshold")
	requireInvalid(t, mustErr2(NormalizeDNSFailThreshold("-1")), "dns_fail_threshold")
	requireInvalid(t, mustErr2(NormalizeDNSFailThreshold("abc")), "dns_fail_threshold")
}

// TestNormalizeLogLevelTheme 枚举
func TestNormalizeLogLevelTheme(t *testing.T) {
	if got, err := NormalizeLogLevel(" WARN "); err != nil || got != "warn" {
		t.Errorf("log_level 归一化失败: %q %v", got, err)
	}
	requireInvalid(t, mustErr(NormalizeLogLevel("trace")), "log_level")

	if got, err := NormalizeTheme(" DARK "); err != nil || got != "dark" {
		t.Errorf("theme 归一化失败: %q %v", got, err)
	}
	requireInvalid(t, mustErr(NormalizeTheme("blue")), "theme")
}

// TestNormalizeSyncEnabled 同步开关只接受 true/false
func TestNormalizeSyncEnabled(t *testing.T) {
	for in, want := range map[string]bool{"true": true, " false ": false} {
		got, err := NormalizeSyncEnabled(in)
		if err != nil || got != want {
			t.Errorf("NormalizeSyncEnabled(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "1", "yes", "TRUE"} {
		if _, err := NormalizeSyncEnabled(in); err == nil {
			t.Errorf("NormalizeSyncEnabled(%q) 应失败", in)
		}
	}
}

// TestNormalizeDNSAddress DNS 地址：hostname / IPv4 / 端口 / 括号 IPv6 与非法端口
func TestNormalizeDNSAddress(t *testing.T) {
	valid := []string{
		"8.8.8.8", "223.5.5.5", "dns.google", "dns.google:5353",
		"8.8.8.8:53", " 1.1.1.1 ", "[2001:db8::1]", "[2001:db8::1]:53", "[::1]:5353",
	}
	for _, in := range valid {
		if _, err := NormalizeDNSAddress(in); err != nil {
			t.Errorf("NormalizeDNSAddress(%q) 应通过: %v", in, err)
		}
	}

	invalid := []string{
		"", "   ",
		"8.8.8.8:0", "8.8.8.8:65536", "8.8.8.8:abc", "8.8.8.8:", ":53",
		"::1", "2001:db8::1", "2001:db8::1:53", // 未加括号的 IPv6 按固定契约拒绝
		"[not-an-ip]", "[1.2.3.4]", "[2001:db8::1]:0", "[2001:db8::1",
		"1.2.3.4:53:53", "host:port",
	}
	for _, in := range invalid {
		if _, err := NormalizeDNSAddress(in); err == nil {
			t.Errorf("NormalizeDNSAddress(%q) 应失败", in)
		}
	}
}

// TestNormalizeAlertEmail 邮件端口、启用必填项与 Build7 主题/正文边界
func TestNormalizeAlertEmail(t *testing.T) {
	got, err := NormalizeAlertEmail(AlertEmailConfig{Security: "auto_starttls",
		Port: " 587 ", Host: " smtp.example.com ", Subject: " [FWAlizer] 告警通知 ",
	})
	if err != nil {
		t.Fatalf("禁用状态应通过: %v", err)
	}
	if got.Port != "587" || got.Host != "smtp.example.com" || got.Subject != "[FWAlizer] 告警通知" {
		t.Errorf("归一化错误: %+v", got)
	}

	if _, err := NormalizeAlertEmail(AlertEmailConfig{Security: "auto_starttls",
		Enabled: true, Port: "587", Host: "h", FromAddr: "f", ToAddr: "t", Subject: "s",
	}); err != nil {
		t.Errorf("启用且字段齐全应通过: %v", err)
	}

	requireInvalid(t, mustErr(NormalizeAlertEmail(AlertEmailConfig{Security: "auto_starttls", Port: ""})), "email.port")
	requireInvalid(t, mustErr(NormalizeAlertEmail(AlertEmailConfig{Security: "auto_starttls", Port: "0"})), "email.port")
	requireInvalid(t, mustErr(NormalizeAlertEmail(AlertEmailConfig{Security: "auto_starttls", Port: "65536"})), "email.port")
	requireInvalid(t, mustErr(NormalizeAlertEmail(AlertEmailConfig{Security: "auto_starttls", Enabled: true, Port: "587"})), "email.host")
	requireInvalid(t, mustErr(NormalizeAlertEmail(AlertEmailConfig{Security: "auto_starttls", Enabled: true, Port: "587", Host: "h"})), "email.from_addr")
	requireInvalid(t, mustErr(NormalizeAlertEmail(AlertEmailConfig{Security: "auto_starttls", Enabled: true, Port: "587", Host: "h", FromAddr: "f"})), "email.to_addr")

	// Build7 §4.5：主题 Trim 后不能为空、禁止换行/控制字符、最多 200 字符
	requireInvalid(t, mustErr(NormalizeAlertEmail(AlertEmailConfig{Security: "auto_starttls", Port: "587", Subject: "   "})), "email.subject")
	requireInvalid(t, mustErr(NormalizeAlertEmail(AlertEmailConfig{Security: "auto_starttls", Port: "587", Subject: "a\nb"})), "email.subject")
	requireInvalid(t, mustErr(NormalizeAlertEmail(AlertEmailConfig{Security: "auto_starttls", Port: "587", Subject: strings.Repeat("a", MaxEmailSubjectRunes+1)})), "email.subject")
	// 正文允许普通换行，但最大 10 KiB
	if _, err := NormalizeAlertEmail(AlertEmailConfig{Security: "auto_starttls", Port: "587", Subject: "s", Body: "第一行\n第二行"}); err != nil {
		t.Errorf("正文允许普通换行: %v", err)
	}
	requireInvalid(t, mustErr(NormalizeAlertEmail(AlertEmailConfig{Security: "auto_starttls",
		Port: "587", Subject: "s", Body: strings.Repeat("x", MaxEmailBodyBytes+1),
	})), "email.body")
}

// TestNormalizeAlertPolicy Build7 §4.1/§4.5：唯一 health_timeout 必须是大于 0 的时长
func TestNormalizeAlertPolicy(t *testing.T) {
	got, err := NormalizeAlertPolicy(AlertPolicyConfig{HealthTimeoutText: " 10m "})
	if err != nil {
		t.Fatalf("合法 health_timeout 应通过: %v", err)
	}
	if got.HealthTimeout != 10*time.Minute || got.HealthTimeoutText != "10m" {
		t.Errorf("health_timeout 归一化错误: %+v", got)
	}

	if _, err := NormalizeAlertPolicy(AlertPolicyConfig{
		DNSFailedEnabled: true, SyncErrorEnabled: true, OperationalErrorEnabled: true, HealthTimeoutText: "25m",
	}); err != nil {
		t.Errorf("三个触发开关同时开启应通过: %v", err)
	}

	requireInvalid(t, mustErr(NormalizeAlertPolicy(AlertPolicyConfig{})), "policy.health_timeout")
	requireInvalid(t, mustErr(NormalizeAlertPolicy(AlertPolicyConfig{HealthTimeoutText: "0s"})), "policy.health_timeout")
	requireInvalid(t, mustErr(NormalizeAlertPolicy(AlertPolicyConfig{HealthTimeoutText: "abc"})), "policy.health_timeout")
}

// TestNormalizeUptimeKumaPush Build7 §4.5：最小 20s、启用时 URL 必须绝对 http/https
func TestNormalizeUptimeKumaPush(t *testing.T) {
	got, err := NormalizeUptimeKumaPush(UptimeKumaPushConfig{IntervalText: " 60s "})
	if err != nil {
		t.Fatalf("关闭且间隔合法应通过: %v", err)
	}
	if got.Interval != time.Minute || got.IntervalText != "60s" {
		t.Errorf("interval 归一化错误: %+v", got)
	}

	// 禁用状态允许 URL 为空，也不校验其他文本
	if _, err := NormalizeUptimeKumaPush(UptimeKumaPushConfig{IntervalText: "60s", URL: "not-a-url"}); err != nil {
		t.Errorf("禁用状态不应校验 URL: %v", err)
	}
	if _, err := NormalizeUptimeKumaPush(UptimeKumaPushConfig{
		Enabled: true, IntervalText: "120s", URL: " https://kuma.example.com/api/push/tok?status=up ",
	}); err != nil {
		t.Errorf("启用且 URL 合法应通过: %v", err)
	}

	requireInvalid(t, mustErr(NormalizeUptimeKumaPush(UptimeKumaPushConfig{IntervalText: "19s"})), "uptime_kuma_push.interval")
	requireInvalid(t, mustErr(NormalizeUptimeKumaPush(UptimeKumaPushConfig{IntervalText: "0s"})), "uptime_kuma_push.interval")
	requireInvalid(t, mustErr(NormalizeUptimeKumaPush(UptimeKumaPushConfig{IntervalText: "abc"})), "uptime_kuma_push.interval")
	requireInvalid(t, mustErr(NormalizeUptimeKumaPush(UptimeKumaPushConfig{Enabled: true, IntervalText: "60s"})), "uptime_kuma_push.url")
	requireInvalid(t, mustErr(NormalizeUptimeKumaPush(UptimeKumaPushConfig{Enabled: true, IntervalText: "60s", URL: "ftp://kuma/x"})), "uptime_kuma_push.url")
	requireInvalid(t, mustErr(NormalizeUptimeKumaPush(UptimeKumaPushConfig{Enabled: true, IntervalText: "60s", URL: "https://"})), "uptime_kuma_push.url")
}

// TestNormalizeAlertWebhook 渠道枚举与启用时的 URL 校验
func TestNormalizeAlertWebhook(t *testing.T) {
	got, err := NormalizeAlertWebhook(AlertWebhookConfig{Channel: " FEISHU "})
	if err != nil || got.Channel != "feishu" {
		t.Errorf("渠道归一化失败: %+v %v", got, err)
	}
	// 禁用状态下允许留空 URL / 任意文本
	if _, err := NormalizeAlertWebhook(AlertWebhookConfig{Channel: "slack", URL: "not-a-url"}); err != nil {
		t.Errorf("禁用状态不应校验 URL: %v", err)
	}
	if _, err := NormalizeAlertWebhook(AlertWebhookConfig{Enabled: true, Channel: "dingtalk", URL: " https://example.com/hook "}); err != nil {
		t.Errorf("启用且 URL 合法应通过: %v", err)
	}

	if _, err := NormalizeAlertWebhook(AlertWebhookConfig{Channel: ""}); err != nil {
		t.Fatalf("关闭时允许空渠道: %v", err)
	}
	requireInvalid(t, mustErr(NormalizeAlertWebhook(AlertWebhookConfig{Enabled: true, Channel: ""})), "webhook.channel")
	requireInvalid(t, mustErr(NormalizeAlertWebhook(AlertWebhookConfig{Channel: "wecom"})), "webhook.channel")
	requireInvalid(t, mustErr(NormalizeAlertWebhook(AlertWebhookConfig{Enabled: true, Channel: "dingtalk", URL: ""})), "webhook.url")
	requireInvalid(t, mustErr(NormalizeAlertWebhook(AlertWebhookConfig{Enabled: true, Channel: "dingtalk", URL: "ftp://example.com"})), "webhook.url")
	requireInvalid(t, mustErr(NormalizeAlertWebhook(AlertWebhookConfig{Enabled: true, Channel: "dingtalk", URL: "https://"})), "webhook.url")
}

// mustErr 丢弃第一返回值（供 requireInvalid 使用）
func mustErr[T any](_ T, err error) error { return err }

// mustErr2 丢弃前两个返回值
func mustErr2[T1 any, T2 any](_ T1, _ T2, err error) error { return err }
