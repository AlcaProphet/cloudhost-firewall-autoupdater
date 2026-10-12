package config

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// TestI8R09SnapshotReferenceIntegrity 完整快照必须拒绝缺失引用，但保留空引用的既有语义。
func TestI8R09SnapshotReferenceIntegrity(t *testing.T) {
	cases := []struct {
		name, raw string
		targets   int
		missing   int
	}{
		{"空库全目标", "[]", 0, 0}, {"历史空串", "", 0, 0}, {"单目标", "[1]", 2, 0}, {"多目标原顺序", "[2,1]", 2, 0},
		{"零目标悬空", "[99]", 0, 99}, {"全部引用缺失", "[98,99]", 2, 98}, {"有效后悬空", "[1,99]", 2, 99}, {"悬空后有效", "[99,1]", 2, 99},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newSnapshotStore(t)
			for i := 0; i < tc.targets; i++ {
				addTargetTxForTest(t, s, TargetConfig{CloudType: CloudTCCVM, Region: "ap-guangzhou", ResourceID: fmt.Sprintf("resource-sentinel-%d", i)})
			}
			id := insertRawRule(t, s, "host-sentinel.example", "'"+tc.raw+"'")
			for _, load := range []string{"snapshot", "config", "transaction"} {
				t.Run(load, func(t *testing.T) {
					var err error
					var nilResult bool
					switch load {
					case "snapshot":
						snap, e := s.LoadBusinessSnapshot()
						err = e
						nilResult = snap == nil
					case "config":
						cfg, e := s.LoadConfig()
						err = e
						nilResult = cfg == nil
					case "transaction":
						tx, e := s.BeginReadOnlyTx(context.Background())
						if e != nil {
							t.Fatal(e)
						}
						snap, e := s.LoadBusinessSnapshotTx(context.Background(), tx)
						err = e
						nilResult = snap == nil
						if e := tx.Rollback(); e != nil && !errors.Is(e, sql.ErrTxDone) {
							t.Fatal(e)
						}
					}
					if tc.missing == 0 {
						if err != nil || nilResult {
							t.Fatalf("合法引用失败: %v nil=%v", err, nilResult)
						}
						return
					}
					if err == nil || !nilResult {
						t.Fatalf("悬空引用必须失败且无半截配置: err=%v nil=%v", err, nilResult)
					}
					var ve *ValidationError
					if errors.As(err, &ve) {
						t.Fatalf("损坏库错误不得分类为请求侧: %v", err)
					}
					want := fmt.Sprintf("规则 #%d 引用了不存在的目标 #%d，数据已损坏", id, tc.missing)
					if err.Error() != want {
						t.Fatalf("错误诊断=%q want=%q", err.Error(), want)
					}
					for _, secret := range []string{"host-sentinel.example", "resource-sentinel", tc.raw} {
						if strings.Contains(err.Error(), secret) {
							t.Fatalf("错误泄露字段值 %q", secret)
						}
					}
				})
			}
			raw, err := s.GetRules()
			if err != nil || len(raw) != 1 {
				t.Fatal(raw, err)
			}
			var stored string
			if err := s.db.QueryRow("SELECT targets FROM rules WHERE id=?", id).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if stored != tc.raw {
				t.Fatalf("读取不得修复或裁剪: got=%q want=%q", stored, tc.raw)
			}
		})
	}
}

// TestI8R09SnapshotChecksEveryRule 不得因前面规则合法而漏掉后面的损坏规则。
func TestI8R09SnapshotChecksEveryRule(t *testing.T) {
	s := newSnapshotStore(t)
	addTargetTxForTest(t, s, TargetConfig{CloudType: CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-a"})
	insertRawRule(t, s, "first.example", "'[1]'")
	rid := insertRawRule(t, s, "second.example", "'[1,77]'")
	insertRawRule(t, s, "third.example", "'[88]'")
	snap, err := s.LoadBusinessSnapshot()
	want := fmt.Sprintf("规则 #%d 引用了不存在的目标 #77，数据已损坏", rid)
	if snap != nil || err == nil || err.Error() != want {
		t.Fatalf("snap=%v err=%v want=%s", snap, err, want)
	}
}

// TestI8R09SnapshotUsesProvidedTransaction 未提交的新目标及其引用必须从调用方事务读取。
func TestI8R09SnapshotUsesProvidedTransaction(t *testing.T) {
	s := newSnapshotStore(t)
	ctx := context.Background()
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Error(err)
		}
	}()
	id, err := s.AddTargetTx(ctx, tx, TargetConfig{CloudType: CloudTCCVM, Region: "ap-guangzhou", ResourceID: "new-uncommitted"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddRuleTx(ctx, tx, DomainRule{Host: "new.example", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{int(id)}}); err != nil {
		t.Fatal(err)
	}
	snap, err := s.LoadBusinessSnapshotTx(ctx, tx)
	if err != nil || snap == nil || len(snap.Targets) != 1 || !reflect.DeepEqual(snap.Rules[0].Targets, []int{int(id)}) {
		t.Fatalf("事务内新引用不可见: snap=%v err=%v", snap, err)
	}
	if _, err := s.DeleteTargetTx(ctx, tx, int(id)); err != nil {
		t.Fatal(err)
	}
	snap, err = s.LoadBusinessSnapshotTx(ctx, tx)
	if err == nil || snap != nil {
		t.Fatalf("事务内删除应使引用失效: snap=%v err=%v", snap, err)
	}
}

// TestI8R09SnapshotKeepsReadTransactionView 外部删库后，已固定的只读快照仍保持原来的完整视图。
func TestI8R09SnapshotKeepsReadTransactionView(t *testing.T) {
	s := newSnapshotStore(t)
	ctx := context.Background()
	addTargetTxForTest(t, s, TargetConfig{CloudType: CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-a"})
	insertRawRule(t, s, "a.example", "'[1]'")
	tx, err := s.BeginReadOnlyTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Error(err)
		}
	}()
	if _, err := s.LoadBusinessSnapshotTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DELETE FROM targets WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	snap, err := s.LoadBusinessSnapshotTx(ctx, tx)
	if err != nil || snap == nil || len(snap.Targets) != 1 {
		t.Fatalf("不得绕过传入事务读最新库: snap=%v err=%v", snap, err)
	}
	if _, err := s.LoadBusinessSnapshot(); err == nil {
		t.Fatal("新的只读事务必须发现悬空引用")
	}
}
