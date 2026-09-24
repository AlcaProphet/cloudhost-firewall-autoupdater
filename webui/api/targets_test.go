package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// TestTargetCreateNormalizesAndPersists 合法目标：Trim 归一化落库且只触发一次运行时更新
func TestTargetCreateNormalizesAndPersists(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodPost, "/api/targets",
		`{"cloud_type":" tc_lighthouse ","region":" ap-guangzhou ","resource_id":" lhins-abc "}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("合法事务应只触发一次运行时更新，实际 %d", got)
	}

	targets, err := e.store.GetTargets()
	if err != nil {
		t.Fatalf("GetTargets 失败: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("目标数 = %d, want 1", len(targets))
	}
	got := targets[0]
	if got.CloudType != config.CloudTCLighthouse || got.Region != "ap-guangzhou" || got.ResourceID != "lhins-abc" {
		t.Errorf("应保存 Trim 后的归一化值: %+v", got)
	}
	if got.ID <= 0 {
		t.Errorf("持久化对象应带数据库 ID: %+v", got)
	}
}

// TestTargetRegionOutsideListAllowed 预填列表外的地域仍允许保存（不调用云 API）
func TestTargetRegionOutsideListAllowed(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodPost, "/api/targets",
		`{"cloud_type":"ali_ecs","region":"xx-unknown-9","resource_id":"sg-1"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("列表外地域应可保存: %d %s", w.Code, w.Body.String())
	}
}

// TestTargetCreateInvalidInputNoWriteNoReload 非法输入：400、零写入、零 reload
func TestTargetCreateInvalidInputNoWriteNoReload(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"未知 cloud_type", `{"cloud_type":"aws_ec2","region":"gz","resource_id":"x"}`},
		{"cloud_type 大小写不符", `{"cloud_type":"TC_CVM","region":"gz","resource_id":"x"}`},
		{"region 空白", `{"cloud_type":"tc_cvm","region":"   ","resource_id":"x"}`},
		{"resource_id 空白", `{"cloud_type":"tc_cvm","region":"gz","resource_id":"\t"}`},
		{"字段全缺", `{}`},
		{"提交数据库 ID", `{"cloud_type":"tc_cvm","region":"gz","resource_id":"x","id":7}`},
		{"未知字段", `{"cloud_type":"tc_cvm","region":"gz","resource_id":"x","extra":1}`},
		{"类型错误", `{"cloud_type":1,"region":"gz","resource_id":"x"}`},
		{"尾随 JSON", `{"cloud_type":"tc_cvm","region":"gz","resource_id":"x"}{}`},
		{"空 body", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			w := e.do(t, http.MethodPost, "/api/targets", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if targets, err := e.store.GetTargets(); err != nil || len(targets) != 0 {
				t.Errorf("非法输入不应写库: %v %v", targets, err)
			}
			if got := e.applyCount(); got != 0 {
				t.Errorf("非法输入不应触发运行时更新，实际 %d", got)
			}
		})
	}
}

// TestTargetCreateOversizeBody 超过 1 MiB 返回 413 且不写库不 reload
func TestTargetCreateOversizeBody(t *testing.T) {
	e := newTestEnv(t)
	body := `{"cloud_type":"tc_cvm","region":"gz","resource_id":"` + strings.Repeat("a", maxJSONBodyBytes) + `"}`
	w := e.do(t, http.MethodPost, "/api/targets", body)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("状态码 = %d, want 413; body=%s", w.Code, w.Body.String())
	}
	if targets, _ := e.store.GetTargets(); len(targets) != 0 {
		t.Errorf("超限请求不应写库")
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("超限请求不应触发运行时更新，实际 %d", got)
	}
}

// TestTargetUpdateNotFound 更新不存在的目标返回 404 且不 reload
func TestTargetUpdateNotFound(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodPut, "/api/targets/9999",
		`{"cloud_type":"tc_cvm","region":"gz","resource_id":"x"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("404 不应触发运行时更新，实际 %d", got)
	}
}

// TestTargetUpdateNormalizes 更新成功：归一化落库且一次 reload
func TestTargetUpdateNormalizes(t *testing.T) {
	e := newTestEnv(t)
	id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-old")

	w := e.do(t, http.MethodPut, pathWithID("/api/targets/", id),
		`{"cloud_type":"ali_swas","region":" cn-hangzhou ","resource_id":" lg-new "}`)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("更新成功应触发一次运行时更新，实际 %d", got)
	}
	targets, _ := e.store.GetTargets()
	if len(targets) != 1 || targets[0].CloudType != config.CloudAliSWAS || targets[0].Region != "cn-hangzhou" || targets[0].ResourceID != "lg-new" {
		t.Errorf("更新结果不正确: %+v", targets)
	}
}

// TestTargetPathIDStrict 路径 ID 宽松值必须 400 且不写库
func TestTargetPathIDStrict(t *testing.T) {
	body := `{"cloud_type":"tc_cvm","region":"gz","resource_id":"x"}`
	for _, raw := range []string{"12abc", "-1", "0", "1e3", "abc"} {
		e := newTestEnv(t)
		w := e.do(t, http.MethodPut, "/api/targets/"+raw, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("PUT id=%q 状态码 = %d, want 400", raw, w.Code)
		}
		w = e.do(t, http.MethodDelete, "/api/targets/"+raw, "")
		if w.Code != http.StatusBadRequest {
			t.Errorf("DELETE id=%q 状态码 = %d, want 400", raw, w.Code)
		}
		if got := e.applyCount(); got != 0 {
			t.Errorf("id=%q 非法路径不应触发运行时更新，实际 %d", raw, got)
		}
	}
}

// TestTargetDeleteReferencedConflict 被规则引用的目标删除必须 409，且不静默删除引用、不扩大规则范围
func TestTargetDeleteReferencedConflict(t *testing.T) {
	e := newTestEnv(t)
	id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-1")
	e.seedRule(t, config.DomainRule{Host: "api.example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{id}})

	w := e.do(t, http.MethodDelete, pathWithID("/api/targets/", id), "")
	if w.Code != http.StatusConflict {
		t.Fatalf("状态码 = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("409 不应触发运行时更新，实际 %d", got)
	}

	targets, _ := e.store.GetTargets()
	if len(targets) != 1 {
		t.Errorf("被引用目标不应被删除: %+v", targets)
	}
	rules, _ := e.store.GetRules()
	if len(rules) != 1 || len(rules[0].Targets) != 1 || rules[0].Targets[0] != id {
		t.Errorf("规则引用不得被静默删除或扩大为全部目标: %+v", rules)
	}
}

// TestTargetDeleteUnreferenced 未被引用的目标可删除且一次 reload
func TestTargetDeleteUnreferenced(t *testing.T) {
	e := newTestEnv(t)
	id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-1")
	other := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-2")
	// 规则只引用 other，删除 id 不应冲突
	e.seedRule(t, config.DomainRule{Host: "api.example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{other}})

	w := e.do(t, http.MethodDelete, pathWithID("/api/targets/", id), "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("删除成功应触发一次运行时更新，实际 %d", got)
	}
	targets, _ := e.store.GetTargets()
	if len(targets) != 1 || targets[0].ID != other {
		t.Errorf("只应删除目标 %d: %+v", id, targets)
	}
}

// TestTargetDeleteNotFound 删除不存在的目标返回 404
func TestTargetDeleteNotFound(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodDelete, "/api/targets/9999", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("404 不应触发运行时更新，实际 %d", got)
	}
}

// TestTargetInternalErrorUsesSafeText 底层写入失败返回 500 安全文案，不回显底层错误
func TestTargetInternalErrorUsesSafeText(t *testing.T) {
	e := newTestEnv(t)
	e.execRaw(t, `CREATE TRIGGER fail_target_insert BEFORE INSERT ON targets BEGIN SELECT RAISE(ABORT, 'fixture-secret-value'); END;`)

	w := e.do(t, http.MethodPost, "/api/targets", `{"cloud_type":"tc_cvm","region":"gz","resource_id":"x"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "fixture-secret-value") {
		t.Errorf("500 不得回显底层错误: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "保存失败") {
		t.Errorf("500 应使用安全通用文案: %s", w.Body.String())
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("提交失败不应触发运行时更新，实际 %d", got)
	}
}

// TestTargetReadInternalErrorUsesSafeText 读取失败返回 500 安全文案
func TestTargetReadInternalErrorUsesSafeText(t *testing.T) {
	e := newTestEnv(t)
	e.execRaw(t, "DROP TABLE targets")

	w := e.do(t, http.MethodGet, "/api/targets", "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "no such table") || strings.Contains(body, "SQL") {
		t.Errorf("500 不得回显数据库错误: %s", body)
	}
	var resp map[string]string
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("响应不是 JSON: %s", body)
	}
	if resp["error"] != "读取目标失败" {
		t.Errorf("错误文案 = %q, want 读取目标失败", resp["error"])
	}
}
