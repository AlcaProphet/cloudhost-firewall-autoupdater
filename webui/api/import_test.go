package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// TestConfigImportSuccessReplacesAndNormalizes 合法 version 1 导入：覆盖式替换、复用同一组校验并归一化、只一次 reload
func TestConfigImportSuccessReplacesAndNormalizes(t *testing.T) {
	e := newTestEnv(t)
	e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-old")
	e.seedRule(t, config.DomainRule{Host: "old.example.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT"})
	if err := e.store.SetSetting("tag", "old-tag"); err != nil {
		t.Fatalf("预置旧设置失败: %v", err)
	}

	// 规则引用 id=7：version 1 没有 export_id → 新 ID 映射，允许引用无法解析的目标（Step 5 才重建引用）
	body := `{"version":1,` +
		`"targets":[{"id":7,"cloud_type":" ali_ecs ","region":" cn-hangzhou ","resource_id":" sg-1 "}],` +
		`"rules":[{"id":9,"host":" api.example.com ","protocol":" tcp ","ports":" 443 ","action":"accept","targets":[7],"comment":"c","enable_ipv6":true}],` +
		`"settings":{"tag":" new-tag ","interval":"7m"}}`
	w := e.do(t, http.MethodPost, "/api/config/import", body)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("合法导入应只触发一次运行时更新，实际 %d", got)
	}

	targets, err := e.store.GetTargets()
	if err != nil || len(targets) != 1 {
		t.Fatalf("GetTargets = %+v, err=%v", targets, err)
	}
	if targets[0].CloudType != config.CloudAliECS || targets[0].Region != "cn-hangzhou" || targets[0].ResourceID != "sg-1" {
		t.Errorf("目标未按校验归一化写入: %+v", targets[0])
	}
	if targets[0].ID == 7 {
		t.Errorf("version 1 不应复用配置包中的 id: %+v", targets[0])
	}

	rules, err := e.store.GetRules()
	if err != nil || len(rules) != 1 {
		t.Fatalf("GetRules = %+v, err=%v", rules, err)
	}
	if rules[0].Host != "api.example.com" || rules[0].Protocol != "TCP" || rules[0].Ports != "443" || rules[0].Action != "ACCEPT" {
		t.Errorf("规则未归一化: %+v", rules[0])
	}

	settings, err := e.store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if settings["tag"] != "new-tag" || settings["interval"] != "7m" {
		t.Errorf("设置未覆盖式替换: %+v", settings)
	}
}

// TestConfigImportInvalidInputNoWriteNoReload 非法配置包：400、旧配置不变、零 reload
func TestConfigImportInvalidInputNoWriteNoReload(t *testing.T) {
	cases := map[string]string{
		"版本不支持":              `{"version":2,"targets":[],"rules":[],"settings":{}}`,
		"未知云类型":              `{"version":1,"targets":[{"cloud_type":"aws_ec2","region":"gz","resource_id":"x"}],"rules":[],"settings":{}}`,
		"空白地域":               `{"version":1,"targets":[{"cloud_type":"tc_cvm","region":"  ","resource_id":"x"}],"rules":[],"settings":{}}`,
		"空白资源 ID":            `{"version":1,"targets":[{"cloud_type":"tc_cvm","region":"gz","resource_id":" "}],"rules":[],"settings":{}}`,
		"未知协议":               `{"version":1,"targets":[],"rules":[{"host":"a","protocol":"SCTP","ports":"80","action":"ACCEPT","targets":[]}],"settings":{}}`,
		"未知动作":               `{"version":1,"targets":[],"rules":[{"host":"a","protocol":"TCP","ports":"80","action":"ALLOW","targets":[]}],"settings":{}}`,
		"非 ICMP 空端口":         `{"version":1,"targets":[],"rules":[{"host":"a","protocol":"TCP","ports":"   ","action":"ACCEPT","targets":[]}],"settings":{}}`,
		"规则 host 空白":         `{"version":1,"targets":[],"rules":[{"host":"  ","protocol":"TCP","ports":"80","action":"ACCEPT","targets":[]}],"settings":{}}`,
		"目标 ID 重复":           `{"version":1,"targets":[],"rules":[{"host":"a","protocol":"TCP","ports":"80","action":"ACCEPT","targets":[1,1]}],"settings":{}}`,
		"目标 ID 非正":           `{"version":1,"targets":[],"rules":[{"host":"a","protocol":"TCP","ports":"80","action":"ACCEPT","targets":[-1]}],"settings":{}}`,
		"设置 interval 非法":     `{"version":1,"targets":[],"rules":[],"settings":{"interval":"abc"}}`,
		"设置 TAG 非法":          `{"version":1,"targets":[],"rules":[],"settings":{"tag":"bad[tag]"}}`,
		"设置 dns 非法":          `{"version":1,"targets":[],"rules":[],"settings":{"dns":"2001:db8::1"}}`,
		"设置 log_level 非法":    `{"version":1,"targets":[],"rules":[],"settings":{"log_level":"trace"}}`,
		"设置 sync_enabled 非法": `{"version":1,"targets":[],"rules":[],"settings":{"sync_enabled":"yes"}}`,
		"webui_port":         `{"version":1,"targets":[],"rules":[],"settings":{"webui_port":"1"}}`,
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
				t.Errorf("非法导入不应触发运行时更新，实际 %d", got)
			}
		})
	}
}

// TestConfigImportErrorPointsToField 数组元素错误应指出位置与字段
func TestConfigImportErrorPointsToField(t *testing.T) {
	e := newTestEnv(t)
	body := `{"version":1,"targets":[{"cloud_type":"tc_cvm","region":"gz","resource_id":"a"},{"cloud_type":"aws_ec2","region":"gz","resource_id":"b"}],"rules":[],"settings":{}}`
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

	// 规则插入失败：清空旧配置与插入新目标都必须一起回滚
	e.execRaw(t, `CREATE TRIGGER fail_rule_insert BEFORE INSERT ON rules BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`)
	body := `{"version":1,` +
		`"targets":[{"cloud_type":"tc_cvm","region":"gz","resource_id":"new"}],` +
		`"rules":[{"host":"a.example.com","protocol":"TCP","ports":"80","action":"ACCEPT","targets":[]}],` +
		`"settings":{"tag":"new-tag"}}`
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
	if got := e.applyCount(); got != 0 {
		t.Errorf("导入失败不应触发运行时更新，实际 %d", got)
	}
}

// TestConfigImportStrictDecoding 导入与普通 API 共用同一严格解码语义，只有大小上限不同（Build6 §12.8）
func TestConfigImportStrictDecoding(t *testing.T) {
	cases := map[string]struct {
		body string
		want int
	}{
		"尾随 JSON":  {`{"version":1,"targets":[],"rules":[],"settings":{"tag":"t"}}{"x":1}`, http.StatusBadRequest},
		"多个顶层值":    {`{"version":1,"targets":[],"rules":[],"settings":{}} 1`, http.StatusBadRequest},
		"尾随垃圾":     {`{"version":1,"targets":[],"rules":[],"settings":{}} xxx`, http.StatusBadRequest},
		"未知顶层字段":   {`{"version":1,"targets":[],"rules":[],"settings":{},"extra":1}`, http.StatusBadRequest},
		"类型错误":     {`{"version":"1","targets":[],"rules":[],"settings":{}}`, http.StatusBadRequest},
		"空 body":   {``, http.StatusBadRequest},
		"超限 10MiB": {`{"version":1,"targets":[],"rules":[],"settings":{"padding":"` + strings.Repeat("a", maxImportBodyBytes) + `"}}`, http.StatusRequestEntityTooLarge},
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
	// 用未校验的未知键承载约 1 MiB 填充，验证体积在限内时不被拒绝
	body := `{"version":1,"targets":[],"rules":[],"settings":{"tag":"ok","padding":"` + strings.Repeat("a", 1<<20) + `"}}`
	w := e.do(t, http.MethodPost, "/api/config/import", body)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("合法导入应只触发一次运行时更新，实际 %d", got)
	}
	settings, _ := e.store.GetSettings()
	if settings["tag"] != "ok" {
		t.Errorf("导入未生效: tag=%q", settings["tag"])
	}
}
