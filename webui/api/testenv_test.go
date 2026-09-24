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

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"

	_ "modernc.org/sqlite"
)

// testEnv 测试环境：独立临时 SQLite + 带“运行时更新次数”计数器的协调器
type testEnv struct {
	deps    *Deps
	store   *config.Store
	dbPath  string
	applies *atomic.Int64
}

// newTestEnv 创建测试环境；applies 即协调器 apply（运行时更新）的调用次数
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
	deps := &Deps{Store: store}
	deps.Coord = NewConfigCoordinator(store, func() { applies.Add(1) })
	return &testEnv{deps: deps, store: store, dbPath: dbPath, applies: applies}
}

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

// applyCount 返回运行时更新次数
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
	if err := e.store.AddTarget(config.TargetConfig{CloudType: ct, Region: region, ResourceID: resource}); err != nil {
		t.Fatalf("预置目标失败: %v", err)
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

// settingsBody 构造合法的告警 PUT 请求体
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
