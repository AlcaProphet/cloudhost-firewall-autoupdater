package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// version2FilenamePattern 固定附件文件名：fwalizer-config-v2-<UTC 20060102T150405Z>.json
var version2FilenamePattern = regexp.MustCompile(`^attachment; filename="fwalizer-config-v2-\d{8}T\d{6}Z\.json"$`)

// TestConfigExportEmptyDatabaseSchema 空数据库导出仍必须是完整合法 Schema：
// 数组为 [] 而不是 null、四个凭据字段存在但为空、告警对象与全部字段存在、默认值齐全。
func TestConfigExportEmptyDatabaseSchema(t *testing.T) {
	e := newTestEnv(t)

	w := e.do(t, http.MethodPost, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// 必须使用缩进格式并以换行结束
	body := w.Body.String()
	if !strings.Contains(body, "\n  \"version\": 2") {
		t.Errorf("导出应为缩进 JSON: %s", body)
	}
	if !strings.HasSuffix(body, "}\n") {
		t.Errorf("导出应以换行结束: %q", body[len(body)-3:])
	}

	// 直接用原始 JSON 断言「数组不是 null」，避免 struct 解码掩盖 null
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("解析导出失败: %v", err)
	}
	if string(raw["targets"]) != "[]" {
		t.Errorf("targets 必须是 []，实际 %s", raw["targets"])
	}
	if string(raw["rules"]) != "[]" {
		t.Errorf("rules 必须是 []，实际 %s", raw["rules"])
	}

	var got bundleV2
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf(" struct 解码失败: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("version = %d, want 2", got.Version)
	}
	if got.Targets == nil || got.Rules == nil {
		t.Errorf("targets/rules 不得为 nil: %+v", got)
	}
	if got.Settings.Credentials.Tencent.SecretID != "" || got.Settings.Credentials.Aliyun.AccessKeySecret != "" {
		t.Errorf("空库凭据必须为空字符串: %+v", got.Settings.Credentials)
	}
	if got.Settings.Tag != "auto-dns" || got.Settings.Interval != "5m" || got.Settings.DNS != "223.5.5.5" ||
		got.Settings.DNSTimeout != "10s" || got.Settings.DNSFailThreshold != 5 ||
		got.Settings.LogLevel != "info" || !got.Settings.SyncEnabled || got.Settings.Theme != "light" {
		t.Errorf("空库导出未补齐默认值: %+v", got.Settings)
	}
	if got.Alerts.Email.Enabled || got.Alerts.Email.Port != "587" {
		t.Errorf("空库邮件告警字段不完整: %+v", got.Alerts.Email)
	}
	if got.Alerts.Webhook.Enabled || got.Alerts.Webhook.Channel != "dingtalk" {
		t.Errorf("空库 Webhook 告警字段不完整: %+v", got.Alerts.Webhook)
	}
}

// TestConfigExportHeaders 导出响应头固定：Content-Type、附件文件名、no-store。
func TestConfigExportHeaders(t *testing.T) {
	e := newTestEnv(t)

	w := e.do(t, http.MethodPost, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	cd := w.Header().Get("Content-Disposition")
	if !version2FilenamePattern.MatchString(cd) {
		t.Errorf("Content-Disposition = %q, 不符合固定文件名格式", cd)
	}
}

// TestConfigExportMetadataUTC exported_at 必须是 UTC RFC3339 且以 Z 结尾。
func TestConfigExportMetadataUTC(t *testing.T) {
	e := newTestEnv(t)

	w := e.do(t, http.MethodPost, "/api/config/export", "")
	var got bundleV2
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !strings.HasSuffix(got.Metadata.ExportedAt, "Z") {
		t.Fatalf("exported_at 必须以 Z 结尾: %q", got.Metadata.ExportedAt)
	}
	parsed, err := time.Parse(time.RFC3339, got.Metadata.ExportedAt)
	if err != nil {
		t.Fatalf("exported_at 不是 RFC3339: %v", err)
	}
	if parsed.Location() != time.UTC {
		t.Errorf("exported_at 必须是 UTC: %v", parsed.Location())
	}
	if diff := time.Since(parsed); diff > time.Minute || diff < -time.Minute {
		t.Errorf("exported_at 与当前时间偏差过大: %v", diff)
	}

	// 文件名时间必须来自同一导出时间
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, parsed.Format(bundleV2FileTimeLayout)) {
		t.Errorf("文件名时间 %q 与 metadata 时间不一致", cd)
	}
}

// TestConfigExportStableOrdering 目标与规则按数据库 ID 升序，target_export_ids 按 export_id 升序。
func TestConfigExportStableOrdering(t *testing.T) {
	e := newTestEnv(t)

	// 制造自增历史：先插入再删除，使后续 ID 不从 1 开始
	e.seedTargetCount(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-1", 3)
	deleteTargetForTest(t, e.store, 2)
	e.seedTarget(t, config.CloudTCCVM, "ap-beijing", "sg-tc")
	e.seedTarget(t, config.CloudAliECS, "cn-hangzhou", "sg-ali")

	targets, err := e.store.GetTargets()
	if err != nil {
		t.Fatalf("GetTargets 失败: %v", err)
	}
	if len(targets) != 4 {
		t.Fatalf("目标数量 = %d, want 4", len(targets))
	}

	// 规则显式引用倒序的 export_id，导出必须升序输出
	refs := []int{targets[2].ID, targets[0].ID}
	e.seedRule(t, config.DomainRule{Host: "z.example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: refs})
	e.seedRule(t, config.DomainRule{Host: "a.example.com", Protocol: "UDP", Ports: "53", Action: "DROP"})

	w := e.do(t, http.MethodPost, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", w.Code)
	}
	var got bundleV2
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	for i := 1; i < len(got.Targets); i++ {
		if got.Targets[i-1].ExportID >= got.Targets[i].ExportID {
			t.Errorf("targets 未按 export_id 升序: %+v", got.Targets)
			break
		}
	}
	if len(got.Rules) != 2 {
		t.Fatalf("规则数量 = %d, want 2", len(got.Rules))
	}
	gotRefs := got.Rules[0].TargetExportIDs
	if len(gotRefs) != 2 || gotRefs[0] != targets[0].ID || gotRefs[1] != targets[2].ID {
		t.Errorf("target_export_ids 必须按 export_id 升序: %v, want [%d %d]", gotRefs, targets[0].ID, targets[2].ID)
	}
	if got.Rules[0].Host != "z.example.com" || got.Rules[1].Host != "a.example.com" {
		t.Errorf("规则未按数据库 ID 升序: %+v", got.Rules)
	}
}

// TestConfigExportOnlyPostRoute 旧 GET 导出路由必须已删除。
func TestConfigExportOnlyPostRoute(t *testing.T) {
	e := newTestEnv(t)
	if w := e.do(t, http.MethodGet, "/api/config/export", ""); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/config/export 状态码 = %d, want 405（旧端点应删除）", w.Code)
	}
}

// TestConfigExportThenImportRoundTrip 同实例「导出 → 清空 → 导入」后规则仍关联同一业务目标。
//
// 这是 R5-01 的核心回归：不比较数字 ID，而是比较导入后规则引用解析出的目标业务字段。
func TestConfigExportThenImportRoundTrip(t *testing.T) {
	e := newTestEnv(t)

	// 制造自增历史，使导入后的新 ID 必然不同于导出时的 ID
	e.seedTargetCount(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-1", 4)
	deleteTargetForTest(t, e.store, 2)
	e.seedTarget(t, config.CloudTCCVM, "ap-beijing", "sg-tc")
	targetsBefore, err := e.store.GetTargets()
	if err != nil {
		t.Fatalf("GetTargets 失败: %v", err)
	}
	// 一条规则引用多个目标 + 一条规则引用单目标 + 一条规则适用全部目标
	e.seedRule(t, config.DomainRule{
		Host: "multi.example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
		Targets: []int{targetsBefore[0].ID, targetsBefore[2].ID}, Comment: "多目标",
	})
	e.seedRule(t, config.DomainRule{
		Host: "single.example.com", Protocol: "UDP", Ports: "53", Action: "DROP",
		Targets: []int{targetsBefore[1].ID}, Comment: "单目标",
	})
	e.seedRule(t, config.DomainRule{Host: "all.example.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT"})
	if err := e.store.SetSetting("theme", "dark"); err != nil {
		t.Fatalf("预置主题失败: %v", err)
	}
	if err := e.store.SetSetting("sync_enabled", "false"); err != nil {
		t.Fatalf("预置同步开关失败: %v", err)
	}

	w := e.do(t, http.MethodPost, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("导出状态码 = %d, want 200", w.Code)
	}
	exported := w.Body.String()

	// 清空全部数据（等效重新初始化），确认目标 ID 与导出时不同
	resetAllForTest(t, e.store)
	// 再写入若干目标，进一步把自增序列推离导出时的 ID
	for i := 0; i < 5; i++ {
		e.seedTarget(t, config.CloudAliSWAS, "cn-shanghai", "swas-x")
	}
	resetAllForTest(t, e.store)

	w = e.do(t, http.MethodPost, "/api/config/import", exported)
	if w.Code != http.StatusOK {
		t.Fatalf("导入状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	targetsAfter, err := e.store.GetTargets()
	if err != nil {
		t.Fatalf("GetTargets 失败: %v", err)
	}
	if len(targetsAfter) != len(targetsBefore) {
		t.Fatalf("目标数量 = %d, want %d", len(targetsAfter), len(targetsBefore))
	}
	// 映射必须按插入顺序一一对应，但数据库 ID 与导出时不同
	byExport := make(map[int]config.TargetConfig, len(targetsBefore))
	for i, before := range targetsBefore {
		byExport[i] = before
		if targetsAfter[i].ID == before.ID {
			t.Errorf("第 %d 个目标复用了来源数据库 ID（不应强行复用）: %+v", i, targetsAfter[i])
		}
		if targetsAfter[i].CloudType != before.CloudType || targetsAfter[i].Region != before.Region || targetsAfter[i].ResourceID != before.ResourceID {
			t.Errorf("第 %d 个目标业务字段不一致: %+v vs %+v", i, targetsAfter[i], before)
		}
	}

	rulesAfter, err := e.store.GetRules()
	if err != nil {
		t.Fatalf("GetRules 失败: %v", err)
	}
	if len(rulesAfter) != 3 {
		t.Fatalf("规则数量 = %d, want 3", len(rulesAfter))
	}
	ruleByHost := make(map[string]config.DomainRule, len(rulesAfter))
	for _, r := range rulesAfter {
		ruleByHost[r.Host] = r
	}

	// 多目标规则必须关联到与导出前相同的业务目标
	assertSameTargets := func(host string, want []config.TargetConfig) {
		t.Helper()
		rule, ok := ruleByHost[host]
		if !ok {
			t.Fatalf("缺少规则 %s: %+v", host, rulesAfter)
		}
		if len(rule.Targets) != len(want) {
			t.Fatalf("规则 %s 引用数量 = %d, want %d（%v）", host, len(rule.Targets), len(want), rule.Targets)
		}
		for i, ref := range rule.Targets {
			var found *config.TargetConfig
			for _, candidate := range targetsAfter {
				if candidate.ID == ref {
					cp := candidate
					found = &cp
					break
				}
			}
			if found == nil {
				t.Fatalf("规则 %s 引用了不存在的目标 #%d", host, ref)
			}
			if found.ResourceID != want[i].ResourceID || found.Region != want[i].Region {
				t.Errorf("规则 %s 第 %d 个引用指向错误目标: %+v, want %+v", host, i, *found, want[i])
			}
		}
	}
	assertSameTargets("multi.example.com", []config.TargetConfig{byExport[0], byExport[2]})
	assertSameTargets("single.example.com", []config.TargetConfig{byExport[1]})
	if rule := ruleByHost["all.example.com"]; len(rule.Targets) != 0 {
		t.Errorf("空引用必须保持「适用于全部目标」: %+v", rule)
	}

	// 主题与同步开关也必须恢复
	settings, _ := e.store.GetSettings()
	if settings["theme"] != "dark" || settings["sync_enabled"] != "false" {
		t.Errorf("设置未按配置包恢复: %+v", settings)
	}
}

// TestConfigImportCrossInstanceDifferentIDs 跨实例导入：来源 ID 与目标库当前 ID 完全不同。
func TestConfigImportCrossInstanceDifferentIDs(t *testing.T) {
	source := newTestEnv(t)
	source.seedTargetCount(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-src", 6)
	srcTargets, err := source.store.GetTargets()
	if err != nil {
		t.Fatalf("GetTargets 失败: %v", err)
	}
	source.seedRule(t, config.DomainRule{
		Host: "cross.example.com", Protocol: "TCP", Ports: "8443", Action: "ACCEPT",
		Targets: []int{srcTargets[5].ID}, Comment: "跨实例",
	})
	w := source.do(t, http.MethodPost, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("导出状态码 = %d", w.Code)
	}
	exported := w.Body.String()

	// 目标实例：只有 1 条自增历史，ID 空间与来源完全不同
	dest := newTestEnv(t)
	dest.seedTarget(t, config.CloudTCCVM, "ap-beijing", "sg-dest")
	destTargets, _ := dest.store.GetTargets()

	w = dest.do(t, http.MethodPost, "/api/config/import", exported)
	if w.Code != http.StatusOK {
		t.Fatalf("导入状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	rules, err := dest.store.GetRules()
	if err != nil || len(rules) != 1 {
		t.Fatalf("GetRules = %+v, err=%v", rules, err)
	}
	if len(rules[0].Targets) != 1 {
		t.Fatalf("规则引用数量 = %d, want 1", len(rules[0].Targets))
	}
	newTargets, _ := dest.store.GetTargets()
	if len(newTargets) != 6 {
		t.Fatalf("目标数量 = %d, want 6", len(newTargets))
	}
	var resolved *config.TargetConfig
	for _, tr := range newTargets {
		if tr.ID == rules[0].Targets[0] {
			cp := tr
			resolved = &cp
		}
	}
	if resolved == nil {
		t.Fatalf("规则引用未解析到目标: %v", rules[0].Targets)
	}
	if resolved.ResourceID != "lhins-src" {
		t.Errorf("跨实例导入后规则未关联来源业务目标: %+v", *resolved)
	}
	// 目标实例原有目标必须被覆盖式替换
	for _, tr := range destTargets {
		for _, now := range newTargets {
			if now.ID == tr.ID && now.ResourceID == "sg-dest" {
				t.Errorf("旧目标未被覆盖式替换: %+v", now)
			}
		}
	}
}

// TestConfigImportClearsScannedKeepsSyncLogsAndSequence 导入清空扫描缓存、保留同步日志、不重置自增序列。
func TestConfigImportClearsScannedKeepsSyncLogsAndSequence(t *testing.T) {
	e := newTestEnv(t)
	// 自增历史：插入 3 个目标后删掉 2 个
	lastID := e.seedTargetCount(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-1", 3)
	deleteTargetForTest(t, e.store, 1)
	deleteTargetForTest(t, e.store, 2)

	if err := e.store.ReplaceScannedResources("tc_lighthouse", "ap-guangzhou", []config.ScannedResource{
		{CloudType: "tc_lighthouse", Region: "ap-guangzhou", ResourceID: "lhins-scan", ResourceName: "扫描"},
	}); err != nil {
		t.Fatalf("预置扫描结果失败: %v", err)
	}
	if err := e.store.AddSyncLog(config.SyncLog{Timestamp: time.Now(), Target: "t", Domain: "d", Result: "success", Added: 1}); err != nil {
		t.Fatalf("预置同步日志失败: %v", err)
	}

	w := e.do(t, http.MethodPost, "/api/config/import", validBundle())
	if w.Code != http.StatusOK {
		t.Fatalf("导入状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	if scanned, _ := e.store.GetScannedResources("tc_lighthouse"); len(scanned) != 0 {
		t.Errorf("导入成功必须清空 scanned_resources: %+v", scanned)
	}
	if logs, _ := e.store.GetSyncLogs(10); len(logs) != 1 {
		t.Errorf("导入必须保留 sync_logs: %+v", logs)
	}

	// 自增序列不得重置：新插入目标的 ID 必须大于导入前的最大 ID
	newID := e.seedTarget(t, config.CloudAliECS, "cn-hangzhou", "sg-after")
	if newID <= lastID {
		t.Errorf("sqlite_sequence 被重置: 新 ID = %d, 导入前最大 ID = %d", newID, lastID)
	}
}

// TestConfigImportAlertsRoundTrip 完整告警（含 SMTP 密码与 Webhook URL）必须被覆盖保存。
func TestConfigImportAlertsRoundTrip(t *testing.T) {
	e := newTestEnv(t)
	if err := e.store.SaveAlertEmail(&config.AlertEmailConfig{Enabled: true, Host: "old-smtp", Port: "25"}); err != nil {
		t.Fatalf("预置告警失败: %v", err)
	}

	body := `{"version":2,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` +
		validBundleSettings() + `,` +
		`"alerts":{"email":{"enabled":true,"host":"smtp.new","port":"587","username":"u","password":"pw-secret",` +
		`"from_addr":"f@example.com","to_addr":"t@example.com"},"webhook":{"enabled":true,` +
		`"url":"https://hook.example.com/abc","channel":"feishu"}}}`
	w := e.do(t, http.MethodPost, "/api/config/import", body)
	if w.Code != http.StatusOK {
		t.Fatalf("导入状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	email, _ := e.store.GetAlertEmail()
	if !email.Enabled || email.Host != "smtp.new" || email.Password != "pw-secret" {
		t.Errorf("邮件告警未完整覆盖: %+v", email)
	}
	webhook, _ := e.store.GetAlertWebhook()
	if !webhook.Enabled || webhook.URL != "https://hook.example.com/abc" || webhook.Channel != "feishu" {
		t.Errorf("Webhook 告警未完整覆盖: %+v", webhook)
	}
	// 运行时状态必须已经看到新告警集合
	if current := e.alerts.Current(); current.email == nil || current.webhook == nil {
		t.Errorf("运行时告警集合未更新: %+v", current)
	}
}

// TestConfigImportSharedTargetAcrossRules 多条规则引用同一目标时，
// v2 导入的 export_id → 新数据库 ID 映射必须让这些规则仍指向同一个新目标，
// 且该目标是本次导入重建的业务目标（Issue5 R5-01「多规则复用同一目标」回归）。
func TestConfigImportSharedTargetAcrossRules(t *testing.T) {
	e := newTestEnv(t)

	// 制造自增历史：插入 3 个目标后删掉 2 个，使导入分配的新 ID 必然不同于导出时的 ID
	e.seedTargetCount(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-shared", 3)
	deleteTargetForTest(t, e.store, 1)
	deleteTargetForTest(t, e.store, 2)
	targetsBefore, err := e.store.GetTargets()
	if err != nil {
		t.Fatalf("GetTargets 失败: %v", err)
	}
	if len(targetsBefore) != 1 {
		t.Fatalf("自增历史夹具目标数 = %d, want 1", len(targetsBefore))
	}
	sharedBefore := targetsBefore[0].ID

	// 两条规则引用同一个目标
	e.seedRule(t, config.DomainRule{
		Host: "a.example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
		Targets: []int{sharedBefore}, Comment: "共享目标 A",
	})
	e.seedRule(t, config.DomainRule{
		Host: "b.example.com", Protocol: "UDP", Ports: "53", Action: "DROP",
		Targets: []int{sharedBefore}, Comment: "共享目标 B",
	})

	w := e.do(t, http.MethodPost, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("导出状态码 = %d, want 200", w.Code)
	}
	exported := w.Body.String()

	// 清空并把自增序列推离导出时的 ID
	resetAllForTest(t, e.store)
	e.seedTargetCount(t, config.CloudAliSWAS, "cn-shanghai", "swas-x", 4)
	resetAllForTest(t, e.store)

	w = e.do(t, http.MethodPost, "/api/config/import", exported)
	if w.Code != http.StatusOK {
		t.Fatalf("导入状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	rules, err := e.store.GetRules()
	if err != nil {
		t.Fatalf("GetRules 失败: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("导入后规则数 = %d, want 2", len(rules))
	}
	refsByHost := make(map[string][]int, len(rules))
	for _, r := range rules {
		refsByHost[r.Host] = r.Targets
	}
	refsA, okA := refsByHost["a.example.com"]
	refsB, okB := refsByHost["b.example.com"]
	if !okA || !okB {
		t.Fatalf("导入后规则主机名缺失: %+v", refsByHost)
	}
	if len(refsA) != 1 || len(refsB) != 1 {
		t.Fatalf("两条规则都应恰好引用一个目标: a=%v b=%v", refsA, refsB)
	}
	if refsA[0] != refsB[0] {
		t.Errorf("多规则复用同一目标必须映射到同一个新 ID: a=%d b=%d", refsA[0], refsB[0])
	}
	if refsA[0] == sharedBefore {
		t.Errorf("新 ID 应不同于导出时的 ID（自增历史已制造）: %d", refsA[0])
	}

	targetsAfter, err := e.store.GetTargets()
	if err != nil {
		t.Fatalf("导入后 GetTargets 失败: %v", err)
	}
	if len(targetsAfter) != 1 {
		t.Fatalf("导入后目标数 = %d, want 1", len(targetsAfter))
	}
	if targetsAfter[0].ID != refsA[0] || targetsAfter[0].ResourceID != "lhins-shared" {
		t.Errorf("规则必须指向本次导入重建的业务目标: target=%+v refs=%d", targetsAfter[0], refsA[0])
	}
}
