// 正式 handler/SQLite/运行时回归，校验有序诊断与协调器前零副作用拒绝。
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 非 presence 错误不扩展响应；解析/版本入口和领域错误保留首错。
func TestI843ErrorShapes(t *testing.T) {
	e, unchanged := i8111Guard(t)
	cases := []struct{ name, body, want string }{
		{"引用null走领域校验", bundleWith(`[]`, `[{"host":"a.test","protocol":"TCP","ports":"443","action":"ACCEPT","comment":"","enable_ipv6":false,"target_export_ids":[null]}]`), "target"},
		{"类型错误优先于缺失", strings.Replace(v3Bundle(`{}`, v3DefaultEmailJSON, `{"uptime_kuma_push":{"enabled":false,"url":"","interval":"60s"}}`), `"enabled":false`, `"enabled":"false"`, 1), "类型错误"},
		{"重复字段优先于缺失", `{"version":3,"version":3}`, "重复字段"},
		{"语法错误", `{"version":3,`, "JSON 格式错误"},
		{"未知字段", `{"version":3,"unknown":null}`, "未知字段"},
		{"缺失版本", `{}`, "version 字段缺失"},
		{"null版本", `{"version":null}`, "version 字段缺失"},
		{"完整结构非法时长", strings.Replace(validBundle(), `"interval":"5m"`, `"interval":"bad"`, 1), "interval"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := e.do(t, http.MethodPost, "/api/config/import", tc.body)
			var got map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			var message string
			if err := json.Unmarshal(got["error"], &message); err != nil {
				t.Fatal(err)
			}
			if w.Code != 400 || w.Header().Get("Cache-Control") != "no-store" || len(got) != 1 || !strings.Contains(message, tc.want) {
				t.Fatalf("状态/响应形状=%d %s", w.Code, w.Body.String())
			}
			unchanged(t)
		})
	}
}

type i843Reply struct {
	Error     string   `json:"error"`
	Fields    []string `json:"missing_fields"`
	Total     int      `json:"missing_fields_total"`
	Truncated bool     `json:"missing_fields_truncated"`
}

func i843ReplyFor(t *testing.T, e *testEnv, body string) i843Reply {
	t.Helper()
	w := e.do(t, http.MethodPost, "/api/config/import", body)
	if w.Code != 400 {
		t.Fatalf("status=%d", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cache policy")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, structured := raw["missing_fields"]; structured {
		if len(raw) != 4 {
			t.Fatalf("结构错误响应字段=%s", w.Body.String())
		}
		for _, key := range []string{"error", "missing_fields", "missing_fields_total", "missing_fields_truncated"} {
			value, ok := raw[key]
			if !ok || string(value) == "null" {
				t.Fatalf("响应字段 %s 缺失/null", key)
			}
		}
		if string(raw["missing_fields_truncated"]) != "false" && string(raw["missing_fields_truncated"]) != "true" {
			t.Fatal("截断标记不是显式 boolean")
		}
	} else if len(raw) != 1 {
		t.Fatalf("普通错误响应被扩展=%s", w.Body.String())
	}
	var reply i843Reply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}
func TestI843Aggregation(t *testing.T) {
	e, unchanged := i8111Guard(t)
	cases := []struct {
		name, body string
		want       []string
	}{
		{"cross-object", v3Bundle(`{"health_timeout":"10m"}`, `{"enabled":false}`, `{"uptime_kuma_push":{}}`), []string{"alerts.policy.dns_failed_enabled", "alerts.policy.sync_error_enabled", "alerts.policy.operational_error_enabled", "alerts.email.host", "alerts.email.port", "alerts.email.username", "alerts.email.password", "alerts.email.from_addr", "alerts.email.to_addr", "alerts.email.subject", "alerts.email.body", "monitoring.uptime_kuma_push.enabled", "monitoring.uptime_kuma_push.url", "monitoring.uptime_kuma_push.interval"}},
		{"parent-only", `{"version":3,"metadata":null,"targets":[],"rules":[],"settings":{},"alerts":{"email":null},"monitoring":{}}`, []string{"metadata", "settings.credentials", "settings.tag", "settings.interval", "settings.dns", "settings.dns_timeout", "settings.dns_fail_threshold", "settings.log_level", "settings.sync_enabled", "settings.theme", "alerts.policy", "alerts.email", "alerts.webhook", "monitoring.uptime_kuma_push"}},
		{"反序JSON仍按DTO顺序", `{"monitoring":null,"alerts":null,"settings":null,"rules":null,"targets":null,"metadata":null,"version":3}`, []string{"metadata", "targets", "rules", "settings", "alerts", "monitoring"}},
		{"array-objects", bundleWith(`[{},null]`, `[{},null]`), []string{"targets[0].export_id", "targets[0].cloud_type", "targets[0].region", "targets[0].resource_id", "targets[1]", "rules[0].host", "rules[0].protocol", "rules[0].ports", "rules[0].action", "rules[0].target_export_ids", "rules[0].comment", "rules[0].enable_ipv6", "rules[1]"}},
		{"missing-before-invalid-value", strings.Replace(v3Bundle(`{}`, v3DefaultEmailJSON, `{"uptime_kuma_push":{"enabled":false,"url":"","interval":"60s"}}`), `"interval":"5m"`, `"interval":"bad"`, 1), []string{"alerts.policy.dns_failed_enabled", "alerts.policy.sync_error_enabled", "alerts.policy.operational_error_enabled", "alerts.policy.health_timeout"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for i := 0; i < 30; i++ {
				r := i843ReplyFor(t, e, tc.body)
				if !reflect.DeepEqual(r.Fields, tc.want) || r.Total != len(tc.want) || r.Truncated {
					t.Fatalf("got=%+v want=%v", r, tc.want)
				}
				if r.Error != fmt.Sprintf("配置包有 %d 项必填内容缺失或为 null", len(tc.want)) {
					t.Fatalf("摘要=%q", r.Error)
				}
				unchanged(t)
			}
		})
	}
}
func TestI843AllFields(t *testing.T) {
	e, unchanged := i8111Guard(t)
	base := json.RawMessage(i8111Fixture())
	paths := i8111Fields(t, base, nil)
	if len(paths) != 57 {
		t.Fatalf("paths=%d", len(paths))
	}
	for _, mode := range []string{"缺失", "null"} {
		for _, path := range paths {
			r := i843ReplyFor(t, e, string(i8111Edit(t, base, path, mode)))
			want := strings.Join(path, ".")
			want = strings.Replace(want, "targets.0.", "targets[0].", 1)
			want = strings.Replace(want, "rules.0.", "rules[0].", 1)
			if want == "version" {
				if r.Fields != nil || r.Error != "version 字段缺失" {
					t.Fatalf("version gate=%+v", r)
				}
			} else if !reflect.DeepEqual(r.Fields, []string{want}) || r.Total != 1 || r.Truncated {
				t.Fatalf("mode=%s path=%v reply=%+v", mode, path, r)
			}
			if want != "version" {
				suffix := " 字段缺失"
				if want == "targets" || want == "rules" || strings.HasSuffix(want, ".target_export_ids") {
					suffix = " 字段缺失或为 null"
				}
				if r.Error != want+suffix {
					t.Fatalf("单项文案=%q，路径=%s", r.Error, want)
				}
			}
			unchanged(t)
		}
	}
}
func TestI843Bounds(t *testing.T) {
	e, unchanged := i8111Guard(t)
	for _, n := range []int{24, 25, 26, 1000} {
		body := bundleWith("["+strings.TrimSuffix(strings.Repeat("{},", n), ",")+"]", `[]`)
		r := i843ReplyFor(t, e, body)
		wantLen := n * 4
		if wantLen > 100 {
			wantLen = 100
		}
		if r.Total != n*4 || len(r.Fields) != wantLen || r.Truncated != (n*4 > 100) {
			t.Fatalf("n=%d total=%d fields=%d truncated=%v", n, r.Total, len(r.Fields), r.Truncated)
		}
		wantFields := make([]string, 0, wantLen)
		for i := 0; len(wantFields) < wantLen; i++ {
			for _, field := range []string{"export_id", "cloud_type", "region", "resource_id"} {
				if len(wantFields) < wantLen {
					wantFields = append(wantFields, fmt.Sprintf("targets[%d].%s", i, field))
				}
			}
		}
		if !reflect.DeepEqual(r.Fields, wantFields) || r.Error != fmt.Sprintf("配置包有 %d 项必填内容缺失或为 null", n*4) {
			t.Fatalf("有序路径/摘要=%+v", r)
		}
		unchanged(t)
	}
	for _, v := range []int{1, 2, 4} {
		r := i843ReplyFor(t, e, fmt.Sprintf(`{"version":%d}`, v))
		if len(r.Fields) != 0 || !strings.Contains(r.Error, "不支持的配置版本") {
			t.Fatalf("version %d reply=%+v", v, r)
		}
		unchanged(t)
	}
}

func TestI843BeforeCoordinator(t *testing.T) {
	e, unchanged := i8111Guard(t)
	body := v3Bundle(`{}`, `{}`, `{}`)
	e.deps.Coord.mu.Lock()
	done := make(chan int, 1)
	go func() { w := e.do(t, http.MethodPost, "/api/config/import", body); done <- w.Code }()
	var status int
	select {
	case status = <-done:
		e.deps.Coord.mu.Unlock()
	case <-time.After(time.Second):
		e.deps.Coord.mu.Unlock()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("request did not exit after unlock")
		}
		t.Fatal("presence failure entered coordinator")
	}
	if status != 400 {
		t.Fatalf("status=%d", status)
	}
	unchanged(t)
}
