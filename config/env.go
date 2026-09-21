package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LoadEnv 从 .env 文件加载配置
func LoadEnv(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 .env 失败: %w", err)
	}
	return ParseEnv(string(data))
}

// ParseEnv 解析 .env 内容
func ParseEnv(content string) (*Config, error) {
	// 1. 反斜杠续行合并
	content = mergeContinuation(content)
	// 2. 解析键值对
	kv := parseKeyValue(content)
	// 3. 构建 Config
	cfg := &Config{
		Tag:              getOr(kv, "TAG", "auto-dns"),
		DNS:              getOr(kv, "DNS", "223.5.5.5"),
		LogLevel:         getOr(kv, "LOG_LEVEL", "info"),
		DNSTimeout:       10 * time.Second,
		DNSFailThreshold: 5,
		WebUIHost:        "127.0.0.1",
		WebUIPort:        60200,
		SyncEnabled:      true, // 默认开启
		TCAccessID:       kv["TC_ACCESS_ID"],
		TCAccessKey:      kv["TC_ACCESS_KEY"],
		AliAccessID:      kv["ALI_ACCESS_ID"],
		AliAccessKey:     kv["ALI_ACCESS_KEY"],
		Mode:             kv["FWALIZER_MODE"],
	}
	// 解析可选值
	if v := kv["DNS_TIMEOUT"]; v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("DNS_TIMEOUT 格式错误: %w", err)
		}
		cfg.DNSTimeout = d
	}
	if v := kv["DNS_FAIL_THRESHOLD"]; v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("DNS_FAIL_THRESHOLD 必须为整数: %w", err)
		}
		cfg.DNSFailThreshold = n
	}
	if v := strings.TrimSpace(kv["WEBUI_HOST"]); v != "" {
		cfg.WebUIHost = v
	}
	if v := kv["WEBUI_PORT"]; v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("WEBUI_PORT 必须为整数: %w", err)
		}
		cfg.WebUIPort = n
	}
	if v := kv["INTERVAL"]; v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("INTERVAL 格式错误: %w", err)
		}
		cfg.Interval = d
	} else {
		cfg.Interval = 5 * time.Minute
	}
	// 解析同步开关（非法值按 false 处理，与 `v == "true"` 语义一致）
	if v := kv["SYNC_ENABLED"]; v != "" {
		cfg.SyncEnabled = v == "true"
	}
	// 解析 TARGETS
	if v := kv["TARGETS"]; v != "" {
		targets, err := parseTargets(v)
		if err != nil {
			return nil, err
		}
		cfg.Targets = targets
	}
	// 解析 RULES
	if v := kv["RULES"]; v != "" {
		rules, err := parseRules(v, len(cfg.Targets))
		if err != nil {
			return nil, err
		}
		cfg.DomainRules = rules
	}
	return cfg, nil
}

// ApplyWebUIEnv 将运行时环境变量覆盖到 WebUI 监听配置。
// WebUI 模式主配置来自 SQLite，容器部署需要通过环境变量单独指定监听地址和端口。
func ApplyWebUIEnv(cfg *Config) error {
	if v := strings.TrimSpace(os.Getenv("WEBUI_HOST")); v != "" {
		cfg.WebUIHost = v
	}
	if v := strings.TrimSpace(os.Getenv("WEBUI_PORT")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("WEBUI_PORT 必须为整数: %w", err)
		}
		cfg.WebUIPort = n
	}
	return nil
}

// mergeContinuation 将 `\` 续行合并为单行
func mergeContinuation(content string) string {
	lines := strings.Split(content, "\n")
	var merged []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		for strings.HasSuffix(strings.TrimSpace(line), "\\") {
			line = strings.TrimSuffix(strings.TrimSpace(line), "\\")
			i++
			if i < len(lines) {
				line += " " + strings.TrimSpace(lines[i])
			}
		}
		merged = append(merged, line)
	}
	return strings.Join(merged, "\n")
}

// parseKeyValue 解析 KEY=VALUE，忽略注释和空行
func parseKeyValue(content string) map[string]string {
	kv := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		kv[key] = val
	}
	return kv
}

// parseTargets 解析 "provider|resource_id|region, ..."
func parseTargets(s string) ([]TargetConfig, error) {
	entries := splitEntries(s)
	var targets []TargetConfig
	for i, entry := range entries {
		parts := strings.Split(entry, "|")
		if len(parts) != 3 {
			return nil, fmt.Errorf("TARGETS 第 %d 项格式错误，应为 provider|resource_id|region", i+1)
		}
		ct := CloudType(strings.TrimSpace(parts[0]))
		switch ct {
		case CloudTCLighthouse, CloudTCCVM, CloudAliSWAS, CloudAliECS:
		default:
			return nil, fmt.Errorf("TARGETS 第 %d 项 provider 不合法: %s", i+1, parts[0])
		}
		targets = append(targets, TargetConfig{
			CloudType:  ct,
			ResourceID: strings.TrimSpace(parts[1]),
			Region:     strings.TrimSpace(parts[2]),
		})
	}
	return targets, nil
}

// parseRules 解析 "host|protocol|ports|action|targets|comment, ..."
// 注意：ports 字段可含逗号（如 443,80），因此使用智能分割而非简单逗号拆分
func parseRules(s string, targetCount int) ([]DomainRule, error) {
	entries := splitRuleEntries(s)
	var rules []DomainRule
	for i, entry := range entries {
		parts := strings.Split(entry, "|")
		if len(parts) < 4 || len(parts) > 6 {
			return nil, fmt.Errorf("RULES 第 %d 项格式错误，应为 host|protocol|ports|action[|targets[|comment]]", i+1)
		}
		rule := DomainRule{
			Host:     strings.TrimSpace(parts[0]),
			Protocol: strings.ToUpper(strings.TrimSpace(parts[1])),
			Ports:    strings.TrimSpace(parts[2]),
			Action:   strings.ToUpper(strings.TrimSpace(parts[3])),
		}
		// 协议校验
		switch rule.Protocol {
		case "TCP", "UDP", "TCP+UDP", "ICMP":
		default:
			return nil, fmt.Errorf("RULES 第 %d 项协议不合法: %s", i+1, rule.Protocol)
		}
		// ICMP 强制端口为 ALL
		if rule.Protocol == "ICMP" {
			rule.Ports = "ALL"
		}
		// Action 校验
		if rule.Action != "ACCEPT" && rule.Action != "DROP" {
			return nil, fmt.Errorf("RULES 第 %d 项 action 不合法: %s", i+1, rule.Action)
		}
		// targets 解析
		if len(parts) >= 5 {
			t := strings.TrimSpace(parts[4])
			if t != "" && t != "*" {
				nums, err := parseTargetNums(t, targetCount)
				if err != nil {
					return nil, fmt.Errorf("RULES 第 %d 项: %w", i+1, err)
				}
				rule.Targets = nums
			}
		}
		// comment
		if len(parts) >= 6 {
			rule.Comment = strings.TrimSpace(parts[5])
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// splitEntries 按逗号拆分条目（忽略尾部空格），用于 TARGETS 等不含内部逗号的字段
func splitEntries(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// splitRuleEntries 智能分割 RULES 条目
// 端口字段可含逗号（如 443,80），因此不能简单按逗号拆分
// 策略：通过检测 "host|PROTOCOL|" 模式识别新条目起始位置
var newEntryPattern = regexp.MustCompile(`^[^|]+\|(TCP|UDP|TCP\+UDP|ICMP)\|`)

func splitRuleEntries(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var entries []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] != ',' {
			continue
		}
		// 检查逗号后是否为新条目起始
		after := strings.TrimSpace(s[i+1:])
		if newEntryPattern.MatchString(after) {
			entry := strings.TrimSpace(s[start:i])
			if entry != "" {
				entries = append(entries, entry)
			}
			start = i + 1
		}
	}
	// 最后一段
	if entry := strings.TrimSpace(s[start:]); entry != "" {
		entries = append(entries, entry)
	}
	return entries
}

// parseTargetNums 解析 "0,2" → []int{0,2}，并校验范围
func parseTargetNums(s string, max int) ([]int, error) {
	parts := strings.Split(s, ",")
	var nums []int
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, fmt.Errorf("targets 编号不合法: %s", p)
		}
		if n < 0 || n >= max {
			return nil, fmt.Errorf("targets 编号 %d 超出范围 [0,%d]", n, max-1)
		}
		nums = append(nums, n)
	}
	return nums, nil
}

func getOr(kv map[string]string, key, def string) string {
	if v := kv[key]; v != "" {
		return v
	}
	return def
}
