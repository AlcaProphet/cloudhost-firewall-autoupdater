package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

func compressionPath(id int) string {
	return pathWithID("/api/targets/", id) + "/lighthouse-compression"
}

func getCompression(t *testing.T, e *testEnv, id int) compressionResponse {
	t.Helper()
	w := e.do(t, http.MethodGet, compressionPath(id), "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET 压缩设置状态码=%d body=%s", w.Code, w.Body.String())
	}
	var out compressionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBuild9S1CompressionAPIAndAliases(t *testing.T) {
	e := newTestEnv(t)
	first := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")

	got := getCompression(t, e, first)
	if got.Enabled || got.ExtraIPv4Allow != 0 || got.PendingReview || len(got.TargetIDs) != 1 {
		t.Fatalf("新实例默认值错误: %+v", got)
	}

	w := e.do(t, http.MethodPut, compressionPath(first), `{"enabled":true,"extra_ipv4_allow":0}`)
	if w.Code != http.StatusOK {
		t.Fatalf("保存显式 0 状态码=%d body=%s", w.Code, w.Body.String())
	}
	got = getCompression(t, e, first)
	if !got.Enabled || got.ExtraIPv4Allow != 0 || got.PendingReview {
		t.Fatalf("显式 0 保存错误: %+v", got)
	}
	if state := e.snapshot(); state == nil || len(state.Config.LighthouseCompression) != 1 ||
		!state.Config.LighthouseCompression[0].Enabled || state.Config.LighthouseCompression[0].ExtraIPv4Allow != 0 {
		t.Fatalf("运行时未发布压缩配置: %+v", state)
	}

	alias := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")
	got = getCompression(t, e, alias)
	if !got.Enabled || len(got.TargetIDs) != 2 {
		t.Fatalf("别名未共享设置: %+v", got)
	}
	other := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-b")
	if got := getCompression(t, e, other); got.Enabled || got.ExtraIPv4Allow != 0 {
		t.Fatalf("其他实例被串用: %+v", got)
	}

	// 启用→关闭：持久化待复查；运行时立即关闭。
	w = e.do(t, http.MethodPut, compressionPath(alias), `{"enabled":false,"extra_ipv4_allow":0}`)
	if w.Code != http.StatusOK {
		t.Fatalf("关闭状态码=%d body=%s", w.Code, w.Body.String())
	}
	got = getCompression(t, e, first)
	if got.Enabled || !got.PendingReview || got.PendingSince == "" {
		t.Fatalf("关闭后待复查状态错误: %+v", got)
	}
	if state := e.snapshot(); state == nil || state.Config.LighthouseCompression[0].Enabled {
		t.Fatal("关闭未发布到运行时")
	}

	// 重新启用清空待复查。
	w = e.do(t, http.MethodPut, compressionPath(first), `{"enabled":true,"extra_ipv4_allow":7}`)
	if w.Code != http.StatusOK {
		t.Fatalf("重新启用状态码=%d body=%s", w.Code, w.Body.String())
	}
	if got := getCompression(t, e, first); !got.Enabled || got.PendingReview || got.ExtraIPv4Allow != 7 {
		t.Fatalf("重新启用未清空待复查: %+v", got)
	}
}

func TestBuild9S1CompressionValidation(t *testing.T) {
	e := newTestEnv(t)
	lighthouse := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")
	cvm := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-1")
	for _, tc := range []struct {
		name, path, body string
		want             int
	}{
		{"非 Lighthouse", compressionPath(cvm), `{"enabled":true,"extra_ipv4_allow":0}`, http.StatusBadRequest},
		{"缺 enabled", compressionPath(lighthouse), `{"extra_ipv4_allow":0}`, http.StatusBadRequest},
		{"缺额度", compressionPath(lighthouse), `{"enabled":true}`, http.StatusBadRequest},
		{"null 额度", compressionPath(lighthouse), `{"enabled":true,"extra_ipv4_allow":null}`, http.StatusBadRequest},
		{"负数", compressionPath(lighthouse), `{"enabled":true,"extra_ipv4_allow":-1}`, http.StatusBadRequest},
		{"超界", compressionPath(lighthouse), fmt.Sprintf(`{"enabled":true,"extra_ipv4_allow":%d}`, config.MaxLighthouseExtraIPv4+1), http.StatusBadRequest},
		{"小数", compressionPath(lighthouse), `{"enabled":true,"extra_ipv4_allow":1.5}`, http.StatusBadRequest},
		{"未知字段", compressionPath(lighthouse), `{"enabled":true,"extra_ipv4_allow":1,"x":2}`, http.StatusBadRequest},
		{"不存在目标", compressionPath(999999), `{"enabled":true,"extra_ipv4_allow":0}`, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := e.applyCount()
			w := e.do(t, http.MethodPut, tc.path, tc.body)
			if w.Code != tc.want {
				t.Fatalf("状态码=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
			if e.applyCount() != before {
				t.Fatal("非法请求不得发布运行时")
			}
		})
	}
	if got := getCompression(t, e, lighthouse); got.Enabled || got.ExtraIPv4Allow != 0 {
		t.Fatalf("非法请求改动了设置: %+v", got)
	}
}

func TestBuild9S1AliasCleanupOnDeleteAndUpdate(t *testing.T) {
	e := newTestEnv(t)
	first := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")
	second := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")
	if w := e.do(t, http.MethodPut, compressionPath(first), `{"enabled":true,"extra_ipv4_allow":9}`); w.Code != http.StatusOK {
		t.Fatalf("保存失败: %d %s", w.Code, w.Body.String())
	}
	if w := e.do(t, http.MethodDelete, pathWithID("/api/targets/", first), ""); w.Code != http.StatusOK {
		t.Fatalf("删除别名失败: %d %s", w.Code, w.Body.String())
	}
	if got := getCompression(t, e, second); !got.Enabled || got.ExtraIPv4Allow != 9 {
		t.Fatalf("删除一个别名不应清除设置: %+v", got)
	}
	if w := e.do(t, http.MethodDelete, pathWithID("/api/targets/", second), ""); w.Code != http.StatusOK {
		t.Fatalf("删除最后别名失败: %d %s", w.Code, w.Body.String())
	}
	settings, err := e.store.LoadLighthouseCompressionSettings()
	if err != nil || len(settings) != 0 {
		t.Fatalf("删除最后别名后设置残留: %+v err=%v", settings, err)
	}

	// 最后一个别名改绑到新实例：旧实例设置删除，新实例默认关闭。
	a := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-c")
	if w := e.do(t, http.MethodPut, compressionPath(a), `{"enabled":true,"extra_ipv4_allow":5}`); w.Code != http.StatusOK {
		t.Fatalf("保存失败: %d %s", w.Code, w.Body.String())
	}
	w := e.do(t, http.MethodPut, pathWithID("/api/targets/", a),
		`{"cloud_type":"tc_lighthouse","region":"ap-guangzhou","resource_id":"lhins-d"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("改绑失败: %d %s", w.Code, w.Body.String())
	}
	if got := getCompression(t, e, a); got.Enabled || got.ExtraIPv4Allow != 0 {
		t.Fatalf("改绑后不应继承旧实例设置: %+v", got)
	}
	settings, _ = e.store.LoadLighthouseCompressionSettings()
	if len(settings) != 0 {
		t.Fatalf("改绑后旧实例设置残留: %+v", settings)
	}
}

func TestBuild9S1FailureRollsBackCompression(t *testing.T) {
	e := newTestEnv(t)
	id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")
	if w := e.do(t, http.MethodPut, compressionPath(id), `{"enabled":true,"extra_ipv4_allow":9}`); w.Code != http.StatusOK {
		t.Fatalf("初始保存失败: %d %s", w.Code, w.Body.String())
	}
	// 关闭时写状态失败：配置 upsert 与状态写入在同一事务内，必须整体回滚。
	e.execRaw(t, `CREATE TRIGGER fail_compression_state BEFORE INSERT ON lighthouse_compression_state
		BEGIN SELECT RAISE(ABORT, 'fixture'); END;`)
	beforeApply := e.applyCount()
	w := e.do(t, http.MethodPut, compressionPath(id), `{"enabled":false,"extra_ipv4_allow":9}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码=%d want=500 body=%s", w.Code, w.Body.String())
	}
	if e.applyCount() != beforeApply {
		t.Fatal("失败保存不得发布运行时")
	}
	got := getCompression(t, e, id)
	if !got.Enabled || got.ExtraIPv4Allow != 9 || got.PendingReview {
		t.Fatalf("失败保存留下部分状态: %+v", got)
	}
}

func TestBuild9S1V5ImportFailureRollsBack(t *testing.T) {
	e := newTestEnv(t)
	id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")
	if w := e.do(t, http.MethodPut, compressionPath(id), `{"enabled":true,"extra_ipv4_allow":9}`); w.Code != http.StatusOK {
		t.Fatalf("保存失败: %d %s", w.Code, w.Body.String())
	}
	w := e.do(t, http.MethodPost, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("导出失败: %d", w.Code)
	}
	body := w.Body.String()
	// 让 v5 导入在 target 已插入、压缩行写入时失败，验证整库回滚。
	e.execRaw(t, `CREATE TRIGGER fail_compression_import BEFORE INSERT ON lighthouse_compression_settings
		BEGIN SELECT RAISE(ABORT, 'fixture'); END;`)
	beforeApply := e.applyCount()
	resp := e.do(t, http.MethodPost, "/api/config/import", body)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("状态码=%d want=500 body=%s", resp.Code, resp.Body.String())
	}
	if e.applyCount() != beforeApply {
		t.Fatal("失败导入不得发布运行时")
	}
	settings, err := e.store.LoadLighthouseCompressionSettings()
	if err != nil || len(settings) != 1 || settings[0].ExtraIPv4Allow != 9 {
		t.Fatalf("失败导入改动了旧压缩配置: %+v err=%v", settings, err)
	}
	got := getCompression(t, e, id)
	if !got.Enabled || got.ExtraIPv4Allow != 9 {
		t.Fatalf("失败导入改动了现有设置: %+v", got)
	}
}

func TestBuild9S1UpdateOneAliasKeepsOtherSettings(t *testing.T) {
	e := newTestEnv(t)
	first := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")
	second := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-a")
	if w := e.do(t, http.MethodPut, compressionPath(first), `{"enabled":true,"extra_ipv4_allow":5}`); w.Code != http.StatusOK {
		t.Fatalf("保存失败: %d %s", w.Code, w.Body.String())
	}
	w := e.do(t, http.MethodPut, pathWithID("/api/targets/", first),
		`{"cloud_type":"tc_lighthouse","region":"ap-guangzhou","resource_id":"lhins-b"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("改绑失败: %d %s", w.Code, w.Body.String())
	}
	if got := getCompression(t, e, second); !got.Enabled || got.ExtraIPv4Allow != 5 {
		t.Fatalf("旧实例仍有别名时应保留设置: %+v", got)
	}
	if got := getCompression(t, e, first); got.Enabled {
		t.Fatalf("改绑后的别名不应继承旧实例: %+v", got)
	}
}
