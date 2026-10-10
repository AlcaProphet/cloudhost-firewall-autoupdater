package api

import (
	"fmt"
	"strings"
)

// bundleV3MissingLimit 仅限制返回路径数，全部缺失项仍计入 Total 并拒绝导入。
const bundleV3MissingLimit = 100

// bundleV3PresenceError 只持有固定字段路径和索引，不保存请求值或原始错误。
type bundleV3PresenceError struct {
	Fields []string
	Total  int
}

func (e *bundleV3PresenceError) Error() string {
	if e.Total == 1 {
		suffix := " 字段缺失"
		p := e.Fields[0]
		if p == "targets" || p == "rules" || strings.HasSuffix(p, ".target_export_ids") {
			suffix = " 字段缺失或为 null"
		}
		return p + suffix
	}
	return fmt.Sprintf("配置包有 %d 项必填内容缺失或为 null", e.Total)
}

func (e *bundleV3PresenceError) add(path string, present bool) {
	if present {
		return
	}
	e.Total++
	if len(e.Fields) < bundleV3MissingLimit {
		e.Fields = append(e.Fields, path)
	}
}

// checkBundleV3Presence 按 DTO 声明顺序检查；父对象缺失只报告父路径。
// version 由调用方先行检查，领域归一化只能在此入口成功后执行。

func checkBundleV3Presence(w *bundleV3Wire) error {
	e := &bundleV3PresenceError{}
	collectBundleV3(w, e)
	if e.Total > 0 {
		return e
	}
	return nil
}

func collectBundleV3Metadata(w *bundleV3WireMetadata, prefix string, e *bundleV3PresenceError) {
	e.add(prefix+".exported_at", w.ExportedAt != nil)
}

func collectBundleV3Target(w *bundleV3WireTarget, prefix string, e *bundleV3PresenceError) {
	if w == nil {
		e.add(prefix, false)
		return
	}
	e.add(prefix+".export_id", w.ExportID != nil)
	e.add(prefix+".cloud_type", w.CloudType != nil)
	e.add(prefix+".region", w.Region != nil)
	e.add(prefix+".resource_id", w.ResourceID != nil)
}

func collectBundleV3Rule(w *bundleV3WireRule, prefix string, e *bundleV3PresenceError) {
	if w == nil {
		e.add(prefix, false)
		return
	}
	e.add(prefix+".host", w.Host != nil)
	e.add(prefix+".protocol", w.Protocol != nil)
	e.add(prefix+".ports", w.Ports != nil)
	e.add(prefix+".action", w.Action != nil)
	e.add(prefix+".target_export_ids", w.TargetExportIDs.Provided)
	e.add(prefix+".comment", w.Comment != nil)
	e.add(prefix+".enable_ipv6", w.EnableIPv6 != nil)
}

func collectBundleV3Tencent(w *bundleV3WireTencent, prefix string, e *bundleV3PresenceError) {
	e.add(prefix+".secret_id", w.SecretID != nil)
	e.add(prefix+".secret_key", w.SecretKey != nil)
}

func collectBundleV3Aliyun(w *bundleV3WireAliyun, prefix string, e *bundleV3PresenceError) {
	e.add(prefix+".access_key_id", w.AccessKeyID != nil)
	e.add(prefix+".access_key_secret", w.AccessKeySecret != nil)
}

func collectBundleV3Credentials(w *bundleV3WireCredentials, prefix string, e *bundleV3PresenceError) {
	if w.Tencent == nil {
		e.add(prefix+".tencent", false)
	} else {
		collectBundleV3Tencent(w.Tencent, prefix+".tencent", e)
	}
	if w.Aliyun == nil {
		e.add(prefix+".aliyun", false)
	} else {
		collectBundleV3Aliyun(w.Aliyun, prefix+".aliyun", e)
	}
}

func collectBundleV3Settings(w *bundleV3WireSettings, prefix string, e *bundleV3PresenceError) {
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
	e.add(prefix+".theme", w.Theme != nil)
}

func collectBundleV3Email(w *bundleV3WireEmail, prefix string, e *bundleV3PresenceError) {
	e.add(prefix+".enabled", w.Enabled != nil)
	e.add(prefix+".host", w.Host != nil)
	e.add(prefix+".port", w.Port != nil)
	e.add(prefix+".security", w.Security != nil)
	e.add(prefix+".username", w.Username != nil)
	e.add(prefix+".password", w.Password != nil)
	e.add(prefix+".from_addr", w.FromAddr != nil)
	e.add(prefix+".to_addr", w.ToAddr != nil)
	e.add(prefix+".subject", w.Subject != nil)
	e.add(prefix+".body", w.Body != nil)
}

func collectBundleV3Policy(w *bundleV3WirePolicy, prefix string, e *bundleV3PresenceError) {
	e.add(prefix+".dns_failed_enabled", w.DNSFailedEnabled != nil)
	e.add(prefix+".sync_error_enabled", w.SyncErrorEnabled != nil)
	e.add(prefix+".operational_error_enabled", w.OperationalErrorEnabled != nil)
	e.add(prefix+".health_timeout", w.HealthTimeout != nil)
}

func collectBundleV3Push(w *bundleV3WirePush, prefix string, e *bundleV3PresenceError) {
	e.add(prefix+".enabled", w.Enabled != nil)
	e.add(prefix+".url", w.URL != nil)
	e.add(prefix+".interval", w.Interval != nil)
}

func collectBundleV3Monitoring(w *bundleV3WireMonitoring, prefix string, e *bundleV3PresenceError) {
	if w.UptimeKumaPush == nil {
		e.add(prefix+".uptime_kuma_push", false)
	} else {
		collectBundleV3Push(w.UptimeKumaPush, prefix+".uptime_kuma_push", e)
	}
}

func collectBundleV3Webhook(w *bundleV3WireWebhook, prefix string, e *bundleV3PresenceError) {
	e.add(prefix+".enabled", w.Enabled != nil)
	e.add(prefix+".url", w.URL != nil)
	e.add(prefix+".channel", w.Channel != nil)
}

func collectBundleV3Alerts(w *bundleV3WireAlerts, prefix string, e *bundleV3PresenceError) {
	if w.Policy == nil {
		e.add(prefix+".policy", false)
	} else {
		collectBundleV3Policy(w.Policy, prefix+".policy", e)
	}
	if w.Email == nil {
		e.add(prefix+".email", false)
	} else {
		collectBundleV3Email(w.Email, prefix+".email", e)
	}
	if w.Webhook == nil {
		e.add(prefix+".webhook", false)
	} else {
		collectBundleV3Webhook(w.Webhook, prefix+".webhook", e)
	}
}

func collectBundleV3(w *bundleV3Wire, e *bundleV3PresenceError) {
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
	if w.Settings == nil {
		e.add("settings", false)
	} else {
		collectBundleV3Settings(w.Settings, "settings", e)
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
