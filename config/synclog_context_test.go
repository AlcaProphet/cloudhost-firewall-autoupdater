package config

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type prepContextKey struct{}
type prepSQLState struct {
	mu                sync.Mutex
	calls             []string
	ctxValues         []any
	fail              string
	err               error
	count             int64
	cancelAfterInsert context.CancelFunc
	waitContext       bool
	entered           chan struct{}
}

func (s *prepSQLState) call(stage string, ctx context.Context) {
	s.mu.Lock()
	s.calls = append(s.calls, stage)
	s.ctxValues = append(s.ctxValues, ctx.Value(prepContextKey{}))
	s.mu.Unlock()
}
func (s *prepSQLState) snapshot() ([]string, []any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...), append([]any(nil), s.ctxValues...)
}

type prepSQLConnector struct{ state *prepSQLState }
type prepSQLDriver struct{ state *prepSQLState }
type prepSQLConn struct{ state *prepSQLState }

func (c prepSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &prepSQLConn{c.state}, nil
}
func (c prepSQLConnector) Driver() driver.Driver         { return prepSQLDriver{c.state} }
func (d prepSQLDriver) Open(string) (driver.Conn, error) { return &prepSQLConn{d.state}, nil }
func (*prepSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("fixture uses context interfaces")
}
func (*prepSQLConn) Close() error { return nil }
func (*prepSQLConn) Begin() (driver.Tx, error) {
	return nil, errors.New("fixture must not begin a transaction")
}
func (c *prepSQLConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	stage := "insert"
	if q != insertSyncLogSQL {
		stage = "delete"
	}
	c.state.call(stage, ctx)
	if c.state.waitContext {
		close(c.state.entered)
		if ctx.Done() == nil {
			return nil, errors.New("operation context was replaced with Background")
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if c.state.fail == stage {
		return nil, c.state.err
	}
	if stage == "insert" && c.state.cancelAfterInsert != nil {
		c.state.cancelAfterInsert()
	}
	return driver.RowsAffected(1), nil
}
func (c *prepSQLConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.call("count", ctx)
	if c.state.fail == "count" {
		return nil, c.state.err
	}
	return &prepCountRows{count: c.state.count}, nil
}

type prepCountRows struct {
	count int64
	sent  bool
}

func (*prepCountRows) Columns() []string { return []string{"count"} }
func (*prepCountRows) Close() error      { return nil }
func (r *prepCountRows) Next(dst []driver.Value) error {
	if r.sent {
		return io.EOF
	}
	r.sent = true
	dst[0] = r.count
	return nil
}
func prepSQLStore(t *testing.T, state *prepSQLState) *Store {
	t.Helper()
	db := sql.OpenDB(prepSQLConnector{state})
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return &Store{db: db}
}

func TestI8R10SQLContextAndPartialSuccessSemantics(t *testing.T) {
	sentinel := errors.New("fixture statement failure")
	for _, tc := range []struct {
		name, fail string
		count      int64
		want       []string
		wantError  bool
	}{
		{"normal", "", 1, []string{"insert", "count"}, false},
		{"trim", "", 1001, []string{"insert", "count", "delete"}, false},
		{"insert_failure", "insert", 1001, []string{"insert"}, true},
		{"count_failure", "count", 1001, []string{"insert", "count"}, false},
		{"delete_failure", "delete", 1001, []string{"insert", "count", "delete"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &prepSQLState{fail: tc.fail, err: sentinel, count: tc.count}
			s := prepSQLStore(t, state)
			ctx := context.WithValue(context.Background(), prepContextKey{}, "same-operation-context")
			err := s.AddSyncLogContext(ctx, SyncLog{Target: "fixture", Result: "success"})
			if tc.wantError && !errors.Is(err, sentinel) || !tc.wantError && err != nil {
				t.Fatalf("outcome=%v", err)
			}
			calls, values := state.snapshot()
			if !reflect.DeepEqual(calls, tc.want) {
				t.Fatalf("calls=%v want=%v", calls, tc.want)
			}
			for i, v := range values {
				if v != "same-operation-context" {
					t.Fatalf("context lost in %s: %v", calls[i], v)
				}
			}
		})
	}
}

func TestI8R10SQLCanceledBeforeAndAfterInsert(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "after_committed_insert"}[after], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			state := &prepSQLState{count: 1001}
			if after {
				state.cancelAfterInsert = cancel
			} else {
				cancel()
			}
			s := prepSQLStore(t, state)
			err := s.AddSyncLogContext(ctx, SyncLog{Target: "fixture"})
			calls, _ := state.snapshot()
			if after {
				if err != nil || !reflect.DeepEqual(calls, []string{"insert"}) {
					t.Fatalf("committed insert must retain COUNT-failure outcome: calls=%v err=%v", calls, err)
				}
			} else {
				if !errors.Is(err, context.Canceled) || len(calls) != 0 {
					t.Fatalf("pre-canceled operation started SQL: calls=%v err=%v", calls, err)
				}
			}
		})
	}
}

func TestI8R10SQLActiveCancellation(t *testing.T) {
	state := &prepSQLState{waitContext: true, entered: make(chan struct{})}
	s := prepSQLStore(t, state)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.AddSyncLogContext(ctx, SyncLog{Target: "fixture"}) }()
	select {
	case <-state.entered:
	case <-time.After(time.Second):
		t.Fatal("SQL not entered")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel identity lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("active SQL not canceled")
	}
}

func TestI8R10SQLiteLogContextLockRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	holder, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := holder.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := holder.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	active := true
	defer func() {
		if active {
			if _, err := holder.ExecContext(context.Background(), "ROLLBACK"); err != nil {
				t.Error(err)
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = s.AddSyncLogContext(ctx, SyncLog{Target: "canceled", Result: "success"})
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline identity=%v", err)
	}
	if _, err := holder.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	active = false
	if err := s.AddSyncLogContext(context.Background(), SyncLog{Target: "recovered", Result: "success"}); err != nil {
		t.Fatalf("recovery=%v", err)
	}
	logs, err := s.GetSyncLogs(10)
	if err != nil || len(logs) != 1 || logs[0].Target != "recovered" {
		t.Fatalf("persisted=%+v err=%v", logs, err)
	}
	t.Logf("100ms context returned after %v under original busy_timeout; zero canceled rows, subsequent write recovered", elapsed)
}
