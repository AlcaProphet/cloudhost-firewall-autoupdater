package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// TestConfigImportSuccessReplacesAndNormalizes 合法 version 2 导入：覆盖式替换、复用同一组校验并归一化、只一次运行时发布
func TestConfigImportSuccessReplacesAndNormalizes(t *testing.T) {
	e := newTestEnv(t)
	e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-old")
	e.seedRule(t, config.DomainRule{Host: "old.example.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT"})
	if err := e.store.SetSetting("tag", "old-tag"); err != nil {
		t.Fatalf("预置旧设置失败: %v", err)
	}

	// version 2：导出 ID 与目标数据库 ID 无关，规则通过 target_export_ids 引用
	body := bundleWithSettings(
		`[{"export_id":7,"cloud_type":" ali_ecs ","region":" cn-hangzhou ","resource_id":" sg-1 "}]`,
		`[{"host":" api.example.com ","protocol":" tcp ","ports":" 443 ","action":"accept",`+
			`"target_export_ids":[7],"comment":"c","enable_ipv6":true}]`,
		`{"credentials":{"tencent":{"secret_id":"AKID-new","secret_key":"sk-new"},`+
			`"aliyun":{"access_key_id":"LTAI-new","access_key_secret":"aks-new"}},`+
			`"tag":" new-tag ","interval":"7m","dns":"223.5.5.5","dns_timeout":"10s",`+
			`"dns_fail_threshold":5,"log_level":"info","sync_enabled":false,"theme":"dark"}`,
	)
	w := e.do(t, http.MethodPost, "/api/config/import", body)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("合法导入应只触发一次运行时发布，实际 %d", got)
	}

	targets, err := e.store.GetTargets()
	if err != nil || len(targets) != 1 {
		t.Fatalf("GetTargets = %+v, err=%v", targets, err)
	}
	if targets[0].CloudType != config.CloudAliECS || targets[0].Region != "cn-hangzhou" || targets[0].ResourceID != "sg-1" {
		t.Errorf("目标未按校验归一化写入: %+v", targets[0])
	}
	if targets[0].ID == 7 {
		t.Errorf("不得复用配置包中的 export_id 作为数据库 ID: %+v", targets[0])
	}

	rules, err := e.store.GetRules()
	if err != nil || len(rules) != 1 {
		t.Fatalf("GetRules = %+v, err=%v", rules, err)
	}
	if rules[0].Host != "api.example.com" || rules[0].Protocol != "TCP" || rules[0].Ports != "443" || rules[0].Action != "ACCEPT" {
		t.Errorf("规则未归一化: %+v", rules[0])
	}
	// 引用必须通过 export_id → 新数据库 ID 映射重建
	if len(rules[0].Targets) != 1 || rules[0].Targets[0] != targets[0].ID {
		t.Errorf("规则引用未按映射重建: rule.Targets=%v, 目标 ID=%d", rules[0].Targets, targets[0].ID)
	}

	settings, err := e.store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if settings["tag"] != "new-tag" || settings["interval"] != "7m" {
		t.Errorf("设置未覆盖式替换: %+v", settings)
	}
	// 空凭据以外的完整凭据覆盖
	if settings["tc_access_id"] != "AKID-new" || settings["ali_access_key"] != "aks-new" {
		t.Errorf("完整凭据未覆盖: %+v", settings)
	}
	// 主题、同步开关也来自配置包
	if settings["theme"] != "dark" || settings["sync_enabled"] != "false" {
		t.Errorf("theme/sync_enabled 未覆盖: %+v", settings)
	}
	// 运行时状态必须已同步发布新值
	st := e.snapshot()
	if st == nil || st.Config.Theme != "dark" || st.Config.SyncEnabled {
		t.Errorf("运行时状态未发布配置包内容: %+v", st)
	}
	if st.Config.Credentials.TencentSecretID != "AKID-new" {
		t.Errorf("运行时凭据未更新: %+v", st.Config.Credentials)
	}
}

// TestConfigImportEmptyCredentialsClearOldValues 空凭据必须能明确清除旧值
func TestConfigImportEmptyCredentialsClearOldValues(t *testing.T) {
	e := newTestEnv(t)
	if err := e.store.SetSetting("tc_access_id", "AKID-old"); err != nil {
		t.Fatalf("预置凭据失败: %v", err)
	}
	if err := e.store.SetSetting("ali_access_key", "aks-old"); err != nil {
		t.Fatalf("预置凭据失败: %v", err)
	}

	w := e.do(t, http.MethodPost, "/api/config/import", validBundle())
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	settings, _ := e.store.GetSettings()
	if settings["tc_access_id"] != "" || settings["ali_access_key"] != "" {
		t.Errorf("空凭据必须清除旧值: %+v", settings)
	}
}

// TestConfigImportInvalidInputNoWriteNoReload 非法配置包：400、旧配置不变、零运行时发布
func TestConfigImportInvalidInputNoWriteNoReload(t *testing.T) {
	cases := map[string]string{
		"version 缺失":             bundleWithout("version"),
		"version 1":              `{"version":1,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` + validBundleSettings() + `,` + validBundleAlerts() + `}`,
		"version 2":              `{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` + validBundleSettings() + `,` + validBundleAlerts() + `}`,
		"version 4":              `{"version":4,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` + validBundleSettings() + `,` + validBundleAlerts() + `,` + validBundleMonitoring() + `}`,
		"version null":           `{"version":null,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` + validBundleSettings() + `,` + validBundleAlerts() + `}`,
		"metadata 缺失":            bundleWithout("metadata"),
		"exported_at 缺失":         `{"version":3,"metadata":{},"targets":[],"rules":[],` + validBundleSettings() + `,` + validBundleAlerts() + `}`,
		"exported_at 非法":         `{"version":3,"metadata":{"exported_at":"2026/09/22"},"targets":[],"rules":[],` + validBundleSettings() + `,` + validBundleAlerts() + `}`,
		"targets 缺失":             bundleWithout("targets"),
		"targets 为 null":         `{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":null,"rules":[],` + validBundleSettings() + `,` + validBundleAlerts() + `}`,
		"rules 缺失":               bundleWithout("rules"),
		"settings 缺失":            bundleWithout("settings"),
		"alerts 缺失":              bundleWithout("alerts"),
		"未知云类型":                  bundleWith(`[{"export_id":1,"cloud_type":"aws_ec2","region":"gz","resource_id":"x"}]`, `[]`),
		"空白地域":                   bundleWith(`[{"export_id":1,"cloud_type":"tc_cvm","region":"  ","resource_id":"x"}]`, `[]`),
		"空白资源 ID":                bundleWith(`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":" "}]`, `[]`),
		"export_id 非正":           bundleWith(`[{"export_id":0,"cloud_type":"tc_cvm","region":"gz","resource_id":"x"}]`, `[]`),
		"export_id 负数":           bundleWith(`[{"export_id":-1,"cloud_type":"tc_cvm","region":"gz","resource_id":"x"}]`, `[]`),
		"export_id 重复":           bundleWith(`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"a"},{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"b"}]`, `[]`),
		"未知 target 引用":           bundleWith(`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"x"}]`, `[{"host":"a","protocol":"TCP","ports":"80","action":"ACCEPT","target_export_ids":[9],"comment":"","enable_ipv6":false}]`),
		"目标引用重复":                 bundleWith(`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"x"}]`, `[{"host":"a","protocol":"TCP","ports":"80","action":"ACCEPT","target_export_ids":[1,1],"comment":"","enable_ipv6":false}]`),
		"目标引用非正":                 bundleWith(`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"x"}]`, `[{"host":"a","protocol":"TCP","ports":"80","action":"ACCEPT","target_export_ids":[-1],"comment":"","enable_ipv6":false}]`),
		"target_export_ids null": bundleWith(`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"x"}]`, `[{"host":"a","protocol":"TCP","ports":"80","action":"ACCEPT","target_export_ids":null,"comment":"","enable_ipv6":false}]`),
		"未知协议":                   bundleWith(`[]`, `[{"host":"a","protocol":"SCTP","ports":"80","action":"ACCEPT","target_export_ids":[],"comment":"","enable_ipv6":false}]`),
		"未知动作":                   bundleWith(`[]`, `[{"host":"a","protocol":"TCP","ports":"80","action":"ALLOW","target_export_ids":[],"comment":"","enable_ipv6":false}]`),
		"非 ICMP 空端口":             bundleWith(`[]`, `[{"host":"a","protocol":"TCP","ports":"   ","action":"ACCEPT","target_export_ids":[],"comment":"","enable_ipv6":false}]`),
		"规则 host 空白":             bundleWith(`[]`, `[{"host":"  ","protocol":"TCP","ports":"80","action":"ACCEPT","target_export_ids":[],"comment":"","enable_ipv6":false}]`),
		"规则字段缺失":                 bundleWith(`[]`, `[{"protocol":"TCP","ports":"80","action":"ACCEPT","target_export_ids":[],"comment":"","enable_ipv6":false}]`),
		"规则 enable_ipv6 缺失":      bundleWith(`[]`, `[{"host":"a","protocol":"TCP","ports":"80","action":"ACCEPT","target_export_ids":[],"comment":""}]`),
		"凭据字段缺失":                 bundleWithSettings(`[]`, `[]`, `{"credentials":{"tencent":{"secret_id":"a"},"aliyun":{"access_key_id":"","access_key_secret":""}},"tag":"t","interval":"5m","dns":"1.1.1.1","dns_timeout":"10s","dns_fail_threshold":5,"log_level":"info","sync_enabled":true,"theme":"light"}`),
		"阈值非正":                   bundleWithSettings(`[]`, `[]`, `{"credentials":{"tencent":{"secret_id":"","secret_key":""},"aliyun":{"access_key_id":"","access_key_secret":""}},"tag":"t","interval":"5m","dns":"1.1.1.1","dns_timeout":"10s","dns_fail_threshold":0,"log_level":"info","sync_enabled":true,"theme":"light"}`),
		"TAG 非法":                 bundleWithSettings(`[]`, `[]`, `{"credentials":{"tencent":{"secret_id":"","secret_key":""},"aliyun":{"access_key_id":"","access_key_secret":""}},"tag":"bad[tag]","interval":"5m","dns":"1.1.1.1","dns_timeout":"10s","dns_fail_threshold":5,"log_level":"info","sync_enabled":true,"theme":"light"}`),
		"interval 非法":            bundleWithSettings(`[]`, `[]`, `{"credentials":{"tencent":{"secret_id":"","secret_key":""},"aliyun":{"access_key_id":"","access_key_secret":""}},"tag":"t","interval":"abc","dns":"1.1.1.1","dns_timeout":"10s","dns_fail_threshold":5,"log_level":"info","sync_enabled":true,"theme":"light"}`),
		"dns 非法":                 bundleWithSettings(`[]`, `[]`, `{"credentials":{"tencent":{"secret_id":"","secret_key":""},"aliyun":{"access_key_id":"","access_key_secret":""}},"tag":"t","interval":"5m","dns":"2001:db8::1","dns_timeout":"10s","dns_fail_threshold":5,"log_level":"info","sync_enabled":true,"theme":"light"}`),
		"log_level 非法":           bundleWithSettings(`[]`, `[]`, `{"credentials":{"tencent":{"secret_id":"","secret_key":""},"aliyun":{"access_key_id":"","access_key_secret":""}},"tag":"t","interval":"5m","dns":"1.1.1.1","dns_timeout":"10s","dns_fail_threshold":5,"log_level":"trace","sync_enabled":true,"theme":"light"}`),
		"theme 非法":               bundleWithSettings(`[]`, `[]`, `{"credentials":{"tencent":{"secret_id":"","secret_key":""},"aliyun":{"access_key_id":"","access_key_secret":""}},"tag":"t","interval":"5m","dns":"1.1.1.1","dns_timeout":"10s","dns_fail_threshold":5,"log_level":"info","sync_enabled":true,"theme":"blue"}`),
		"webui_port 未知字段":        bundleWithSettings(`[]`, `[]`, `{"credentials":{"tencent":{"secret_id":"","secret_key":""},"aliyun":{"access_key_id":"","access_key_secret":""}},"tag":"t","interval":"5m","dns":"1.1.1.1","dns_timeout":"10s","dns_fail_threshold":5,"log_level":"info","sync_enabled":true,"theme":"light","webui_port":"1"}`),
		"启用邮件但 host 空":           `{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` + validBundleSettings() + `,` + `"alerts":{"policy":{"dns_failed_enabled":false,"sync_error_enabled":false,"operational_error_enabled":false,"health_timeout":"10m"},"email":{"enabled":true,"host":"","port":"587","username":"","password":"","from_addr":"f@x.com","to_addr":"t@x.com","subject":"s","body":"b"},"webhook":{"enabled":false,"url":"","channel":"dingtalk"}},` + validBundleMonitoring() + `}`,
		"启用 webhook 但 URL 非法":    `{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` + validBundleSettings() + `,` + `"alerts":{"policy":{"dns_failed_enabled":false,"sync_error_enabled":false,"operational_error_enabled":false,"health_timeout":"10m"},"email":{"enabled":false,"host":"","port":"587","username":"","password":"","from_addr":"","to_addr":"","subject":"s","body":"b"},"webhook":{"enabled":true,"url":"ftp://x","channel":"dingtalk"}},` + validBundleMonitoring() + `}`,
		"webhook 渠道非法":           `{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` + validBundleSettings() + `,` + `"alerts":{"policy":{"dns_failed_enabled":false,"sync_error_enabled":false,"operational_error_enabled":false,"health_timeout":"10m"},"email":{"enabled":false,"host":"","port":"587","username":"","password":"","from_addr":"","to_addr":"","subject":"s","body":"b"},"webhook":{"enabled":false,"url":"","channel":"telegram"}},` + validBundleMonitoring() + `}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "keep-me")
			if err := e.store.SetSetting("tag", "keep-tag"); err != nil {
				t.Fatalf("预置旧设置失败: %v", err)
			}

			w := e.do(t, http.MethodPost, "/api/config/import", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			targets, _ := e.store.GetTargets()
			if len(targets) != 1 || targets[0].ResourceID != "keep-me" {
				t.Errorf("拒绝导入后旧目标应保留: %+v", targets)
			}
			settings, _ := e.store.GetSettings()
			if settings["tag"] != "keep-tag" {
				t.Errorf("拒绝导入后旧设置应保留: %+v", settings)
			}
			if got := e.applyCount(); got != 0 {
				t.Errorf("非法导入不应触发运行时发布，实际 %d", got)
			}
		})
	}
}

// TestConfigImportErrorPointsToField 数组元素错误应指出位置与字段
func TestConfigImportErrorPointsToField(t *testing.T) {
	e := newTestEnv(t)
	body := bundleWith(
		`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"a"},{"export_id":2,"cloud_type":"aws_ec2","region":"gz","resource_id":"b"}]`,
		`[]`)
	w := e.do(t, http.MethodPost, "/api/config/import", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "targets[1]") || !strings.Contains(w.Body.String(), "cloud_type") {
		t.Errorf("错误应指出 targets[1].cloud_type: %s", w.Body.String())
	}
}

// TestConfigImportTransactionFailureRollsBack 导入事务中途失败必须完整回滚（含清空动作）
func TestConfigImportTransactionFailureRollsBack(t *testing.T) {
	e := newTestEnv(t)
	e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "keep-me")
	if err := e.store.SetSetting("tag", "keep-tag"); err != nil {
		t.Fatalf("预置旧设置失败: %v", err)
	}
	if err := e.store.SaveAlertEmail(&config.AlertEmailConfig{Enabled: true, Host: "smtp.keep", Port: "587"}); err != nil {
		t.Fatalf("预置告警失败: %v", err)
	}

	// 规则插入失败：清空旧配置与插入新目标都必须一起回滚
	e.execRaw(t, `CREATE TRIGGER fail_rule_insert BEFORE INSERT ON rules BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`)
	body := bundleWith(
		`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"new"}]`,
		`[{"host":"a.example.com","protocol":"TCP","ports":"80","action":"ACCEPT","target_export_ids":[1],"comment":"","enable_ipv6":false}]`)
	w := e.do(t, http.MethodPost, "/api/config/import", body)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "fixture failure") {
		t.Errorf("500 不得回显底层错误: %s", w.Body.String())
	}

	targets, _ := e.store.GetTargets()
	if len(targets) != 1 || targets[0].ResourceID != "keep-me" {
		t.Errorf("导入失败必须回滚清空动作: %+v", targets)
	}
	settings, _ := e.store.GetSettings()
	if settings["tag"] != "keep-tag" {
		t.Errorf("导入失败必须回滚设置: %+v", settings)
	}
	if email, _ := e.store.GetAlertEmail(); !email.Enabled || email.Host != "smtp.keep" {
		t.Errorf("导入失败必须回滚告警: %+v", email)
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("导入失败不应触发运行时发布，实际 %d", got)
	}
}

// TestConfigImportStrictDecoding 导入与普通 API 共用同一严格解码语义，只有大小上限不同（Build6 §12.8）
func TestConfigImportStrictDecoding(t *testing.T) {
	cases := map[string]struct {
		body string
		want int
	}{
		"尾随 JSON":  {validBundle() + `{"x":1}`, http.StatusBadRequest},
		"多个顶层值":    {validBundle() + ` 1`, http.StatusBadRequest},
		"尾随垃圾":     {validBundle() + ` xxx`, http.StatusBadRequest},
		"未知顶层字段":   {`{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` + validBundleSettings() + `,` + validBundleAlerts() + `,"extra":1}`, http.StatusBadRequest},
		"任意层级未知字段": {bundleWith(`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"x","extra":"y"}]`, `[]`), http.StatusBadRequest},
		"类型错误":     {`{"version":"3","metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` + validBundleSettings() + `,` + validBundleAlerts() + `}`, http.StatusBadRequest},
		"空 body":   {``, http.StatusBadRequest},
		"超限 10MiB": {bundleWithSettings(`[]`, `[]`, `{"credentials":{"tencent":{"secret_id":"","secret_key":""},"aliyun":{"access_key_id":"","access_key_secret":""}},"tag":"t","interval":"5m","dns":"1.1.1.1","dns_timeout":"10s","dns_fail_threshold":5,"log_level":"info","sync_enabled":true,"theme":"light","extra":"`+strings.Repeat("a", maxImportBodyBytes)+`"}`), http.StatusRequestEntityTooLarge},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "keep-me")
			if err := e.store.SetSetting("tag", "keep-tag"); err != nil {
				t.Fatalf("预置旧设置失败: %v", err)
			}

			w := e.do(t, http.MethodPost, "/api/config/import", tc.body)
			if w.Code != tc.want {
				t.Fatalf("状态码 = %d, want %d; body=%s", w.Code, tc.want, w.Body.String())
			}
			// 被拒绝的配置包不得清空或部分替换旧配置，也不得触发 reload
			if targets, _ := e.store.GetTargets(); len(targets) != 1 || targets[0].ResourceID != "keep-me" {
				t.Errorf("拒绝导入后旧目标应保留: %+v", targets)
			}
			if settings, _ := e.store.GetSettings(); settings["tag"] != "keep-tag" {
				t.Errorf("拒绝导入后旧设置应保留: %+v", settings)
			}
			if got := e.applyCount(); got != 0 {
				t.Errorf("非法导入不应触发运行时更新，实际 %d", got)
			}
		})
	}
}

// TestConfigImportWithin10MiBAllowed 10 MiB 以内的合法配置包仍可导入（上限没有误伤正常体积）
func TestConfigImportWithin10MiBAllowed(t *testing.T) {
	e := newTestEnv(t)
	// 用一条超长 comment 承载约 1 MiB 合法内容，验证体积在限内时不被拒绝
	longComment := strings.Repeat("a", 1<<20)
	body := bundleWith(`[]`, `[{"host":"a.example.com","protocol":"TCP","ports":"80","action":"ACCEPT",`+
		`"target_export_ids":[],"comment":"`+longComment+`","enable_ipv6":false}]`)
	w := e.do(t, http.MethodPost, "/api/config/import", body)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("合法导入应只触发一次运行时发布，实际 %d", got)
	}
	rules, _ := e.store.GetRules()
	if len(rules) != 1 || rules[0].Comment != longComment {
		t.Errorf("导入未生效: rules=%d", len(rules))
	}
}
