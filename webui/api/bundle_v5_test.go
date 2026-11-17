package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestBuild9S1V5RoundTripPreview(t *testing.T) {
	e := newTestEnv(t)
	id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")
	if w := e.do(t, http.MethodPut, compressionPath(id), `{"enabled":true,"extra_ipv4_allow":123}`); w.Code != http.StatusOK {
		t.Fatalf("保存失败: %d %s", w.Code, w.Body.String())
	}

	w := e.do(t, http.MethodPost, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("导出失败: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	beforeApply := e.applyCount()
	beforeSettings, _ := e.store.LoadLighthouseCompressionSettings()
	w = e.do(t, http.MethodPost, "/api/config/import/preview", body)
	if w.Code != http.StatusOK {
		t.Fatalf("预览失败: %d %s", w.Code, w.Body.String())
	}
	var preview importPreviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Version != 5 || preview.CompressionEntriesCount != 1 || preview.CompressionEnabledCount != 1 ||
		preview.CompressionDefaultDisabled || preview.LighthouseCompression[0].TargetCount != 1 ||
		preview.LighthouseCompression[0].ExtraIPv4Allow != 123 {
		t.Fatalf("预览摘要错误: %+v", preview)
	}
	if e.applyCount() != beforeApply {
		t.Fatal("预览不得发布运行时")
	}
	afterSettings, _ := e.store.LoadLighthouseCompressionSettings()
	if len(afterSettings) != len(beforeSettings) || len(afterSettings) != 1 || !afterSettings[0].Enabled {
		t.Fatal("预览改动了配置")
	}

	if w := e.do(t, http.MethodPost, "/api/config/reset", "{}"); w.Code != http.StatusOK {
		t.Fatalf("reset 失败: %d %s", w.Code, w.Body.String())
	}
	if w := e.do(t, http.MethodPost, "/api/config/import", body); w.Code != http.StatusOK {
		t.Fatalf("v5 导入失败: %d %s", w.Code, w.Body.String())
	}
	targets, err := e.store.GetTargets()
	if err != nil || len(targets) != 1 {
		t.Fatalf("导入后目标 = %+v err=%v", targets, err)
	}
	got := getCompression(t, e, targets[0].ID)
	if !got.Enabled || got.ExtraIPv4Allow != 123 || got.PendingReview {
		t.Fatalf("v5 往返未保留设置: %+v", got)
	}
	if state := e.snapshot(); state == nil || len(state.Config.LighthouseCompression) != 1 ||
		state.Config.LighthouseCompression[0].ExtraIPv4Allow != 123 {
		t.Fatalf("v5 导入未发布运行时: %+v", state)
	}
}

func TestBuild9S1V5RejectsInvalid(t *testing.T) {
	e := newTestEnv(t)
	id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")
	if w := e.do(t, http.MethodPut, compressionPath(id), `{"enabled":true,"extra_ipv4_allow":123}`); w.Code != http.StatusOK {
		t.Fatalf("保存失败: %d %s", w.Code, w.Body.String())
	}
	w := e.do(t, http.MethodPost, "/api/config/export", "")
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	entries, ok := raw["lighthouse_compression"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("导出压缩段错误: %T %v", raw["lighthouse_compression"], raw["lighthouse_compression"])
	}
	entry := entries[0].(map[string]any)

	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"重复身份", func(m map[string]any) {
			arr := m["lighthouse_compression"].([]any)
			copyEntry := map[string]any{}
			for k, v := range entry {
				copyEntry[k] = v
			}
			m["lighthouse_compression"] = append(arr, copyEntry)
		}},
		{"悬空引用", func(m map[string]any) { entry["resource_id"] = "lhins-missing" }},
		{"非法平台", func(m map[string]any) { entry["cloud_type"] = "tc_cvm" }},
		{"缺失额度", func(m map[string]any) { delete(entry, "extra_ipv4_allow") }},
		{"缺失压缩段", func(m map[string]any) { delete(m, "lighthouse_compression") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 每个子用例从原始导出的深拷贝开始，避免前一个用例污染。
			base := map[string]any{}
			if err := json.Unmarshal(w.Body.Bytes(), &base); err != nil {
				t.Fatal(err)
			}
			rawEntry := base["lighthouse_compression"].([]any)[0].(map[string]any)
			oldEntry := entry
			entry = rawEntry
			tc.mutate(base)
			entry = oldEntry
			body := mustJSON(t, base)
			beforeApply := e.applyCount()
			resp := e.do(t, http.MethodPost, "/api/config/import", body)
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("状态码=%d want=400 body=%s", resp.Code, resp.Body.String())
			}
			if e.applyCount() != beforeApply {
				t.Fatal("非法 v5 不得发布")
			}
			got := getCompression(t, e, id)
			if !got.Enabled || got.ExtraIPv4Allow != 123 {
				t.Fatalf("非法 v5 改动了现有配置: %+v", got)
			}
		})
	}
}

func TestBuild9S1V3V4ImportDefaultsOff(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"v3", bundleWith(`[{"export_id":1,"cloud_type":"tc_lighthouse","region":"ap-guangzhou","resource_id":"lhins-a"}]`, `[]`)},
		{"v4", func() string {
			e := newTestEnv(t)
			id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")
			if w := e.do(t, http.MethodPut, compressionPath(id), `{"enabled":true,"extra_ipv4_allow":1}`); w.Code != http.StatusOK {
				t.Fatalf("保存失败: %d", w.Code)
			}
			w := e.do(t, http.MethodPost, "/api/config/export", "")
			var raw map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			raw["version"] = 4
			delete(raw, "lighthouse_compression")
			return mustJSON(t, raw)
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")
			if w := e.do(t, http.MethodPut, compressionPath(id), `{"enabled":true,"extra_ipv4_allow":1}`); w.Code != http.StatusOK {
				t.Fatalf("保存失败: %d", w.Code)
			}
			if w := e.do(t, http.MethodPost, "/api/config/import", tc.body); w.Code != http.StatusOK {
				t.Fatalf("导入失败: %d %s", w.Code, w.Body.String())
			}
			settings, err := e.store.LoadLighthouseCompressionSettings()
			if err != nil || len(settings) != 0 {
				t.Fatalf("%s 导入后压缩配置未清空: %+v err=%v", tc.name, settings, err)
			}
			if state := e.snapshot(); state == nil || len(state.Config.LighthouseCompression) != 0 {
				t.Fatalf("%s 导入后运行时未清空: %+v", tc.name, state)
			}
		})
	}
}

func TestBuild9S1ImportReconcilesCompressionReview(t *testing.T) {
	for _, tc := range []struct {
		name       string
		version    int
		omit       bool
		wasPending bool
		enable     bool
		remove     bool
		fail       bool
	}{
		{name: "v3 default off", version: 3},
		{name: "v4 default off", version: 4},
		{name: "v5 omitted instance", version: 5, omit: true},
		{name: "v5 explicit off", version: 5},
		{name: "retain pending timestamp without saved settings", version: 5, omit: true, wasPending: true},
		{name: "reenabled", version: 5, wasPending: true, enable: true},
		{name: "removed", version: 5, wasPending: true, remove: true},
		{name: "state insert rolls back", version: 5, omit: true, fail: true},
		{name: "state delete rolls back", version: 5, wasPending: true, enable: true, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")
			alias := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")
			if w := e.do(t, http.MethodPut, compressionPath(id), `{"enabled":true,"extra_ipv4_allow":9}`); w.Code != http.StatusOK {
				t.Fatalf("开启压缩失败: %s", w.Body)
			}
			if tc.wasPending {
				if w := e.do(t, http.MethodPut, compressionPath(alias), `{"enabled":false,"extra_ipv4_allow":9}`); w.Code != http.StatusOK {
					t.Fatalf("关闭压缩失败: %s", w.Body)
				}
				if tc.omit {
					e.execRaw(t, "DELETE FROM lighthouse_compression_settings")
				}
			}
			before := getCompression(t, e, id)
			beforeApply := e.applyCount()
			key := config.LighthouseCompression{CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "lhins-a"}

			w := e.do(t, http.MethodPost, "/api/config/export", "")
			var body map[string]any
			if w.Code != http.StatusOK {
				t.Fatalf("导出失败: %s", w.Body)
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.version == 3 {
				targets, err := json.Marshal(body["targets"])
				if err != nil {
					t.Fatal(err)
				}
				body = nil
				if err := json.Unmarshal([]byte(bundleWith(string(targets), `[]`)), &body); err != nil {
					t.Fatal(err)
				}
			}
			body["version"] = tc.version
			switch {
			case tc.version < 5:
				delete(body, "lighthouse_compression")
			case tc.remove:
				body["targets"], body["lighthouse_compression"] = []any{}, []any{}
			case tc.omit:
				body["lighthouse_compression"] = []any{}
			default:
				key.Enabled, key.ExtraIPv4Allow = tc.enable, 9
				body["lighthouse_compression"] = []config.LighthouseCompression{key}
			}
			if tc.fail {
				if tc.wasPending {
					e.execRaw(t, `CREATE TRIGGER fail_review BEFORE DELETE ON lighthouse_compression_state BEGIN SELECT RAISE(ABORT, 'fixture'); END;`)
				} else {
					e.execRaw(t, `CREATE TRIGGER fail_review BEFORE INSERT ON lighthouse_compression_state BEGIN SELECT RAISE(ABORT, 'fixture'); END;`)
				}
			}
			w = e.do(t, http.MethodPost, "/api/config/import", mustJSON(t, body))
			if tc.fail {
				if w.Code != http.StatusInternalServerError || e.applyCount() != beforeApply {
					t.Fatalf("失败导入: status=%d apply=%d", w.Code, e.applyCount())
				}
				got := getCompression(t, e, id)
				if got.Enabled != before.Enabled || got.ExtraIPv4Allow != before.ExtraIPv4Allow ||
					got.PendingReview != before.PendingReview || got.PendingSince != before.PendingSince ||
					len(got.TargetIDs) != 2 {
					t.Fatalf("失败导入改变了旧实例状态: before=%+v got=%+v", before, got)
				}
				return
			}
			if w.Code != http.StatusOK || e.applyCount() != beforeApply+1 {
				t.Fatalf("导入失败: status=%d body=%s apply=%d", w.Code, w.Body, e.applyCount())
			}
			state, found, err := e.store.LoadLighthouseCompressionState(key)
			if err != nil {
				t.Fatal(err)
			}
			if tc.remove || tc.enable {
				if found {
					t.Fatalf("移除或开启实例后仍有待复查: %+v", state)
				}
				return
			}
			targets, err := e.store.GetTargets()
			if err != nil || len(targets) != 2 || targets[0].ID == id || targets[1].ID == alias {
				t.Fatalf("导入需要重建两个别名: %v err=%v", targets, err)
			}
			for _, target := range targets {
				got := getCompression(t, e, target.ID)
				if got.Enabled || !got.PendingReview || got.PendingSince == "" || !found || state.PendingReason != "disabled" {
					t.Fatalf("导入后待复查错误: response=%+v state=%+v", got, state)
				}
				if tc.wasPending && got.PendingSince != before.PendingSince {
					t.Fatalf("复查时间被替换: %q -> %q", before.PendingSince, got.PendingSince)
				}
			}
		})
	}
}
