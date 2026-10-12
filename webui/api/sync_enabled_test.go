package api

import (
	"context"
	"database/sql"
	"net/http"
	"reflect"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// 真实 SQLite → 协调器 → 真实 Syncer；不启动 Run，不访问 DNS 或云 API。
func TestI8R07RealCoordinatorControl(t *testing.T) {
	e := newTestEnv(t)
	engine := syncer.New(e.runtime)
	e.deps.Syncer = engine
	err := e.deps.Coord.Mutate(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, kv := range [][2]string{{"tag", "committed-tag"}, {"interval", "2h"}, {"sync_enabled", "true"}} {
			if err := e.store.SetSettingTx(ctx, tx, kv[0], kv[1]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, endpoint := range []string{"pause", "resume"} {
		w := e.do(t, http.MethodPost, "/api/sync/"+endpoint, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", endpoint, w.Code, w.Body.String())
		}
		business, err := e.store.LoadBusinessSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(engine.Runtime().Snapshot().Config, business.ToRuntimeConfig()) {
			t.Fatal("协调器提交与真实引擎状态不一致")
		}
		if engine.IsEnabled() != (endpoint == "resume") || e.applyCount() != int64(i+2) {
			t.Fatal("真实引擎开关或恰好一次发布错误")
		}
	}
	before, applies := e.snapshot(), e.applyCount()
	e.execRaw(t, `CREATE TRIGGER fail_settings BEFORE INSERT ON settings BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`)
	w := e.do(t, http.MethodPost, "/api/sync/pause", "")
	if w.Code != http.StatusInternalServerError || e.snapshot() != before || e.applyCount() != applies {
		t.Fatal("事务失败不得发布或改写真实引擎")
	}
}
