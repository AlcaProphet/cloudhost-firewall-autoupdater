package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// 本文件是 Build8 Step 4 的 API 回归：v4 导出形状、v4 导入/往返、
// v3 兼容、站点绑定 export_id 重映射、严格负向用例与事务回滚。

func v4SettingsJSON(tag, interval, edgeOneTag, edgeOneInterval string, dnsEnabled, edgeOneEnabled, syncEnabled bool) string {
	return `{"credentials":{"tencent":{"secret_id":"","secret_key":""},` +
		`"aliyun":{"access_key_id":"","access_key_secret":""}},` +
		`"tag":` + jsonQuote(tag) + `,"interval":` + jsonQuote(interval) +
		`,"dns":"223.5.5.5","dns_timeout":"10s","dns_fail_threshold":5,"log_level":"info",` +
		`"sync_enabled":` + boolJSON(syncEnabled) + `,"dns_enabled":` + boolJSON(dnsEnabled) +
		`,"edgeone_enabled":` + boolJSON(edgeOneEnabled) + `,"edgeone_tag":` + jsonQuote(edgeOneTag) +
		`,"edgeone_interval":` + jsonQuote(edgeOneInterval) + `,"theme":"light"}`
}

func v4Bundle(targets, rules, sites, settings string) string {
	return `{"version":4,"metadata":{"exported_at":"2026-11-12T00:00:00Z"},` +
		`"targets":` + targets + `,"rules":` + rules + `,"edgeone_sites":` + sites + `,` +
		`"settings":` + settings + `,` + validBundleAlerts() + `,` + validBundleMonitoring() + `}`
}

func defaultV4Settings() string {
	return v4SettingsJSON(config.DefaultDNSTag, "5m", config.DefaultEdgeOneTag, config.DefaultEdgeOneIntervalText, true, false, true)
}

func validV4Bundle() string {
	return v4Bundle(`[]`, `[]`, `[]`, defaultV4Settings())
}

func jsonQuote(v string) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func boolJSON(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func TestExportV4ShapeAndNoRuntimeState(t *testing.T) {
	e := newTestEnv(t)
	a := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-a")
	b := e.seedTarget(t, config.CloudAliECS, "cn-hangzhou", "sg-b")
	w := e.do(t, http.MethodPost, "/api/edgeone/sites",
		edgeOneSiteBody("zone-export", []int{b, a}, "TCP", "80,443", "ipv4"))
	if w.Code != http.StatusCreated {
		t.Fatalf("创建站点状态码 = %d; body=%s", w.Code, w.Body.String())
	}
	w = e.do(t, http.MethodPut, "/api/settings",
		`{"tag":"dns-custom","edgeone_tag":"edge-custom","dns_enabled":"false",`+
			`"edgeone_enabled":"true","edgeone_interval":"36h"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("保存设置状态码 = %d; body=%s", w.Code, w.Body.String())
	}

	w = e.do(t, http.MethodPost, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("导出状态码 = %d; body=%s", w.Code, w.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("解析导出失败: %v", err)
	}
	for _, key := range []string{"version", "metadata", "targets", "rules", "edgeone_sites", "settings", "alerts", "monitoring"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("v4 导出缺少顶层字段 %s", key)
		}
	}
	for _, key := range []string{"sync_address_conflict", "edgeone_origin_transitions", "sync_logs", "scanned_resources"} {
		if _, ok := raw[key]; ok {
			t.Errorf("运行状态 %s 不得进入配置包", key)
		}
	}
	var version int
	if err := json.Unmarshal(raw["version"], &version); err != nil || version != 4 {
		t.Fatalf("version = %d err=%v, want 4", version, err)
	}

	var settingsRaw map[string]json.RawMessage
	if err := json.Unmarshal(raw["settings"], &settingsRaw); err != nil {
		t.Fatalf("解析 settings 失败: %v", err)
	}
	if string(settingsRaw["dns_enabled"]) != "false" || string(settingsRaw["edgeone_enabled"]) != "true" {
		t.Errorf("v4 开关必须是 JSON boolean: dns=%s edgeone=%s",
			settingsRaw["dns_enabled"], settingsRaw["edgeone_enabled"])
	}

	var sites []bundleV4Site
	if err := json.Unmarshal(raw["edgeone_sites"], &sites); err != nil {
		t.Fatalf("解析 edgeone_sites 失败: %v", err)
	}
	if len(sites) != 1 || sites[0].ZoneID != "zone-export" {
		t.Fatalf("v4 站点导出不正确: %+v", sites)
	}
	if !reflect.DeepEqual(sites[0].TargetExportIDs, []int{a, b}) {
		t.Errorf("target_export_ids 必须按 export_id 升序: %v, want [%d %d]", sites[0].TargetExportIDs, a, b)
	}
	// 站点对象不得把本地数据库 ID 或运行字段带出。
	if strings.Contains(string(raw["edgeone_sites"]), `"id"`) {
		t.Errorf("站点导出不得包含数据库 id: %s", raw["edgeone_sites"])
	}
}

func TestImportV4RoundTripWithSitesAndSettings(t *testing.T) {
	source := newTestEnv(t)
	a := source.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-src-a")
	b := source.seedTarget(t, config.CloudAliECS, "cn-hangzhou", "sg-src-b")
	w := source.do(t, http.MethodPost, "/api/edgeone/sites",
		edgeOneSiteBody("zone-roundtrip", []int{a, b}, "TCP", "8443", "dual"))
	if w.Code != http.StatusCreated {
		t.Fatalf("来源创建站点状态码 = %d; body=%s", w.Code, w.Body.String())
	}
	w = source.do(t, http.MethodPut, "/api/settings",
		`{"tag":"dns-custom","edgeone_tag":"edge-custom","dns_enabled":"false",`+
			`"edgeone_enabled":"true","edgeone_interval":"36h","sync_enabled":"true"}`)
	// sync_enabled 只能由 pause/resume 或导入写入，普通 PUT 必须拒绝；去掉后重试。
	if w.Code != http.StatusBadRequest {
		t.Fatalf("普通 PUT sync_enabled 状态码 = %d, want 400", w.Code)
	}
	w = source.do(t, http.MethodPut, "/api/settings",
		`{"tag":"dns-custom","edgeone_tag":"edge-custom","dns_enabled":"false",`+
			`"edgeone_enabled":"true","edgeone_interval":"36h"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("来源设置状态码 = %d; body=%s", w.Code, w.Body.String())
	}
	w = source.do(t, http.MethodPost, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("来源导出状态码 = %d; body=%s", w.Code, w.Body.String())
	}
	exported := w.Body.String()

	dest := newTestEnv(t)
	dest.seedTargetCount(t, config.CloudTCLighthouse, "ap-shanghai", "lhins-dest", 3)
	destTargetsBefore, _ := dest.store.GetTargets()
	// 目标库预置旧站点：导入必须整体替换，而不是保留旧 ZoneId。
	withTxForTest(t, dest.store, func(ctx context.Context, tx *sql.Tx) error {
		return dest.store.AddEdgeOneSiteTx(ctx, tx, config.EdgeOneSite{
			ZoneID: "zone-old", TargetIDs: []int{destTargetsBefore[0].ID},
			Protocol: "TCP", Ports: "80", AddressFamily: "ipv4",
		})
	})

	w = dest.do(t, http.MethodPost, "/api/config/import", exported)
	if w.Code != http.StatusOK {
		t.Fatalf("目标导入状态码 = %d; body=%s", w.Code, w.Body.String())
	}
	if got := dest.applyCount(); got != 1 {
		t.Errorf("合法 v4 导入应只发布一次，实际 %d", got)
	}
	settings, err := dest.store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if settings["tag"] != "dns-custom" || settings["edgeone_tag"] != "edge-custom" ||
		settings["dns_enabled"] != "false" || settings["edgeone_enabled"] != "true" ||
		settings["edgeone_interval"] != "36h" {
		t.Errorf("v4 设置未完整恢复: %+v", settings)
	}
	st := dest.snapshot()
	if st == nil || st.Config.DNSEnabled || !st.Config.EdgeOneEnabled ||
		st.Config.EdgeOneTag != "edge-custom" || st.Config.EdgeOneInterval.String() != "36h0m0s" {
		t.Fatalf("v4 运行时状态未恢复: %+v", st)
	}

	sites, err := dest.store.LoadEdgeOneSites()
	if err != nil || len(sites) != 1 {
		t.Fatalf("导入后站点 = %+v err=%v", sites, err)
	}
	if sites[0].ZoneID != "zone-roundtrip" || sites[0].Protocol != "TCP" ||
		sites[0].Ports != "8443" || sites[0].AddressFamily != "dual" {
		t.Errorf("v4 站点参数未恢复: %+v", sites[0])
	}
	for _, site := range sites {
		if site.ZoneID == "zone-old" {
			t.Errorf("v4 导入未清除目标库旧站点: %+v", sites)
		}
	}
	if len(sites[0].TargetIDs) != 2 {
		t.Fatalf("站点目标数量 = %d, want 2", len(sites[0].TargetIDs))
	}
	targetsAfter, err := dest.store.GetTargets()
	if err != nil {
		t.Fatalf("GetTargets 失败: %v", err)
	}
	byID := make(map[int]config.TargetConfig, len(targetsAfter))
	for _, tr := range targetsAfter {
		byID[tr.ID] = tr
	}
	resources := map[string]bool{}
	for _, id := range sites[0].TargetIDs {
		tr, ok := byID[id]
		if !ok {
			t.Fatalf("站点引用了不存在的目标 #%d", id)
		}
		resources[tr.ResourceID] = true
	}
	if !resources["sg-src-a"] || !resources["sg-src-b"] || len(resources) != 2 {
		t.Errorf("站点目标未重映射到正确资源: %v", resources)
	}
	// 导入前目标库已有自增历史，导入后的目标 ID 必须与来源不同。
	for _, before := range destTargetsBefore {
		for _, tr := range targetsAfter {
			if tr.ID == before.ID && tr.ResourceID == before.ResourceID {
				t.Errorf("导入目标复用了目标库旧 ID: %+v", tr)
			}
		}
	}
	if sourceTargets, _ := source.store.GetTargets(); len(sourceTargets) == 2 {
		for _, src := range sourceTargets {
			for _, id := range sites[0].TargetIDs {
				if src.ID == id {
					t.Errorf("导入后站点仍引用来源数据库 ID %d", id)
				}
			}
		}
	}
}

func TestImportV4ValidationZeroWriteNoApply(t *testing.T) {
	e := newTestEnv(t)
	e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "keep-me")
	if err := e.store.SetSetting("tag", "keep-tag"); err != nil {
		t.Fatalf("预置设置失败: %v", err)
	}

	validTargets := `[{"export_id":1,"cloud_type":"tc_cvm","region":"ap-guangzhou","resource_id":"sg-a"},` +
		`{"export_id":2,"cloud_type":"tc_cvm","region":"ap-guangzhou","resource_id":"sg-b"},` +
		`{"export_id":3,"cloud_type":"tc_cvm","region":"ap-guangzhou","resource_id":"sg-a"}]`
	validSite := func(targets, ports, family, zone string) string {
		return `[{"zone_id":` + jsonQuote(zone) + `,"target_export_ids":` + targets +
			`,"protocol":"TCP","ports":` + jsonQuote(ports) + `,"address_family":` + jsonQuote(family) + `}]`
	}

	cases := map[string]string{
		"缺 edgeone_sites":      strings.Replace(validV4Bundle(), `"edgeone_sites":[],`, ``, 1),
		"edgeone_sites 为 null": strings.Replace(validV4Bundle(), `"edgeone_sites":[]`, `"edgeone_sites":null`, 1),
		"缺 dns_enabled":        strings.Replace(defaultV4Settings(), `"dns_enabled":true,`, ``, 1),
		"开关类型字符串":              strings.Replace(defaultV4Settings(), `"dns_enabled":true`, `"dns_enabled":"true"`, 1),
		"重复 export_id":         v4Bundle(`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"a"},{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"b"}]`, `[]`, `[]`, defaultV4Settings()),
		"站点空目标":                v4Bundle(validTargets, `[]`, validSite(`[]`, "80", "ipv4", "z"), defaultV4Settings()),
		"站点缺目标数组":              v4Bundle(validTargets, `[]`, `[{"zone_id":"z","protocol":"TCP","ports":"80","address_family":"ipv4"}]`, defaultV4Settings()),
		"站点 zone 重复":           v4Bundle(validTargets, `[]`, `[{"zone_id":"z","target_export_ids":[1],"protocol":"TCP","ports":"80","address_family":"ipv4"},{"zone_id":"z","target_export_ids":[2],"protocol":"TCP","ports":"443","address_family":"ipv4"}]`, defaultV4Settings()),
		"站点悬空引用":               v4Bundle(validTargets, `[]`, validSite(`[1,999]`, "80", "ipv4", "z"), defaultV4Settings()),
		"站点重复本地目标":             v4Bundle(validTargets, `[]`, validSite(`[1,1]`, "80", "ipv4", "z"), defaultV4Settings()),
		"站点实际资源别名":             v4Bundle(validTargets, `[]`, validSite(`[1,3]`, "80", "ipv4", "z"), defaultV4Settings()),
		"站点非法端口":               v4Bundle(validTargets, `[]`, validSite(`[1]`, "ALL,80", "ipv4", "z"), defaultV4Settings()),
		"站点非法地址族":              v4Bundle(validTargets, `[]`, validSite(`[1]`, "80", "ipx", "z"), defaultV4Settings()),
		"站点非法 ZoneId":          v4Bundle(validTargets, `[]`, validSite(`[1]`, "80", "ipv4", "a/b"), defaultV4Settings()),
		"TAG 重名":               v4Bundle(`[]`, `[]`, `[]`, v4SettingsJSON("same", "5m", "same", "72h", true, false, true)),
		"未知版本 5":               strings.Replace(validV4Bundle(), `"version":4`, `"version":5`, 1),
		"未知运行字段":               strings.Replace(validV4Bundle(), `{`, `{"edgeone_status":{}`, 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			w := e.do(t, http.MethodPost, "/api/config/import", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if got := e.applyCount(); got != 0 {
				t.Errorf("非法导入不得发布，实际 %d", got)
			}
			targets, err := e.store.GetTargets()
			if err != nil || len(targets) != 1 || targets[0].ResourceID != "keep-me" {
				t.Errorf("非法导入不得改动目标: %+v err=%v", targets, err)
			}
			settings, err := e.store.GetSettings()
			if err != nil || settings["tag"] != "keep-tag" {
				t.Errorf("非法导入不得改动设置: %+v err=%v", settings, err)
			}
			sites, err := e.store.LoadEdgeOneSites()
			if err != nil || len(sites) != 0 {
				t.Errorf("非法导入不得写入站点: %+v err=%v", sites, err)
			}
		})
	}
}

func TestImportV4TransactionFailureRollsBack(t *testing.T) {
	e := newTestEnv(t)
	e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "keep-me")
	if err := e.store.SetSetting("tag", "keep-tag"); err != nil {
		t.Fatalf("预置设置失败: %v", err)
	}
	e.execRaw(t, `CREATE TRIGGER fail_v4_site BEFORE INSERT ON edgeone_site_targets
		BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`)

	body := v4Bundle(
		`[{"export_id":1,"cloud_type":"tc_cvm","region":"ap-guangzhou","resource_id":"new"}]`,
		`[]`,
		`[{"zone_id":"zone-fail","target_export_ids":[1],"protocol":"TCP","ports":"443","address_family":"ipv4"}]`,
		defaultV4Settings(),
	)
	w := e.do(t, http.MethodPost, "/api/config/import", body)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("导入状态码 = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("事务失败不得发布，实际 %d", got)
	}
	targets, _ := e.store.GetTargets()
	if len(targets) != 1 || targets[0].ResourceID != "keep-me" {
		t.Errorf("事务失败必须回滚目标: %+v", targets)
	}
	settings, _ := e.store.GetSettings()
	if settings["tag"] != "keep-tag" {
		t.Errorf("事务失败必须回滚设置: %+v", settings)
	}
	sites, _ := e.store.LoadEdgeOneSites()
	if len(sites) != 0 {
		t.Errorf("事务失败必须回滚站点: %+v", sites)
	}
}

func TestImportV3PreservesLegacySettingsAndAddsNewDefaults(t *testing.T) {
	e := newTestEnv(t)
	settings := `{"credentials":{"tencent":{"secret_id":"","secret_key":""},` +
		`"aliyun":{"access_key_id":"","access_key_secret":""}},` +
		`"tag":"legacy-tag","interval":"7m","dns":"223.5.5.5","dns_timeout":"10s",` +
		`"dns_fail_threshold":5,"log_level":"info","sync_enabled":false,"theme":"dark"}`
	body := bundleWithSettings(`[]`, `[]`, settings)
	w := e.do(t, http.MethodPost, "/api/config/import", body)
	if w.Code != http.StatusOK {
		t.Fatalf("v3 导入状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("v3 导入应只发布一次，实际 %d", got)
	}
	stored, _ := e.store.GetSettings()
	for k, want := range map[string]string{
		"tag": "legacy-tag", "interval": "7m", "sync_enabled": "false", "theme": "dark",
		"dns_enabled": "true", "edgeone_enabled": "false",
		"edgeone_tag": config.DefaultEdgeOneTag, "edgeone_interval": config.DefaultEdgeOneIntervalText,
	} {
		if got := stored[k]; got != want {
			t.Errorf("v3 导入设置 %s = %q, want %q", k, got, want)
		}
	}
	st := e.snapshot()
	if st == nil || !st.Config.DNSEnabled || st.Config.EdgeOneEnabled ||
		st.Config.EdgeOneTag != config.DefaultEdgeOneTag ||
		st.Config.EdgeOneInterval != config.DefaultEdgeOneInterval ||
		st.Config.SyncEnabled || st.Config.Theme != "dark" {
		t.Errorf("v3 导入运行时状态不正确: %+v", st)
	}
	sites, err := e.store.LoadEdgeOneSites()
	if err != nil || len(sites) != 0 {
		t.Errorf("v3 导入必须得到无站点状态: %+v err=%v", sites, err)
	}
	w = e.do(t, http.MethodGet, "/api/edgeone/sites", "")
	if sites := decodeEdgeOneSites(t, w.Body.Bytes()); len(sites) != 0 {
		t.Errorf("v3 导入后 GET 站点应为空: %+v", sites)
	}

	// v3 原 DTO 不得接受 v4 专有字段：同一版本加入 edgeone_sites 必须 400。
	v4Only := strings.Replace(body, `"rules":[]`, `"rules":[],"edgeone_sites":[]`, 1)
	w = e.do(t, http.MethodPost, "/api/config/import", v4Only)
	if w.Code != http.StatusBadRequest {
		t.Errorf("v3 包混入 edgeone_sites 状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("v3 拒绝请求不得发布，实际 %d", got)
	}
}

func TestImportV3ClearsExistingSites(t *testing.T) {
	e := newTestEnv(t)
	a := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-a")
	w := e.do(t, http.MethodPost, "/api/edgeone/sites",
		edgeOneSiteBody("zone-old", []int{a}, "TCP", "443", "ipv4"))
	if w.Code != http.StatusCreated {
		t.Fatalf("预置站点状态码 = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	w = e.do(t, http.MethodPost, "/api/config/import", validBundle())
	if w.Code != http.StatusOK {
		t.Fatalf("v3 导入状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 2 {
		t.Errorf("预置站点 + v3 导入应发布两次，实际 %d", got)
	}
	w = e.do(t, http.MethodGet, "/api/edgeone/sites", "")
	if sites := decodeEdgeOneSites(t, w.Body.Bytes()); len(sites) != 0 {
		t.Errorf("v3 导入必须清除目标库旧站点: %+v", sites)
	}
}

func TestImportV4PreservesSyncLogsAndReplacesAlerts(t *testing.T) {
	e := newTestEnv(t)
	e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-a")
	if err := e.store.AddSyncLog(config.SyncLog{
		Timestamp: time.Now(), Target: "keep", Domain: "keep.example", Result: "success", Added: 1,
	}); err != nil {
		t.Fatalf("预置同步日志失败: %v", err)
	}
	if err := e.store.ReplaceScannedResources("tc_cvm", "ap-guangzhou", []config.ScannedResource{
		{CloudType: "tc_cvm", Region: "ap-guangzhou", ResourceID: "scan"},
	}); err != nil {
		t.Fatalf("预置扫描缓存失败: %v", err)
	}
	body := v4Bundle(
		`[{"export_id":1,"cloud_type":"tc_cvm","region":"ap-guangzhou","resource_id":"sg-a"}]`,
		`[]`,
		`[{"zone_id":"zone-audit","target_export_ids":[1],"protocol":"TCP","ports":"443","address_family":"ipv4"}]`,
		defaultV4Settings(),
	)
	body = strings.Replace(body, `"health_timeout":"10m"`, `"health_timeout":"25m"`, 1)
	w := e.do(t, http.MethodPost, "/api/config/import", body)
	if w.Code != http.StatusOK {
		t.Fatalf("v4 导入状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("v4 导入应只发布一次，实际 %d", got)
	}
	logs, err := e.store.GetSyncLogs(10)
	if err != nil || len(logs) != 1 || logs[0].Domain != "keep.example" {
		t.Errorf("v4 导入未保留 sync_logs: %+v err=%v", logs, err)
	}
	scanned, err := e.store.GetScannedResources("tc_cvm")
	if err != nil || len(scanned) != 0 {
		t.Errorf("v4 导入未清空扫描缓存: %+v err=%v", scanned, err)
	}
	policy, err := e.store.GetAlertPolicy()
	if err != nil || policy.HealthTimeoutText != "25m" {
		t.Errorf("v4 导入未覆盖告警策略: %+v err=%v", policy, err)
	}
	sites, err := e.store.LoadEdgeOneSites()
	if err != nil || len(sites) != 1 || sites[0].ZoneID != "zone-audit" {
		t.Errorf("v4 导入站点不正确: %+v err=%v", sites, err)
	}
}

func v4Envelope(t *testing.T, body string) map[string]json.RawMessage {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("解析 v4 配置包失败: %v", err)
	}
	return raw
}

func v4Marshal(t *testing.T, raw map[string]json.RawMessage) string {
	t.Helper()
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("序列化 v4 配置包失败: %v", err)
	}
	return string(data)
}

func TestImportV4PresenceMatrix(t *testing.T) {
	cases := make(map[string]string)

	for _, field := range []string{"metadata", "targets", "rules", "edgeone_sites", "settings", "alerts", "monitoring"} {
		raw := v4Envelope(t, validV4Bundle())
		delete(raw, field)
		cases["缺顶层字段/"+field] = v4Marshal(t, raw)
	}
	for _, field := range []string{"dns_enabled", "edgeone_enabled", "edgeone_tag", "edgeone_interval"} {
		raw := v4Envelope(t, validV4Bundle())
		var settings map[string]json.RawMessage
		if err := json.Unmarshal(raw["settings"], &settings); err != nil {
			t.Fatalf("解析 settings 失败: %v", err)
		}
		delete(settings, field)
		raw["settings"], _ = json.Marshal(settings)
		cases["缺 settings."+field] = v4Marshal(t, raw)
	}
	cases["站点 target_export_ids 为 null"] = v4Bundle(
		`[{"export_id":1,"cloud_type":"tc_cvm","region":"ap-guangzhou","resource_id":"sg-a"}]`,
		`[]`,
		`[{"zone_id":"zone-null","target_export_ids":null,"protocol":"TCP","ports":"80","address_family":"ipv4"}]`,
		defaultV4Settings(),
	)
	cases["站点元素为 null"] = v4Bundle(
		`[]`, `[]`, `[null]`, defaultV4Settings(),
	)

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			w := e.do(t, http.MethodPost, "/api/config/import", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if got := e.applyCount(); got != 0 {
				t.Errorf("presence 失败不得发布，实际 %d", got)
			}
			if sites, err := e.store.LoadEdgeOneSites(); err != nil || len(sites) != 0 {
				t.Errorf("presence 失败不得写入站点: %+v err=%v", sites, err)
			}
		})
	}
}
