package api

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"

	_ "modernc.org/sqlite"
)

// testEnv 测试环境：独立临时 SQLite + 真实运行时接线 + 带“运行时发布次数”计数器的协调器
type testEnv struct {
	deps    *Deps
	store   *config.Store
	dbPath  string
	applies *atomic.Int64
	runtime *syncer.RuntimeManager
	alerts  *AlertManager
}

// newTestEnv 创建测试环境。
//
// 使用与生产相同的完整协调器路径：事务内读取业务快照 → 构造候选 RuntimeState
// 与候选告警集合 → commit → 无失败发布（日志级别 → 告警集合 → RuntimeState）。
// applies 记录 apply 次数，供「非法输入零写入零 apply / 合法事务只 apply 一次」断言使用。
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "api-test.db")
	store, err := config.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if cerr := store.Close(); cerr != nil {
			t.Errorf("关闭测试数据库失败: %v", cerr)
		}
	})

	applies := &atomic.Int64{}
	rt := syncer.NewRuntimeManager(nil)
	alerts := NewAlertManager(nil)
	deps := &Deps{
		Store:   store,
		Runtime: rt,
		Alerts:  alerts,
	}
	deps.Coord = NewConfigCoordinator(store, deps.buildCandidate, func(c Candidate) error {
		if err := deps.applyCandidate(c); err != nil {
			return err
		}
		applies.Add(1)
		return nil
	})
	deps.createRuntime = true
	return &testEnv{deps: deps, store: store, dbPath: dbPath, applies: applies, runtime: rt, alerts: alerts}
}

// snapshot 返回当前已发布的运行时状态（可能为 nil）。
func (e *testEnv) snapshot() *syncer.RuntimeState { return e.runtime.Snapshot() }

// do 向注册了全部路由的 handler 发送 JSON 请求
func (e *testEnv) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	e.deps.Register(mux)
	mux.ServeHTTP(w, req)
	return w
}

// applyCount 返回运行时发布次数
func (e *testEnv) applyCount() int64 { return e.applies.Load() }

// pathWithID 拼接带数据库 ID 的路径
func pathWithID(prefix string, id int) string {
	return prefix + strconv.Itoa(id)
}

// execRaw 通过独立连接在同一数据库上执行 SQL：仅用于测试夹具
// （临时 trigger 制造写入失败、DROP TABLE 制造读取失败）
func (e *testEnv) execRaw(t *testing.T, stmt string) {
	t.Helper()
	db, err := sql.Open("sqlite", e.dbPath)
	if err != nil {
		t.Fatalf("打开夹具连接失败: %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("关闭夹具连接失败: %v", cerr)
		}
	}()
	if _, err := db.Exec(stmt); err != nil {
		t.Fatalf("执行夹具 SQL 失败: %v; stmt=%s", err, stmt)
	}
}

// seedTarget 直接写库预置目标，返回数据库 ID
func (e *testEnv) seedTarget(t *testing.T, ct config.CloudType, region, resource string) int {
	t.Helper()
	return e.seedTargetCount(t, ct, region, resource, 1)
}

// seedTargetCount 连续插入 n 个目标并返回最后一个的数据库 ID
func (e *testEnv) seedTargetCount(t *testing.T, ct config.CloudType, region, resource string, n int) int {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := e.store.AddTarget(config.TargetConfig{CloudType: ct, Region: region, ResourceID: resource}); err != nil {
			t.Fatalf("预置目标失败: %v", err)
		}
	}
	targets, err := e.store.GetTargets()
	if err != nil {
		t.Fatalf("GetTargets 失败: %v", err)
	}
	if len(targets) == 0 {
		t.Fatal("预置目标后 GetTargets 为空")
	}
	return targets[len(targets)-1].ID
}

// seedRule 直接写库预置规则，返回数据库 ID
func (e *testEnv) seedRule(t *testing.T, r config.DomainRule) int {
	t.Helper()
	if err := e.store.AddRule(r); err != nil {
		t.Fatalf("预置规则失败: %v", err)
	}
	rules, err := e.store.GetRules()
	if err != nil {
		t.Fatalf("GetRules 失败: %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("预置规则后 GetRules 为空")
	}
	return rules[len(rules)-1].ID
}

// alertsBody 构造合法的告警 PUT 请求体
func alertsBody(emailHost, emailPassword, webhookURL, channel string) string {
	return `{"email":{"enabled":false,"host":"` + emailHost + `","port":"587","username":"u",` +
		`"password":"` + emailPassword + `","from_addr":"f@example.com","to_addr":"t@example.com"},` +
		`"webhook":{"enabled":false,"url":"` + webhookURL + `","channel":"` + channel + `"}}`
}

// settingsWithSentinels 构造含唯一 sentinel 的设置 PUT 请求体
func settingsWithSentinels(tcID, tcKey, aliID, aliKey string) string {
	return `{"tc_access_id":"` + tcID + `","tc_access_key":"` + tcKey + `",` +
		`"ali_access_id":"` + aliID + `","ali_access_key":"` + aliKey + `"}`
}

// validBundleSettings 构造一份合法的 version 2 settings JSON 片段
func validBundleSettings() string {
	return `"settings":{"credentials":{"tencent":{"secret_id":"","secret_key":""},` +
		`"aliyun":{"access_key_id":"","access_key_secret":""}},` +
		`"tag":"auto-dns","interval":"5m","dns":"223.5.5.5","dns_timeout":"10s",` +
		`"dns_fail_threshold":5,"log_level":"info","sync_enabled":true,"theme":"light"}`
}

// validBundleAlerts 构造一份合法的 version 2 alerts JSON 片段
func validBundleAlerts() string {
	return `"alerts":{"email":{"enabled":false,"host":"","port":"587","username":"","password":"",` +
		`"from_addr":"","to_addr":""},"webhook":{"enabled":false,"url":"","channel":"dingtalk"}}`
}

// validBundle 构造一份可导入的最小合法 version 2 配置包
func validBundle() string {
	return `{"version":2,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},` +
		`"targets":[],"rules":[],` + validBundleSettings() + `,` + validBundleAlerts() + `}`
}

// bundleWith 用给定的 targets/rules 片段构造完整合法 version 2 配置包
// （settings/alerts 使用合法默认值，便于聚焦被替换的部分）。
func bundleWith(targets, rules string) string {
	return `{"version":2,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},` +
		`"targets":` + targets + `,"rules":` + rules + `,` +
		validBundleSettings() + `,` + validBundleAlerts() + `}`
}

// bundleWithSettings 用给定的 targets/rules/settings 片段构造完整配置包。
func bundleWithSettings(targets, rules, settings string) string {
	return `{"version":2,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},` +
		`"targets":` + targets + `,"rules":` + rules + `,"settings":` + settings + `,` +
		validBundleAlerts() + `}`
}

// bundleWithout 在合法配置包基础上删除指定顶层字段（用于 presence 断言）。
func bundleWithout(field string) string {
	full := map[string]string{
		"version":  `"version":2`,
		"metadata": `"metadata":{"exported_at":"2026-09-22T08:00:00Z"}`,
		"targets":  `"targets":[]`,
		"rules":    `"rules":[]`,
		"settings": validBundleSettings(),
		"alerts":   validBundleAlerts(),
	}
	parts := make([]string, 0, len(full))
	for _, key := range []string{"version", "metadata", "targets", "rules", "settings", "alerts"} {
		if key == field {
			continue
		}
		parts = append(parts, full[key])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// runtimeWithEmail 构造带启用邮件告警的运行时配置（用于告警接线断言）
func runtimeWithEmail(host, password string) config.RuntimeConfig {
	return config.RuntimeConfig{
		Credentials: config.Credentials{}, Tag: "auto-dns", Interval: 5 * time.Minute,
		DNS: "223.5.5.5", DNSTimeout: 10 * time.Second, DNSFailThreshold: 5,
		LogLevel: "info", SyncEnabled: true, Theme: "light",
		Email: config.AlertEmailConfig{
			Enabled: true, Host: host, Port: "587", Username: "u", Password: password,
			FromAddr: "f@example.com", ToAddr: "t@example.com",
		},
	}
}

// resetEnvRuntimeForTest 为需要显式运行时的用例发布一份初始状态。
func resetEnvRuntimeForTest(t *testing.T, e *testEnv, rc config.RuntimeConfig) {
	t.Helper()
	state, err := syncer.BuildRuntimeState(nil, rc, syncer.BreakerReset)
	if err != nil {
		t.Fatalf("构造初始运行时状态失败: %v", err)
	}
	e.runtime.Apply(state)
}
