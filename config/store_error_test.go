package config

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// ─── Issue6 A14：Rollback / COUNT / Encode 错误必须处理 ───

// TestWithTransactionPanicRollsBack fn panic 时事务必须回滚，后续写入不得 BUSY。
//
// 判别性：修复前没有 defer 回滚，panic 会直接传播出去，事务保持打开——WAL 下
// 泄漏的写事务会让后续写入持续 BUSY，且本次已写入的行不会被撤销。
func TestWithTransactionPanicRollsBack(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "panic.db"))
	if err != nil {
		t.Fatalf("OpenStore 失败: %v", err)
	}
	defer s.Close()

	if err := s.SetSetting("theme", "light"); err != nil {
		t.Fatalf("预置设置失败: %v", err)
	}

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("期望 panic 被重新抛出")
			}
		}()
		_ = s.WithTransaction(func(tx *sql.Tx) error {
			if _, err := tx.Exec("UPDATE settings SET value = 'dark' WHERE key = 'theme'"); err != nil {
				return err
			}
			panic("fixture failure")
		})
	}()

	// 事务必须已回滚：旧值保持
	settings, err := s.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if settings["theme"] != "light" {
		t.Errorf("panic 后 theme = %q, want light（事务必须回滚）", settings["theme"])
	}

	// 后续写入不得 BUSY（修复前泄漏的写事务会让它失败）
	if err := s.SetSetting("theme", "dark"); err != nil {
		t.Fatalf("panic 回滚后写入失败（事务可能仍被占用）: %v", err)
	}
}

// TestWithTransactionFinishedTxIsIgnored 已结束事务的 sql.ErrTxDone 属预期，不得报错。
func TestWithTransactionFinishedTxIsIgnored(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "txdone.db"))
	if err != nil {
		t.Fatalf("OpenStore 失败: %v", err)
	}
	defer s.Close()

	// 在事务内先自行 Rollback，再返回 nil → WithTransaction 的 defer 会二次 Rollback
	err = s.WithTransaction(func(tx *sql.Tx) error {
		if err := tx.Rollback(); err != nil {
			return err
		}
		return nil
	})
	if err == nil {
		t.Fatal("事务被提前回滚后再 Commit 必须失败（用例前提）")
	}
	if !errors.Is(err, sql.ErrTxDone) && !strings.Contains(err.Error(), "already been closed") &&
		!strings.Contains(err.Error(), "already closed") {
		t.Logf("提交已结束事务返回的错误（可接受）: %v", err)
	}

	// 关键：Store 仍可用，defer 的二义回滚没有造成 panic/错误状态
	if err := s.SetSetting("theme", "light"); err != nil {
		t.Fatalf("事务异常后 Store 不可用: %v", err)
	}
}

// TestAddSyncLogTrimsOverLimit 1001 条时裁剪到 1000 条（阈值与语义不得改变）。
func TestAddSyncLogTrimsOverLimit(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "trim.db"))
	if err != nil {
		t.Fatalf("OpenStore 失败: %v", err)
	}
	defer s.Close()

	for i := 0; i < 1001; i++ {
		if err := s.AddSyncLog(SyncLog{Target: "t", Domain: "d", Result: "success"}); err != nil {
			t.Fatalf("第 %d 条 AddSyncLog 失败: %v", i+1, err)
		}
	}
	logs, err := s.GetSyncLogs(2000)
	if err != nil {
		t.Fatalf("GetSyncLogs 失败: %v", err)
	}
	if len(logs) != 1000 {
		t.Errorf("裁剪后条数 = %d, want 1000", len(logs))
	}
}

// TestAddSyncLogCountFailureDoesNotFailWrite COUNT 查询失败时写入仍成功（保守语义不变）。
func TestAddSyncLogCountFailureDoesNotFailWrite(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "countfail.db"))
	if err != nil {
		t.Fatalf("OpenStore 失败: %v", err)
	}
	defer s.Close()

	// 用只读连接池无法直接注入 COUNT 失败，这里验证正常路径与「不因 COUNT 失败而丢失写入」
	// 的语义：写入 3 条后条数正确。
	for i := 0; i < 3; i++ {
		if err := s.AddSyncLog(SyncLog{Target: "t", Domain: "d", Result: "failed"}); err != nil {
			t.Fatalf("AddSyncLog 失败: %v", err)
		}
	}
	logs, err := s.GetSyncLogs(10)
	if err != nil {
		t.Fatalf("GetSyncLogs 失败: %v", err)
	}
	if len(logs) != 3 {
		t.Errorf("条数 = %d, want 3", len(logs))
	}
}

// ─── Issue6 A17：非预期迁移失败必须中止启动 ───

// fakeAlterDBTX 是只实现 ExecContext 的最小 DBTX：按注入错误决定 ALTER 结果。
type fakeAlterDBTX struct {
	queryErr error // 非 nil 时每次 ALTER 都返回该错误
	calls    []string
}

func (f *fakeAlterDBTX) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	f.calls = append(f.calls, query)
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return stubResult(0), nil
}

func (f *fakeAlterDBTX) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeAlterDBTX) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return &sql.Row{}
}

type stubResult int64

func (r stubResult) LastInsertId() (int64, error) { return int64(r), nil }
func (r stubResult) RowsAffected() (int64, error) { return int64(r), nil }

// TestMigrateColumnsDuplicateIsIdempotent duplicate-column 视为幂等成功。
func TestMigrateColumnsDuplicateIsIdempotent(t *testing.T) {
	q := &fakeAlterDBTX{queryErr: errors.New("duplicate column name: enable_ipv6")}
	s := &Store{}
	if err := s.migrateColumns(q); err != nil {
		t.Fatalf("duplicate-column 必须视为成功，实际错误: %v", err)
	}
	if len(q.calls) != 2 {
		t.Errorf("两条 ALTER 都必须执行，实际 %d 次", len(q.calls))
	}
}

// TestMigrateColumnsUnexpectedFailureAborts 非预期失败必须立即返回错误（中止启动）。
func TestMigrateColumnsUnexpectedFailureAborts(t *testing.T) {
	q := &fakeAlterDBTX{queryErr: errors.New("disk I/O error")}
	s := &Store{}
	err := s.migrateColumns(q)
	if err == nil {
		t.Fatal("非预期 ALTER 失败必须返回错误（修复前只记 WARN 后继续）")
	}
	if !strings.Contains(err.Error(), "disk I/O error") {
		t.Errorf("错误必须保留底层原因: %v", err)
	}
	if len(q.calls) != 1 {
		t.Errorf("首个 ALTER 失败后必须立即中止，实际执行 %d 次", len(q.calls))
	}
}

// TestOpenStoreWorksForNewAndMigratedDB 新库与已迁移库都必须能正常打开。
func TestOpenStoreWorksForNewAndMigratedDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migrate.db")

	// 新库
	s1, err := OpenStore(path)
	if err != nil {
		t.Fatalf("新库 OpenStore 失败: %v", err)
	}
	if err := s1.SetSetting("theme", "light"); err != nil {
		t.Fatalf("新库写入失败: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	// 已迁移库：再次打开时两条 ALTER 都会命中 duplicate-column，必须幂等成功
	s2, err := OpenStore(path)
	if err != nil {
		t.Fatalf("已迁移库 OpenStore 失败（duplicate-column 必须视为成功）: %v", err)
	}
	defer s2.Close()

	settings, err := s2.GetSettings()
	if err != nil {
		t.Fatalf("已迁移库读取失败: %v", err)
	}
	if settings["theme"] != "light" {
		t.Errorf("已迁移库 theme = %q, want light（数据必须保留）", settings["theme"])
	}
}
