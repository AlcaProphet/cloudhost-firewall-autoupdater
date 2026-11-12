package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// 本文件是 Build8 Step 2 的 API 回归：双功能开关、EdgeOne TAG/间隔的字符串
// 设置、解析后的运行时值、事务内 TAG 互斥、并发保存与 v3 兼容初始化。

func decodeSettings(t *testing.T, body []byte) map[string]string {
	t.Helper()
	var settings map[string]string
	if err := json.Unmarshal(body, &settings); err != nil {
		t.Fatalf("解析设置响应失败: %v", err)
	}
	return settings
}

func TestPutSettingsFeatureFieldsAndRuntime(t *testing.T) {
	e := newTestEnv(t)

	w := e.do(t, http.MethodPut, "/api/settings",
		`{"dns_enabled":"false","edgeone_enabled":"true","edgeone_tag":"edge-tag","edgeone_interval":"36h"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("保存状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Fatalf("合法保存应只发布一次，实际 %d", got)
	}
	st := e.snapshot()
	if st == nil || st.Config.DNSEnabled || !st.Config.EdgeOneEnabled ||
		st.Config.EdgeOneTag != "edge-tag" || st.Config.EdgeOneInterval != 36*time.Hour {
		t.Fatalf("运行时未发布新设置: %+v", st)
	}

	w = e.do(t, http.MethodGet, "/api/settings", "")
	got := decodeSettings(t, w.Body.Bytes())
	if got["dns_enabled"] != "false" || got["edgeone_enabled"] != "true" ||
		got["edgeone_tag"] != "edge-tag" || got["edgeone_interval"] != "36h" {
		t.Errorf("GET 未回显新设置: %+v", got)
	}

	for _, interval := range []string{"1h30m", "1.5h"} {
		w = e.do(t, http.MethodPut, "/api/settings", `{"edgeone_interval":"`+interval+`"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("保存 %s 状态码 = %d, want 200; body=%s", interval, w.Code, w.Body.String())
		}
		if st := e.snapshot(); st == nil || st.Config.EdgeOneInterval != 90*time.Minute {
			t.Errorf("%s 未解析为 90m: %+v", interval, st)
		}
	}
	if got := e.applyCount(); got != 3 {
		t.Errorf("三次合法保存应发布三次，实际 %d", got)
	}

	before := e.snapshot()
	beforeApplies := e.applyCount()
	for name, body := range map[string]string{
		"间隔零":             `{"edgeone_interval":"0s"}`,
		"间隔负数":            `{"edgeone_interval":"-5m"}`,
		"间隔不可解析":          `{"edgeone_interval":"abc"}`,
		"间隔空白":            `{"edgeone_interval":"   "}`,
		"间隔空串":            `{"edgeone_interval":""}`,
		"间隔 JSON 数字":      `{"edgeone_interval":72}`,
		"DNS 开关 JSON 布尔":  `{"dns_enabled":true}`,
		"DNS 开关大写":        `{"dns_enabled":"TRUE"}`,
		"EdgeOne 开关数字":    `{"edgeone_enabled":"1"}`,
		"EdgeOne TAG 空白":  `{"edgeone_tag":"   "}`,
		"EdgeOne TAG 方括号": `{"edgeone_tag":"[bad]"}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := e.do(t, http.MethodPut, "/api/settings", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if got := e.applyCount(); got != beforeApplies {
				t.Errorf("非法保存不得发布，实际 %d -> %d", beforeApplies, got)
			}
			if after := e.snapshot(); after == nil || !reflect.DeepEqual(after.Config, before.Config) {
				t.Errorf("非法保存不得改变运行时状态: %+v -> %+v", before, after)
			}
		})
	}
}

func TestPutSettingsRejectsExplicitNullFeatureFields(t *testing.T) {
	for _, tc := range []struct {
		name, field, body string
	}{
		{"DNS 开关", "dns_enabled", `{"dns_enabled":null,"theme":"dark"}`},
		{"EdgeOne 开关", "edgeone_enabled", `{"edgeone_enabled":null,"theme":"dark"}`},
		{"EdgeOne TAG", "edgeone_tag", `{"edgeone_tag":null,"theme":"dark"}`},
		{"EdgeOne 间隔", "edgeone_interval", `{"edgeone_interval":null,"theme":"dark"}`},
		{"后续值不能覆盖 null", "edgeone_interval", `{"edgeone_interval":null,"edgeone_interval":"36h","theme":"dark"}`},
		{"后续同名变体不能覆盖 null", "dns_enabled", `{"dns_enabled":null,"DNS_ENABLED":"false","theme":"dark"}`},
		{"重复字段末尾为 null", "edgeone_enabled", `{"edgeone_enabled":"true","edgeone_enabled":null,"theme":"dark"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			before, err := e.store.GetSettings()
			if err != nil {
				t.Fatalf("读取原设置失败: %v", err)
			}
			w := e.do(t, http.MethodPut, "/api/settings", tc.body)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), tc.field) {
				t.Errorf("应返回 HTTP 400 和字段名 %s，实际 %d: %s", tc.field, w.Code, w.Body.String())
			}
			after, err := e.store.GetSettings()
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Errorf("拒绝请求后数据库设置应保持原样，err=%v", err)
			}
			if got := e.applyCount(); got != 0 {
				t.Errorf("拒绝请求后发布次数 = %d，want 0", got)
			}
		})
	}
}

func TestPutSettingsTagConflictAfterMerge(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodPut, "/api/settings", `{"tag":"alpha","edgeone_tag":"beta"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("初始保存状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Fatalf("初始保存发布次数 = %d, want 1", got)
	}

	for name, body := range map[string]string{
		"单独改 DNS 撞车":     `{"tag":"beta"}`,
		"单独改 EdgeOne 撞车": `{"edgeone_tag":"alpha"}`,
		"同时提交相同值":        `{"tag":"same","edgeone_tag":"same"}`,
		"去空白后撞车":         `{"tag":" beta "}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := e.do(t, http.MethodPut, "/api/settings", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if got := e.applyCount(); got != 1 {
				t.Errorf("冲突保存不得发布，实际 %d", got)
			}
			settings, err := e.store.GetSettings()
			if err != nil {
				t.Fatalf("GetSettings 失败: %v", err)
			}
			if settings["tag"] != "alpha" || settings["edgeone_tag"] != "beta" {
				t.Errorf("冲突保存必须完整回滚: %+v", settings)
			}
			if st := e.snapshot(); st == nil || st.Config.Tag != "alpha" || st.Config.EdgeOneTag != "beta" {
				t.Errorf("冲突保存不得改变运行时快照: %+v", st)
			}
		})
	}

	// 同一请求同时换两项为不同值：一次提交、一次发布。
	w = e.do(t, http.MethodPut, "/api/settings", `{"tag":"beta","edgeone_tag":"alpha"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("同时换标签状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 2 {
		t.Errorf("同时换标签应只发布一次，实际 %d", got)
	}
	st := e.snapshot()
	if st == nil || st.Config.Tag != "beta" || st.Config.EdgeOneTag != "alpha" {
		t.Errorf("运行时未取得交换后的 TAG: %+v", st)
	}
	settings, err := e.store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if settings["tag"] != "beta" || settings["edgeone_tag"] != "alpha" {
		t.Errorf("数据库未取得交换后的 TAG: %+v", settings)
	}
}

func TestPutSettingsConcurrentTagChangesSerialized(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodPut, "/api/settings", `{"tag":"alpha","edgeone_tag":"beta"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("初始保存状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	start := make(chan struct{})
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i, body := range []string{`{"tag":"gamma"}`, `{"edgeone_tag":"gamma"}`} {
		wg.Add(1)
		go func(i int, body string) {
			defer wg.Done()
			<-start
			codes[i] = e.do(t, http.MethodPut, "/api/settings", body).Code
		}(i, body)
	}
	close(start)
	wg.Wait()

	success, rejected := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			success++
		case http.StatusBadRequest:
			rejected++
		default:
			t.Fatalf("并发保存返回意外状态码: %v", codes)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("并发保存应恰好一成一败，实际 %v", codes)
	}
	if got := e.applyCount(); got != 2 {
		t.Errorf("并发保存只应有一个成功提交，总发布次数 = %d, want 2", got)
	}
	settings, err := e.store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	tag, edge := settings["tag"], settings["edgeone_tag"]
	if tag == edge {
		t.Fatalf("并发保存后 TAG 不得相同: %+v", settings)
	}
	if tag != "gamma" && edge != "gamma" {
		t.Errorf("至少一项应取得 gamma: %+v", settings)
	}
	if tag != "gamma" && tag != "alpha" {
		t.Errorf("DNS TAG 应为 gamma 或原值 alpha: %q", tag)
	}
	if edge != "gamma" && edge != "beta" {
		t.Errorf("EdgeOne TAG 应为 gamma 或原值 beta: %q", edge)
	}
}

func TestV3ImportBootstrapsEdgeOneTag(t *testing.T) {
	cases := map[string]struct {
		dnsTag   string
		wantEdge string
	}{
		"旧 DNS TAG 为 edgeone": {dnsTag: "edgeone", wantEdge: config.LegacyEdgeOneTag},
		"普通 DNS TAG":          {dnsTag: "my-dns", wantEdge: config.DefaultEdgeOneTag},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			settings := `{"credentials":{"tencent":{"secret_id":"","secret_key":""},` +
				`"aliyun":{"access_key_id":"","access_key_secret":""}},` +
				`"tag":"` + tc.dnsTag + `","interval":"5m","dns":"223.5.5.5","dns_timeout":"10s",` +
				`"dns_fail_threshold":5,"log_level":"info","sync_enabled":true,"theme":"light"}`
			body := bundleWithSettings(`[]`, `[]`, settings)
			w := e.do(t, http.MethodPost, "/api/config/import", body)
			if w.Code != http.StatusOK {
				t.Fatalf("v3 导入状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
			}
			if got := e.applyCount(); got != 1 {
				t.Errorf("v3 导入应只发布一次，实际 %d", got)
			}
			stored, err := e.store.GetSettings()
			if err != nil {
				t.Fatalf("GetSettings 失败: %v", err)
			}
			if stored["tag"] != tc.dnsTag {
				t.Errorf("DNS TAG 必须保留 %q，实际 %q", tc.dnsTag, stored["tag"])
			}
			if stored["edgeone_tag"] != tc.wantEdge {
				t.Errorf("EdgeOne TAG = %q, want %q", stored["edgeone_tag"], tc.wantEdge)
			}
			if stored["dns_enabled"] != "true" || stored["edgeone_enabled"] != "false" ||
				stored["edgeone_interval"] != config.DefaultEdgeOneIntervalText {
				t.Errorf("v3 导入未补双功能默认值: %+v", stored)
			}
			st := e.snapshot()
			if st == nil || !st.Config.DNSEnabled || st.Config.EdgeOneEnabled ||
				st.Config.EdgeOneTag != tc.wantEdge || st.Config.EdgeOneInterval != config.DefaultEdgeOneInterval {
				t.Errorf("v3 导入运行时状态不正确: %+v", st)
			}
		})
	}
}

// TestPutSettingsRejectsUnknownAfterFeatureExpansion 确认新增已知键没有放宽严格 DTO。
func TestPutSettingsRejectsUnknownAfterFeatureExpansion(t *testing.T) {
	e := newTestEnv(t)
	for _, body := range []string{
		`{"sync_enabled":"false"}`,
		`{"edgeone_sites":[]}`,
		`{"unknown_key":"x"}`,
	} {
		w := e.do(t, http.MethodPut, "/api/settings", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body=%s 状态码 = %d, want 400", body, w.Code)
		}
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("被拒绝的请求不得发布，实际 %d", got)
	}
}
