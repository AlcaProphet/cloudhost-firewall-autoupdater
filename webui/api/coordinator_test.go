package api

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"modernc.org/sqlite"
)

// TestCoordinatorApplyOnceOnSuccess 成功提交后恰好 apply 一次
func TestCoordinatorApplyOnceOnSuccess(t *testing.T) {
	e := newTestEnv(t)
	err := e.deps.Coord.Mutate(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		return e.store.SetSettingTx(ctx, tx, "tag", "my-tag")
	})
	if err != nil {
		t.Fatalf("Mutate 失败: %v", err)
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("apply 次数 = %d, want 1", got)
	}
	if settings, _ := e.store.GetSettings(); settings["tag"] != "my-tag" {
		t.Errorf("设置未提交: %+v", settings)
	}
}

// TestCoordinatorNoApplyOnCallbackError 回调返回错误时不提交、不 apply（非法输入零写入零 reload）
func TestCoordinatorNoApplyOnCallbackError(t *testing.T) {
	e := newTestEnv(t)
	err := e.deps.Coord.Mutate(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		if serr := e.store.SetSettingTx(ctx, tx, "tag", "should-rollback"); serr != nil {
			return serr
		}
		return badRequest("领域校验失败")
	})
	if err == nil {
		t.Fatal("回调错误必须向外返回")
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("回调失败不应 apply，实际 %d", got)
	}
	if settings, _ := e.store.GetSettings(); len(settings) != 0 {
		t.Errorf("回调失败必须回滚: %+v", settings)
	}
}

// TestCoordinatorRollsBackOnStoreError Store 写入错误也必须回滚且不 apply
func TestCoordinatorRollsBackOnStoreError(t *testing.T) {
	e := newTestEnv(t)
	e.execRaw(t, `CREATE TRIGGER fail_settings BEFORE INSERT ON settings BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`)

	err := e.deps.Coord.Mutate(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		return e.store.SetSettingTx(ctx, tx, "tag", "x")
	})
	if err == nil {
		t.Fatal("Store 错误必须向外返回")
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("Store 错误不应 apply，实际 %d", got)
	}
}

// TestCoordinatorSerializesConcurrentMutations 并发变更被串行化：每次提交各 apply 一次且无竞态
func TestCoordinatorSerializesConcurrentMutations(t *testing.T) {
	e := newTestEnv(t)
	const n = 16

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "key-" + strconv.Itoa(i)
			err := e.deps.Coord.Mutate(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
				return e.store.SetSettingTx(ctx, tx, key, "v")
			})
			if err != nil {
				t.Errorf("并发 Mutate 失败: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if got := e.applyCount(); got != n {
		t.Errorf("apply 次数 = %d, want %d", got, n)
	}
	settings, err := e.store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if len(settings) != n {
		t.Errorf("设置键数 = %d, want %d", len(settings), n)
	}
}

// TestCoordinatorReadThenWriteReservesLock 实际引用检查与规则写入不受独立日志连接提交干扰，commit 后只发布一次。
func TestCoordinatorReadThenWriteReservesLock(t *testing.T) {
	e := newTestEnv(t)
	addTargetForTest(t, e.store, config.TargetConfig{CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "lhins-research"})
	targets, err := e.store.GetTargets()
	if err != nil {
		t.Fatal(err)
	}
	dsn := (&url.URL{Scheme: "file", Path: e.dbPath, RawQuery: "_pragma=busy_timeout(0)"}).String()
	other, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := other.Close(); err != nil {
			t.Error(err)
		}
	}()
	rule := config.DomainRule{Host: "research.example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{targets[0].ID}}
	foreignCode := 0
	err = e.deps.Coord.Mutate(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		if err := e.store.ValidateRuleTargetsTx(ctx, tx, rule.Targets); err != nil {
			return err
		}
		_, foreignErr := other.Exec("INSERT INTO sync_logs (target) VALUES ('foreign')")
		var se *sqlite.Error
		if errors.As(foreignErr, &se) {
			foreignCode = se.Code()
		}
		return e.store.AddRuleTx(ctx, tx, rule)
	})
	t.Logf("foreign_code=%d mutation=%v apply=%d", foreignCode, err, e.applyCount())
	if err != nil {
		t.Fatalf("配置协调器先读后写失败: %v", err)
	}
	if foreignCode != 5 {
		t.Errorf("未在事务开始预留写锁: foreign=%d", foreignCode)
	}
	if e.applyCount() != 1 {
		t.Fatalf("apply=%d", e.applyCount())
	}
	stored, err := e.store.GetRules()
	if err != nil || len(stored) != 1 || stored[0].Host != rule.Host || len(stored[0].Targets) != 1 || stored[0].Targets[0] != targets[0].ID {
		t.Fatalf("stored=%v err=%v", stored, err)
	}
	if state := e.snapshot(); state == nil || len(state.Config.DomainRules) != 1 || state.Config.DomainRules[0].ID != stored[0].ID || state.Config.DomainRules[0].Host != rule.Host {
		t.Fatal("运行时未发布完整规则")
	}
}

// TestCoordinatorCanceledLockWait 不承诺毫秒级取消，但取消请求必须零提交、零发布并释放连接。
func TestCoordinatorCanceledLockWait(t *testing.T) {
	e := newTestEnv(t)
	holder, err := e.store.BeginTx(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := holder.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Error(err)
		}
	}()
	if err = e.store.SetSettingTx(context.Background(), holder, "theme", "light"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		done <- e.deps.Coord.Mutate(ctx, func(ctx context.Context, tx *sql.Tx) error {
			return e.store.SetSettingTx(ctx, tx, "tag", "canceled-write")
		})
	}()
	<-started
	var earlyErr error
	early := false
	select {
	case earlyErr = <-done:
		early = true
		t.Error("写锁释放或取消前请求已结束")
	case <-time.After(150 * time.Millisecond):
	}
	cancel()
	if err = holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	if early {
		err = earlyErr
	} else {
		select {
		case err = <-done:
		case <-time.After(7 * time.Second):
			t.Fatal("取消请求未结束")
		}
	}
	if err == nil {
		t.Fatal("取消请求不应提交")
	}
	if e.applyCount() != 0 || e.snapshot() != nil {
		t.Fatal("取消请求发布了运行时")
	}
	settings, readErr := e.store.GetSettings()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(settings) != 0 {
		t.Fatalf("取消请求或已回滚的持锁事务留下写入: %v", settings)
	}
	if err = e.deps.Coord.Mutate(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		return e.store.SetSettingTx(ctx, tx, "tag", "after-cancel")
	}); err != nil {
		t.Fatal(err)
	}
	if e.applyCount() != 1 {
		t.Fatal("后续正常请求应只发布一次")
	}
	settings, err = e.store.GetSettings()
	if err != nil || settings["tag"] != "after-cancel" {
		t.Fatalf("后续请求未提交: %v %v", settings, err)
	}
}
