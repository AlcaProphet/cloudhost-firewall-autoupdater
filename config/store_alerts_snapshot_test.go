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
	"sync/atomic"
	"testing"
	"time"

	sqlite "modernc.org/sqlite"
)

// 仅测试驱动包装，不增加生产钩子；所有查询仍由真实 SQLite 执行。
type i818SnapshotConnector struct {
	dsn                                  string
	before                               func(string) error
	beginErr, commitErr, rollbackErr     error
	begins, readOnly, commits, rollbacks atomic.Int64
}

func (c *i818SnapshotConnector) Driver() driver.Driver { return &sqlite.Driver{} }
func (c *i818SnapshotConnector) Connect(ctx context.Context) (driver.Conn, error) {
	raw, err := (&sqlite.Driver{}).Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &i818SnapshotConn{raw, c}, nil
}

type i818SnapshotConn struct {
	driver.Conn
	owner *i818SnapshotConnector
}

func (c *i818SnapshotConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	c.owner.begins.Add(1)
	if o.ReadOnly {
		c.owner.readOnly.Add(1)
	}
	if c.owner.beginErr != nil {
		return nil, c.owner.beginErr
	}
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, o)
	if err != nil {
		return nil, err
	}
	return &i818SnapshotTx{tx, c.owner}, nil
}
func (c *i818SnapshotConn) QueryContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	if c.owner.before != nil {
		if err := c.owner.before(q); err != nil {
			return nil, err
		}
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, a)
}
func (c *i818SnapshotConn) ExecContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, a)
}

type i818SnapshotTx struct {
	driver.Tx
	owner *i818SnapshotConnector
}

func (x *i818SnapshotTx) Commit() error {
	x.owner.commits.Add(1)
	if x.owner.commitErr != nil {
		// 模拟驱动提交失败并清理底层事务，符合 database/sql 的连接归还要求。
		if err := x.Tx.Rollback(); err != nil {
			return errors.Join(x.owner.commitErr, err)
		}
		return x.owner.commitErr
	}
	return x.Tx.Commit()
}
func (x *i818SnapshotTx) Rollback() error {
	x.owner.rollbacks.Add(1)
	return errors.Join(x.Tx.Rollback(), x.owner.rollbackErr)
}
func i818SnapshotEnv(t *testing.T) (*Store, *Store, *i818SnapshotConnector) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "alerts-snapshot.db")
	writer, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
	})
	dsn, err := sqliteDSN(path)
	if err != nil {
		t.Fatal(err)
	}
	c := &i818SnapshotConnector{dsn: dsn}
	db := sql.OpenDB(c)
	reader := &Store{db: db}
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	})
	return reader, writer, c
}
func i818SnapshotSave(s *Store, tag string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	security := SMTPSecuritySTARTTLS
	p := DefaultAlertPolicy()
	if tag == "new" {
		p.HealthTimeoutText = "11m"
		security = SMTPSecurityImplicitTLS
	}
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			slog.Error("回滚测试夹具失败", "error", rbErr)
		}
	}()
	if err = s.ReplaceBusinessAlertsTx(ctx, tx, p, AlertEmailConfig{Security: security, Host: tag, Port: "587", Subject: tag, Body: tag}, AlertWebhookConfig{URL: tag, Channel: "dingtalk"}, UptimeKumaPushConfig{URL: tag, IntervalText: "60s"}); err != nil {
		return err
	}
	return tx.Commit()
}

// TestI818SnapshotBarrierWithoutProductionHooks 在第四读前完成写提交，验证同事务快照。
func TestI818SnapshotBarrierWithoutProductionHooks(t *testing.T) {
	r, w, c := i818SnapshotEnv(t)
	if err := i818SnapshotSave(w, "old"); err != nil {
		t.Fatal(err)
	}
	var hits int
	c.before = func(q string) error {
		if strings.Contains(q, "FROM uptime_kuma_push") && hits == 0 {
			hits++
			return i818SnapshotSave(w, "new")
		}
		return nil
	}
	got, err := r.LoadAlertsSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hits != 1 || got.Policy.HealthTimeoutText != "10m" || got.Email.Host != "old" || got.Email.Security != SMTPSecuritySTARTTLS || got.Webhook.URL != "old" || got.UptimeKumaPush.URL != "old" {
		t.Fatalf("同一事务快照不一致: %+v;屏障次数=%d", got, hits)
	}
	next, err := r.LoadAlertsSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if next.Policy.HealthTimeoutText != "11m" || next.Email.Host != "new" || next.Email.Security != SMTPSecurityImplicitTLS || next.Webhook.URL != "new" || next.UptimeKumaPush.URL != "new" {
		t.Fatal("新快照未更新")
	}
	if c.begins.Load() != 2 || c.readOnly.Load() != 2 || c.commits.Load() != 2 || r.db.Stats().InUse != 0 {
		t.Fatal("事务模式或释放异常")
	}
}

// TestI818SnapshotTransactionErrors 失败不返回半截快照，释放连接且可恢复读取。
func TestI818SnapshotTransactionErrors(t *testing.T) {
	for _, kind := range []string{"begin", "policy", "email", "webhook", "push", "commit", "rollback"} {
		t.Run(kind, func(t *testing.T) {
			r, _, c := i818SnapshotEnv(t)
			marker := errors.New("读取注入错误")
			var logBuf bytes.Buffer
			if kind == "rollback" {
				previous := slog.Default()
				slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
				defer slog.SetDefault(previous)
			}
			tables := map[string]string{"policy": "alert_policy", "email": "alert_email", "webhook": "alert_webhook", "push": "uptime_kuma_push"}
			switch kind {
			case "begin":
				c.beginErr = marker
			case "commit":
				c.commitErr = marker
			case "rollback":
				c.rollbackErr = errors.New("回滚注入错误")
				c.before = func(string) error { return marker }
			default:
				c.before = func(q string) error {
					if strings.Contains(q, "FROM "+tables[kind]+" ") {
						return marker
					}
					return nil
				}
			}
			got, err := r.LoadAlertsSnapshot(context.Background())
			if got != nil || !errors.Is(err, marker) {
				t.Fatalf("失败返回不符合合同: %+v %v", got, err)
			}
			if r.db.Stats().InUse != 0 {
				t.Fatal("失败后连接未释放")
			}
			if kind == "rollback" && (!strings.Contains(logBuf.String(), "回滚告警只读事务失败") || !strings.Contains(logBuf.String(), "回滚注入错误")) {
				t.Fatal("回滚错误未记录")
			}
			if kind == "commit" && c.commits.Load() != 1 {
				t.Fatal("未走提交失败出口")
			}
			if kind != "begin" && kind != "commit" && c.rollbacks.Load() != 1 {
				t.Fatal("读取失败未回滚")
			}
			c.beginErr = nil
			c.commitErr = nil
			c.rollbackErr = nil
			c.before = nil
			if _, err = r.LoadAlertsSnapshot(context.Background()); err != nil {
				t.Fatalf("恢复读取失败: %v", err)
			}
		})
	}
}

// TestI818SnapshotMidReadCancellation 读取中取消后有界释放连接，不返回成功快照。
func TestI818SnapshotMidReadCancellation(t *testing.T) {
	r, _, c := i818SnapshotEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.before = func(q string) error {
		if strings.Contains(q, "FROM uptime_kuma_push") {
			cancel()
		}
		return nil
	}
	got, err := r.LoadAlertsSnapshot(ctx)
	if got != nil || err == nil {
		t.Fatal("取消后返回成功")
	}
	deadline := time.Now().Add(time.Second)
	for r.db.Stats().InUse != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if r.db.Stats().InUse != 0 {
		t.Fatal("取消后连接未释放")
	}
	c.before = nil
	if _, err = r.LoadAlertsSnapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
}
