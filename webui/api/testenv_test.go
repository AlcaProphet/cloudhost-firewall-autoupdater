package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"

	_ "modernc.org/sqlite"
)

// withTxForTest 在事务内执行测试夹具写入并提交。
//
// 非事务 Store 写入入口（AddTarget/DeleteTarget/AddRule/ResetAll）已删除：
// 生产写入统一经协调器使用 *Tx 方法，测试夹具也必须走同一事务路径。
func withTxForTest(t *testing.T, s *config.Store, fn func(ctx context.Context, tx *sql.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("开始测试事务失败: %v", err)
	}
	if err := fn(ctx, tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			t.Errorf("回滚测试事务失败: %v", rbErr)
		}
		t.Fatalf("测试夹具写入失败: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("提交测试事务失败: %v", err)
	}
}

// addTargetForTest 事务内插入目标（测试夹具）
func addTargetForTest(t *testing.T, s *config.Store, tc config.TargetConfig) {
	t.Helper()
	withTxForTest(t, s, func(ctx context.Context, tx *sql.Tx) error {
		_, err := s.AddTargetTx(ctx, tx, tc)
		return err
	})
}

// addRuleForTest 事务内插入规则（测试夹具）
func addRuleForTest(t *testing.T, s *config.Store, r config.DomainRule) {
	t.Helper()
	withTxForTest(t, s, func(ctx context.Context, tx *sql.Tx) error {
		return s.AddRuleTx(ctx, tx, r)
	})
}

// deleteTargetForTest 事务内删除目标（用于制造自增历史，测试夹具）
func deleteTargetForTest(t *testing.T, s *config.Store, id int) {
	t.Helper()
	withTxForTest(t, s, func(ctx context.Context, tx *sql.Tx) error {
		_, err := s.DeleteTargetTx(ctx, tx, id)
		return err
	})
}

// resetAllForTest 事务内清空全部业务数据（测试夹具）
func resetAllForTest(t *testing.T, s *config.Store) {
	t.Helper()
	withTxForTest(t, s, func(ctx context.Context, tx *sql.Tx) error {
		return s.ResetAllTx(ctx, tx)
	})
}

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
// 与候选告警集合 → commit → 无失败发布（日志级别 → 告警集合 → RuntimeState → Health/Push 唤醒）。
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
	deps.Coord = NewConfigCoordinator(store, deps.buildCandidate, func(c Candidate) {
		deps.applyCandidate(c)
		applies.Add(1)
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
		addTargetForTest(t, e.store, config.TargetConfig{CloudType: ct, Region: region, ResourceID: resource})
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
	addRuleForTest(t, e.store, r)
	rules, err := e.store.GetRules()
	if err != nil {
		t.Fatalf("GetRules 失败: %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("预置规则后 GetRules 为空")
	}
	return rules[len(rules)-1].ID
}

// alertsBody 构造合法的告警 PUT 请求体（Build7 §4.6：四个完整对象）
func alertsBody(emailHost, emailPassword, webhookURL, channel string) string {
	return `{"policy":{"dns_failed_enabled":false,"sync_error_enabled":false,` +
		`"operational_error_enabled":false,"health_timeout":"10m"},` +
		`"email":{"enabled":false,"host":"` + emailHost + `","port":"587","username":"u",` +
		`"password":"` + emailPassword + `","from_addr":"f@example.com","to_addr":"t@example.com",` +
		`"subject":"[FWAlizer] 告警通知","body":"FWAlizer 检测到运行异常，请检查同步日志。"},` +
		`"webhook":{"enabled":false,"url":"` + webhookURL + `","channel":"` + channel + `"},` +
		`"uptime_kuma_push":{"enabled":false,"url":"","interval":"60s"}}`
}

// settingsWithSentinels 构造含唯一 sentinel 的设置 PUT 请求体
func settingsWithSentinels(tcID, tcKey, aliID, aliKey string) string {
	return `{"tc_access_id":"` + tcID + `","tc_access_key":"` + tcKey + `",` +
		`"ali_access_id":"` + aliID + `","ali_access_key":"` + aliKey + `"}`
}

// validBundleSettings 构造一份合法的配置包 settings JSON 片段
func validBundleSettings() string {
	return `"settings":{"credentials":{"tencent":{"secret_id":"","secret_key":""},` +
		`"aliyun":{"access_key_id":"","access_key_secret":""}},` +
		`"tag":"auto-dns","interval":"5m","dns":"223.5.5.5","dns_timeout":"10s",` +
		`"dns_fail_threshold":5,"log_level":"info","sync_enabled":true,"theme":"light"}`
}

// validBundleAlerts 构造一份合法的 version 3 alerts JSON 片段（policy + email + webhook）
func validBundleAlerts() string {
	return `"alerts":{"policy":{"dns_failed_enabled":false,"sync_error_enabled":false,` +
		`"operational_error_enabled":false,"health_timeout":"10m"},` +
		`"email":{"enabled":false,"host":"","port":"587","username":"","password":"",` +
		`"from_addr":"","to_addr":"","subject":"[FWAlizer] 告警通知",` +
		`"body":"FWAlizer 检测到运行异常，请检查同步日志。"},` +
		`"webhook":{"enabled":false,"url":"","channel":"dingtalk"}}`
}

// validBundleMonitoring 构造一份合法的 version 3 monitoring JSON 片段
func validBundleMonitoring() string {
	return `"monitoring":{"uptime_kuma_push":{"enabled":false,"url":"","interval":"60s"}}`
}

// validBundle 构造一份可导入的最小合法 version 3 配置包
func validBundle() string {
	return `{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},` +
		`"targets":[],"rules":[],` + validBundleSettings() + `,` + validBundleAlerts() + `,` +
		validBundleMonitoring() + `}`
}

// bundleWith 用给定的 targets/rules 片段构造完整合法 version 3 配置包
// （settings/alerts/monitoring 使用合法默认值，便于聚焦被替换的部分）。
func bundleWith(targets, rules string) string {
	return `{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},` +
		`"targets":` + targets + `,"rules":` + rules + `,` +
		validBundleSettings() + `,` + validBundleAlerts() + `,` + validBundleMonitoring() + `}`
}

// bundleWithSettings 用给定的 targets/rules/settings 片段构造完整 version 3 配置包。
func bundleWithSettings(targets, rules, settings string) string {
	return `{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},` +
		`"targets":` + targets + `,"rules":` + rules + `,"settings":` + settings + `,` +
		validBundleAlerts() + `,` + validBundleMonitoring() + `}`
}

// bundleWithout 在合法配置包基础上删除指定顶层字段（用于 presence 断言）。
func bundleWithout(field string) string {
	full := map[string]string{
		"version":    `"version":3`,
		"metadata":   `"metadata":{"exported_at":"2026-09-22T08:00:00Z"}`,
		"targets":    `"targets":[]`,
		"rules":      `"rules":[]`,
		"settings":   validBundleSettings(),
		"alerts":     validBundleAlerts(),
		"monitoring": validBundleMonitoring(),
	}
	parts := make([]string, 0, len(full))
	for _, key := range []string{"version", "metadata", "targets", "rules", "settings", "alerts", "monitoring"} {
		if key == field {
			continue
		}
		parts = append(parts, full[key])
	}
	return "{" + strings.Join(parts, ",") + "}"
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
