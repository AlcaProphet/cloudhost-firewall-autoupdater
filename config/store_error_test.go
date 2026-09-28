package config

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

const insertSyncLogSQL = "INSERT INTO sync_logs (timestamp, target, domain, result, added, deleted, error) VALUES (?, ?, ?, ?, ?, ?, ?)"

var errFixtureCountFailure = errors.New("fixture count failure")

type syncLogDriverCall struct {
	kind  string
	query string
	args  []driver.NamedValue
}

// syncLogDriverState 记录 database/sql 发给测试驱动的调用；锁使夹具在 race 下安全。
type syncLogDriverState struct {
	mu    sync.Mutex
	calls []syncLogDriverCall
}

func (s *syncLogDriverState) record(kind, query string, args []driver.NamedValue) {
	copiedArgs := append([]driver.NamedValue(nil), args...)
	s.mu.Lock()
	s.calls = append(s.calls, syncLogDriverCall{kind: kind, query: query, args: copiedArgs})
	s.mu.Unlock()
}

func (s *syncLogDriverState) snapshot() []syncLogDriverCall {
	s.mu.Lock()
	defer s.mu.Unlock()

	calls := make([]syncLogDriverCall, len(s.calls))
	for i, call := range s.calls {
		calls[i] = syncLogDriverCall{
			kind:  call.kind,
			query: call.query,
			args:  append([]driver.NamedValue(nil), call.args...),
		}
	}
	return calls
}

type syncLogFailureConnector struct {
	state *syncLogDriverState
}

func (c *syncLogFailureConnector) Connect(context.Context) (driver.Conn, error) {
	return &syncLogFailureConn{state: c.state}, nil
}

func (c *syncLogFailureConnector) Driver() driver.Driver {
	return &syncLogFailureDriver{state: c.state}
}

type syncLogFailureDriver struct {
	state *syncLogDriverState
}

func (d *syncLogFailureDriver) Open(string) (driver.Conn, error) {
	return &syncLogFailureConn{state: d.state}, nil
}

type syncLogFailureConn struct {
	state *syncLogDriverState
}

func (c *syncLogFailureConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("fixture does not support prepared statements")
}

func (c *syncLogFailureConn) Close() error { return nil }

func (c *syncLogFailureConn) Begin() (driver.Tx, error) {
	return nil, errors.New("fixture does not support transactions")
}

func (c *syncLogFailureConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.state.record("exec", query, args)
	if query != insertSyncLogSQL {
		return nil, errors.New("fixture only accepts the sync log INSERT")
	}
	return driver.RowsAffected(1), nil
}

func (c *syncLogFailureConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.record("query", query, args)
	if query != "SELECT COUNT(*) FROM sync_logs" {
		return nil, errors.New("fixture only accepts the sync log COUNT query")
	}
	return nil, errFixtureCountFailure
}

// TestAddSyncLogCountFailureDoesNotFailWrite COUNT 查询失败时写入仍成功、跳过裁剪并记录安全日志。
func TestAddSyncLogCountFailureDoesNotFailWrite(t *testing.T) {
	state := &syncLogDriverState{}
	db := sql.OpenDB(&syncLogFailureConnector{state: state})
	defer db.Close()
	s := &Store{db: db}

	var logBuf bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(previousLogger)

	entry := SyncLog{
		Timestamp: time.Date(2026, time.September, 28, 12, 34, 56, 0, time.UTC),
		Target:    "fixture-target-business-sentinel",
		Domain:    "fixture-domain-business-sentinel.example",
		Result:    "failed",
		Added:     3,
		Deleted:   2,
		Error:     "fixture-error-business-sentinel",
	}
	if err := s.AddSyncLog(entry); err != nil {
		t.Fatalf("COUNT 失败不得让已成功的 AddSyncLog 报错: %v", err)
	}

	calls := state.snapshot()
	if len(calls) != 2 {
		t.Fatalf("驱动调用次数 = %d, want 2（一次 INSERT 后一次 COUNT）: %+v", len(calls), calls)
	}
	if calls[0].kind != "exec" || calls[0].query != insertSyncLogSQL {
		t.Errorf("第一个调用 = %s %q, want sync log INSERT", calls[0].kind, calls[0].query)
	}
	if calls[1].kind != "query" || calls[1].query != "SELECT COUNT(*) FROM sync_logs" {
		t.Errorf("第二个调用 = %s %q, want COUNT", calls[1].kind, calls[1].query)
	}
	if len(calls[0].args) != 7 {
		t.Fatalf("INSERT 参数数 = %d, want 7", len(calls[0].args))
	}
	wantArgs := []any{entry.Timestamp, entry.Target, entry.Domain, entry.Result, int64(entry.Added), int64(entry.Deleted), entry.Error}
	for i, want := range wantArgs {
		if calls[0].args[i].Value != want {
			t.Errorf("INSERT 参数[%d] = %#v, want %#v", i, calls[0].args[i].Value, want)
		}
	}

	logs := logBuf.String()
	for _, required := range []string{"统计同步日志条数失败，跳过本次裁剪", errFixtureCountFailure.Error()} {
		if !strings.Contains(logs, required) {
			t.Errorf("错误日志缺少 %q: %s", required, logs)
		}
	}
	for _, forbidden := range []string{entry.Target, entry.Domain, entry.Error} {
		if strings.Contains(logs, forbidden) {
			t.Errorf("错误日志不得包含业务值 %q: %s", forbidden, logs)
		}
	}
}

// ─── Issue6 A17：非预期迁移失败必须中止启动 ───

// ─── Build7 Step 1：Schema 迁移错误路径（原 migrateColumns 断言的新形态） ───

// TestEnsureColumnTxUnexpectedFailureAborts 列探测/ALTER 的非预期失败必须返回错误
// 并保留底层原因（Issue6 A17 口径：迁移失败中止启动，不得带不完整 Schema 运行）。
func TestEnsureColumnTxUnexpectedFailureAborts(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "ensure.db"))
	if err != nil {
		t.Fatalf("打开库失败: %v", err)
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			t.Errorf("关闭库失败: %v", cerr)
		}
	}()

	ctx := context.Background()
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("开始事务失败: %v", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			t.Errorf("回滚失败: %v", rbErr)
		}
	}()

	added, err := ensureColumnTx(ctx, tx, "no_such_table", "x", "ALTER TABLE no_such_table ADD COLUMN x TEXT")
	if err == nil {
		t.Fatal("对不存在的表执行迁移列必须返回错误")
	}
	if added {
		t.Error("失败时不得报告已新增列（一次性迁移判定不得被污染）")
	}
	if !strings.Contains(err.Error(), "no_such_table") {
		t.Errorf("错误必须包含表名以便定位: %v", err)
	}
}

// TestOpenStoreMigrationFailureRollsBack 迁移失败必须整体回滚：不得留下新建的
// alert_policy / uptime_kuma_push 表，也不得把同名对象改成表。
//
// 注入方式：用同名 VIEW 占据 alert_email 名称，使目标 Schema 无法建立。
func TestOpenStoreMigrationFailureRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开夹具库失败: %v", err)
	}
	defer func() {
		if cerr := raw.Close(); cerr != nil {
			t.Errorf("关闭夹具库失败: %v", cerr)
		}
	}()

	if _, err := raw.Exec("CREATE VIEW alert_email AS SELECT 1 AS id"); err != nil {
		t.Fatalf("建立同名视图失败: %v", err)
	}

	if _, err := OpenStore(path); err == nil {
		t.Fatal("迁移/建表失败时 OpenStore 必须返回错误，不得带不完整 Schema 启动")
	}

	// 事务必须整体回滚：新表不存在，alert_email 仍是 VIEW
	if tableExists(t, raw, "alert_policy") {
		t.Error("失败后不得留下 alert_policy（Schema 迁移必须整体回滚）")
	}
	if tableExists(t, raw, "uptime_kuma_push") {
		t.Error("失败后不得留下 uptime_kuma_push（Schema 迁移必须整体回滚）")
	}
	var objType string
	if err := raw.QueryRow("SELECT type FROM sqlite_master WHERE name='alert_email'").Scan(&objType); err != nil {
		t.Fatalf("查询 alert_email 对象类型失败: %v", err)
	}
	if objType != "view" {
		t.Errorf("失败后 alert_email 对象类型 = %q, want view（不得被改写成表）", objType)
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

	// 已迁移库：再次打开时必须幂等成功（列已存在，PRAGMA 探测直接跳过 ALTER）
	s2, err := OpenStore(path)
	if err != nil {
		t.Fatalf("已迁移库 OpenStore 失败（重复迁移必须幂等）: %v", err)
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
