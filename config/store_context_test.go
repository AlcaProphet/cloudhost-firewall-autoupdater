package config

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sqlite "modernc.org/sqlite"
)

func i8r14Store(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "i8-r14.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s, path
}

// 包装真实驱动以在语句完成后设置取消屏障，不增加生产钩子。
type i8r14Connector struct {
	dsn         string
	beforeBegin func(context.Context)
	beforeExec  func(context.Context, string) error
	afterExec   func(context.Context, string)
}

func (c *i8r14Connector) Driver() driver.Driver { return &sqlite.Driver{} }
func (c *i8r14Connector) Connect(context.Context) (driver.Conn, error) {
	raw, err := (&sqlite.Driver{}).Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &i8r14Conn{Conn: raw, owner: c}, nil
}

type i8r14Conn struct {
	driver.Conn
	owner *i8r14Connector
}

func (c *i8r14Conn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	if c.owner.beforeBegin != nil {
		c.owner.beforeBegin(ctx)
	}
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, o)
}
func (c *i8r14Conn) ExecContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	if c.owner.beforeExec != nil {
		if err := c.owner.beforeExec(ctx, q); err != nil {
			return nil, err
		}
	}
	result, err := c.Conn.(driver.ExecerContext).ExecContext(ctx, q, a)
	if err == nil && c.owner.afterExec != nil {
		c.owner.afterExec(ctx, q)
	}
	return result, err
}
func (c *i8r14Conn) QueryContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, a)
}
func i8r14Wrapped(t *testing.T, path string, c *i8r14Connector) *Store {
	t.Helper()
	var err error
	c.dsn, err = sqliteDSN(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{db: sql.OpenDB(c)}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

type i8r14ContextKey struct{}

func TestI8R14ScanPersistenceContext(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cancelAt   string
		empty      bool
		failSecond bool
	}{
		{name: "pre_canceled", cancelAt: "before"},
		{name: "after_delete", cancelAt: "delete"},
		{name: "after_delete_empty", cancelAt: "delete", empty: true},
		{name: "after_first_insert", cancelAt: "insert"},
		{name: "second_insert_failure", failSecond: true},
		{name: "normal"},
		{name: "normal_empty", empty: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, path := i8r14Store(t)
			for _, region := range []string{"selected", "other"} {
				if err := s.ReplaceScannedResources("ali_ecs", region, []ScannedResource{{ResourceID: "old-" + region}}); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.ReplaceScannedResources("tc_cvm", "selected", []ScannedResource{{ResourceID: "other-cloud"}}); err != nil {
				t.Fatal(err)
			}
			before, err := s.GetScannedResources("ali_ecs")
			if err != nil {
				t.Fatal(err)
			}
			otherCloud, err := s.GetScannedResources("tc_cvm")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), i8r14ContextKey{}, "operation"))
			defer cancel()
			sentinel := errors.New("fixture second insert failure")
			inserts := 0
			begins := 0
			statements := 0
			c := &i8r14Connector{beforeBegin: func(got context.Context) {
				begins++
				if got.Value(i8r14ContextKey{}) != "operation" {
					t.Error("事务丢失调用方context")
				}
			}}
			c.beforeExec = func(got context.Context, q string) error {
				statements++
				if got.Value(i8r14ContextKey{}) != "operation" {
					t.Error("语句丢失调用方context")
				}
				if strings.HasPrefix(q, "INSERT INTO scanned_resources") {
					inserts++
					if tc.failSecond && inserts == 2 {
						return sentinel
					}
				}
				return nil
			}
			c.afterExec = func(_ context.Context, q string) {
				if tc.cancelAt == "delete" && strings.HasPrefix(q, "DELETE FROM scanned_resources") {
					cancel()
				}
				if tc.cancelAt == "insert" && strings.HasPrefix(q, "INSERT INTO scanned_resources") {
					cancel()
				}
			}
			subject := i8r14Wrapped(t, path, c)
			resources := []ScannedResource{{ResourceID: "new-a"}, {ResourceID: "new-b"}}
			if tc.empty {
				resources = []ScannedResource{}
			}
			if tc.cancelAt == "before" {
				cancel()
			}
			err = subject.ReplaceScannedResourcesContext(ctx, "ali_ecs", "selected", resources)
			failed := tc.cancelAt != "" || tc.failSecond
			if failed && err == nil {
				t.Error("取消或SQL失败后不应提交成功")
			}
			if !failed && err != nil {
				t.Fatal(err)
			}
			if tc.failSecond && !errors.Is(err, sentinel) {
				t.Errorf("SQL错误身份丢失: %v", err)
			}
			after, readErr := s.GetScannedResources("ali_ecs")
			if readErr != nil {
				t.Fatal(readErr)
			}
			if failed {
				if !reflect.DeepEqual(before, after) {
					t.Errorf("失败改变旧缓存: before=%+v after=%+v", before, after)
				}
			} else {
				selected := []string{}
				for _, item := range after {
					if item.Region == "selected" {
						selected = append(selected, item.ResourceID)
					}
				}
				want := []string{}
				if !tc.empty {
					want = []string{"new-a", "new-b"}
				}
				if !reflect.DeepEqual(selected, want) {
					t.Errorf("替换结果=%v want=%v", selected, want)
				}
				for _, old := range before {
					if old.Region == "other" {
						found := false
						for _, item := range after {
							if item == old {
								found = true
							}
						}
						if !found {
							t.Error("修改了其他地域")
						}
					}
				}
			}
			cloudAfter, readErr := s.GetScannedResources("tc_cvm")
			if readErr != nil || !reflect.DeepEqual(otherCloud, cloudAfter) {
				t.Errorf("修改了其他产品: %+v %v", cloudAfter, readErr)
			}
			if tc.cancelAt == "before" && (begins != 0 || statements != 0) {
				t.Errorf("已取消入口仍进入SQL: begin=%d statement=%d", begins, statements)
			}
			// 取消后用未包装的同一SQLite库再次提交，验证写锁已释放。
			if err := s.ReplaceScannedResources("ali_ecs", "selected", []ScannedResource{{ResourceID: "recovered"}}); err != nil {
				t.Fatalf("回滚后未恢复: %v", err)
			}
		})
	}
}

func TestI8R14TransactionContextCleanup(t *testing.T) {
	for _, kind := range []string{"callback_error", "panic", "canceled_before_commit", "already_rolled_back", "normal", "legacy"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := i8r14Store(t)
			if err := s.SetSetting("theme", "light"); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sentinel := errors.New("fixture callback failure")
			var result error
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				fn := func(tx *sql.Tx) error {
					if _, err := tx.ExecContext(ctx, "UPDATE settings SET value='dark' WHERE key='theme'"); err != nil {
						return err
					}
					switch kind {
					case "callback_error":
						return sentinel
					case "panic":
						panic(sentinel)
					case "canceled_before_commit":
						cancel()
					case "already_rolled_back":
						if err := tx.Rollback(); err != nil {
							return err
						}
					}
					return nil
				}
				if kind == "legacy" {
					result = s.WithTransaction(fn)
				} else {
					result = s.WithTransactionContext(ctx, fn)
				}
			}()
			successful := kind == "normal" || kind == "legacy"
			if kind == "panic" {
				if recovered != sentinel {
					t.Errorf("panic未原样传播: %v", recovered)
				}
			} else if recovered != nil {
				t.Errorf("意外panic: %v", recovered)
			}
			if successful && result != nil {
				t.Fatal(result)
			}
			if !successful && kind != "panic" && result == nil {
				t.Error("失败事务返回成功")
			}
			if kind == "callback_error" && !errors.Is(result, sentinel) {
				t.Errorf("callback错误身份丢失: %v", result)
			}
			settings, err := s.GetSettings()
			if err != nil {
				t.Fatal(err)
			}
			want := "light"
			if successful {
				want = "dark"
			}
			if settings["theme"] != want {
				t.Errorf("theme=%q want=%q", settings["theme"], want)
			}
			if err := s.SetSetting("theme", "recovered"); err != nil {
				t.Fatalf("事务清理后无法写入: %v", err)
			}
		})
	}
}

func TestI8R14CanceledBeginLockRecovery(t *testing.T) {
	s, path := i8r14Store(t)
	if err := s.SetSetting("theme", "light"); err != nil {
		t.Fatal(err)
	}
	holder, err := s.BeginTx(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := holder.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Error(err)
		}
	}()
	entered := make(chan struct{})
	var callback atomic.Int32
	subject := i8r14Wrapped(t, path, &i8r14Connector{beforeBegin: func(context.Context) { close(entered) }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- subject.WithTransactionContext(ctx, func(tx *sql.Tx) error {
			callback.Add(1)
			_, err := tx.ExecContext(ctx, "UPDATE settings SET value='dark' WHERE key='theme'")
			return err
		})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("未进入驱动BeginTx")
	}
	cancel()
	// 不设毫秒级完成断言；主动释放真实写锁，确保测试自身有界。
	if err := holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Error("取消的事务不应提交")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("释放写锁后事务未结束")
	}
	settings, err := s.GetSettings()
	if err != nil || settings["theme"] != "light" {
		t.Fatalf("取消事务改变配置: %v %v", settings, err)
	}
	if err := s.SetSetting("theme", "recovered"); err != nil {
		t.Fatal(err)
	}
	t.Logf("取消入口回调次数=%d；不约束驱动错误一定是context取消类型", callback.Load())
}
