package config

import (
	"context"
	"database/sql"
	"net/netip"
	"path/filepath"
	"testing"
	"time"
)

func TestEdgeOneTransitionLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transition.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	v4 := netip.MustParsePrefix("203.0.113.0/24")

	obs := EdgeOneTransition{
		ZoneID: "zone-a", BaseVersion: "v1", PendingVersion: "v2",
		PendingCIDRs: []netip.Prefix{v4}, ConfirmationState: EdgeOneConfirmationNotAttempted,
	}
	if err := store.SaveEdgeOneObservation(ctx, obs); err != nil {
		t.Fatalf("保存观察失败: %v", err)
	}
	got, err := store.GetEdgeOneTransition(ctx, "zone-a")
	if err != nil || got == nil {
		t.Fatalf("读取观察失败: %+v %v", got, err)
	}
	if got.ConfirmationState != EdgeOneConfirmationNotAttempted || len(got.PendingCIDRs) != 1 ||
		got.PendingCIDRs[0] != v4 || len(got.ProtectedCIDRs) != 0 {
		t.Fatalf("观察记录 = %+v", got)
	}

	ok, err := store.SetEdgeOneConfirmation(ctx, "zone-a", "v2", []netip.Prefix{v4}, EdgeOneConfirmationUnknown, "", time.Now().UTC())
	if err != nil || !ok {
		t.Fatalf("提交 unknown 失败: %v %v", ok, err)
	}
	ok, err = store.SetEdgeOneConfirmation(ctx, "zone-a", "v2", []netip.Prefix{v4}, EdgeOneConfirmationAccepted, "req-1", time.Now().UTC())
	if err != nil || !ok {
		t.Fatalf("提交 accepted 失败: %v %v", ok, err)
	}
	// 同指纹重新观察必须保留 accepted 与 RequestId。
	if err := store.SaveEdgeOneObservation(ctx, EdgeOneTransition{
		ZoneID: "zone-a", BaseVersion: "v1b", PendingVersion: "v2", PendingCIDRs: []netip.Prefix{v4},
	}); err != nil {
		t.Fatalf("同指纹观察失败: %v", err)
	}
	got, _ = store.GetEdgeOneTransition(ctx, "zone-a")
	if got.ConfirmationState != EdgeOneConfirmationAccepted || got.ConfirmationRequestID != "req-1" {
		t.Fatalf("同指纹覆盖了 accepted: %+v", got)
	}

	// 新指纹重置为 not_attempted。
	if err := store.SaveEdgeOneObservation(ctx, EdgeOneTransition{
		ZoneID: "zone-a", BaseVersion: "v2", PendingVersion: "v3", PendingCIDRs: []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")},
	}); err != nil {
		t.Fatalf("新指纹观察失败: %v", err)
	}
	got, _ = store.GetEdgeOneTransition(ctx, "zone-a")
	if got.PendingVersion != "v3" || got.ConfirmationState != EdgeOneConfirmationNotAttempted || got.ConfirmationRequestID != "" {
		t.Fatalf("新指纹未重置: %+v", got)
	}
}

func TestEdgeOneTransitionPruneDeleteAndReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transition-prune.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	v4 := netip.MustParsePrefix("203.0.113.0/24")
	for _, zone := range []string{"zone-a", "zone-b"} {
		if err := store.SaveEdgeOneObservation(ctx, EdgeOneTransition{
			ZoneID: zone, BaseVersion: "v1", PendingVersion: "v2", PendingCIDRs: []netip.Prefix{v4},
		}); err != nil {
			t.Fatalf("保存 %s 失败: %v", zone, err)
		}
	}
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("开始事务失败: %v", err)
	}
	if err := store.AddEdgeOneSiteTx(ctx, tx, EdgeOneSite{ZoneID: "zone-a", TargetIDs: []int{1}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4"}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("插入站点失败: %v", err)
	}
	if err := store.PruneEdgeOneTransitionsTx(ctx, tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("剪枝失败: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	if row, _ := store.GetEdgeOneTransition(ctx, "zone-a"); row == nil {
		t.Fatal("仍存在的站点记录被误删")
	}
	if row, _ := store.GetEdgeOneTransition(ctx, "zone-b"); row != nil {
		t.Fatal("不存在站点的记录未被删除")
	}

	// 删除站点同时删除过渡记录。
	tx, _ = store.BeginTx(ctx)
	if _, err := store.DeleteEdgeOneSiteTx(ctx, tx, "zone-a"); err != nil {
		_ = tx.Rollback()
		t.Fatalf("删除站点失败: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("提交删除失败: %v", err)
	}
	if row, _ := store.GetEdgeOneTransition(ctx, "zone-a"); row != nil {
		t.Fatal("删除站点未清理过渡记录")
	}

	// reset 清空。
	if err := store.SaveEdgeOneObservation(ctx, EdgeOneTransition{ZoneID: "zone-a", BaseVersion: "v1", PendingVersion: "v2", PendingCIDRs: []netip.Prefix{v4}}); err != nil {
		t.Fatalf("重新保存失败: %v", err)
	}
	if err := store.WithTransactionContext(ctx, func(tx *sql.Tx) error { return store.ResetAllTx(ctx, tx) }); err != nil {
		t.Fatalf("reset 失败: %v", err)
	}
	rows, err := store.ListEdgeOneTransitions(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("reset 后过渡记录 = %+v err=%v", rows, err)
	}
}
