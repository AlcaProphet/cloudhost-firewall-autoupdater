package config

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// p310Connector 用标准驱动接口注入初始化和关闭失败，不替换生产全局变量。
type p310Connector struct {
	stage             string
	primary, closing  error
	closes, rollbacks int
}

func (c *p310Connector) Connect(context.Context) (driver.Conn, error) { return &p310Conn{c}, nil }
func (c *p310Connector) Driver() driver.Driver                        { return p310Driver{} }

type p310Driver struct{}

func (p310Driver) Open(string) (driver.Conn, error) { return nil, errors.New("unused") }

type p310Conn struct{ c *p310Connector }

func (c *p310Conn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (c *p310Conn) Close() error                        { c.c.closes++; return c.c.closing }
func (c *p310Conn) Begin() (driver.Tx, error) {
	if c.c.stage == "begin" {
		return nil, c.c.primary
	}
	return &p310Tx{c.c}, nil
}
func (c *p310Conn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if (c.c.stage == "wal" && strings.Contains(q, "journal_mode")) || (c.c.stage == "schema" && strings.Contains(q, "CREATE TABLE")) {
		return nil, c.c.primary
	}
	return driver.RowsAffected(0), nil
}

type p310Tx struct{ c *p310Connector }

func (tx *p310Tx) Commit() error   { return nil }
func (tx *p310Tx) Rollback() error { tx.c.rollbacks++; return nil }

// TestOpenStoreFailurePreservesCloseError 同时保留主错误、错误类型与关闭错误。
func TestOpenStoreFailurePreservesCloseError(t *testing.T) {
	for _, stage := range []string{"wal", "begin", "schema"} {
		for _, closeFails := range []bool{false, true} {
			name := stage
			if closeFails {
				name += "_close_error"
			}
			t.Run(name, func(t *testing.T) {
				primary := &os.PathError{Op: stage, Path: "fixture.db", Err: errors.New("primary")}
				var closeErr error
				if closeFails {
					closeErr = errors.New("close")
				}
				c := &p310Connector{stage: stage, primary: primary, closing: closeErr}
				db := sql.OpenDB(c)
				t.Cleanup(func() {
					if err := db.Close(); err != nil {
						t.Errorf("关闭测试数据库失败: %v", err)
					}
				})
				store, err := openStoreDB(db, filepath.Join(t.TempDir(), "config.db"))
				if store != nil || !errors.Is(err, primary) {
					t.Fatalf("主错误被丢失: store=%v err=%v", store, err)
				}
				var pathErr *os.PathError
				if !errors.As(err, &pathErr) || pathErr != primary {
					t.Error("主错误类型未保留")
				}
				if closeErr != nil && !errors.Is(err, closeErr) {
					t.Error("关闭错误被丢失")
				}
				if closeErr != nil && strings.Index(err.Error(), primary.Error()) > strings.Index(err.Error(), "关闭数据库失败") {
					t.Error("关闭错误不得覆盖或先于主错误")
				}
				if c.closes != 1 {
					t.Errorf("close=%d want 1", c.closes)
				}
				if stage == "schema" && c.rollbacks != 1 {
					t.Errorf("rollback=%d want 1", c.rollbacks)
				}
				if err := db.Ping(); err == nil {
					t.Error("失败数据库仍可用")
				}
			})
		}
	}
}

// TestOpenStoreSuccessKeepsDBOpen 防止失败收尾误关正常数据库。
func TestOpenStoreSuccessKeepsDBOpen(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "success.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := s.db.Ping(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting("theme", "dark"); err != nil {
		t.Fatal(err)
	}

	settings, err := s.GetSettings()
	if err != nil || settings["theme"] != "dark" {
		t.Fatalf("初始化成功后无法正常读写: settings=%v err=%v", settings, err)
	}
}
