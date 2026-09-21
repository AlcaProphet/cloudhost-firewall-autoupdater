package config

import (
	"testing"
	"time"
)

func TestParseEnv_Normal(t *testing.T) {
	content := `
# 云资源目标
TARGETS=tc_lighthouse|lhins-abc|ap-guangzhou, tc_cvm|sg-def|ap-shanghai

# 凭据
TC_ACCESS_ID=AKIDxxxx
TC_ACCESS_KEY=secretxxxx

# 域名规则
RULES=api.example.com|TCP|443,80|ACCEPT||生产API, vpn.example.com|UDP|1194|ACCEPT|1|VPN接入

# 全局设置
TAG=auto-dns
INTERVAL=10m
DNS=1.1.1.1:53
LOG_LEVEL=debug
`
	cfg, err := ParseEnv(content)
	if err != nil {
		t.Fatalf("ParseEnv 失败: %v", err)
	}
	if len(cfg.Targets) != 2 {
		t.Errorf("Targets 数量 = %d, want 2", len(cfg.Targets))
	}
	if cfg.Targets[0].CloudType != CloudTCLighthouse {
		t.Errorf("Targets[0].CloudType = %s, want tc_lighthouse", cfg.Targets[0].CloudType)
	}
	if cfg.Targets[0].ResourceID != "lhins-abc" {
		t.Errorf("Targets[0].ResourceID = %s, want lhins-abc", cfg.Targets[0].ResourceID)
	}
	if cfg.Targets[1].Region != "ap-shanghai" {
		t.Errorf("Targets[1].Region = %s, want ap-shanghai", cfg.Targets[1].Region)
	}
	if len(cfg.DomainRules) != 2 {
		t.Errorf("DomainRules 数量 = %d, want 2", len(cfg.DomainRules))
	}
	if cfg.DomainRules[0].Host != "api.example.com" {
		t.Errorf("DomainRules[0].Host = %s, want api.example.com", cfg.DomainRules[0].Host)
	}
	if cfg.DomainRules[0].Ports != "443,80" {
		t.Errorf("DomainRules[0].Ports = %s, want 443,80", cfg.DomainRules[0].Ports)
	}
	if cfg.DomainRules[0].Comment != "生产API" {
		t.Errorf("DomainRules[0].Comment = %s, want 生产API", cfg.DomainRules[0].Comment)
	}
	if cfg.DomainRules[1].Targets[0] != 1 {
		t.Errorf("DomainRules[1].Targets[0] = %d, want 1", cfg.DomainRules[1].Targets[0])
	}
	if cfg.Interval != 10*time.Minute {
		t.Errorf("Interval = %v, want 10m", cfg.Interval)
	}
	if cfg.DNS != "1.1.1.1:53" {
		t.Errorf("DNS = %s, want 1.1.1.1:53", cfg.DNS)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %s, want debug", cfg.LogLevel)
	}
	if cfg.TCAccessID != "AKIDxxxx" {
		t.Errorf("TCAccessID = %s, want AKIDxxxx", cfg.TCAccessID)
	}
}

func TestParseEnv_Continuation(t *testing.T) {
	content := `TARGETS=tc_lighthouse|lhins-abc|ap-guangzhou, \
        tc_cvm|sg-def|ap-shanghai, \
        ali_swas|ace0706b|cn-hangzhou
TC_ACCESS_ID=AKIDxxxx
TC_ACCESS_KEY=secret
ALI_ACCESS_ID=ali_id
ALI_ACCESS_KEY=ali_key
RULES=api.example.com|TCP|443|ACCEPT
`
	cfg, err := ParseEnv(content)
	if err != nil {
		t.Fatalf("ParseEnv 续行合并失败: %v", err)
	}
	if len(cfg.Targets) != 3 {
		t.Errorf("Targets 数量 = %d, want 3", len(cfg.Targets))
	}
	if cfg.Targets[2].CloudType != CloudAliSWAS {
		t.Errorf("Targets[2].CloudType = %s, want ali_swas", cfg.Targets[2].CloudType)
	}
}

func TestParseEnv_Defaults(t *testing.T) {
	content := `
TARGETS=tc_lighthouse|lhins-abc|ap-guangzhou
TC_ACCESS_ID=id
TC_ACCESS_KEY=key
RULES=a.com|TCP|80|ACCEPT
`
	cfg, err := ParseEnv(content)
	if err != nil {
		t.Fatalf("ParseEnv 失败: %v", err)
	}
	if cfg.Tag != "auto-dns" {
		t.Errorf("Tag = %s, want auto-dns", cfg.Tag)
	}
	if cfg.DNS != "223.5.5.5" {
		t.Errorf("DNS = %s, want 223.5.5.5", cfg.DNS)
	}
	if cfg.Interval != 5*time.Minute {
		t.Errorf("Interval = %v, want 5m", cfg.Interval)
	}
	if cfg.DNSTimeout != 10*time.Second {
		t.Errorf("DNSTimeout = %v, want 10s", cfg.DNSTimeout)
	}
	if cfg.DNSFailThreshold != 5 {
		t.Errorf("DNSFailThreshold = %d, want 5", cfg.DNSFailThreshold)
	}
	if cfg.WebUIPort != 60200 {
		t.Errorf("WebUIPort = %d, want 60200", cfg.WebUIPort)
	}
	if cfg.WebUIHost != "127.0.0.1" {
		t.Errorf("WebUIHost = %s, want 127.0.0.1", cfg.WebUIHost)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %s, want info", cfg.LogLevel)
	}
}

func TestParseEnv_WebUIListen(t *testing.T) {
	cfg, err := ParseEnv("WEBUI_HOST=0.0.0.0\nWEBUI_PORT=61234\n")
	if err != nil {
		t.Fatalf("ParseEnv 失败: %v", err)
	}
	if cfg.WebUIHost != "0.0.0.0" {
		t.Errorf("WebUIHost = %s, want 0.0.0.0", cfg.WebUIHost)
	}
	if cfg.WebUIPort != 61234 {
		t.Errorf("WebUIPort = %d, want 61234", cfg.WebUIPort)
	}
}

func TestApplyWebUIEnv(t *testing.T) {
	t.Setenv("WEBUI_HOST", "0.0.0.0")
	t.Setenv("WEBUI_PORT", "61234")
	cfg := &Config{WebUIHost: "127.0.0.1", WebUIPort: 60200}
	if err := ApplyWebUIEnv(cfg); err != nil {
		t.Fatalf("ApplyWebUIEnv 失败: %v", err)
	}
	if cfg.WebUIHost != "0.0.0.0" || cfg.WebUIPort != 61234 {
		t.Errorf("监听配置 = %s:%d, want 0.0.0.0:61234", cfg.WebUIHost, cfg.WebUIPort)
	}
}

func TestApplyWebUIEnv_InvalidPort(t *testing.T) {
	t.Setenv("WEBUI_PORT", "invalid")
	if err := ApplyWebUIEnv(&Config{}); err == nil {
		t.Fatal("WEBUI_PORT 非整数时应报错")
	}
}

func TestParseEnv_InvalidProtocol(t *testing.T) {
	content := `
TARGETS=tc_lighthouse|lhins-abc|ap-guangzhou
TC_ACCESS_ID=id
TC_ACCESS_KEY=key
RULES=a.com|HTTP|80|ACCEPT
`
	_, err := ParseEnv(content)
	if err == nil {
		t.Fatal("协议不合法应报错")
	}
}

func TestParseEnv_InvalidAction(t *testing.T) {
	content := `
TARGETS=tc_lighthouse|lhins-abc|ap-guangzhou
TC_ACCESS_ID=id
TC_ACCESS_KEY=key
RULES=a.com|TCP|80|REJECT
`
	_, err := ParseEnv(content)
	if err == nil {
		t.Fatal("action 不合法应报错")
	}
}

func TestParseEnv_TargetNumOutOfRange(t *testing.T) {
	content := `
TARGETS=tc_lighthouse|lhins-abc|ap-guangzhou
TC_ACCESS_ID=id
TC_ACCESS_KEY=key
RULES=a.com|TCP|80|ACCEPT|5|超出范围
`
	_, err := ParseEnv(content)
	if err == nil {
		t.Fatal("编号越界应报错")
	}
}

func TestParseEnv_InvalidProvider(t *testing.T) {
	content := `
TARGETS=aws_ec2|i-123|us-east-1
RULES=a.com|TCP|80|ACCEPT
`
	_, err := ParseEnv(content)
	if err == nil {
		t.Fatal("provider 不合法应报错")
	}
}

func TestParseEnv_EmptyContent(t *testing.T) {
	cfg, err := ParseEnv("")
	if err != nil {
		t.Fatalf("空内容不应报错: %v", err)
	}
	if len(cfg.Targets) != 0 {
		t.Errorf("空内容 Targets 应为空")
	}
}

func TestParseEnv_ICMPForceAll(t *testing.T) {
	content := `
TARGETS=tc_lighthouse|lhins-abc|ap-guangzhou
TC_ACCESS_ID=id
TC_ACCESS_KEY=key
RULES=ping.example.com|ICMP|8080|ACCEPT
`
	cfg, err := ParseEnv(content)
	if err != nil {
		t.Fatalf("ParseEnv 失败: %v", err)
	}
	if cfg.DomainRules[0].Ports != "ALL" {
		t.Errorf("ICMP 端口应强制为 ALL, got %s", cfg.DomainRules[0].Ports)
	}
}

func TestValidate_MissingCredentials(t *testing.T) {
	cfg := &Config{
		Targets: []TargetConfig{
			{CloudType: CloudTCLighthouse, ResourceID: "lhins-abc", Region: "ap-guangzhou"},
		},
		DomainRules: []DomainRule{
			{Host: "a.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("缺少腾讯云凭据应报错")
	}
}

func TestValidate_EmptyTargets(t *testing.T) {
	cfg := &Config{
		DomainRules: []DomainRule{
			{Host: "a.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("TARGETS 为空应报错")
	}
}

func TestValidate_EmptyRules(t *testing.T) {
	cfg := &Config{
		Targets: []TargetConfig{
			{CloudType: CloudTCLighthouse, ResourceID: "lhins-abc", Region: "ap-guangzhou"},
		},
		TCAccessID:  "id",
		TCAccessKey: "key",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("RULES 为空应报错")
	}
}

func TestValidate_Success(t *testing.T) {
	cfg := &Config{
		Targets: []TargetConfig{
			{CloudType: CloudTCLighthouse, ResourceID: "lhins-abc", Region: "ap-guangzhou"},
			{CloudType: CloudAliSWAS, ResourceID: "ace0706b", Region: "cn-hangzhou"},
		},
		DomainRules: []DomainRule{
			{Host: "a.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT"},
		},
		TCAccessID:  "tc_id",
		TCAccessKey: "tc_key",
		AliAccessID:  "ali_id",
		AliAccessKey: "ali_key",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("合法配置不应报错: %v", err)
	}
}

// ─── SyncEnabled 开关解析 ───

func TestParseEnv_SyncEnabledTrue(t *testing.T) {
	cfg, err := ParseEnv("SYNC_ENABLED=true\n")
	if err != nil {
		t.Fatalf("ParseEnv 失败: %v", err)
	}
	if !cfg.SyncEnabled {
		t.Error("SYNC_ENABLED=true 应解析为 true")
	}
}

func TestParseEnv_SyncEnabledFalse(t *testing.T) {
	cfg, err := ParseEnv("SYNC_ENABLED=false\n")
	if err != nil {
		t.Fatalf("ParseEnv 失败: %v", err)
	}
	if cfg.SyncEnabled {
		t.Error("SYNC_ENABLED=false 应解析为 false")
	}
}

func TestParseEnv_SyncEnabledDefault(t *testing.T) {
	cfg, err := ParseEnv("")
	if err != nil {
		t.Fatalf("ParseEnv 失败: %v", err)
	}
	if !cfg.SyncEnabled {
		t.Error("未设置 SYNC_ENABLED 应默认 true（向后兼容）")
	}
}

func TestParseEnv_SyncEnabledInvalid(t *testing.T) {
	cfg, err := ParseEnv("SYNC_ENABLED=abc\n")
	if err != nil {
		t.Fatalf("ParseEnv 失败: %v", err)
	}
	if cfg.SyncEnabled {
		t.Error("SYNC_ENABLED=abc 非法值应解析为 false（v == true 语义）")
	}
}
