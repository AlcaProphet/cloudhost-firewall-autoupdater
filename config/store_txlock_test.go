package config

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"modernc.org/sqlite"
)

func newTxlockStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "txlock.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func rollbackTxlockTest(t *testing.T, tx *sql.Tx) {
	t.Helper()
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Error(err)
	}
}

// 用另一条真实连接在读写之间尝试提交；零等待只用于判别锁是否已被预留。
func TestStoreWriteTransactionReservesLock(t *testing.T) {
	s := newTxlockStore(t)
	ctx := context.Background()
	other, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := other.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err = other.ExecContext(ctx, "PRAGMA busy_timeout=0"); err != nil {
		t.Fatal(err)
	}
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackTxlockTest(t, tx)
	var n int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM targets").Scan(&n); err != nil {
		t.Fatal(err)
	}
	_, foreignErr := other.ExecContext(ctx, "INSERT INTO sync_logs (target) VALUES ('foreign')")
	writeErr := s.SetSettingTx(ctx, tx, "theme", "dark")
	var code *sqlite.Error
	foreignCode := 0
	if errors.As(foreignErr, &code) {
		foreignCode = code.Code()
	}
	var own *sqlite.Error
	ownCode := 0
	if errors.As(writeErr, &own) {
		ownCode = own.Code()
	}
	t.Logf("foreign_code=%d own_write_code=%d", foreignCode, ownCode)
	if foreignCode != 5 {
		t.Errorf("写事务未预留写锁，另一连接应 SQLITE_BUSY(5)，实际 %v", foreignErr)
	}
	if writeErr != nil {
		t.Fatalf("先读后写失败: %v", writeErr)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = other.ExecContext(ctx, "INSERT INTO sync_logs (target) VALUES ('after')"); err != nil {
		t.Fatal(err)
	}
}

// 写锁竞争必须发生在 BeginTx，释放后同一调用可继续，不重放 mutation。
func TestStoreBeginTxWaitsForWriter(t *testing.T) {
	s := newTxlockStore(t)
	ctx := context.Background()
	holder, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := holder.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err = holder.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	active := true
	defer func() {
		if active {
			if _, err := holder.ExecContext(ctx, "ROLLBACK"); err != nil {
				t.Error(err)
			}
		}
	}()
	type result struct {
		tx  *sql.Tx
		err error
	}
	done := make(chan result, 1)
	start := make(chan struct{})
	go func() { close(start); tx, err := s.BeginTx(ctx); done <- result{tx, err} }()
	<-start
	var premature *result
	select {
	case r := <-done:
		premature = &r
	case <-time.After(150 * time.Millisecond):
	}
	if _, err = holder.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	active = false
	var r result
	if premature != nil {
		r = *premature
		t.Error("BeginTx 在写锁释放前已返回，仍是 deferred")
	} else {
		select {
		case r = <-done:
		case <-time.After(7 * time.Second):
			t.Fatal("BeginTx 未在释放后完成")
		}
	}
	if r.err != nil {
		t.Fatal(r.err)
	}
	defer rollbackTxlockTest(t, r.tx)
	var n int
	if err = r.tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM targets").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err = s.SetSettingTx(ctx, r.tx, "theme", "dark"); err != nil {
		t.Fatal(err)
	}
	if err = r.tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// 持有写锁时仍可导出旧快照；写提交后老快照不变，新快照更新。
func TestStoreReadOnlySnapshotDuringWrite(t *testing.T) {
	s := newTxlockStore(t)
	ctx := context.Background()
	if err := s.SetSetting("theme", "light"); err != nil {
		t.Fatal(err)
	}
	writer, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackTxlockTest(t, writer)
	if err = s.SetSettingTx(ctx, writer, "theme", "dark"); err != nil {
		t.Fatal(err)
	}
	reader, err := s.BeginReadOnlyTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackTxlockTest(t, reader)
	var first, second string
	if err = reader.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='theme'").Scan(&first); err != nil {
		t.Fatal(err)
	}
	if _, err = s.LoadBusinessSnapshotTx(ctx, reader); err != nil {
		t.Fatal(err)
	}
	if err = writer.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = reader.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='theme'").Scan(&second); err != nil {
		t.Fatal(err)
	}
	if first != "light" || second != "light" {
		t.Fatalf("读快照不一致 %q/%q", first, second)
	}
	if err = reader.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSettings()
	if err != nil || got["theme"] != "dark" {
		t.Fatalf("新读取=%v %v", got, err)
	}
}

// TestStoreBackgroundWritesWaitForCommit 真实日志与扫描写入不能使配置快照过期，提交后均完整落库。
func TestStoreBackgroundWritesWaitForCommit(t *testing.T) {
	for _, kind := range []string{"sync_log", "scan_cache"} {
		t.Run(kind, func(t *testing.T) {
			s := newTxlockStore(t)
			ctx := context.Background()
			tx, err := s.BeginTx(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollbackTxlockTest(t, tx)
			var n int
			if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM targets").Scan(&n); err != nil {
				t.Fatal(err)
			}
			started := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				close(started)
				if kind == "sync_log" {
					done <- s.AddSyncLog(SyncLog{Timestamp: time.Now(), Target: "research", Result: "success"})
				} else {
					done <- s.ReplaceScannedResources("tc_lighthouse", "ap-guangzhou", []ScannedResource{{ResourceID: "lhins-research", ResourceName: "research"}})
				}
			}()
			<-started
			early := false
			select {
			case backgroundErr := <-done:
				early = true
				if backgroundErr != nil {
					t.Error(backgroundErr)
				}
				t.Error("后台写入抢先提交，使配置读快照过期")
			case <-time.After(150 * time.Millisecond):
			}
			if err = s.SetSettingTx(ctx, tx, "theme", "dark"); err != nil {
				t.Error(err)
			} else if err = tx.Commit(); err != nil {
				t.Error(err)
			}
			// 失败时也释放读事务，再回收后台调用。
			rollbackTxlockTest(t, tx)
			if !early {
				select {
				case backgroundErr := <-done:
					if backgroundErr != nil {
						t.Error(backgroundErr)
					}
				case <-time.After(7 * time.Second):
					t.Fatal("后台写入未结束")
				}
			}
			if kind == "sync_log" {
				logs, err := s.GetSyncLogs(10)
				if err != nil || len(logs) != 1 {
					t.Fatalf("后台日志未落库: %v %v", logs, err)
				}
			} else {
				rows, err := s.GetScannedResources("tc_lighthouse")
				if err != nil || len(rows) != 1 || rows[0].ResourceID != "lhins-research" {
					t.Fatalf("扫描缓存未完整落库: %v %v", rows, err)
				}
			}
		})
	}
}

// TestStoreBeginTxBusyTimeoutAndRecovery 验证写锁超时发生在入口，且失败未泄漏事务。
func TestStoreBeginTxBusyTimeoutAndRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过约 5 秒的入口锁等待用例")
	}
	s := newTxlockStore(t)
	ctx := context.Background()
	holder, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := holder.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err = holder.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	tx, beginErr := s.BeginTx(ctx)
	elapsed := time.Since(start)
	if tx != nil {
		rollbackTxlockTest(t, tx)
	}
	if _, err = holder.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	var se *sqlite.Error
	t.Logf("BeginTx elapsed=%v err=%v", elapsed, beginErr)
	if !errors.As(beginErr, &se) || se.Code() != 5 {
		t.Fatalf("应是 SQLITE_BUSY(5): %v", beginErr)
	}
	if elapsed < 4*time.Second || elapsed > 15*time.Second {
		t.Fatalf("busy_timeout 等待异常: %v", elapsed)
	}
	next, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackTxlockTest(t, next)
	if err = s.SetSettingTx(ctx, next, "theme", "dark"); err != nil {
		t.Fatal(err)
	}
	if err = next.Commit(); err != nil {
		t.Fatal(err)
	}
}

// TestStoreWithTransactionReservesLock 验证扫描使用的通用写事务也在第一次读之前预留写锁。
func TestStoreWithTransactionReservesLock(t *testing.T) {
	s := newTxlockStore(t)
	other, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := other.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err = other.ExecContext(context.Background(), "PRAGMA busy_timeout=0"); err != nil {
		t.Fatal(err)
	}
	err = s.WithTransaction(func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow("SELECT COUNT(*) FROM targets").Scan(&n); err != nil {
			return err
		}
		_, foreignErr := other.ExecContext(context.Background(), "INSERT INTO sync_logs (target) VALUES ('foreign')")
		var se *sqlite.Error
		if !errors.As(foreignErr, &se) || se.Code() != 5 {
			t.Errorf("通用写事务没有预留写锁: %v", foreignErr)
		}
		return s.SetSettingTx(context.Background(), tx, "theme", "dark")
	})
	if err != nil {
		t.Fatal(err)
	}
	settings, err := s.GetSettings()
	if err != nil || settings["theme"] != "dark" {
		t.Fatalf("写事务未提交: %v %v", settings, err)
	}
	if _, err = other.ExecContext(context.Background(), "INSERT INTO sync_logs (target) VALUES ('after')"); err != nil {
		t.Fatal(err)
	}
}
