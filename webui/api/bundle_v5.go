package api

import (
	"fmt"
	"sort"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// version 5 在 v4 基础上增加实例级 Lighthouse 策略压缩配置（Build9 Step 1）。
//
// 压缩段按物理实例身份保存，不引用 target export_id：多别名共享同一条配置，
// 悬空引用由“身份必须命中本包至少一个 target”统一校验。

type bundleV5Compression struct {
	CloudType      string `json:"cloud_type"`
	Region         string `json:"region"`
	ResourceID     string `json:"resource_id"`
	Enabled        bool   `json:"enabled"`
	ExtraIPv4Allow int64  `json:"extra_ipv4_allow"`
}

// bundleV5 是导出使用的完整配置包。字段集合 = v4 + lighthouse_compression。
type bundleV5 struct {
	Version               int                   `json:"version"`
	Metadata              bundleV3Metadata      `json:"metadata"`
	Targets               []bundleV3Target      `json:"targets"`
	Rules                 []bundleV3Rule        `json:"rules"`
	EdgeOneSites          []bundleV4Site        `json:"edgeone_sites"`
	LighthouseCompression []bundleV5Compression `json:"lighthouse_compression"`
	Settings              bundleV4Settings      `json:"settings"`
	Alerts                bundleV3Alerts        `json:"alerts"`
	Monitoring            bundleV3Monitoring    `json:"monitoring"`
}

// bundleV5WireCompression 是 v5 导入 DTO；所有字段用指针区分缺失/null 与合法零值。
type bundleV5WireCompression struct {
	CloudType      *string `json:"cloud_type"`
	Region         *string `json:"region"`
	ResourceID     *string `json:"resource_id"`
	Enabled        *bool   `json:"enabled"`
	ExtraIPv4Allow *int64  `json:"extra_ipv4_allow"`
}

type bundleV5Wire struct {
	Version               *int                                    `json:"version"`
	Metadata              *bundleV3WireMetadata                   `json:"metadata"`
	Targets               presenceSlice[*bundleV3WireTarget]      `json:"targets"`
	Rules                 presenceSlice[*bundleV3WireRule]        `json:"rules"`
	EdgeOneSites          presenceSlice[*bundleV4WireSite]        `json:"edgeone_sites"`
	LighthouseCompression presenceSlice[*bundleV5WireCompression] `json:"lighthouse_compression"`
	Settings              *bundleV4WireSettings                   `json:"settings"`
	Alerts                *bundleV3WireAlerts                     `json:"alerts"`
	Monitoring            *bundleV3WireMonitoring                 `json:"monitoring"`
}

// asV4Wire 复用 v4 的严格字段/引用校验；Version 只作为占位，不读取原 v5 值。
func (w *bundleV5Wire) asV4Wire() *bundleV4Wire {
	version := schemaVersionV4
	return &bundleV4Wire{
		Version:      &version,
		Metadata:     w.Metadata,
		Targets:      w.Targets,
		Rules:        w.Rules,
		EdgeOneSites: w.EdgeOneSites,
		Settings:     w.Settings,
		Alerts:       w.Alerts,
		Monitoring:   w.Monitoring,
	}
}

// checkBundleV5Presence 在 v4 必填结构之外要求 lighthouse_compression 数组存在。
func checkBundleV5Presence(w *bundleV5Wire) error {
	e := &bundleV3PresenceError{}
	collectBundleV4(w.asV4Wire(), e)
	e.add("lighthouse_compression", w.LighthouseCompression.Provided)
	if w.LighthouseCompression.Provided {
		for i, item := range w.LighthouseCompression.Values {
			prefix := fmt.Sprintf("lighthouse_compression[%d]", i)
			if item == nil {
				e.add(prefix, false)
				continue
			}
			e.add(prefix+".cloud_type", item.CloudType != nil)
			e.add(prefix+".region", item.Region != nil)
			e.add(prefix+".resource_id", item.ResourceID != nil)
			e.add(prefix+".enabled", item.Enabled != nil)
			e.add(prefix+".extra_ipv4_allow", item.ExtraIPv4Allow != nil)
		}
	}
	if e.Total > 0 {
		return e
	}
	return nil
}

// validateAndNormalizeBundleV5 先复用 v4 的全部目标/规则/站点/设置/告警校验，
// 再校验压缩段身份、平台、去重、引用闭包与额度。
func validateAndNormalizeBundleV5(wire *bundleV5Wire) (*checkedBundle, error) {
	if wire.Version == nil {
		return nil, badRequest("version 字段缺失")
	}
	if *wire.Version != schemaVersionV5 {
		return nil, badRequest(fmt.Sprintf("不支持的配置版本: %d", *wire.Version))
	}
	if err := checkBundleV5Presence(wire); err != nil {
		return nil, err
	}

	bundle, err := validateAndNormalizeBundleV4(wire.asV4Wire())
	if err != nil {
		return nil, err
	}

	identities := make(map[string]config.TargetConfig, len(bundle.Targets))
	for _, target := range bundle.Targets {
		identityTarget := config.TargetConfig{
			CloudType:  config.CloudType(target.CloudType),
			Region:     target.Region,
			ResourceID: target.ResourceID,
		}
		identities[config.TargetIdentity(identityTarget)] = identityTarget
	}

	entries := make([]config.LighthouseCompression, 0, len(wire.LighthouseCompression.Values))
	seen := make(map[string]int, len(wire.LighthouseCompression.Values))
	for i, item := range wire.LighthouseCompression.Values {
		cloudType := config.CloudType("")
		if item.CloudType != nil {
			cloudType = config.CloudType(*item.CloudType)
		}
		if cloudType != config.CloudTCLighthouse {
			return nil, badRequest(fmt.Sprintf(
				"lighthouse_compression[%d].cloud_type 只支持 tc_lighthouse", i))
		}
		if item.Region == nil || item.ResourceID == nil || item.Enabled == nil || item.ExtraIPv4Allow == nil {
			// 正常已由 presence 检查拦截；这里是防御内部接线错误。
			return nil, badRequest(fmt.Sprintf("lighthouse_compression[%d] 字段不完整", i))
		}
		normalized, nerr := config.NormalizeLighthouseCompression(config.LighthouseCompression{
			CloudType:      cloudType,
			Region:         *item.Region,
			ResourceID:     *item.ResourceID,
			Enabled:        *item.Enabled,
			ExtraIPv4Allow: *item.ExtraIPv4Allow,
		})
		if nerr != nil {
			return nil, importFieldError("lighthouse_compression", i, nerr)
		}
		identity := normalized.Identity()
		if previous, ok := seen[identity]; ok {
			return nil, badRequest(fmt.Sprintf(
				"lighthouse_compression[%d] 与 [%d] 指向同一物理实例（不静默去重）", previous, i))
		}
		seen[identity] = i
		if _, ok := identities[identity]; !ok {
			return nil, badRequest(fmt.Sprintf(
				"lighthouse_compression[%d] 引用了本包不存在的 Lighthouse 实例 %s/%s/%s",
				i, normalized.CloudType, normalized.Region, normalized.ResourceID))
		}
		entries = append(entries, normalized)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].CloudType != entries[j].CloudType {
			return entries[i].CloudType < entries[j].CloudType
		}
		if entries[i].Region != entries[j].Region {
			return entries[i].Region < entries[j].Region
		}
		return entries[i].ResourceID < entries[j].ResourceID
	})
	bundle.LighthouseCompression = entries
	return bundle, nil
}

// toBundleV5 基于 v4 导出构造 v5；压缩段只包含已保存的实例设置。
func toBundleV5(snapshot *config.BusinessSnapshot, exportedAt time.Time) bundleV5 {
	base := toBundleV4(snapshot, exportedAt)
	compression := make([]bundleV5Compression, 0, len(snapshot.LighthouseCompression))
	for _, item := range snapshot.LighthouseCompression {
		compression = append(compression, bundleV5Compression{
			CloudType:      string(item.CloudType),
			Region:         item.Region,
			ResourceID:     item.ResourceID,
			Enabled:        item.Enabled,
			ExtraIPv4Allow: item.ExtraIPv4Allow,
		})
	}
	return bundleV5{
		Version:               schemaVersionV5,
		Metadata:              base.Metadata,
		Targets:               base.Targets,
		Rules:                 base.Rules,
		EdgeOneSites:          base.EdgeOneSites,
		LighthouseCompression: compression,
		Settings:              base.Settings,
		Alerts:                base.Alerts,
		Monitoring:            base.Monitoring,
	}
}
