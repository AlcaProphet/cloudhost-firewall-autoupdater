package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// TestRuleCreateNormalizes ICMP 归一化、Trim、大写与空目标数组语义
func TestRuleCreateNormalizes(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodPost, "/api/rules",
		`{"host":" api.example.com ","protocol":" tcp ","ports":" 443 ","action":"accept","targets":[],"comment":"生产 API","enable_ipv6":true}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("合法事务应只触发一次运行时更新，实际 %d", got)
	}

	rules, err := e.store.GetRules()
	if err != nil || len(rules) != 1 {
		t.Fatalf("GetRules = %d 条, err=%v", len(rules), err)
	}
	got := rules[0]
	if got.Host != "api.example.com" || got.Protocol != "TCP" || got.Ports != "443" || got.Action != "ACCEPT" {
		t.Errorf("归一化结果不正确: %+v", got)
	}
	if len(got.Targets) != 0 {
		t.Errorf("空数组必须保留“适用于全部目标”语义: %+v", got.Targets)
	}
	if !got.EnableIPv6 || got.Comment != "生产 API" {
		t.Errorf("comment/enable_ipv6 不应被改动: %+v", got)
	}
}

// TestRuleCreateICMPPortsAreAll ICMP 的 ports 统一归一化为 ALL
func TestRuleCreateICMPPortsAreAll(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodPost, "/api/rules",
		`{"host":"icmp.example.com","protocol":"icmp","ports":"80","action":"drop","targets":[]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	rules, _ := e.store.GetRules()
	if len(rules) != 1 || rules[0].Ports != "ALL" || rules[0].Protocol != "ICMP" || rules[0].Action != "DROP" {
		t.Errorf("ICMP 归一化错误: %+v", rules)
	}
}

// TestRuleCreateValidationErrors 规则字段与引用校验：400、零写入、零 reload
func TestRuleCreateValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"host 空白", `{"host":"  ","protocol":"TCP","ports":"80","action":"ACCEPT","targets":[]}`},
		{"未知协议", `{"host":"a.example.com","protocol":"SCTP","ports":"80","action":"ACCEPT","targets":[]}`},
		{"未知动作", `{"host":"a.example.com","protocol":"TCP","ports":"80","action":"ALLOW","targets":[]}`},
		{"非 ICMP 空端口", `{"host":"a.example.com","protocol":"TCP","ports":"  ","action":"ACCEPT","targets":[]}`},
		{"空目标数组外还缺字段", `{"protocol":"TCP","ports":"80","action":"ACCEPT","targets":[]}`},
		{"提交数据库 ID", `{"id":3,"host":"a.example.com","protocol":"TCP","ports":"80","action":"ACCEPT","targets":[]}`},
		{"未知字段", `{"host":"a.example.com","protocol":"TCP","ports":"80","action":"ACCEPT","targets":[],"extra":1}`},
		{"目标 ID 为 0", `{"host":"a.example.com","protocol":"TCP","ports":"80","action":"ACCEPT","targets":[0]}`},
		{"目标 ID 为负", `{"host":"a.example.com","protocol":"TCP","ports":"80","action":"ACCEPT","targets":[-3]}`},
		{"目标 ID 重复", `{"host":"a.example.com","protocol":"TCP","ports":"80","action":"ACCEPT","targets":[1,1]}`},
		{"引用不存在目标", `{"host":"a.example.com","protocol":"TCP","ports":"80","action":"ACCEPT","targets":[9999]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			w := e.do(t, http.MethodPost, "/api/rules", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if rules, err := e.store.GetRules(); err != nil || len(rules) != 0 {
				t.Errorf("非法输入不应写库: %v %v", rules, err)
			}
			if got := e.applyCount(); got != 0 {
				t.Errorf("非法输入不应触发运行时更新，实际 %d", got)
			}
		})
	}
}

// TestRuleTargetsMustBeExplicit 规则 targets 必须显式提供：省略字段返回 400，
// 且不得把已有规则静默扩大为“适用于全部目标”（Build6 §4.3、AGENTS §9.1）。
func TestRuleTargetsMustBeExplicit(t *testing.T) {
	e := newTestEnv(t)
	targetID := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-1")
	ruleID := e.seedRule(t, config.DomainRule{
		Host: "keep.example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
		Targets: []int{targetID},
	})

	// POST 省略 targets → 400，零写入零发布
	w := e.do(t, http.MethodPost, "/api/rules",
		`{"host":"a.example.com","protocol":"TCP","ports":"80","action":"ACCEPT"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("省略 targets 的 POST 状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if rules, err := e.store.GetRules(); err != nil || len(rules) != 1 {
		t.Errorf("拒绝后规则不应变化: %v %v", rules, err)
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("拒绝请求不应发布运行时状态，实际 %d", got)
	}

	// PUT 省略 targets → 400，且原规则仍只引用原目标（不得被扩大为全部目标）
	w = e.do(t, http.MethodPut, pathWithID("/api/rules/", ruleID),
		`{"host":"keep.example.com","protocol":"TCP","ports":"443","action":"ACCEPT"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("省略 targets 的 PUT 状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	rules, err := e.store.GetRules()
	if err != nil || len(rules) != 1 {
		t.Fatalf("GetRules = %d 条, err=%v", len(rules), err)
	}
	if len(rules[0].Targets) != 1 || rules[0].Targets[0] != targetID {
		t.Errorf("省略 targets 不得把规则扩大为全部目标: %+v", rules[0].Targets)
	}

	// 显式空数组仍表示“适用于全部目标”
	w = e.do(t, http.MethodPut, pathWithID("/api/rules/", ruleID),
		`{"host":"keep.example.com","protocol":"TCP","ports":"443","action":"ACCEPT","targets":[]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("显式空数组 PUT 状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	rules, err = e.store.GetRules()
	if err != nil || len(rules) != 1 || len(rules[0].Targets) != 0 {
		t.Errorf("显式空数组必须保留“适用于全部目标”语义: %+v (%v)", rules, err)
	}
}

// TestRuleCreateWithExistingTargets 引用真实存在的目标可写入
func TestRuleCreateWithExistingTargets(t *testing.T) {
	e := newTestEnv(t)
	id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-1")

	w := e.do(t, http.MethodPost, "/api/rules",
		`{"host":"a.example.com","protocol":"TCP+UDP","ports":"443,80","action":"ACCEPT","targets":[`+strconv.Itoa(id)+`]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	rules, _ := e.store.GetRules()
	if len(rules) != 1 || len(rules[0].Targets) != 1 || rules[0].Targets[0] != id {
		t.Errorf("规则引用未正确保存: %+v", rules)
	}
}

// TestRuleUpdateAndDeleteNotFound 更新/删除不存在的规则返回 404
func TestRuleUpdateAndDeleteNotFound(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodPut, "/api/rules/9999",
		`{"host":"a.example.com","protocol":"TCP","ports":"80","action":"ACCEPT","targets":[]}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("PUT 状态码 = %d, want 404; body=%s", w.Code, w.Body.String())
	}
	w = e.do(t, http.MethodDelete, "/api/rules/9999", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("DELETE 状态码 = %d, want 404; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("404 不应触发运行时更新，实际 %d", got)
	}
}

// TestRuleUpdateRejectsUnknownTargetReference 更新引用不存在的目标必须 400 且不覆盖既有规则
func TestRuleUpdateRejectsUnknownTargetReference(t *testing.T) {
	e := newTestEnv(t)
	id := e.seedRule(t, config.DomainRule{Host: "old.example.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT"})

	w := e.do(t, http.MethodPut, pathWithID("/api/rules/", id),
		`{"host":"new.example.com","protocol":"TCP","ports":"81","action":"ACCEPT","targets":[9999]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	rules, _ := e.store.GetRules()
	if len(rules) != 1 || rules[0].Host != "old.example.com" {
		t.Errorf("校验失败不应覆盖既有规则: %+v", rules)
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("校验失败不应触发运行时更新，实际 %d", got)
	}
}

// TestRuleDeleteSuccess 删除存在的规则返回 200 且一次 reload
func TestRuleDeleteSuccess(t *testing.T) {
	e := newTestEnv(t)
	id := e.seedRule(t, config.DomainRule{Host: "a.example.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT"})

	w := e.do(t, http.MethodDelete, pathWithID("/api/rules/", id), "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("删除成功应触发一次运行时更新，实际 %d", got)
	}
	if rules, _ := e.store.GetRules(); len(rules) != 0 {
		t.Errorf("规则应被删除: %+v", rules)
	}
}

// TestRuleTargetReferenceIsExact 引用关系精确：只引用目标 B 的规则不应阻止删除目标 A
func TestRuleTargetReferenceIsExact(t *testing.T) {
	e := newTestEnv(t)
	a := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")
	b := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-b")
	e.seedRule(t, config.DomainRule{Host: "b.example.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT", Targets: []int{b}})

	if w := e.do(t, http.MethodDelete, pathWithID("/api/targets/", a), ""); w.Code != http.StatusOK {
		t.Fatalf("未被引用的目标 A 应可删除: %d %s", w.Code, w.Body.String())
	}
	if w := e.do(t, http.MethodDelete, pathWithID("/api/targets/", b), ""); w.Code != http.StatusConflict {
		t.Fatalf("被引用的目标 B 应返回 409: %d %s", w.Code, w.Body.String())
	}
}

// TestRuleInternalErrorUsesSafeText 规则写入失败返回 500 安全文案
func TestRuleInternalErrorUsesSafeText(t *testing.T) {
	e := newTestEnv(t)
	e.execRaw(t, `CREATE TRIGGER fail_rule_insert BEFORE INSERT ON rules BEGIN SELECT RAISE(ABORT, 'fixture-secret-value'); END;`)

	w := e.do(t, http.MethodPost, "/api/rules",
		`{"host":"a.example.com","protocol":"TCP","ports":"80","action":"ACCEPT","targets":[]}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "fixture-secret-value") {
		t.Errorf("500 不得回显底层错误: %s", w.Body.String())
	}
}
