package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"
)

// EdgeOne 确认状态枚举（Design6 §7.1 冻结表）。
const (
	EdgeOneConfirmationNotAttempted = "not_attempted"
	EdgeOneConfirmationUnknown      = "unknown"
	EdgeOneConfirmationAccepted     = "accepted"
)

// EdgeOneTransition 是一个站点的过渡/确认运行记录。
//
// 该记录属于本机运行态，不进入 v4 导出；protected_cidrs 在 Step 11 使用，
// Step 10 只写空数组，避免提前改变清理授权。
type EdgeOneTransition struct {
	ZoneID                string
	BaseVersion           string
	PendingVersion        string
	PendingCIDRs          []netip.Prefix
	ProtectedCIDRs        []netip.Prefix
	ConfirmationState     string
	ConfirmationRequestID string
	UpdatedAt             time.Time
}

func canonicalCIDRJSON(prefixes []netip.Prefix) (string, error) {
	values := make([]string, 0, len(prefixes))
	seen := make(map[string]bool, len(prefixes))
	for _, p := range prefixes {
		if !p.IsValid() {
			return "", fmt.Errorf("非法 CIDR")
		}
		s := p.Masked().String()
		if seen[s] {
			continue
		}
		seen[s] = true
		values = append(values, s)
	}
	sort.Strings(values)
	raw, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func parseCIDRJSON(raw string) ([]netip.Prefix, error) {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	out := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		p, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return nil, err
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

func normalizeTransition(t EdgeOneTransition) (EdgeOneTransition, error) {
	t.ZoneID = strings.TrimSpace(t.ZoneID)
	if t.ZoneID == "" {
		return EdgeOneTransition{}, fmt.Errorf("ZoneId 为空")
	}
	t.BaseVersion = strings.TrimSpace(t.BaseVersion)
	t.PendingVersion = strings.TrimSpace(t.PendingVersion)
	if t.PendingVersion == "" {
		return EdgeOneTransition{}, fmt.Errorf("待更新版本为空")
	}
	switch t.ConfirmationState {
	case EdgeOneConfirmationNotAttempted, EdgeOneConfirmationUnknown, EdgeOneConfirmationAccepted:
	default:
		return EdgeOneTransition{}, fmt.Errorf("非法确认状态")
	}
	t.ConfirmationRequestID = strings.TrimSpace(t.ConfirmationRequestID)
	return t, nil
}

func (s *Store) scanEdgeOneTransition(scan func(dest ...any) error) (*EdgeOneTransition, error) {
	var (
		t         EdgeOneTransition
		pending   string
		protected string
		updated   string
	)
	if err := scan(&t.ZoneID, &t.BaseVersion, &t.PendingVersion, &pending, &protected,
		&t.ConfirmationState, &t.ConfirmationRequestID, &updated); err != nil {
		return nil, err
	}
	var err error
	if t.PendingCIDRs, err = parseCIDRJSON(pending); err != nil {
		return nil, fmt.Errorf("解析 pending_cidrs_json 失败: %w", err)
	}
	if t.ProtectedCIDRs, err = parseCIDRJSON(protected); err != nil {
		return nil, fmt.Errorf("解析 protected_cidrs_json 失败: %w", err)
	}
	if t.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return nil, fmt.Errorf("解析 updated_at 失败: %w", err)
	}
	return &t, nil
}

// GetEdgeOneTransition 返回一个站点的运行记录；缺行返回 (nil, nil)。
func (s *Store) GetEdgeOneTransition(ctx context.Context, zoneID string) (*EdgeOneTransition, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT zone_id, base_version, pending_version, pending_cidrs_json,
		       protected_cidrs_json, confirmation_state, confirmation_request_id, updated_at
		FROM edgeone_origin_transitions WHERE zone_id = ?`, strings.TrimSpace(zoneID))
	t, err := s.scanEdgeOneTransition(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// ListEdgeOneTransitions 返回全部运行记录，按 ZoneId 稳定排序。
func (s *Store) ListEdgeOneTransitions(ctx context.Context) ([]EdgeOneTransition, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT zone_id, base_version, pending_version, pending_cidrs_json,
		       protected_cidrs_json, confirmation_state, confirmation_request_id, updated_at
		FROM edgeone_origin_transitions ORDER BY zone_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]EdgeOneTransition, 0)
	for rows.Next() {
		t, err := s.scanEdgeOneTransition(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// SaveEdgeOneObservation 写入一次站点观察。
//
// 同一待更新指纹保留已 accepted/unknown 状态；指纹变化则重置为
// not_attempted。已有 protected_cidrs（Step 11 写入）必须保留。
func (s *Store) SaveEdgeOneObservation(ctx context.Context, t EdgeOneTransition) error {
	if t.ConfirmationState == "" {
		t.ConfirmationState = EdgeOneConfirmationNotAttempted
	}
	t, err := normalizeTransition(t)
	if err != nil {
		return err
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = time.Now().UTC()
	}
	pendingJSON, err := canonicalCIDRJSON(t.PendingCIDRs)
	if err != nil {
		return err
	}
	return s.WithTransactionContext(ctx, func(tx *sql.Tx) error {
		existing, err := loadEdgeOneTransitionTx(ctx, tx, t.ZoneID)
		if err != nil {
			return err
		}
		state := EdgeOneConfirmationNotAttempted
		requestID := ""
		protectedJSON := "[]"
		if existing != nil {
			protectedJSON, err = canonicalCIDRJSON(existing.ProtectedCIDRs)
			if err != nil {
				return err
			}
			existingJSON, jerr := canonicalCIDRJSON(existing.PendingCIDRs)
			if jerr != nil {
				return jerr
			}
			if existing.PendingVersion == t.PendingVersion && existingJSON == pendingJSON {
				switch existing.ConfirmationState {
				case EdgeOneConfirmationAccepted:
					state, requestID = EdgeOneConfirmationAccepted, existing.ConfirmationRequestID
				case EdgeOneConfirmationUnknown:
					state = EdgeOneConfirmationUnknown
				}
			}
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO edgeone_origin_transitions
				(zone_id, base_version, pending_version, pending_cidrs_json,
				 protected_cidrs_json, confirmation_state, confirmation_request_id, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(zone_id) DO UPDATE SET
				base_version = excluded.base_version,
				pending_version = excluded.pending_version,
				pending_cidrs_json = excluded.pending_cidrs_json,
				protected_cidrs_json = excluded.protected_cidrs_json,
				confirmation_state = excluded.confirmation_state,
				confirmation_request_id = excluded.confirmation_request_id,
				updated_at = excluded.updated_at`,
			t.ZoneID, t.BaseVersion, t.PendingVersion, pendingJSON, protectedJSON,
			state, requestID, t.UpdatedAt.UTC().Format(time.RFC3339Nano))
		return err
	})
}

func loadEdgeOneTransitionTx(ctx context.Context, tx *sql.Tx, zoneID string) (*EdgeOneTransition, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT zone_id, base_version, pending_version, pending_cidrs_json,
		       protected_cidrs_json, confirmation_state, confirmation_request_id, updated_at
		FROM edgeone_origin_transitions WHERE zone_id = ?`, zoneID)
	var (
		t         EdgeOneTransition
		pending   string
		protected string
		updated   string
	)
	if err := row.Scan(&t.ZoneID, &t.BaseVersion, &t.PendingVersion, &pending, &protected,
		&t.ConfirmationState, &t.ConfirmationRequestID, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var err error
	if t.PendingCIDRs, err = parseCIDRJSON(pending); err != nil {
		return nil, err
	}
	if t.ProtectedCIDRs, err = parseCIDRJSON(protected); err != nil {
		return nil, err
	}
	if t.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return nil, err
	}
	return &t, nil
}

// SetEdgeOneConfirmation 仅对同一待更新指纹更新确认状态。
//
// 返回 false 表示记录已被另一指纹替换，调用方不得把旧确认写回新状态。
func (s *Store) SetEdgeOneConfirmation(ctx context.Context, zoneID, pendingVersion string, pendingCIDRs []netip.Prefix, state, requestID string, updated time.Time) (bool, error) {
	zoneID = strings.TrimSpace(zoneID)
	pendingVersion = strings.TrimSpace(pendingVersion)
	if zoneID == "" || pendingVersion == "" {
		return false, fmt.Errorf("确认状态缺少 ZoneId 或待更新版本")
	}
	switch state {
	case EdgeOneConfirmationNotAttempted, EdgeOneConfirmationUnknown, EdgeOneConfirmationAccepted:
	default:
		return false, fmt.Errorf("非法确认状态")
	}
	pendingJSON, err := canonicalCIDRJSON(pendingCIDRs)
	if err != nil {
		return false, err
	}
	if updated.IsZero() {
		updated = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE edgeone_origin_transitions
		SET confirmation_state = ?, confirmation_request_id = ?, updated_at = ?
		WHERE zone_id = ? AND pending_version = ? AND pending_cidrs_json = ?`,
		state, strings.TrimSpace(requestID), updated.UTC().Format(time.RFC3339Nano),
		zoneID, pendingVersion, pendingJSON)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RestoreUnsentEdgeOneConfirmation 只撤销本次已证明未发送的 unknown。
// 指纹、状态和前置提交时间必须仍匹配，已删除的记录不会重建。
func (s *Store) RestoreUnsentEdgeOneConfirmation(ctx context.Context, zoneID, pendingVersion string, pendingCIDRs []netip.Prefix, preparedAt time.Time) (bool, error) {
	pendingJSON, err := canonicalCIDRJSON(pendingCIDRs)
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE edgeone_origin_transitions
		SET confirmation_state = ?, confirmation_request_id = '', updated_at = ?
		WHERE zone_id = ? AND pending_version = ? AND pending_cidrs_json = ?
		      AND confirmation_state = ? AND updated_at = ?`,
		EdgeOneConfirmationNotAttempted, time.Now().UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(zoneID), strings.TrimSpace(pendingVersion), pendingJSON,
		EdgeOneConfirmationUnknown, preparedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// PruneEdgeOneTransitionsTx 删除已不存在站点的过渡记录；同一站点保留记录。
func (s *Store) PruneEdgeOneTransitionsTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx,
		"DELETE FROM edgeone_origin_transitions WHERE zone_id NOT IN (SELECT zone_id FROM edgeone_sites)")
	return err
}

// DeleteEdgeOneTransition 删除一个站点的过渡记录。
func (s *Store) DeleteEdgeOneTransition(ctx context.Context, zoneID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM edgeone_origin_transitions WHERE zone_id = ?", strings.TrimSpace(zoneID))
	return err
}
