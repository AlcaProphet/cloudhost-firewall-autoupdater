package config

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// AddressConflict 是本机唯一一条已查实的全局地址冲突记录（Build8 Step 7）。
//
// 该结构属于运行态证据，不进入 v4 配置包；reset 清除，普通配置保存与完整导入
// 不清除（仍由有效预检解除）。DNSAddress 使用 Unmap 后的 IPv4/IPv6 表示，
// EdgeOnePrefix 使用 Masked 后的规范网段。
type AddressConflict struct {
	DNSName       string    `json:"dns_name"`
	DNSAddress    string    `json:"dns_address"`
	EdgeOnePrefix string    `json:"edgeone_prefix"`
	ZoneID        string    `json:"zone_id"`
	FirstSeenAt   time.Time `json:"first_seen_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
}

// NormalizeAddressConflict 校验并规范化运行态冲突证据。
//
// 时间为零值时保留零值，由 Save 补齐；非零时间必须可比较且 last 不早于 first。
func NormalizeAddressConflict(c AddressConflict) (AddressConflict, error) {
	c.DNSName = strings.TrimSpace(c.DNSName)
	if c.DNSName == "" {
		return AddressConflict{}, fmt.Errorf("冲突记录缺少 DNS 名称")
	}
	c.ZoneID = strings.TrimSpace(c.ZoneID)
	if c.ZoneID == "" || hasControlChar(c.ZoneID) {
		return AddressConflict{}, fmt.Errorf("冲突记录缺少有效 ZoneId")
	}

	addr, err := netip.ParseAddr(strings.TrimSpace(c.DNSAddress))
	if err != nil {
		return AddressConflict{}, fmt.Errorf("冲突记录 DNS 地址无效: %w", err)
	}
	c.DNSAddress = addr.Unmap().String()

	prefix, err := netip.ParsePrefix(strings.TrimSpace(c.EdgeOnePrefix))
	if err != nil {
		return AddressConflict{}, fmt.Errorf("冲突记录 EdgeOne 网段无效: %w", err)
	}
	c.EdgeOnePrefix = prefix.Masked().String()

	if !c.FirstSeenAt.IsZero() {
		c.FirstSeenAt = c.FirstSeenAt.UTC()
	}
	if !c.LastSeenAt.IsZero() {
		c.LastSeenAt = c.LastSeenAt.UTC()
	}
	if !c.FirstSeenAt.IsZero() && !c.LastSeenAt.IsZero() && c.LastSeenAt.Before(c.FirstSeenAt) {
		return AddressConflict{}, fmt.Errorf("冲突记录最后观察时间早于首次观察时间")
	}
	return c, nil
}

// GetAddressConflict 读取已提交的全局冲突记录。
//
// 返回 (nil, nil) 表示没有已持久化的冲突；读取失败或坏记录返回错误，调用方必须
// 把错误当作“状态不可用”并保持阻断，不能降级为无冲突。
func (s *Store) GetAddressConflict(ctx context.Context) (*AddressConflict, error) {
	var (
		dnsName, dnsAddress, edgeOnePrefix, zoneID string
		firstSeen, lastSeen                        string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT dns_name, dns_address, edgeone_prefix, zone_id, first_seen_at, last_seen_at
		   FROM sync_address_conflict WHERE id = 1`).
		Scan(&dnsName, &dnsAddress, &edgeOnePrefix, &zoneID, &firstSeen, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取全局地址冲突记录失败: %w", err)
	}

	firstAt, err := parseConflictTime(firstSeen)
	if err != nil {
		return nil, fmt.Errorf("全局地址冲突记录首次观察时间无效: %w", err)
	}
	lastAt, err := parseConflictTime(lastSeen)
	if err != nil {
		return nil, fmt.Errorf("全局地址冲突记录最后观察时间无效: %w", err)
	}
	normalized, err := NormalizeAddressConflict(AddressConflict{
		DNSName:       dnsName,
		DNSAddress:    dnsAddress,
		EdgeOnePrefix: edgeOnePrefix,
		ZoneID:        zoneID,
		FirstSeenAt:   firstAt,
		LastSeenAt:    lastAt,
	})
	if err != nil {
		return nil, fmt.Errorf("全局地址冲突记录损坏: %w", err)
	}
	return &normalized, nil
}

// SaveAddressConflict 写入或更新唯一冲突行。
//
// 更新时保留原 first_seen_at，只刷新证据与 last_seen_at；短事务在调用方 context
// 内执行，失败时不留下部分写入。首次写库失败由调用方保留进程内证据并在后续轮次重试。
func (s *Store) SaveAddressConflict(ctx context.Context, c AddressConflict) error {
	normalized, err := NormalizeAddressConflict(c)
	if err != nil {
		return fmt.Errorf("地址冲突记录无效: %w", err)
	}
	now := time.Now().UTC()
	if normalized.FirstSeenAt.IsZero() {
		normalized.FirstSeenAt = now
	}
	if normalized.LastSeenAt.IsZero() {
		normalized.LastSeenAt = now
	}
	if normalized.LastSeenAt.Before(normalized.FirstSeenAt) {
		return fmt.Errorf("地址冲突记录时间顺序无效：最后观察时间早于首次观察时间")
	}
	firstSeen := formatConflictTime(normalized.FirstSeenAt)
	lastSeen := formatConflictTime(normalized.LastSeenAt)

	return s.WithTransactionContext(ctx, func(tx *sql.Tx) error {
		// 写事务已预留写锁；比较实际会保留的首次时间，避免在检查后被其他写入替换。
		var storedFirst string
		err := tx.QueryRowContext(ctx, `SELECT first_seen_at FROM sync_address_conflict WHERE id = 1`).Scan(&storedFirst)
		if err == nil {
			firstAt, err := parseConflictTime(storedFirst)
			if err != nil {
				return fmt.Errorf("全局地址冲突记录首次观察时间无效: %w", err)
			}
			if normalized.LastSeenAt.Before(firstAt) {
				return fmt.Errorf("地址冲突记录时间顺序无效：最后观察时间早于已保存的首次观察时间")
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("读取全局地址冲突记录首次观察时间失败: %w", err)
		}

		_, err = tx.ExecContext(ctx,
			`INSERT INTO sync_address_conflict
			     (id, dns_name, dns_address, edgeone_prefix, zone_id, first_seen_at, last_seen_at)
			 VALUES (1, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
			     dns_name = excluded.dns_name,
			     dns_address = excluded.dns_address,
			     edgeone_prefix = excluded.edgeone_prefix,
			     zone_id = excluded.zone_id,
			     last_seen_at = excluded.last_seen_at`,
			normalized.DNSName, normalized.DNSAddress, normalized.EdgeOnePrefix,
			normalized.ZoneID, firstSeen, lastSeen,
		)
		if err != nil {
			return fmt.Errorf("保存全局地址冲突记录失败: %w", err)
		}
		return nil
	})
}

// ClearAddressConflict 删除已持久化的冲突记录。
//
// 只有完整有效输入证明无重叠后才调用；删除失败时调用方必须保留进程内阻断。
func (s *Store) ClearAddressConflict(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM sync_address_conflict WHERE id = 1"); err != nil {
		return fmt.Errorf("清除全局地址冲突记录失败: %w", err)
	}
	return nil
}

func formatConflictTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseConflictTime(value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
