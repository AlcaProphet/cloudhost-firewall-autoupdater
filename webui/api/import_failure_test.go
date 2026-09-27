package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// importState 记录一次导入前的旧配置，用于断言失败注入后完整保留。
type importState struct {
	targets  []config.TargetConfig
	rules    []config.DomainRule
	settings map[string]string
	email    *config.AlertEmailConfig
	webhook  *config.AlertWebhookConfig
	scanned  int
	logs     int
}

// captureImportState 抓取当前业务配置快照（断言回滚完整性用）。
func captureImportState(t *testing.T, e *testEnv) importState {
	t.Helper()
	targets, err := e.store.GetTargets()
	if err != nil {
		t.Fatalf("GetTargets 失败: %v", err)
	}
	rules, err := e.store.GetRules()
	if err != nil {
		t.Fatalf("GetRules 失败: %v", err)
	}
	settings, err := e.store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	email, err := e.store.GetAlertEmail()
	if err != nil {
		t.Fatalf("GetAlertEmail 失败: %v", err)
	}
	webhook, err := e.store.GetAlertWebhook()
	if err != nil {
		t.Fatalf("GetAlertWebhook 失败: %v", err)
	}
	scanned, err := e.store.GetScannedResources("tc_lighthouse")
	if err != nil {
		t.Fatalf("GetScannedResources 失败: %v", err)
	}
	logs, err := e.store.GetSyncLogs(10)
	if err != nil {
		t.Fatalf("GetSyncLogs 失败: %v", err)
	}
	return importState{targets: targets, rules: rules, settings: settings, email: email, webhook: webhook, scanned: len(scanned), logs: len(logs)}
}

// seedImportBaseline 预置一套完整的旧业务配置（目标/规则/设置/告警/扫描缓存/同步日志）。
func seedImportBaseline(t *testing.T, e *testEnv) {
	t.Helper()
	id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "keep-me")
	e.seedRule(t, config.DomainRule{Host: "keep.example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{id}})
	if err := e.store.SetSetting("tag", "keep-tag"); err != nil {
		t.Fatalf("预置设置失败: %v", err)
	}
	if err := e.store.SetSetting("theme", "dark"); err != nil {
		t.Fatalf("预置设置失败: %v", err)
	}
	if err := e.store.SaveAlertEmail(&config.AlertEmailConfig{
		Enabled: true, Host: "smtp.keep", Port: "587", Username: "u", Password: "keep-pass",
		FromAddr: "f@example.com", ToAddr: "t@example.com",
	}); err != nil {
		t.Fatalf("预置邮件告警失败: %v", err)
	}
	if err := e.store.SaveAlertWebhook(&config.AlertWebhookConfig{
		Enabled: true, URL: "https://keep.example.com/hook", Channel: "dingtalk",
	}); err != nil {
		t.Fatalf("预置 Webhook 告警失败: %v", err)
	}
	if err := e.store.ReplaceScannedResources("tc_lighthouse", "ap-guangzhou", []config.ScannedResource{
		{CloudType: "tc_lighthouse", Region: "ap-guangzhou", ResourceID: "lhins-scan", ResourceName: "扫描"},
	}); err != nil {
		t.Fatalf("预置扫描结果失败: %v", err)
	}
	if err := e.store.AddSyncLog(config.SyncLog{Timestamp: time.Now(), Target: "t", Domain: "keep.example.com", Result: "success"}); err != nil {
		t.Fatalf("预置同步日志失败: %v", err)
	}
}

// assertImportRolledBack 断言失败注入后旧数据库、旧运行时状态与计数器全部未变。
func assertImportRolledBack(t *testing.T, e *testEnv, before importState) {
	t.Helper()
	after := captureImportState(t, e)
	if len(after.targets) != len(before.targets) || after.targets[0].ResourceID != before.targets[0].ResourceID {
		t.Errorf("旧目标未保留: %+v", after.targets)
	}
	if len(after.rules) != len(before.rules) || after.rules[0].Host != before.rules[0].Host {
		t.Errorf("旧规则未保留: %+v", after.rules)
	}
	if after.settings["tag"] != before.settings["tag"] || after.settings["theme"] != before.settings["theme"] {
		t.Errorf("旧设置未保留: %+v", after.settings)
	}
	if after.email.Host != before.email.Host || after.email.Password != before.email.Password || !after.email.Enabled {
		t.Errorf("旧邮件告警未保留: %+v", after.email)
	}
	if after.webhook.URL != before.webhook.URL || !after.webhook.Enabled {
		t.Errorf("旧 Webhook 告警未保留: %+v", after.webhook)
	}
	if after.scanned != before.scanned {
		t.Errorf("旧扫描缓存未保留: %d → %d", before.scanned, after.scanned)
	}
	if after.logs != before.logs {
		t.Errorf("同步日志数量变化: %d → %d", before.logs, after.logs)
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("失败导入不得发布运行时状态，实际 %d 次", got)
	}
	if st := e.snapshot(); st != nil {
		t.Errorf("失败导入不得发布任何运行时状态，实际 %+v", st)
	}
}

// TestConfigImportFailureInjection 逐阶段失败注入：
// 清表、目标插入、规则插入、settings、email、webhook、scanned_resources 清空。
//
// 每项都断言旧数据库完整、旧 RuntimeState 指针仍生效、未发布新状态、未返回成功。
func TestConfigImportFailureInjection(t *testing.T) {
	cases := map[string]struct {
		trigger string
		body    string
	}{
		"清表失败": {
			trigger: `CREATE TRIGGER fail_rule_delete BEFORE DELETE ON rules BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`,
			body:    bundleWith(`[]`, `[]`),
		},
		"目标插入失败": {
			trigger: `CREATE TRIGGER fail_target_insert BEFORE INSERT ON targets BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`,
			body:    bundleWith(`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"new"}]`, `[]`),
		},
		"规则插入失败": {
			trigger: `CREATE TRIGGER fail_rule_insert BEFORE INSERT ON rules BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`,
			body: bundleWith(`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"new"}]`,
				`[{"host":"a.example.com","protocol":"TCP","ports":"80","action":"ACCEPT","target_export_ids":[1],"comment":"","enable_ipv6":false}]`),
		},
		"settings 写入失败": {
			trigger: `CREATE TRIGGER fail_settings_insert BEFORE INSERT ON settings BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`,
			body:    validBundle(),
		},
		"email 写入失败": {
			trigger: `CREATE TRIGGER fail_email_insert BEFORE INSERT ON alert_email BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`,
			body:    validBundle(),
		},
		"webhook 写入失败": {
			trigger: `CREATE TRIGGER fail_webhook_insert BEFORE INSERT ON alert_webhook BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`,
			body:    validBundle(),
		},
		"scanned_resources 清空失败": {
			trigger: `CREATE TRIGGER fail_scanned_delete BEFORE DELETE ON scanned_resources BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`,
			body:    validBundle(),
		},
		"commit 失败": {
			trigger: `CREATE TRIGGER fail_rules_commit AFTER INSERT ON rules BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`,
			body: bundleWith(`[]`,
				`[{"host":"a.example.com","protocol":"TCP","ports":"80","action":"ACCEPT","target_export_ids":[],"comment":"","enable_ipv6":false}]`),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			seedImportBaseline(t, e)
			before := captureImportState(t, e)
			e.execRaw(t, tc.trigger)

			w := e.do(t, http.MethodPost, "/api/config/import", tc.body)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("状态码 = %d, want 500; body=%s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "fixture failure") {
				t.Errorf("500 不得回显底层错误: %s", w.Body.String())
			}
			assertImportRolledBack(t, e, before)
		})
	}
}

// TestConfigImportCandidateConstructionFailureRollsBack 候选 Provider 构造失败必须回滚且不发布。
//
// 说明：HTTP 路径上「未注册云类型」在打开写事务前就被预校验拒绝（见
// TestConfigImportInvalidInputNoWriteNoReload），因此这里直接对协调器注入一个
// 构造失败的候选 builder，验证「事务内候选构造失败 → 完整回滚 → 不 apply」
// 这条防御性路径，以及 400/500 的错误分类边界。
func TestConfigImportCandidateConstructionFailureRollsBack(t *testing.T) {
	e := newTestEnv(t)
	seedImportBaseline(t, e)
	before := captureImportState(t, e)

	applied := 0
	failing := NewConfigCoordinator(e.store, func(*config.BusinessSnapshot, syncer.BreakerPolicy) (Candidate, error) {
		return Candidate{}, ErrRuntimeCandidateUnsupported
	}, func(Candidate) error {
		applied++
		return nil
	})

	err := failing.Mutate(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		if cerr := e.store.DeleteImportOwnedTablesTx(ctx, tx); cerr != nil {
			return cerr
		}
		_, aerr := e.store.AddTargetTx(ctx, tx, config.TargetConfig{
			CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "should-rollback",
		})
		return aerr
	})
	if err == nil {
		t.Fatal("候选构造失败必须向外返回错误")
	}
	if !errors.Is(err, ErrRuntimeCandidateUnsupported) {
		t.Errorf("错误应可判定为请求侧: %v", err)
	}
	if applied != 0 {
		t.Errorf("候选构造失败不得 apply，实际 %d 次", applied)
	}
	assertImportRolledBack(t, e, before)
}

// TestConfigImportCommitFailureDoesNotApply commit 失败同样不得 apply。
func TestConfigImportCommitFailureDoesNotApply(t *testing.T) {
	e := newTestEnv(t)
	seedImportBaseline(t, e)
	before := captureImportState(t, e)

	// AFTER INSERT 触发器在事务提交时触发：使 commit 阶段返回错误
	e.execRaw(t, `CREATE TRIGGER fail_commit AFTER INSERT ON rules BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`)

	body := bundleWith(`[]`,
		`[{"host":"a.example.com","protocol":"TCP","ports":"80","action":"ACCEPT","target_export_ids":[],"comment":"","enable_ipv6":false}]`)
	w := e.do(t, http.MethodPost, "/api/config/import", body)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	assertImportRolledBack(t, e, before)
}

// TestConfigImportOldRuntimeStateKeepsWorking 导入失败时旧 RuntimeState 指针必须仍然可用。
func TestConfigImportOldRuntimeStateKeepsWorking(t *testing.T) {
	e := newTestEnv(t)
	rc := config.RuntimeConfig{
		Tag: "old-tag", Interval: 5 * time.Minute, DNS: "223.5.5.5", DNSTimeout: 10 * time.Second,
		DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: true, Theme: "light",
		Targets: []config.TargetConfig{{ID: 1, CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "lhins-a"}},
	}
	resetEnvRuntimeForTest(t, e, rc)
	oldState := e.snapshot()
	if oldState == nil {
		t.Fatal("初始运行时状态未发布")
	}

	e.execRaw(t, `CREATE TRIGGER fail_target_insert BEFORE INSERT ON targets BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`)
	body := bundleWith(`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"new"}]`, `[]`)
	if w := e.do(t, http.MethodPost, "/api/config/import", body); w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want 500; body=%s", w.Code, w.Body.String())
	}

	if got := e.snapshot(); got != oldState {
		t.Errorf("导入失败后旧 RuntimeState 指针必须保持: %p → %p", oldState, got)
	}
	if got := e.snapshot().Config.Tag; got != "old-tag" {
		t.Errorf("旧运行时配置被破坏: tag=%q", got)
	}
}
