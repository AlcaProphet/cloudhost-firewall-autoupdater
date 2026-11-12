package api

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// ─── version 4 导入 DTO（presence 型：缺失与 null 都能与合法零值区分） ───

type bundleV4WireSite struct {
	ZoneID          *string            `json:"zone_id"`
	TargetExportIDs presenceSlice[int] `json:"target_export_ids"`
	Protocol        *string            `json:"protocol"`
	Ports           *string            `json:"ports"`
	AddressFamily   *string            `json:"address_family"`
}

type bundleV4WireSettings struct {
	Credentials      *bundleV3WireCredentials `json:"credentials"`
	Tag              *string                  `json:"tag"`
	Interval         *string                  `json:"interval"`
	DNS              *string                  `json:"dns"`
	DNSTimeout       *string                  `json:"dns_timeout"`
	DNSFailThreshold *int                     `json:"dns_fail_threshold"`
	LogLevel         *string                  `json:"log_level"`
	SyncEnabled      *bool                    `json:"sync_enabled"`
	DNSEnabled       *bool                    `json:"dns_enabled"`
	EdgeOneEnabled   *bool                    `json:"edgeone_enabled"`
	EdgeOneTag       *string                  `json:"edgeone_tag"`
	EdgeOneInterval  *string                  `json:"edgeone_interval"`
	Theme            *string                  `json:"theme"`
}

type bundleV4Wire struct {
	Version      *int                                `json:"version"`
	Metadata     *bundleV3WireMetadata               `json:"metadata"`
	Targets      presenceSlice[*bundleV3WireTarget]  `json:"targets"`
	Rules        presenceSlice[*bundleV3WireRule]    `json:"rules"`
	EdgeOneSites presenceSlice[*bundleV4WireSite]    `json:"edgeone_sites"`
	Settings     *bundleV4WireSettings               `json:"settings"`
	Alerts       *bundleV3WireAlerts                 `json:"alerts"`
	Monitoring   *bundleV3WireMonitoring             `json:"monitoring"`
}

// checkBundleV4Presence 按 v4 DTO 声明顺序检查必填结构。
// version 由调用方先行检查；父对象缺失只报告父路径。
func checkBundleV4Presence(w *bundleV4Wire) error {
	e := &bundleV3PresenceError{}
	collectBundleV4(w, e)
	if e.Total > 0 {
		return e
	}
	return nil
}

func collectBundleV4Settings(w *bundleV4WireSettings, prefix string, e *bundleV3PresenceError) {
	if w.Credentials == nil {
		e.add(prefix+".credentials", false)
	} else {
		collectBundleV3Credentials(w.Credentials, prefix+".credentials", e)
	}
	e.add(prefix+".tag", w.Tag != nil)
	e.add(prefix+".interval", w.Interval != nil)
	e.add(prefix+".dns", w.DNS != nil)
	e.add(prefix+".dns_timeout", w.DNSTimeout != nil)
	e.add(prefix+".dns_fail_threshold", w.DNSFailThreshold != nil)
	e.add(prefix+".log_level", w.LogLevel != nil)
	e.add(prefix+".sync_enabled", w.SyncEnabled != nil)
	e.add(prefix+".dns_enabled", w.DNSEnabled != nil)
	e.add(prefix+".edgeone_enabled", w.EdgeOneEnabled != nil)
	e.add(prefix+".edgeone_tag", w.EdgeOneTag != nil)
	e.add(prefix+".edgeone_interval", w.EdgeOneInterval != nil)
	e.add(prefix+".theme", w.Theme != nil)
}

func collectBundleV4Site(w *bundleV4WireSite, prefix string, e *bundleV3PresenceError) {
	if w == nil {
		e.add(prefix, false)
		return
	}
	e.add(prefix+".zone_id", w.ZoneID != nil)
	e.add(prefix+".target_export_ids", w.TargetExportIDs.Provided)
	e.add(prefix+".protocol", w.Protocol != nil)
	e.add(prefix+".ports", w.Ports != nil)
	e.add(prefix+".address_family", w.AddressFamily != nil)
}

func collectBundleV4(w *bundleV4Wire, e *bundleV3PresenceError) {
	if w.Metadata == nil {
		e.add("metadata", false)
	} else {
		collectBundleV3Metadata(w.Metadata, "metadata", e)
	}
	e.add("targets", w.Targets.Provided)
	if w.Targets.Provided {
		for i := range w.Targets.Values {
			collectBundleV3Target(w.Targets.Values[i], fmt.Sprintf("targets[%d]", i), e)
		}
	}
	e.add("rules", w.Rules.Provided)
	if w.Rules.Provided {
		for i := range w.Rules.Values {
			collectBundleV3Rule(w.Rules.Values[i], fmt.Sprintf("rules[%d]", i), e)
		}
	}
	e.add("edgeone_sites", w.EdgeOneSites.Provided)
	if w.EdgeOneSites.Provided {
		for i := range w.EdgeOneSites.Values {
			collectBundleV4Site(w.EdgeOneSites.Values[i], fmt.Sprintf("edgeone_sites[%d]", i), e)
		}
	}
	if w.Settings == nil {
		e.add("settings", false)
	} else {
		collectBundleV4Settings(w.Settings, "settings", e)
	}
	if w.Alerts == nil {
		e.add("alerts", false)
	} else {
		collectBundleV3Alerts(w.Alerts, "alerts", e)
	}
	if w.Monitoring == nil {
		e.add("monitoring", false)
	} else {
		collectBundleV3Monitoring(w.Monitoring, "monitoring", e)
	}
}

// validateAndNormalizeBundleV4 在打开写事务之前完成 v4 的全部纯数据校验。
func validateAndNormalizeBundleV4(wire *bundleV4Wire) (*checkedBundle, error) {
	if wire.Version == nil {
		return nil, badRequest("version 字段缺失")
	}
	if *wire.Version != schemaVersionV4 {
		return nil, badRequest(fmt.Sprintf("不支持的配置版本: %d", *wire.Version))
	}
	if err := checkBundleV4Presence(wire); err != nil {
		return nil, err
	}

	exportedAt, err := time.Parse(time.RFC3339, *wire.Metadata.ExportedAt)
	if err != nil {
		return nil, badRequest("metadata.exported_at 必须是 RFC3339 时间")
	}

	targets := make([]bundleV3Target, 0, len(wire.Targets.Values))
	targetByExport := make(map[int]config.TargetConfig, len(wire.Targets.Values))
	seenExportID := make(map[int]bool, len(wire.Targets.Values))
	for i, t := range wire.Targets.Values {
		if *t.ExportID <= 0 {
			return nil, badRequest(fmt.Sprintf("targets[%d].export_id 必须是正数", i))
		}
		if seenExportID[*t.ExportID] {
			return nil, badRequest(fmt.Sprintf("targets[%d].export_id 重复: %d（不静默去重）", i, *t.ExportID))
		}
		seenExportID[*t.ExportID] = true
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
		targetByExport[*t.ExportID] = normalized
	}

	rules := make([]bundleV3Rule, 0, len(wire.Rules.Values))
	for i, r := range wire.Rules.Values {
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

	sites := make([]config.EdgeOneSite, 0, len(wire.EdgeOneSites.Values))
	seenZones := make(map[string]bool, len(wire.EdgeOneSites.Values))
	for i, s := range wire.EdgeOneSites.Values {
		refs, err := config.NormalizeTargetIDs(s.TargetExportIDs.Values)
		if err != nil {
			return nil, importFieldError("edgeone_sites", i, err)
		}
		if len(refs) == 0 {
			return nil, badRequest(fmt.Sprintf("edgeone_sites[%d].target_export_ids 至少需要一个目标", i))
		}
		seenResources := make(map[string]int, len(refs))
		for _, ref := range refs {
			target, ok := targetByExport[ref]
			if !ok {
				return nil, badRequest(fmt.Sprintf(
					"edgeone_sites[%d].target_export_ids 引用了本包不存在的 export_id: %d（不静默删除引用）", i, ref))
			}
			key := config.TargetIdentity(target)
			if previous, exists := seenResources[key]; exists {
				return nil, badRequest(fmt.Sprintf(
					"edgeone_sites[%d].target_export_ids 中 export_id %d 与 %d 指向同一实际资源", i, previous, ref))
			}
			seenResources[key] = ref
		}
		normalized, err := config.NormalizeEdgeOneSite(config.EdgeOneSite{
			ZoneID:        *s.ZoneID,
			TargetIDs:     refs,
			Protocol:      *s.Protocol,
			Ports:         *s.Ports,
			AddressFamily: *s.AddressFamily,
		})
		if err != nil {
			return nil, importFieldError("edgeone_sites", i, err)
		}
		if seenZones[normalized.ZoneID] {
			return nil, badRequest(fmt.Sprintf("edgeone_sites[%d].zone_id 重复: %s（不静默去重）", i, normalized.ZoneID))
		}
		seenZones[normalized.ZoneID] = true
		sites = append(sites, normalized)
	}

	settings, err := normalizeBundleV4Settings(wire.Settings)
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

	return &checkedBundle{
		ExportedAt: exportedAt,
		Targets:    targets,
		Rules:      rules,
		Sites:      sites,
		Settings:   settings,
		Policy:     policy,
		Email:      email,
		Webhook:    webhook,
		Push:       push,
	}, nil
}

// normalizeBundleV4Settings 校验并归一化 v4 设置；v4 不补旧配置默认 TAG，
// 两个 TAG 重名按显式校验失败返回。
func normalizeBundleV4Settings(w *bundleV4WireSettings) (checkedBundleSettings, error) {
	var out checkedBundleSettings
	out.Credentials = config.Credentials{
		TencentSecretID:       *w.Credentials.Tencent.SecretID,
		TencentSecretKey:      *w.Credentials.Tencent.SecretKey,
		AliyunAccessKeyID:     *w.Credentials.Aliyun.AccessKeyID,
		AliyunAccessKeySecret: *w.Credentials.Aliyun.AccessKeySecret,
	}
	tag, err := config.NormalizeTag(*w.Tag)
	if err != nil {
		return out, err
	}
	out.Tag = tag
	interval, _, err := config.ParsePositiveDuration("interval", *w.Interval)
	if err != nil {
		return out, err
	}
	out.Interval = interval
	dnsAddr, err := config.NormalizeDNSAddress(*w.DNS)
	if err != nil {
		return out, err
	}
	out.DNS = dnsAddr
	dnsTimeout, _, err := config.ParsePositiveDuration("dns_timeout", *w.DNSTimeout)
	if err != nil {
		return out, err
	}
	out.DNSTimeout = dnsTimeout
	threshold, _, err := config.NormalizeDNSFailThreshold(strconv.Itoa(*w.DNSFailThreshold))
	if err != nil {
		return out, err
	}
	out.DNSFailThreshold = threshold
	logLevel, err := config.NormalizeLogLevel(*w.LogLevel)
	if err != nil {
		return out, err
	}
	out.LogLevel = logLevel
	out.SyncEnabled = *w.SyncEnabled
	out.DNSEnabled = *w.DNSEnabled
	out.EdgeOneEnabled = *w.EdgeOneEnabled
	edgeOneTag, err := config.NormalizeEdgeOneTag(*w.EdgeOneTag)
	if err != nil {
		return out, err
	}
	out.EdgeOneTag = edgeOneTag
	edgeOneInterval, _, err := config.ParsePositiveDuration("edgeone_interval", *w.EdgeOneInterval)
	if err != nil {
		return out, err
	}
	out.EdgeOneInterval = edgeOneInterval
	theme, err := config.NormalizeTheme(*w.Theme)
	if err != nil {
		return out, err
	}
	out.Theme = theme

	if strings.TrimSpace(out.Tag) == strings.TrimSpace(out.EdgeOneTag) {
		return out, &config.ValidationError{
			Field:  "settings.edgeone_tag",
			Reason: "不能与 DNS TAG 相同",
		}
	}
	return out, nil
}
