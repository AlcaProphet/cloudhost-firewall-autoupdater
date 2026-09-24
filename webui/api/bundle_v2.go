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

// schemaVersionV2 是唯一被接受的配置包版本（Build6 §3.1）。
//
// version 1 与其他版本一律返回 400：不迁移、不猜测、不补全、无隐藏兼容入口。
const schemaVersionV2 = 2

// bundleV2SecondsRFC3339 是导出时间与附件文件名共用的 UTC 时间格式。
const (
	bundleV2TimeLayout = time.RFC3339
	// bundleV2FileTimeLayout 固定为 20060102T150405Z（Build6 §3.3）
	bundleV2FileTimeLayout = "20060102T150405Z"
	// bundleV2FilenamePrefix 附件文件名前缀
	bundleV2FilenamePrefix = "fwalizer-config-v2-"
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

// bundleV2Metadata 导出元数据：仅用于人工识别与审计，导入时校验格式但不参与运行配置。
type bundleV2Metadata struct {
	ExportedAt string `json:"exported_at"`
}

// bundleV2Target 配置包目标：export_id 只用于配置包内部引用。
type bundleV2Target struct {
	ExportID   int    `json:"export_id"`
	CloudType  string `json:"cloud_type"`
	Region     string `json:"region"`
	ResourceID string `json:"resource_id"`
}

// bundleV2Rule 配置包规则：不导出规则数据库 ID，只通过 target_export_ids 引用目标。
type bundleV2Rule struct {
	Host            string `json:"host"`
	Protocol        string `json:"protocol"`
	Ports           string `json:"ports"`
	Action          string `json:"action"`
	TargetExportIDs []int  `json:"target_export_ids"`
	Comment         string `json:"comment"`
	EnableIPv6      bool   `json:"enable_ipv6"`
}

// bundleV2Credentials 四个云凭据：字段必须存在，但允许空字符串（显式清除旧值）。
type bundleV2Credentials struct {
	Tencent bundleV2TencentCredentials `json:"tencent"`
	Aliyun  bundleV2AliyunCredentials  `json:"aliyun"`
}

type bundleV2TencentCredentials struct {
	SecretID  string `json:"secret_id"`
	SecretKey string `json:"secret_key"`
}

type bundleV2AliyunCredentials struct {
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
}

// bundleV2Settings 完整业务设置。dns_fail_threshold 在配置包中是 JSON number，
// 与普通 settings API 的字符串形态不同（Build6 §12.9）。
type bundleV2Settings struct {
	Credentials      bundleV2Credentials `json:"credentials"`
	Tag              string              `json:"tag"`
	Interval         string              `json:"interval"`
	DNS              string              `json:"dns"`
	DNSTimeout       string              `json:"dns_timeout"`
	DNSFailThreshold int                 `json:"dns_fail_threshold"`
	LogLevel         string              `json:"log_level"`
	SyncEnabled      bool                `json:"sync_enabled"`
	Theme            string              `json:"theme"`
}

type bundleV2Email struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	FromAddr string `json:"from_addr"`
	ToAddr   string `json:"to_addr"`
}

type bundleV2Webhook struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Channel string `json:"channel"`
}

type bundleV2Alerts struct {
	Email   bundleV2Email   `json:"email"`
	Webhook bundleV2Webhook `json:"webhook"`
}

// bundleV2 是导出使用的完整配置包（缺失字段无法表达，因此必然全部出现）。
type bundleV2 struct {
	Version  int              `json:"version"`
	Metadata bundleV2Metadata `json:"metadata"`
	Targets  []bundleV2Target `json:"targets"`
	Rules    []bundleV2Rule   `json:"rules"`
	Settings bundleV2Settings `json:"settings"`
	Alerts   bundleV2Alerts   `json:"alerts"`
}

// ─── 导入 DTO（presence 型：缺失与 null 都能与合法零值区分） ───

type bundleV2WireMetadata struct {
	ExportedAt *string `json:"exported_at"`
}

type bundleV2WireTarget struct {
	ExportID   *int    `json:"export_id"`
	CloudType  *string `json:"cloud_type"`
	Region     *string `json:"region"`
	ResourceID *string `json:"resource_id"`
}

type bundleV2WireRule struct {
	Host            *string            `json:"host"`
	Protocol        *string            `json:"protocol"`
	Ports           *string            `json:"ports"`
	Action          *string            `json:"action"`
	TargetExportIDs presenceSlice[int] `json:"target_export_ids"`
	Comment         *string            `json:"comment"`
	EnableIPv6      *bool              `json:"enable_ipv6"`
}

type bundleV2WireTencent struct {
	SecretID  *string `json:"secret_id"`
	SecretKey *string `json:"secret_key"`
}

type bundleV2WireAliyun struct {
	AccessKeyID     *string `json:"access_key_id"`
	AccessKeySecret *string `json:"access_key_secret"`
}

type bundleV2WireCredentials struct {
	Tencent *bundleV2WireTencent `json:"tencent"`
	Aliyun  *bundleV2WireAliyun  `json:"aliyun"`
}

type bundleV2WireSettings struct {
	Credentials      *bundleV2WireCredentials `json:"credentials"`
	Tag              *string                  `json:"tag"`
	Interval         *string                  `json:"interval"`
	DNS              *string                  `json:"dns"`
	DNSTimeout       *string                  `json:"dns_timeout"`
	DNSFailThreshold *int                     `json:"dns_fail_threshold"`
	LogLevel         *string                  `json:"log_level"`
	SyncEnabled      *bool                    `json:"sync_enabled"`
	Theme            *string                  `json:"theme"`
}

type bundleV2WireEmail struct {
	Enabled  *bool   `json:"enabled"`
	Host     *string `json:"host"`
	Port     *string `json:"port"`
	Username *string `json:"username"`
	Password *string `json:"password"`
	FromAddr *string `json:"from_addr"`
	ToAddr   *string `json:"to_addr"`
}

type bundleV2WireWebhook struct {
	Enabled *bool   `json:"enabled"`
	URL     *string `json:"url"`
	Channel *string `json:"channel"`
}

type bundleV2WireAlerts struct {
	Email   *bundleV2WireEmail   `json:"email"`
	Webhook *bundleV2WireWebhook `json:"webhook"`
}

// bundleV2Wire 是导入使用的强类型 version 2 DTO。
//
// 必需 scalar 用指针、必需 object 用指针、必需 array 用 presenceSlice，
// 因此「字段缺失」「显式 null」与「合法零值/空数组」可以严格区分。
type bundleV2Wire struct {
	Version  *int                              `json:"version"`
	Metadata *bundleV2WireMetadata             `json:"metadata"`
	Targets  presenceSlice[bundleV2WireTarget] `json:"targets"`
	Rules    presenceSlice[bundleV2WireRule]   `json:"rules"`
	Settings *bundleV2WireSettings             `json:"settings"`
	Alerts   *bundleV2WireAlerts               `json:"alerts"`
}

// checkedBundleV2 是预校验通过、已归一化的配置包。
type checkedBundleV2 struct {
	ExportedAt time.Time
	Targets    []bundleV2Target
	Rules      []bundleV2Rule
	Settings   checkedBundleV2Settings
	Email      config.AlertEmailConfig
	Webhook    config.AlertWebhookConfig
}

type checkedBundleV2Settings struct {
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

// toBundleV2 把事务内取到的业务快照转换为 version 2 配置包。
//
// 目标与规则按数据库 ID 升序稳定输出；每条规则的 target_export_ids 按 export_id
// 升序输出（Build6 §12.11）；数组一律初始化为空切片，保证编码为 [] 而不是 null。
func toBundleV2(snapshot *config.BusinessSnapshot, exportedAt time.Time) bundleV2 {
	targets := make([]bundleV2Target, 0, len(snapshot.Targets))
	for _, t := range snapshot.Targets {
		targets = append(targets, bundleV2Target{
			ExportID:   t.ID,
			CloudType:  string(t.CloudType),
			Region:     t.Region,
			ResourceID: t.ResourceID,
		})
	}

	rules := make([]bundleV2Rule, 0, len(snapshot.Rules))
	for _, r := range snapshot.Rules {
		// 空切片而非 nil：保证 JSON 编码为 []，避免导出出现 null
		refs := make([]int, len(r.Targets))
		copy(refs, r.Targets)
		sortIntsAscending(refs)
		rules = append(rules, bundleV2Rule{
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
	email := snapshot.Email
	if email.Port == "" {
		email.Port = "587"
	}
	webhook := snapshot.Webhook
	if webhook.Channel == "" {
		webhook.Channel = "dingtalk"
	}

	return bundleV2{
		Version:  schemaVersionV2,
		Metadata: bundleV2Metadata{ExportedAt: exportedAt.UTC().Format(bundleV2TimeLayout)},
		Targets:  targets,
		Rules:    rules,
		Settings: bundleV2Settings{
			Credentials: bundleV2Credentials{
				Tencent: bundleV2TencentCredentials{
					SecretID:  snapshot.Settings["tc_access_id"],
					SecretKey: snapshot.Settings["tc_access_key"],
				},
				Aliyun: bundleV2AliyunCredentials{
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
		Alerts: bundleV2Alerts{
			Email: bundleV2Email{
				Enabled:  email.Enabled,
				Host:     email.Host,
				Port:     email.Port,
				Username: email.Username,
				Password: email.Password,
				FromAddr: email.FromAddr,
				ToAddr:   email.ToAddr,
			},
			Webhook: bundleV2Webhook{
				Enabled: webhook.Enabled,
				URL:     webhook.URL,
				Channel: webhook.Channel,
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
func validateAndNormalizeBundle(wire *bundleV2Wire) (*checkedBundleV2, error) {
	// version：只接受 2，缺失/null/其他一律 400，不做迁移或字段补全
	if wire.Version == nil {
		return nil, badRequest("version 字段缺失")
	}
	if *wire.Version != schemaVersionV2 {
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

	// targets：export_id 正数且全局唯一，cloud_type/region/resource_id 复用领域校验
	targets := make([]bundleV2Target, 0, len(wire.Targets.Values))
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
		targets = append(targets, bundleV2Target{
			ExportID:   *t.ExportID,
			CloudType:  string(normalized.CloudType),
			Region:     normalized.Region,
			ResourceID: normalized.ResourceID,
		})
	}

	// rules：字段 presence、数组元素正数与组内唯一、引用闭包
	rules := make([]bundleV2Rule, 0, len(wire.Rules.Values))
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
		rules = append(rules, bundleV2Rule{
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

	email, err := normalizeBundleEmail(wire.Alerts.Email)
	if err != nil {
		return nil, err
	}
	webhook, err := normalizeBundleWebhook(wire.Alerts.Webhook)
	if err != nil {
		return nil, err
	}

	return &checkedBundleV2{
		ExportedAt: exportedAt,
		Targets:    targets,
		Rules:      rules,
		Settings:   settings,
		Email:      email,
		Webhook:    webhook,
	}, nil
}

// normalizeBundleSettings 校验并归一化配置包设置（复用同一组领域校验函数）。
func normalizeBundleSettings(w *bundleV2WireSettings) (checkedBundleV2Settings, error) {
	var out checkedBundleV2Settings

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
func normalizeBundleEmail(w *bundleV2WireEmail) (config.AlertEmailConfig, error) {
	if w == nil {
		return config.AlertEmailConfig{}, badRequest("alerts.email 字段缺失")
	}
	for name, present := range map[string]bool{
		"enabled": w.Enabled != nil, "host": w.Host != nil, "port": w.Port != nil,
		"username": w.Username != nil, "password": w.Password != nil,
		"from_addr": w.FromAddr != nil, "to_addr": w.ToAddr != nil,
	} {
		if !present {
			return config.AlertEmailConfig{}, badRequest("alerts.email." + name + " 字段缺失")
		}
	}
	return config.NormalizeAlertEmail(config.AlertEmailConfig{
		Enabled:  *w.Enabled,
		Host:     *w.Host,
		Port:     *w.Port,
		Username: *w.Username,
		Password: *w.Password,
		FromAddr: *w.FromAddr,
		ToAddr:   *w.ToAddr,
	})
}

// normalizeBundleWebhook 校验并归一化配置包 Webhook 告警（禁用状态下字段仍必须存在）。
func normalizeBundleWebhook(w *bundleV2WireWebhook) (config.AlertWebhookConfig, error) {
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
// 必须逐键写入 version 2 的完整键集合（Build6 §12.12）：不遍历任意 map，
// 因此旧数据库的未知键不会进入新快照。
func bundleSettingsToStore(s checkedBundleV2Settings) map[string]string {
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
