package config

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func i8r14Exec(t *testing.T, s *Store, q string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func i8r14LogIDs(t *testing.T, s *Store) []int64 {
	t.Helper()
	rows, err := s.db.Query("SELECT id FROM sync_logs ORDER BY id DESC")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}
func TestI8R14SyncLogRetention(t *testing.T) {
	for _, n := range []int{0, 998, 999, 1000, 1001, 5000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s, _ := i8r14Store(t)
			if n > 0 {
				i8r14Exec(t, s, `WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<?) INSERT INTO sync_logs(id,timestamp,target) SELECT x*17,'2099-01-01','seed' FROM n`, n)
			}
			if err := s.AddSyncLogContext(context.Background(), SyncLog{Timestamp: time.Unix(0, 0), Target: "latest"}); err != nil {
				t.Fatal(err)
			}
			want := []int64{int64(n*17 + 1)}
			if n == 0 {
				want = []int64{1}
			}
			for i := n; i > 0 && len(want) < 1000; i-- {
				want = append(want, int64(i*17))
			}
			got := i8r14LogIDs(t, s)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("保留ID不符: count=%d want=%d got=%v", len(got), len(want), got)
			}
			logs, err := s.GetSyncLogs(1)
			if err != nil || len(logs) != 1 || logs[0].Target != "latest" {
				t.Fatalf("应按插入ID而非时间戳保留: %+v %v", logs, err)
			}
		})
	}
}
func TestI8R14SyncLogClearKeepsSequence(t *testing.T) {
	s, _ := i8r14Store(t)
	i8r14Exec(t, s, "INSERT INTO sync_logs(id,target) VALUES(100000,'old')")
	if err := s.ClearSyncLogs(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1001; i++ {
		if err := s.AddSyncLog(SyncLog{Timestamp: time.Unix(0, 0), Target: "after-clear"}); err != nil {
			t.Fatal(err)
		}
	}
	got := i8r14LogIDs(t, s)
	if len(got) != 1000 || got[0] != 101001 || got[len(got)-1] != 100002 {
		t.Fatalf("清空后序列/保留错误: %v", got)
	}
}
func TestI8R14SyncLogTrimFailureKeepsInsert(t *testing.T) {
	s, _ := i8r14Store(t)
	i8r14Exec(t, s, `WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000) INSERT INTO sync_logs(target) SELECT 'seed' FROM n`)
	i8r14Exec(t, s, `CREATE TRIGGER i8r14_reject_delete BEFORE DELETE ON sync_logs BEGIN SELECT RAISE(ABORT,'fixture trim failure'); END`)
	err := s.AddSyncLogContext(context.Background(), SyncLog{Timestamp: time.Now(), Target: "committed-before-trim"})
	if err == nil {
		t.Fatal("DELETE失败应返回错误")
	}
	logs, readErr := s.GetSyncLogs(1)
	if readErr != nil || len(logs) != 1 || logs[0].Target != "committed-before-trim" {
		t.Fatalf("裁剪失败回滚了INSERT: %+v %v", logs, readErr)
	}
	if got := i8r14LogIDs(t, s); len(got) != 1001 {
		t.Errorf("失败后的已提交条数=%d", len(got))
	}
	i8r14Exec(t, s, "DROP TRIGGER i8r14_reject_delete")
	if err := s.AddSyncLog(SyncLog{Timestamp: time.Now(), Target: "recovered"}); err != nil {
		t.Fatal(err)
	}
	if got := i8r14LogIDs(t, s); len(got) != 1000 {
		t.Errorf("裁剪恢复后条数=%d", len(got))
	}
}
func TestI8R14SyncLogClearBetweenCountAndTrim(t *testing.T) {
	s, path := i8r14Store(t)
	i8r14Exec(t, s, `WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000) INSERT INTO sync_logs(target) SELECT 'seed' FROM n`)
	cleared := false
	subject := i8r14Wrapped(t, path, &i8r14Connector{beforeExec: func(_ context.Context, q string) error {
		if strings.HasPrefix(q, "DELETE FROM sync_logs ") && !cleared {
			cleared = true
			if err := s.ClearSyncLogs(); err != nil {
				return err
			}
			_, err := s.db.Exec("INSERT INTO sync_logs(target,domain,result) VALUES('after-clear','','success')")
			return err
		}
		return nil
	}})
	if err := subject.AddSyncLog(SyncLog{Timestamp: time.Now(), Target: "before-clear"}); err != nil {
		t.Fatal(err)
	}
	if !cleared {
		t.Fatal("未进入裁剪屏障")
	}
	logs, err := s.GetSyncLogs(10)
	if err != nil || len(logs) != 1 || logs[0].Target != "after-clear" {
		t.Fatalf("裁剪删除了清空后的新记录: %+v %v", logs, err)
	}
}
