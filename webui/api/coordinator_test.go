package api

import (
	"context"
	"database/sql"
	"strconv"
	"sync"
	"testing"
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
