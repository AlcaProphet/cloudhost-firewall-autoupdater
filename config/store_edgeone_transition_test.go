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

func TestEdgeOneTransitionRestoreUnsentMatchesPreparedRecord(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	prefixes := []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	zone := "zone-a"
	initial := EdgeOneTransition{ZoneID: zone, BaseVersion: "v1", PendingVersion: "v2", PendingCIDRs: prefixes}
	if err := store.SaveEdgeOneObservation(ctx, initial); err != nil {
		t.Fatal(err)
	}
	prepared := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	set := func(at time.Time, state, id string) {
		t.Helper()
		ok, err := store.SetEdgeOneConfirmation(ctx, zone, "v2", prefixes, state, id, at)
		if !ok || err != nil {
			t.Fatalf("set: %v %v", ok, err)
		}
	}
	restore := func(want bool) {
		t.Helper()
		ok, err := store.RestoreUnsentEdgeOneConfirmation(ctx, zone, "v2", prefixes, prepared)
		if ok != want || err != nil {
			t.Fatalf("restore: %v %v; want %v", ok, err, want)
		}
	}
	set(prepared, EdgeOneConfirmationUnknown, "")
	restore(true)
	row, err := store.GetEdgeOneTransition(ctx, zone)
	if err != nil || row == nil || row.ConfirmationState != EdgeOneConfirmationNotAttempted {
		t.Fatalf("restore status: %+v %v", row, err)
	}
	set(prepared.Add(time.Second), EdgeOneConfirmationUnknown, "")
	restore(false)
	set(prepared, EdgeOneConfirmationAccepted, "req-accepted")
	restore(false)
	row, err = store.GetEdgeOneTransition(ctx, zone)
	if err != nil || row == nil || row.ConfirmationState != EdgeOneConfirmationAccepted || row.ConfirmationRequestID != "req-accepted" {
		t.Fatalf("accepted touched: %+v %v", row, err)
	}
	newPrefixes := []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")}
	initial.PendingCIDRs = newPrefixes
	if err := store.SaveEdgeOneObservation(ctx, initial); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.SetEdgeOneConfirmation(ctx, zone, "v2", newPrefixes, EdgeOneConfirmationUnknown, "", prepared); !ok || err != nil {
		t.Fatalf("保存新指纹：%v %v", ok, err)
	}
	restore(false)
	if err := store.DeleteEdgeOneTransition(ctx, zone); err != nil {
		t.Fatal(err)
	}
	restore(false)
	row, err = store.GetEdgeOneTransition(ctx, zone)
	if err != nil || row != nil {
		t.Fatalf("deleted record recreated: %+v %v", row, err)
	}
}

func TestEdgeOneTransitionProtectedObservation(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "protected.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	v4 := netip.MustParsePrefix("203.0.113.0/24")
	v6 := netip.MustParsePrefix("2001:db8::/64")
	zone := "zone-a"
	if err := store.SaveEdgeOneObservation(ctx, EdgeOneTransition{
		ZoneID: zone, BaseVersion: "v1", PendingVersion: "v2", PendingCIDRs: []netip.Prefix{v4},
	}); err != nil {
		t.Fatal(err)
	}
	ok, err := store.SetEdgeOneConfirmation(ctx, zone, "v2", []netip.Prefix{v4}, EdgeOneConfirmationAccepted, "req-1", time.Now().UTC())
	if err != nil || !ok {
		t.Fatalf("accepted: %v %v", ok, err)
	}
	// 显式写入保护但同一指纹：保留 accepted。
	if err := store.SaveEdgeOneProtectedObservation(ctx, EdgeOneTransition{
		ZoneID: zone, BaseVersion: "v1", PendingVersion: "v2",
		PendingCIDRs: []netip.Prefix{v4}, ProtectedCIDRs: []netip.Prefix{v4, v6},
	}, false); err != nil {
		t.Fatal(err)
	}
	row, err := store.GetEdgeOneTransition(ctx, zone)
	if err != nil || row == nil || row.ConfirmationState != EdgeOneConfirmationAccepted ||
		row.ConfirmationRequestID != "req-1" || len(row.ProtectedCIDRs) != 2 {
		t.Fatalf("同指纹显式保护写入 = %+v %v", row, err)
	}
	// 新指纹 + reset：写入 not_attempted 并清空 RequestId。
	if err := store.SaveEdgeOneProtectedObservation(ctx, EdgeOneTransition{
		ZoneID: zone, BaseVersion: "v1", PendingVersion: "v3",
		PendingCIDRs: []netip.Prefix{v6}, ProtectedCIDRs: []netip.Prefix{v4, v6},
		ConfirmationState: EdgeOneConfirmationNotAttempted,
	}, true); err != nil {
		t.Fatal(err)
	}
	row, err = store.GetEdgeOneTransition(ctx, zone)
	if err != nil || row == nil || row.PendingVersion != "v3" ||
		row.ConfirmationState != EdgeOneConfirmationNotAttempted || row.ConfirmationRequestID != "" ||
		len(row.ProtectedCIDRs) != 2 {
		t.Fatalf("新指纹 reset = %+v %v", row, err)
	}
}
