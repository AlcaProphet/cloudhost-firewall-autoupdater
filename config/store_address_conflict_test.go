package config

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// 本文件是 Build8 Step 7 的 SQLite 运行态记录回归：唯一行、坏记录拒绝、
// reset 清理与完整导入保留边界。

func TestAddressConflictStoreRoundTripAndNormalization(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	row, err := s.GetAddressConflict(ctx)
	if err != nil {
		t.Fatalf("空库读取失败: %v", err)
	}
	if row != nil {
		t.Fatalf("空库应返回 nil, got %+v", row)
	}

	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	last := first.Add(time.Hour)
	if err := s.SaveAddressConflict(ctx, AddressConflict{
		DNSName:       " home.example ",
		DNSAddress:    "::ffff:203.0.113.7",
		EdgeOnePrefix: "203.0.113.7/24",
		ZoneID:        " zone-a ",
		FirstSeenAt:   first,
		LastSeenAt:    last,
	}); err != nil {
		t.Fatalf("保存冲突失败: %v", err)
	}
	row, err = s.GetAddressConflict(ctx)
	if err != nil || row == nil {
		t.Fatalf("读取冲突 = %+v, err=%v", row, err)
	}
	if row.DNSName != "home.example" || row.DNSAddress != "203.0.113.7" ||
		row.EdgeOnePrefix != "203.0.113.0/24" || row.ZoneID != "zone-a" {
		t.Fatalf("规范化结果不正确: %+v", row)
	}
	if !row.FirstSeenAt.Equal(first) || !row.LastSeenAt.Equal(last) {
		t.Fatalf("时间 = %v / %v", row.FirstSeenAt, row.LastSeenAt)
	}

	// 更新保留 first_seen_at，只刷新证据与 last_seen_at。
	later := last.Add(time.Hour)
	if err := s.SaveAddressConflict(ctx, AddressConflict{
		DNSName:       "other.example",
		DNSAddress:    "198.51.100.9",
		EdgeOnePrefix: "198.51.100.0/24",
		ZoneID:        "zone-b",
		FirstSeenAt:   first.Add(-time.Hour), // 已有行不得被它覆盖
		LastSeenAt:    later,
	}); err != nil {
		t.Fatalf("更新冲突失败: %v", err)
	}
	row, err = s.GetAddressConflict(ctx)
	if err != nil || row == nil {
		t.Fatalf("更新后读取失败: %+v err=%v", row, err)
	}
	if !row.FirstSeenAt.Equal(first) {
		t.Fatalf("first_seen_at 应保留原值 %v, got %v", first, row.FirstSeenAt)
	}
	if row.DNSName != "other.example" || row.ZoneID != "zone-b" || !row.LastSeenAt.Equal(later) {
		t.Fatalf("更新证据不正确: %+v", row)
	}

	if err := s.SaveAddressConflict(ctx, AddressConflict{DNSAddress: "203.0.113.7", EdgeOnePrefix: "203.0.113.0/24", ZoneID: "zone-a"}); err == nil {
		t.Fatal("缺少 DNS 名称应拒绝")
	}
	row, _ = s.GetAddressConflict(ctx)
	if row == nil || row.DNSName != "other.example" {
		t.Fatalf("非法保存不得改变已有记录: %+v", row)
	}

	if err := s.ClearAddressConflict(ctx); err != nil {
		t.Fatalf("清除失败: %v", err)
	}
	row, err = s.GetAddressConflict(ctx)
	if err != nil || row != nil {
		t.Fatalf("清除后 = %+v err=%v", row, err)
	}
}

func TestAddressConflictGetRejectsCorruptRows(t *testing.T) {
	cases := []struct {
		name  string
		apply string
	}{
		{"DNS 名称空", "dns_name = ''"},
		{"DNS 地址非法", "dns_address = 'not-an-ip'"},
		{"网段非法", "edgeone_prefix = 'not-a-prefix'"},
		{"ZoneId 空", "zone_id = ''"},
		{"首次时间非法", "first_seen_at = 'not-a-time'"},
		{"最后时间非法", "last_seen_at = 'not-a-time'"},
		{"最后时间早于首次", "last_seen_at = '2020-01-01T00:00:00Z'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStore(t)
			ctx := context.Background()
			if err := s.SaveAddressConflict(ctx, AddressConflict{
				DNSName:       "home.example",
				DNSAddress:    "203.0.113.7",
				EdgeOnePrefix: "203.0.113.0/24",
				ZoneID:        "zone-a",
				FirstSeenAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				LastSeenAt:    time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
			}); err != nil {
				t.Fatalf("预置记录失败: %v", err)
			}
			if _, err := s.db.ExecContext(ctx,
				"UPDATE sync_address_conflict SET "+tc.apply+" WHERE id = 1"); err != nil {
				t.Fatalf("制造坏记录失败: %v", err)
			}
			row, err := s.GetAddressConflict(ctx)
			if err == nil {
				t.Fatalf("坏记录必须返回错误, got %+v", row)
			}
			if row != nil {
				t.Fatalf("坏记录不得返回半截证据: %+v", row)
			}
			if !strings.Contains(err.Error(), "冲突记录") {
				t.Errorf("错误应说明冲突记录不可用: %v", err)
			}
		})
	}
}

func TestAddressConflictResetAndImportBoundary(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	row := AddressConflict{
		DNSName:       "home.example",
		DNSAddress:    "203.0.113.7",
		EdgeOnePrefix: "203.0.113.0/24",
		ZoneID:        "zone-a",
		FirstSeenAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastSeenAt:    time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
	}
	if err := s.SaveAddressConflict(ctx, row); err != nil {
		t.Fatalf("保存冲突失败: %v", err)
	}

	// reset 清理运行态冲突记录。
	if err := s.WithTransactionContext(ctx, func(tx *sql.Tx) error {
		return s.ResetAllTx(ctx, tx)
	}); err != nil {
		t.Fatalf("reset 失败: %v", err)
	}
	got, err := s.GetAddressConflict(ctx)
	if err != nil || got != nil {
		t.Fatalf("reset 后冲突记录 = %+v err=%v", got, err)
	}

	// 完整导入不清理本机已查实冲突，仍由有效预检解除。
	if err := s.SaveAddressConflict(ctx, row); err != nil {
		t.Fatalf("重新保存冲突失败: %v", err)
	}
	if err := s.WithTransactionContext(ctx, func(tx *sql.Tx) error {
		return s.DeleteImportOwnedTablesTx(ctx, tx)
	}); err != nil {
		t.Fatalf("导入清理失败: %v", err)
	}
	got, err = s.GetAddressConflict(ctx)
	if err != nil || got == nil {
		t.Fatalf("完整导入后冲突记录应保留: %+v err=%v", got, err)
	}
	if got.DNSName != row.DNSName || got.ZoneID != row.ZoneID {
		t.Fatalf("导入保留证据不正确: %+v", got)
	}
}
