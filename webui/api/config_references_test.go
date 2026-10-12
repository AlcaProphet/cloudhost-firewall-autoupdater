package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/app"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

type i8r09DBView struct {
	Targets  []config.TargetConfig
	Rules    []config.DomainRule
	Settings map[string]string
	Alerts   *config.AlertsSnapshot
	Scanned  []config.ScannedResource
}

func i8r09ReadDB(t *testing.T, e *testEnv) i8r09DBView {
	t.Helper()
	var view i8r09DBView
	var err error
	view.Targets, err = e.store.GetTargets()
	if err != nil {
		t.Fatal(err)
	}
	view.Rules, err = e.store.GetRules()
	if err != nil {
		t.Fatal(err)
	}
	view.Settings, err = e.store.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	view.Alerts, err = e.store.LoadAlertsSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	view.Scanned, err = e.store.GetScannedResources(string(config.CloudTCCVM))
	if err != nil {
		t.Fatal(err)
	}
	return view
}
func i8r09Seed(t *testing.T, e *testEnv) (a, b, c, broken, healthy int) {
	t.Helper()
	a = e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "resource-sentinel-a")
	b = e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "resource-sentinel-b")
	c = e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "resource-sentinel-c")
	broken = e.seedRule(t, config.DomainRule{Host: "host-sentinel.example", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{a, b}})
	healthy = e.seedRule(t, config.DomainRule{Host: "healthy.example", Protocol: "TCP", Ports: "80", Action: "ACCEPT", Targets: []int{b}})
	body := strings.Replace(alertsBody("smtp-sentinel.example", "password-sentinel", "", ""), `"dns_failed_enabled":false`, `"dns_failed_enabled":true`, 1)
	body = strings.Replace(body, `"email":{"enabled":false`, `"email":{"enabled":true`, 1)
	w := e.do(t, http.MethodPut, "/api/alerts", body)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = e.do(t, http.MethodPut, "/api/settings", `{"tag":"old-tag","log_level":"debug","tc_access_key":"credential-sentinel"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := e.store.ReplaceScannedResources(string(config.CloudTCCVM), "ap-guangzhou", []config.ScannedResource{{ResourceID: "cached-resource", Region: "ap-guangzhou"}}); err != nil {
		t.Fatal(err)
	}
	e.deps.Syncer = syncer.New(e.runtime)
	e.execRaw(t, fmt.Sprintf("DELETE FROM targets WHERE id=%d", a))
	return
}

// TestI8R09MutationRejectsCorruptFinalSnapshot 所有普通写入口必须因损坏最终快照回滚，并且不构造或发布候选。
func TestI8R09MutationRejectsCorruptFinalSnapshot(t *testing.T) {
	for _, action := range []string{"settings", "add-rule", "update-rule", "delete-rule", "add-target", "update-target", "delete-target", "alerts", "pause", "resume"} {
		t.Run(action, func(t *testing.T) {
			e := newTestEnv(t)
			_, b, c, _, healthy := i8r09Seed(t, e)
			oldDB := i8r09ReadDB(t, e)
			oldState := e.snapshot()
			oldAlerts := e.alerts.Current()
			oldLevel := app.LogLevelVar.Level()
			oldApplies := e.applyCount()
			builds, wakes := 0, 0
			build := e.deps.Coord.buildCandidate
			e.deps.Coord.buildCandidate = func(s *config.BusinessSnapshot, p syncer.BreakerPolicy) (Candidate, error) {
				builds++
				return build(s, p)
			}
			probe := runtimeWakeProbe{fn: func() { wakes++ }}
			e.deps.Health = probe
			e.deps.Push = probe
			var method, path, body string
			switch action {
			case "settings":
				method, path, body = http.MethodPut, "/api/settings", `{"tag":"new-tag","log_level":"error","tc_access_key":"changed-secret"}`
			case "add-rule":
				method, path, body = http.MethodPost, "/api/rules", fmt.Sprintf(`{"host":"new.example","protocol":"TCP","ports":"443","action":"ACCEPT","targets":[%d]}`, b)
			case "update-rule":
				method, path, body = http.MethodPut, pathWithID("/api/rules/", healthy), fmt.Sprintf(`{"host":"changed.example","protocol":"TCP","ports":"443","action":"ACCEPT","targets":[%d]}`, b)
			case "delete-rule":
				method, path = http.MethodDelete, pathWithID("/api/rules/", healthy)
			case "add-target":
				method, path, body = http.MethodPost, "/api/targets", `{"cloud_type":"tc_cvm","region":"ap-guangzhou","resource_id":"new-target"}`
			case "update-target":
				method, path, body = http.MethodPut, pathWithID("/api/targets/", b), `{"cloud_type":"tc_cvm","region":"ap-beijing","resource_id":"changed-target"}`
			case "delete-target":
				method, path = http.MethodDelete, pathWithID("/api/targets/", c)
			case "alerts":
				method, path, body = http.MethodPut, "/api/alerts", alertsBody("changed.example", "changed-password", "", "")
			case "pause":
				method, path, body = http.MethodPost, "/api/sync/pause", `{}`
			case "resume":
				method, path, body = http.MethodPost, "/api/sync/resume", `{}`
			}
			w := e.do(t, method, path, body)
			if w.Code != 500 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
				t.Fatalf("损坏库必须安全500: status=%d body=%s", w.Code, w.Body.String())
			}
			var got map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, map[string]string{"error": "保存失败"}) {
				t.Fatalf("响应泄露细节: %v", got)
			}
			if !reflect.DeepEqual(i8r09ReadDB(t, e), oldDB) {
				t.Fatal("失败变更未完整回滚")
			}
			if e.snapshot() != oldState || e.applyCount() != oldApplies || builds != 0 || wakes != 0 || app.LogLevelVar.Level() != oldLevel {
				t.Fatalf("失败变更改变运行时或发布副作用: builds=%d wakes=%d", builds, wakes)
			}
			current := e.alerts.Current()
			if current.email != oldAlerts.email || current.webhook != oldAlerts.webhook || !reflect.DeepEqual(current.events, oldAlerts.events) {
				t.Fatal("旧告警集合被替换")
			}
		})
	}
}

// TestI8R09ExportRejectsCorruptReferences 导出失败只返回安全 JSON，不发送敏感附件；普通列表仍能用于定位。
func TestI8R09ExportRejectsCorruptReferences(t *testing.T) {
	e := newTestEnv(t)
	a, _, _, broken, _ := i8r09Seed(t, e)
	oldDB := i8r09ReadDB(t, e)
	oldState := e.snapshot()
	oldApplies := e.applyCount()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	w := e.do(t, http.MethodPost, "/api/config/export", `{}`)
	if w.Code != 500 || w.Header().Get("Content-Disposition") != "" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("非法导出 status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body, map[string]string{"error": "导出配置失败"}) {
		t.Fatalf("导出错误=%v", body)
	}
	want := fmt.Sprintf("规则 #%d 引用了不存在的目标 #%d", broken, a)
	if !strings.Contains(logs.String(), want) {
		t.Fatalf("日志缺诊断: %s", logs.String())
	}
	for _, value := range []string{"host-sentinel.example", "resource-sentinel", "credential-sentinel", "password-sentinel", "smtp-sentinel.example"} {
		if strings.Contains(logs.String(), value) || strings.Contains(w.Body.String(), value) {
			t.Fatalf("泄露字段值 %q", value)
		}
	}
	for _, path := range []string{"/api/rules", "/api/targets"} {
		if w := e.do(t, http.MethodGet, path, ""); w.Code != 200 {
			t.Fatalf("定位列表被阻断: %s %d", path, w.Code)
		}
	}
	if !reflect.DeepEqual(i8r09ReadDB(t, e), oldDB) || e.snapshot() != oldState || e.applyCount() != oldApplies {
		t.Fatal("只读导出或定位修改了配置")
	}
}

// TestI8R09RepairFinalSnapshot 只对最终完整状态校验，允许本次操作消除全部悬空引用。
func TestI8R09RepairFinalSnapshot(t *testing.T) {
	for _, method := range []string{"update", "delete", "import", "reset"} {
		t.Run(method, func(t *testing.T) {
			e := newTestEnv(t)
			_, b, _, broken, _ := i8r09Seed(t, e)
			oldState := e.snapshot()
			before := e.applyCount()
			var wcode int
			switch method {
			case "update":
				wcode = e.do(t, http.MethodPut, pathWithID("/api/rules/", broken), fmt.Sprintf(`{"host":"repaired.example","protocol":"TCP","ports":"443","action":"ACCEPT","targets":[%d]}`, b)).Code
			case "delete":
				wcode = e.do(t, http.MethodDelete, pathWithID("/api/rules/", broken), "").Code
			case "import":
				wcode = e.do(t, http.MethodPost, "/api/config/import", bundleWith(`[{"export_id":91,"cloud_type":"tc_cvm","region":"ap-beijing","resource_id":"imported-target"}]`, `[{"host":"imported.example","protocol":"TCP","ports":"443","action":"ACCEPT","target_export_ids":[91],"comment":"","enable_ipv6":false}]`)).Code
			case "reset":
				wcode = e.do(t, http.MethodPost, "/api/config/reset", `{}`).Code
			}
			if wcode != 200 || e.applyCount() != before+1 || e.snapshot() == oldState {
				t.Fatalf("修复未提交或未恰好发布一次: code=%d applies=%d", wcode, e.applyCount())
			}
			snap, err := e.store.LoadBusinessSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			targets := map[int]bool{}
			for _, target := range snap.Targets {
				targets[target.ID] = true
			}
			for _, rule := range snap.Rules {
				for _, ref := range rule.Targets {
					if !targets[ref] {
						t.Fatalf("修复后的引用仍无效: %d", ref)
					}
				}
			}
			if method == "update" {
				found := false
				for _, rule := range snap.Rules {
					if rule.ID == broken {
						found = true
						if !reflect.DeepEqual(rule.Targets, []int{b}) {
							t.Fatal("引用被扩大或重写")
						}
					}
				}
				if !found {
					t.Fatal("修复规则丢失")
				}
			}
			w := e.do(t, http.MethodPost, "/api/config/export", `{}`)
			if w.Code != 200 {
				t.Fatal("修复后导出失败", w.Code)
			}
		})
	}
}

// TestI8R09MultipleBrokenRulesCannotPartiallyCommit 多条损坏时逐条修改或删除不得留下部分提交。
func TestI8R09MultipleBrokenRulesCannotPartiallyCommit(t *testing.T) {
	for _, method := range []string{"update", "delete"} {
		t.Run(method, func(t *testing.T) {
			e := newTestEnv(t)
			a, b, _, broken, _ := i8r09Seed(t, e)
			e.seedRule(t, config.DomainRule{Host: "also-broken.example", Protocol: "TCP", Ports: "80", Action: "ACCEPT", Targets: []int{a}})
			before := i8r09ReadDB(t, e)
			state := e.snapshot()
			applies := e.applyCount()
			var code int
			if method == "update" {
				code = e.do(t, http.MethodPut, pathWithID("/api/rules/", broken), fmt.Sprintf(`{"host":"repaired.example","protocol":"TCP","ports":"443","action":"ACCEPT","targets":[%d]}`, b)).Code
			} else {
				code = e.do(t, http.MethodDelete, pathWithID("/api/rules/", broken), "").Code
			}
			if code != 500 || !reflect.DeepEqual(i8r09ReadDB(t, e), before) || e.snapshot() != state || e.applyCount() != applies {
				t.Fatalf("逐条修复必须完整回滚: code=%d", code)
			}
		})
	}
}
