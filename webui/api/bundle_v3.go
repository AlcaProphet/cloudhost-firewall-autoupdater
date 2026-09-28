package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// schemaVersionV3 是唯一被接受的配置包版本（Build7 固定决策 20）。
//
// version 1/2 与其他版本一律返回 400：不迁移、不猜测、不补全、无隐藏兼容入口。
const schemaVersionV3 = 3

// bundleV3SecondsRFC3339 是导出时间与附件文件名共用的 UTC 时间格式。
const (
	bundleV3TimeLayout = time.RFC3339
	// bundleV3FileTimeLayout 固定为 20060102T150405Z（Build6 §3.3）
	bundleV3FileTimeLayout = "20060102T150405Z"
	// bundleV3FilenamePrefix 附件文件名前缀
	bundleV3FilenamePrefix = "fwalizer-config-v3-"
)

// presenceSlice 是带 presence 信息的 JSON 数组：缺失与 null 都视为未提供。
//
// 用它替代裸切片，避免把「字段缺失」误当成合法的空数组（Build6 §3.2、§12.8）。
type presenceSlice[T any] struct {
	Values   []T
	Provided bool
}

// UnmarshalJSON 实现 json.Unmarshaler。
//
// 必须使用独立的严格 decoder：encoding/json 在外层设置了 DisallowUnknownFields
// 后**不会**传播到自定义 UnmarshalJSON 内部，直接用 json.Unmarshal 会让数组元素
// 里的未知字段被静默忽略（Build6 §3.2「任意层级未知字段」）。
func (p *presenceSlice[T]) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		p.Provided = false
		p.Values = nil
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var values []T
	if err := dec.Decode(&values); err != nil {
		return err
	}
	// 只允许一个顶层数组值
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("数组元素后存在多余内容")
	}
	p.Provided = true
	p.Values = values
	return nil
}

// MarshalJSON 保证导出时数组编码为 [] 而不是 null。
func (p presenceSlice[T]) MarshalJSON() ([]byte, error) {
	values := p.Values
	if values == nil {
		values = []T{}
	}
	return json.Marshal(values)
}

// ─── 导出 DTO（value 型：所有字段必然出现，见 Build6 §3.1、§12.8） ───

// bundleV3Metadata 导出元数据：仅用于人工识别与审计，导入时校验格式但不参与运行配置。
type bundleV3Metadata struct {
	ExportedAt string `json:"exported_at"`
}

// bundleV3Target 配置包目标：export_id 只用于配置包内部引用。
type bundleV3Target struct {
	ExportID   int    `json:"export_id"`
	CloudType  string `json:"cloud_type"`
	Region     string `json:"region"`
	ResourceID string `json:"resource_id"`
}

// bundleV3Rule 配置包规则：不导出规则数据库 ID，只通过 target_export_ids 引用目标。
type bundleV3Rule struct {
	Host            string `json:"host"`
	Protocol        string `json:"protocol"`
	Ports           string `json:"ports"`
	Action          string `json:"action"`
	TargetExportIDs []int  `json:"target_export_ids"`
	Comment         string `json:"comment"`
	EnableIPv6      bool   `json:"enable_ipv6"`
}

// bundleV3Credentials 四个云凭据：字段必须存在，但允许空字符串（显式清除旧值）。
type bundleV3Credentials struct {
	Tencent bundleV3TencentCredentials `json:"tencent"`
	Aliyun  bundleV3AliyunCredentials  `json:"aliyun"`
}

type bundleV3TencentCredentials struct {
	SecretID  string `json:"secret_id"`
	SecretKey string `json:"secret_key"`
}

type bundleV3AliyunCredentials struct {
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
}

// bundleV3Settings 完整业务设置。dns_fail_threshold 在配置包中是 JSON number，
// 与普通 settings API 的字符串形态不同（Build6 §12.9）。
type bundleV3Settings struct {
	Credentials      bundleV3Credentials `json:"credentials"`
	Tag              string              `json:"tag"`
	Interval         string              `json:"interval"`
	DNS              string              `json:"dns"`
	DNSTimeout       string              `json:"dns_timeout"`
	DNSFailThreshold int                 `json:"dns_fail_threshold"`
	LogLevel         string              `json:"log_level"`
	SyncEnabled      bool                `json:"sync_enabled"`
	Theme            string              `json:"theme"`
}

type bundleV3Email struct {
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

type bundleV3Webhook struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Channel string `json:"channel"`
}

// bundleV3Policy 告警触发策略（Build7 §4.3）：三个触发开关共用，health_timeout 为时长文本。
type bundleV3Policy struct {
	DNSFailedEnabled        bool   `json:"dns_failed_enabled"`
	SyncErrorEnabled        bool   `json:"sync_error_enabled"`
	OperationalErrorEnabled bool   `json:"operational_error_enabled"`
	HealthTimeout           string `json:"health_timeout"`
}

type bundleV3Alerts struct {
	Policy  bundleV3Policy  `json:"policy"`
	Email   bundleV3Email   `json:"email"`
	Webhook bundleV3Webhook `json:"webhook"`
}

// bundleV3UptimeKumaPush 是 monitoring 段：Push 配置独立于普通 Webhook 渠道（Build7 §4.3）。
type bundleV3UptimeKumaPush struct {
	Enabled  bool   `json:"enabled"`
	URL      string `json:"url"`
	Interval string `json:"interval"`
}

type bundleV3Monitoring struct {
	UptimeKumaPush bundleV3UptimeKumaPush `json:"uptime_kuma_push"`
}

// bundleV3 是导出使用的完整配置包（缺失字段无法表达，因此必然全部出现）。
type bundleV3 struct {
	Version    int                `json:"version"`
	Metadata   bundleV3Metadata   `json:"metadata"`
	Targets    []bundleV3Target   `json:"targets"`
	Rules      []bundleV3Rule     `json:"rules"`
	Settings   bundleV3Settings   `json:"settings"`
	Alerts     bundleV3Alerts     `json:"alerts"`
	Monitoring bundleV3Monitoring `json:"monitoring"`
}

// ─── 导入 DTO（presence 型：缺失与 null 都能与合法零值区分） ───

type bundleV3WireMetadata struct {
	ExportedAt *string `json:"exported_at"`
}

type bundleV3WireTarget struct {
	ExportID   *int    `json:"export_id"`
	CloudType  *string `json:"cloud_type"`
	Region     *string `json:"region"`
	ResourceID *string `json:"resource_id"`
}

type bundleV3WireRule struct {
	Host            *string            `json:"host"`
	Protocol        *string            `json:"protocol"`
	Ports           *string            `json:"ports"`
	Action          *string            `json:"action"`
	TargetExportIDs presenceSlice[int] `json:"target_export_ids"`
	Comment         *string            `json:"comment"`
	EnableIPv6      *bool              `json:"enable_ipv6"`
}

type bundleV3WireTencent struct {
	SecretID  *string `json:"secret_id"`
	SecretKey *string `json:"secret_key"`
}

type bundleV3WireAliyun struct {
	AccessKeyID     *string `json:"access_key_id"`
	AccessKeySecret *string `json:"access_key_secret"`
}

type bundleV3WireCredentials struct {
	Tencent *bundleV3WireTencent `json:"tencent"`
	Aliyun  *bundleV3WireAliyun  `json:"aliyun"`
}

type bundleV3WireSettings struct {
	Credentials      *bundleV3WireCredentials `json:"credentials"`
	Tag              *string                  `json:"tag"`
	Interval         *string                  `json:"interval"`
	DNS              *string                  `json:"dns"`
	DNSTimeout       *string                  `json:"dns_timeout"`
	DNSFailThreshold *int                     `json:"dns_fail_threshold"`
	LogLevel         *string                  `json:"log_level"`
	SyncEnabled      *bool                    `json:"sync_enabled"`
	Theme            *string                  `json:"theme"`
}

type bundleV3WireEmail struct {
	Enabled  *bool   `json:"enabled"`
	Host     *string `json:"host"`
	Port     *string `json:"port"`
	Username *string `json:"username"`
	Password *string `json:"password"`
	FromAddr *string `json:"from_addr"`
	ToAddr   *string `json:"to_addr"`
	Subject  *string `json:"subject"`
	Body     *string `json:"body"`
}

type bundleV3WirePolicy struct {
	DNSFailedEnabled        *bool   `json:"dns_failed_enabled"`
	SyncErrorEnabled        *bool   `json:"sync_error_enabled"`
	OperationalErrorEnabled *bool   `json:"operational_error_enabled"`
	HealthTimeout           *string `json:"health_timeout"`
}

type bundleV3WirePush struct {
	Enabled  *bool   `json:"enabled"`
	URL      *string `json:"url"`
	Interval *string `json:"interval"`
}

type bundleV3WireMonitoring struct {
	UptimeKumaPush *bundleV3WirePush `json:"uptime_kuma_push"`
}

type bundleV3WireWebhook struct {
	Enabled *bool   `json:"enabled"`
	URL     *string `json:"url"`
	Channel *string `json:"channel"`
}

type bundleV3WireAlerts struct {
	Policy  *bundleV3WirePolicy  `json:"policy"`
	Email   *bundleV3WireEmail   `json:"email"`
	Webhook *bundleV3WireWebhook `json:"webhook"`
}

// bundleV3Wire 是导入使用的强类型 version 3 DTO。
//
// 必需 scalar 用指针、必需 object 用指针、必需 array 用 presenceSlice，
// 因此「字段缺失」「显式 null」与「合法零值/空数组」可以严格区分。
type bundleV3Wire struct {
	Version    *int                              `json:"version"`
	Metadata   *bundleV3WireMetadata             `json:"metadata"`
	Targets    presenceSlice[bundleV3WireTarget] `json:"targets"`
	Rules      presenceSlice[bundleV3WireRule]   `json:"rules"`
	Settings   *bundleV3WireSettings             `json:"settings"`
	Alerts     *bundleV3WireAlerts               `json:"alerts"`
	Monitoring *bundleV3WireMonitoring           `json:"monitoring"`
}

// checkedBundleV3 是预校验通过、已归一化的 version 3 配置包。
type checkedBundleV3 struct {
	ExportedAt time.Time
	Targets    []bundleV3Target
	Rules      []bundleV3Rule
	Settings   checkedBundleV3Settings
	Policy     config.AlertPolicyConfig
	Email      config.AlertEmailConfig
	Webhook    config.AlertWebhookConfig
	Push       config.UptimeKumaPushConfig
}

type checkedBundleV3Settings struct {
	Credentials      config.Credentials
	Tag              string
	Interval         string
	DNS              string
	DNSTimeout       string
	DNSFailThreshold string
	LogLevel         string
	SyncEnabled      bool
	Theme            string
}

// ─── 导出构造 ───

// toBundleV3 把事务内取到的业务快照转换为 version 3 配置包。
//
// 目标与规则按数据库 ID 升序稳定输出；每条规则的 target_export_ids 按 export_id
// 升序输出（Build6 §12.11）；数组一律初始化为空切片，保证编码为 [] 而不是 null。
func toBundleV3(snapshot *config.BusinessSnapshot, exportedAt time.Time) bundleV3 {
	targets := make([]bundleV3Target, 0, len(snapshot.Targets))
	for _, t := range snapshot.Targets {
		targets = append(targets, bundleV3Target{
			ExportID:   t.ID,
			CloudType:  string(t.CloudType),
			Region:     t.Region,
			ResourceID: t.ResourceID,
		})
	}

	rules := make([]bundleV3Rule, 0, len(snapshot.Rules))
	for _, r := range snapshot.Rules {
		// 空切片而非 nil：保证 JSON 编码为 []，避免导出出现 null
		refs := make([]int, len(r.Targets))
		copy(refs, r.Targets)
		sortIntsAscending(refs)
		rules = append(rules, bundleV3Rule{
			Host:            r.Host,
			Protocol:        r.Protocol,
			Ports:           r.Ports,
			Action:          r.Action,
			TargetExportIDs: refs,
			Comment:         r.Comment,
			EnableIPv6:      r.EnableIPv6,
		})
	}

	// 告警默认值：与 GET /api/alerts 一致，保证导出的配置包必然可以通过导入校验
	policy := snapshot.Policy
	if policy.HealthTimeoutText == "" {
		policy = config.DefaultAlertPolicy()
	}
	email := snapshot.Email
	if email.Port == "" {
		email.Port = "587"
	}
	if email.Subject == "" {
		email.Subject = config.DefaultEmailSubject
	}
	if email.Body == "" {
		email.Body = config.DefaultEmailBody
	}
	webhook := snapshot.Webhook
	if webhook.Channel == "" {
		webhook.Channel = "dingtalk"
	}
	push := snapshot.UptimeKumaPush
	if push.IntervalText == "" {
		push = config.DefaultUptimeKumaPush()
	}

	return bundleV3{
		Version:  schemaVersionV3,
		Metadata: bundleV3Metadata{ExportedAt: exportedAt.UTC().Format(bundleV3TimeLayout)},
		Targets:  targets,
		Rules:    rules,
		Settings: bundleV3Settings{
			Credentials: bundleV3Credentials{
				Tencent: bundleV3TencentCredentials{
					SecretID:  snapshot.Settings["tc_access_id"],
					SecretKey: snapshot.Settings["tc_access_key"],
				},
				Aliyun: bundleV3AliyunCredentials{
					AccessKeyID:     snapshot.Settings["ali_access_id"],
					AccessKeySecret: snapshot.Settings["ali_access_key"],
				},
			},
			Tag:              snapshot.Settings["tag"],
			Interval:         snapshot.Settings["interval"],
			DNS:              snapshot.Settings["dns"],
			DNSTimeout:       snapshot.Settings["dns_timeout"],
			DNSFailThreshold: atoiOrZero(snapshot.Settings["dns_fail_threshold"]),
			LogLevel:         snapshot.Settings["log_level"],
			SyncEnabled:      snapshot.Settings["sync_enabled"] == "true",
			Theme:            snapshot.Settings["theme"],
		},
		Alerts: bundleV3Alerts{
			Policy: bundleV3Policy{
				DNSFailedEnabled:        policy.DNSFailedEnabled,
				SyncErrorEnabled:        policy.SyncErrorEnabled,
				OperationalErrorEnabled: policy.OperationalErrorEnabled,
				HealthTimeout:           policy.HealthTimeoutText,
			},
			Email: bundleV3Email{
				Enabled:  email.Enabled,
				Host:     email.Host,
				Port:     email.Port,
				Username: email.Username,
				Password: email.Password,
				FromAddr: email.FromAddr,
				ToAddr:   email.ToAddr,
				Subject:  email.Subject,
				Body:     email.Body,
			},
			Webhook: bundleV3Webhook{
				Enabled: webhook.Enabled,
				URL:     webhook.URL,
				Channel: webhook.Channel,
			},
		},
		Monitoring: bundleV3Monitoring{
			UptimeKumaPush: bundleV3UptimeKumaPush{
				Enabled:  push.Enabled,
				URL:      push.URL,
				Interval: push.IntervalText,
			},
		},
	}
}

// ─── 导入预校验与归一化 ───

// validateAndNormalizeBundle 在打开写事务之前完成全部纯数据校验：
// version、metadata、必需字段、枚举、时长、TAG、DNS、告警、export_id 唯一性与引用闭包。
//
// 校验全部复用 config 包已实现的领域校验函数，不建立第二套更宽松或重复的规则
// （Build6 §3.2、§4.2～§4.5、§12.10、§12.12）。
func validateAndNormalizeBundle(wire *bundleV3Wire) (*checkedBundleV3, error) {
	// version：只接受 3，缺失/null/其他一律 400，不做迁移或字段补全
	if wire.Version == nil {
		return nil, badRequest("version 字段缺失")
	}
	if *wire.Version != schemaVersionV3 {
		return nil, badRequest(fmt.Sprintf("不支持的配置版本: %d", *wire.Version))
	}

	// metadata.exported_at：必须存在且为 RFC3339；仅用于审计，不参与运行配置
	if wire.Metadata == nil {
		return nil, badRequest("metadata 字段缺失")
	}
	if wire.Metadata.ExportedAt == nil {
		return nil, badRequest("metadata.exported_at 字段缺失")
	}
	exportedAt, err := time.Parse(time.RFC3339, *wire.Metadata.ExportedAt)
	if err != nil {
		return nil, badRequest("metadata.exported_at 必须是 RFC3339 时间")
	}

	if !wire.Targets.Provided {
		return nil, badRequest("targets 字段缺失或为 null")
	}
	if !wire.Rules.Provided {
		return nil, badRequest("rules 字段缺失或为 null")
	}
	if wire.Settings == nil {
		return nil, badRequest("settings 字段缺失")
	}
	if wire.Alerts == nil {
		return nil, badRequest("alerts 字段缺失")
	}
	if wire.Alerts.Policy == nil {
		return nil, badRequest("alerts.policy 字段缺失")
	}
	if wire.Monitoring == nil {
		return nil, badRequest("monitoring 字段缺失")
	}
	if wire.Monitoring.UptimeKumaPush == nil {
		return nil, badRequest("monitoring.uptime_kuma_push 字段缺失")
	}

	// targets：export_id 正数且全局唯一，cloud_type/region/resource_id 复用领域校验
	targets := make([]bundleV3Target, 0, len(wire.Targets.Values))
	seenExportID := make(map[int]bool, len(wire.Targets.Values))
	for i, t := range wire.Targets.Values {
		if t.ExportID == nil {
			return nil, badRequest(fmt.Sprintf("targets[%d].export_id 字段缺失", i))
		}
		if *t.ExportID <= 0 {
			return nil, badRequest(fmt.Sprintf("targets[%d].export_id 必须是正数", i))
		}
		if seenExportID[*t.ExportID] {
			return nil, badRequest(fmt.Sprintf("targets[%d].export_id 重复: %d（不静默去重）", i, *t.ExportID))
		}
		seenExportID[*t.ExportID] = true

		if t.CloudType == nil {
			return nil, badRequest(fmt.Sprintf("targets[%d].cloud_type 字段缺失", i))
		}
		if t.Region == nil {
			return nil, badRequest(fmt.Sprintf("targets[%d].region 字段缺失", i))
		}
		if t.ResourceID == nil {
			return nil, badRequest(fmt.Sprintf("targets[%d].resource_id 字段缺失", i))
		}
		normalized, err := config.NormalizeTarget(config.TargetConfig{
			CloudType:  config.CloudType(*t.CloudType),
			Region:     *t.Region,
			ResourceID: *t.ResourceID,
		})
		if err != nil {
			return nil, importFieldError("targets", i, err)
		}
		targets = append(targets, bundleV3Target{
			ExportID:   *t.ExportID,
			CloudType:  string(normalized.CloudType),
			Region:     normalized.Region,
			ResourceID: normalized.ResourceID,
		})
	}

	// rules：字段 presence、数组元素正数与组内唯一、引用闭包
	rules := make([]bundleV3Rule, 0, len(wire.Rules.Values))
	for i, r := range wire.Rules.Values {
		if r.Host == nil {
			return nil, badRequest(fmt.Sprintf("rules[%d].host 字段缺失", i))
		}
		if r.Protocol == nil {
			return nil, badRequest(fmt.Sprintf("rules[%d].protocol 字段缺失", i))
		}
		if r.Ports == nil {
			return nil, badRequest(fmt.Sprintf("rules[%d].ports 字段缺失", i))
		}
		if r.Action == nil {
			return nil, badRequest(fmt.Sprintf("rules[%d].action 字段缺失", i))
		}
		if r.Comment == nil {
			return nil, badRequest(fmt.Sprintf("rules[%d].comment 字段缺失", i))
		}
		if r.EnableIPv6 == nil {
			return nil, badRequest(fmt.Sprintf("rules[%d].enable_ipv6 字段缺失", i))
		}
		if !r.TargetExportIDs.Provided {
			return nil, badRequest(fmt.Sprintf("rules[%d].target_export_ids 字段缺失或为 null", i))
		}

		refs, err := config.NormalizeTargetIDs(r.TargetExportIDs.Values)
		if err != nil {
			return nil, importFieldError("rules", i, err)
		}
		for _, ref := range refs {
			if !seenExportID[ref] {
				return nil, badRequest(fmt.Sprintf(
					"rules[%d].target_export_ids 引用了本包不存在的 export_id: %d（不静默删除引用）", i, ref))
			}
		}

		normalized, err := config.NormalizeRule(config.DomainRule{
			Host:       *r.Host,
			Protocol:   *r.Protocol,
			Ports:      *r.Ports,
			Action:     *r.Action,
			Targets:    refs,
			Comment:    *r.Comment,
			EnableIPv6: *r.EnableIPv6,
		})
		if err != nil {
			return nil, importFieldError("rules", i, err)
		}
		rules = append(rules, bundleV3Rule{
			Host:            normalized.Host,
			Protocol:        normalized.Protocol,
			Ports:           normalized.Ports,
			Action:          normalized.Action,
			TargetExportIDs: refs,
			Comment:         normalized.Comment,
			EnableIPv6:      normalized.EnableIPv6,
		})
	}

	settings, err := normalizeBundleSettings(wire.Settings)
	if err != nil {
		return nil, err
	}

	policy, err := normalizeBundlePolicy(wire.Alerts.Policy)
	if err != nil {
		return nil, err
	}
	email, err := normalizeBundleEmail(wire.Alerts.Email)
	if err != nil {
		return nil, err
	}
	webhook, err := normalizeBundleWebhook(wire.Alerts.Webhook)
	if err != nil {
		return nil, err
	}
	push, err := normalizeBundlePush(wire.Monitoring.UptimeKumaPush)
	if err != nil {
		return nil, err
	}

	return &checkedBundleV3{
		ExportedAt: exportedAt,
		Targets:    targets,
		Rules:      rules,
		Settings:   settings,
		Policy:     policy,
		Email:      email,
		Webhook:    webhook,
		Push:       push,
	}, nil
}

// normalizeBundlePolicy 校验并归一化配置包触发策略（三个开关与 health_timeout 都必须存在）。
func normalizeBundlePolicy(w *bundleV3WirePolicy) (config.AlertPolicyConfig, error) {
	if w == nil {
		return config.AlertPolicyConfig{}, badRequest("alerts.policy 字段缺失")
	}
	for name, present := range map[string]bool{
		"dns_failed_enabled":        w.DNSFailedEnabled != nil,
		"sync_error_enabled":        w.SyncErrorEnabled != nil,
		"operational_error_enabled": w.OperationalErrorEnabled != nil,
		"health_timeout":            w.HealthTimeout != nil,
	} {
		if !present {
			return config.AlertPolicyConfig{}, badRequest("alerts.policy." + name + " 字段缺失")
		}
	}
	cfg := config.AlertPolicyConfig{
		DNSFailedEnabled:        *w.DNSFailedEnabled,
		SyncErrorEnabled:        *w.SyncErrorEnabled,
		OperationalErrorEnabled: *w.OperationalErrorEnabled,
		HealthTimeoutText:       *w.HealthTimeout,
	}
	normalized, err := config.NormalizeAlertPolicy(cfg)
	if err != nil {
		return config.AlertPolicyConfig{}, wrapBundleField("alerts.policy", err)
	}
	return normalized, nil
}

// normalizeBundlePush 校验并归一化配置包 Push 配置（字段必须存在，禁用时允许空 URL）。
func normalizeBundlePush(w *bundleV3WirePush) (config.UptimeKumaPushConfig, error) {
	if w == nil {
		return config.UptimeKumaPushConfig{}, badRequest("monitoring.uptime_kuma_push 字段缺失")
	}
	if w.Enabled == nil {
		return config.UptimeKumaPushConfig{}, badRequest("monitoring.uptime_kuma_push.enabled 字段缺失")
	}
	if w.URL == nil {
		return config.UptimeKumaPushConfig{}, badRequest("monitoring.uptime_kuma_push.url 字段缺失")
	}
	if w.Interval == nil {
		return config.UptimeKumaPushConfig{}, badRequest("monitoring.uptime_kuma_push.interval 字段缺失")
	}
	normalized, err := config.NormalizeUptimeKumaPush(config.UptimeKumaPushConfig{
		Enabled:      *w.Enabled,
		URL:          *w.URL,
		IntervalText: *w.Interval,
	})
	if err != nil {
		return config.UptimeKumaPushConfig{}, wrapBundleField("monitoring.uptime_kuma_push", err)
	}
	return normalized, nil
}

// wrapBundleField 给领域校验错误补上配置包路径前缀（保留字段与原因，不回显原值）。
func wrapBundleField(prefix string, err error) error {
	var ve *config.ValidationError
	if errors.As(err, &ve) {
		return &config.ValidationError{
			Field:  prefix + "." + ve.Field,
			Reason: ve.Reason,
		}
	}
	return err
}

// normalizeBundleSettings 校验并归一化配置包设置（复用同一组领域校验函数）。
func normalizeBundleSettings(w *bundleV3WireSettings) (checkedBundleV3Settings, error) {
	var out checkedBundleV3Settings

	if w.Credentials == nil {
		return out, badRequest("settings.credentials 字段缺失")
	}
	if w.Credentials.Tencent == nil {
		return out, badRequest("settings.credentials.tencent 字段缺失")
	}
	if w.Credentials.Tencent.SecretID == nil {
		return out, badRequest("settings.credentials.tencent.secret_id 字段缺失")
	}
	if w.Credentials.Tencent.SecretKey == nil {
		return out, badRequest("settings.credentials.tencent.secret_key 字段缺失")
	}
	if w.Credentials.Aliyun == nil {
		return out, badRequest("settings.credentials.aliyun 字段缺失")
	}
	if w.Credentials.Aliyun.AccessKeyID == nil {
		return out, badRequest("settings.credentials.aliyun.access_key_id 字段缺失")
	}
	if w.Credentials.Aliyun.AccessKeySecret == nil {
		return out, badRequest("settings.credentials.aliyun.access_key_secret 字段缺失")
	}
	// 凭据按原值保存，允许空字符串（明确清除旧凭据）
	out.Credentials = config.Credentials{
		TencentSecretID:       *w.Credentials.Tencent.SecretID,
		TencentSecretKey:      *w.Credentials.Tencent.SecretKey,
		AliyunAccessKeyID:     *w.Credentials.Aliyun.AccessKeyID,
		AliyunAccessKeySecret: *w.Credentials.Aliyun.AccessKeySecret,
	}

	if w.Tag == nil {
		return out, badRequest("settings.tag 字段缺失")
	}
	tag, err := config.NormalizeTag(*w.Tag)
	if err != nil {
		return out, err
	}
	out.Tag = tag

	if w.Interval == nil {
		return out, badRequest("settings.interval 字段缺失")
	}
	interval, _, err := config.ParsePositiveDuration("interval", *w.Interval)
	if err != nil {
		return out, err
	}
	out.Interval = interval

	if w.DNS == nil {
		return out, badRequest("settings.dns 字段缺失")
	}
	dnsAddr, err := config.NormalizeDNSAddress(*w.DNS)
	if err != nil {
		return out, err
	}
	out.DNS = dnsAddr

	if w.DNSTimeout == nil {
		return out, badRequest("settings.dns_timeout 字段缺失")
	}
	dnsTimeout, _, err := config.ParsePositiveDuration("dns_timeout", *w.DNSTimeout)
	if err != nil {
		return out, err
	}
	out.DNSTimeout = dnsTimeout

	if w.DNSFailThreshold == nil {
		return out, badRequest("settings.dns_fail_threshold 字段缺失")
	}
	threshold, _, err := config.NormalizeDNSFailThreshold(strconv.Itoa(*w.DNSFailThreshold))
	if err != nil {
		return out, err
	}
	out.DNSFailThreshold = threshold

	if w.LogLevel == nil {
		return out, badRequest("settings.log_level 字段缺失")
	}
	logLevel, err := config.NormalizeLogLevel(*w.LogLevel)
	if err != nil {
		return out, err
	}
	out.LogLevel = logLevel

	if w.SyncEnabled == nil {
		return out, badRequest("settings.sync_enabled 字段缺失")
	}
	out.SyncEnabled = *w.SyncEnabled

	if w.Theme == nil {
		return out, badRequest("settings.theme 字段缺失")
	}
	theme, err := config.NormalizeTheme(*w.Theme)
	if err != nil {
		return out, err
	}
	out.Theme = theme

	return out, nil
}

// normalizeBundleEmail 校验并归一化配置包邮件告警（禁用状态下全部字段仍必须存在）。
func normalizeBundleEmail(w *bundleV3WireEmail) (config.AlertEmailConfig, error) {
	if w == nil {
		return config.AlertEmailConfig{}, badRequest("alerts.email 字段缺失")
	}
	for name, present := range map[string]bool{
		"enabled": w.Enabled != nil, "host": w.Host != nil, "port": w.Port != nil,
		"username": w.Username != nil, "password": w.Password != nil,
		"from_addr": w.FromAddr != nil, "to_addr": w.ToAddr != nil,
		"subject": w.Subject != nil, "body": w.Body != nil,
	} {
		if !present {
			return config.AlertEmailConfig{}, badRequest("alerts.email." + name + " 字段缺失")
		}
	}
	normalized, err := config.NormalizeAlertEmail(config.AlertEmailConfig{
		Enabled:  *w.Enabled,
		Host:     *w.Host,
		Port:     *w.Port,
		Username: *w.Username,
		Password: *w.Password,
		FromAddr: *w.FromAddr,
		ToAddr:   *w.ToAddr,
		Subject:  *w.Subject,
		Body:     *w.Body,
	})
	if err != nil {
		return config.AlertEmailConfig{}, wrapBundleField("alerts.email", err)
	}
	return normalized, nil
}

// normalizeBundleWebhook 校验并归一化配置包 Webhook 告警（禁用状态下字段仍必须存在）。
func normalizeBundleWebhook(w *bundleV3WireWebhook) (config.AlertWebhookConfig, error) {
	if w == nil {
		return config.AlertWebhookConfig{}, badRequest("alerts.webhook 字段缺失")
	}
	if w.Enabled == nil {
		return config.AlertWebhookConfig{}, badRequest("alerts.webhook.enabled 字段缺失")
	}
	if w.URL == nil {
		return config.AlertWebhookConfig{}, badRequest("alerts.webhook.url 字段缺失")
	}
	if w.Channel == nil {
		return config.AlertWebhookConfig{}, badRequest("alerts.webhook.channel 字段缺失")
	}
	return config.NormalizeAlertWebhook(config.AlertWebhookConfig{
		Enabled: *w.Enabled,
		URL:     *w.URL,
		Channel: *w.Channel,
	})
}

// bundleSettingsToStore 把已校验的配置包设置转换为**显式**的落库键值集合。
//
// 必须逐键写入 version 3 的完整键集合（Build6 §12.12）：不遍历任意 map，
// 因此旧数据库的未知键不会进入新快照。
func bundleSettingsToStore(s checkedBundleV3Settings) map[string]string {
	syncEnabled := "false"
	if s.SyncEnabled {
		syncEnabled = "true"
	}
	return map[string]string{
		"tc_access_id":       s.Credentials.TencentSecretID,
		"tc_access_key":      s.Credentials.TencentSecretKey,
		"ali_access_id":      s.Credentials.AliyunAccessKeyID,
		"ali_access_key":     s.Credentials.AliyunAccessKeySecret,
		"tag":                s.Tag,
		"interval":           s.Interval,
		"dns":                s.DNS,
		"dns_timeout":        s.DNSTimeout,
		"dns_fail_threshold": s.DNSFailThreshold,
		"log_level":          s.LogLevel,
		"sync_enabled":       syncEnabled,
		"theme":              s.Theme,
	}
}

// atoiOrZero 解析已完成校验的整数设置；失败时返回 0（快照中的值不可能非法）。
func atoiOrZero(v string) int {
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

// importFieldError 为数组元素校验错误补上位置信息，便于定位配置包中的问题项。
func importFieldError(section string, index int, err error) error {
	var ve *config.ValidationError
	if errors.As(err, &ve) {
		return &config.ValidationError{
			Field:  fmt.Sprintf("%s[%d].%s", section, index, ve.Field),
			Reason: ve.Reason,
		}
	}
	return err
}

// sortIntsAscending 就地升序排序（小切片插入排序，避免为一个排序引入额外依赖）。
func sortIntsAscending(values []int) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j-1] > values[j]; j-- {
			values[j-1], values[j] = values[j], values[j-1]
		}
	}
}
