package config

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// 本文件承载 Lighthouse 实例级策略压缩的业务配置与运行观察读写（Build9 Step 1）。
// 业务配置存 lighthouse_compression_settings，运行观察存 lighthouse_compression_state；
// 两者都以物理实例身份为键，绝不使用本地 target ID。

const lighthouseCompressionTimeLayout = time.RFC3339Nano

// loadLighthouseCompressionSettings 读取全部已保存的实例压缩配置（事务内可复用）。
//
// 结果按物理身份稳定排序；空表返回非 nil 空切片，保证 v5 导出为 [] 而不是 null。
func loadLighthouseCompressionSettings(ctx context.Context, q DBTX) ([]LighthouseCompression, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT cloud_type, region, resource_id, enabled, extra_ipv4_allow
		FROM lighthouse_compression_settings
		ORDER BY cloud_type, region, resource_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]LighthouseCompression, 0)
	for rows.Next() {
		var raw LighthouseCompression
		var enabled int
		var cloudType string
		if err := rows.Scan(&cloudType, &raw.Region, &raw.ResourceID, &enabled, &raw.ExtraIPv4Allow); err != nil {
			return nil, err
		}
		raw.CloudType = CloudType(cloudType)
		raw.Enabled = enabled != 0
		normalized, err := NormalizeLighthouseCompression(raw)
		if err != nil {
			var ve *ValidationError
			if errors.As(err, &ve) {
				return nil, fmt.Errorf("Lighthouse 压缩配置损坏（%s/%s/%s）：%s",
					raw.CloudType, raw.Region, raw.ResourceID, ve.Error())
			}
			return nil, fmt.Errorf("Lighthouse 压缩配置损坏（%s/%s/%s）：%w",
				raw.CloudType, raw.Region, raw.ResourceID, err)
		}
		out = append(out, normalized)
	}
	return out, rows.Err()
}

// LoadLighthouseCompressionSettings 读取全部实例压缩配置（非事务路径，供 API/测试使用）。
func (s *Store) LoadLighthouseCompressionSettings() ([]LighthouseCompression, error) {
	return loadLighthouseCompressionSettings(context.Background(), s.db)
}

// LoadLighthouseCompressionSettingsTx 读取本次导入将替换的实例设置。
func (s *Store) LoadLighthouseCompressionSettingsTx(ctx context.Context, tx *sql.Tx) ([]LighthouseCompression, error) {
	return loadLighthouseCompressionSettings(ctx, tx)
}

// loadLighthouseCompressionState 读取指定实例的持久化运行观察；不存在返回 found=false。
func loadLighthouseCompressionState(ctx context.Context, q DBTX, c LighthouseCompression) (LighthouseCompressionState, bool, error) {
	var state LighthouseCompressionState
	var pending int
	err := q.QueryRowContext(ctx, `
		SELECT pending_review, pending_reason, pending_since
		FROM lighthouse_compression_state
		WHERE cloud_type = ? AND region = ? AND resource_id = ?`,
		string(c.CloudType), c.Region, c.ResourceID,
	).Scan(&pending, &state.PendingReason, &state.PendingSince)
	if errors.Is(err, sql.ErrNoRows) {
		return LighthouseCompressionState{}, false, nil
	}
	if err != nil {
		return LighthouseCompressionState{}, false, err
	}
	state.CloudType = c.CloudType
	state.Region = c.Region
	state.ResourceID = c.ResourceID
	state.PendingReview = pending != 0
	return state, true, nil
}

// LoadLighthouseCompressionState 读取指定实例的持久化运行观察（非事务路径）。
func (s *Store) LoadLighthouseCompressionState(c LighthouseCompression) (LighthouseCompressionState, bool, error) {
	return loadLighthouseCompressionState(context.Background(), s.db, c)
}

// SaveLighthouseCompressionTx 在事务内 upsert 一条实例压缩配置（不触碰运行观察）。
func (s *Store) SaveLighthouseCompressionTx(ctx context.Context, tx *sql.Tx, c LighthouseCompression) error {
	normalized, err := NormalizeLighthouseCompression(c)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(lighthouseCompressionTimeLayout)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO lighthouse_compression_settings
			(cloud_type, region, resource_id, enabled, extra_ipv4_allow, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(cloud_type, region, resource_id) DO UPDATE SET
			enabled = excluded.enabled,
			extra_ipv4_allow = excluded.extra_ipv4_allow,
			updated_at = excluded.updated_at`,
		string(normalized.CloudType), normalized.Region, normalized.ResourceID,
		boolToInt(normalized.Enabled), normalized.ExtraIPv4Allow, now,
	); err != nil {
		return err
	}
	return nil
}

// SaveLighthouseCompressionStateTx 在事务内 upsert 运行观察。
func (s *Store) SaveLighthouseCompressionStateTx(ctx context.Context, tx *sql.Tx, state LighthouseCompressionState) error {
	now := time.Now().UTC().Format(lighthouseCompressionTimeLayout)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO lighthouse_compression_state
			(cloud_type, region, resource_id, pending_review, pending_reason, pending_since, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(cloud_type, region, resource_id) DO UPDATE SET
			pending_review = excluded.pending_review,
			pending_reason = excluded.pending_reason,
			pending_since = excluded.pending_since,
			updated_at = excluded.updated_at`,
		string(state.CloudType), state.Region, state.ResourceID,
		boolToInt(state.PendingReview), state.PendingReason, state.PendingSince, now,
	); err != nil {
		return err
	}
	return nil
}

// DeleteLighthouseCompressionStateTx 在事务内删除指定实例的运行观察。
func (s *Store) DeleteLighthouseCompressionStateTx(ctx context.Context, tx *sql.Tx, c LighthouseCompression) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM lighthouse_compression_state
		WHERE cloud_type = ? AND region = ? AND resource_id = ?`,
		string(c.CloudType), c.Region, c.ResourceID)
	return err
}

// MarkLighthouseCompressionDisabledTx 建立退出复查，已有复查保留其原因和时间。
func (s *Store) MarkLighthouseCompressionDisabledTx(ctx context.Context, tx *sql.Tx, c LighthouseCompression) error {
	state, _, err := loadLighthouseCompressionState(ctx, tx, c)
	if err != nil || state.PendingReview {
		return err
	}
	return s.SaveLighthouseCompressionStateTx(ctx, tx, LighthouseCompressionState{
		CloudType: c.CloudType, Region: c.Region, ResourceID: c.ResourceID,
		PendingReview: true, PendingReason: "disabled",
		PendingSince: time.Now().UTC().Format(lighthouseCompressionTimeLayout),
	})
}

// PruneLighthouseCompressionStatesTx 清除导入后已移除或重新开启的实例观察。
func (s *Store) PruneLighthouseCompressionStatesTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM lighthouse_compression_state AS state
		WHERE NOT EXISTS (
			SELECT 1 FROM targets AS target
			WHERE (target.cloud_type, target.region, target.resource_id) =
			      (state.cloud_type, state.region, state.resource_id)
		) OR EXISTS (
			SELECT 1 FROM lighthouse_compression_settings AS settings
			WHERE (settings.cloud_type, settings.region, settings.resource_id) =
			      (state.cloud_type, state.region, state.resource_id) AND settings.enabled = 1
		)`)
	return err
}

// DeleteLighthouseCompressionIfUnreferencedTx 在目标已删除/改绑后清理最后一个别名对应的实例设置。
//
// 只有当前 targets 中已不存在同一物理身份时才删除 settings 与 state；仍有别名则保留。
// 返回是否执行了删除。
func (s *Store) DeleteLighthouseCompressionIfUnreferencedTx(ctx context.Context, tx *sql.Tx, c LighthouseCompression) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx, `
		SELECT 1 FROM targets
		WHERE cloud_type = ? AND region = ? AND resource_id = ?
		LIMIT 1`,
		string(c.CloudType), c.Region, c.ResourceID,
	).Scan(&one)
	switch {
	case err == nil:
		return false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return false, err
	}
	if err := s.DeleteLighthouseCompressionStateTx(ctx, tx, c); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM lighthouse_compression_settings
		WHERE cloud_type = ? AND region = ? AND resource_id = ?`,
		string(c.CloudType), c.Region, c.ResourceID,
	); err != nil {
		return false, err
	}
	return true, nil
}

// LoadTargetByIDTx 在同一事务内读取单个目标；不存在返回 found=false。
//
// 目标改绑需要旧身份来清理最后一个别名对应的压缩设置，禁止在事务外预读。
func (s *Store) LoadTargetByIDTx(ctx context.Context, tx *sql.Tx, id int) (TargetConfig, bool, error) {
	var target TargetConfig
	var cloudType string
	err := tx.QueryRowContext(ctx,
		"SELECT id, cloud_type, region, resource_id FROM targets WHERE id = ?", id,
	).Scan(&target.ID, &cloudType, &target.Region, &target.ResourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return TargetConfig{}, false, nil
	}
	if err != nil {
		return TargetConfig{}, false, err
	}
	target.CloudType = CloudType(cloudType)
	return target, true, nil
}

// loadLighthouseCompressionSetting 读取指定物理实例的一条压缩配置。
func loadLighthouseCompressionSetting(ctx context.Context, q DBTX, c LighthouseCompression) (LighthouseCompression, bool, error) {
	var out LighthouseCompression
	var enabled int
	err := q.QueryRowContext(ctx, `
		SELECT cloud_type, region, resource_id, enabled, extra_ipv4_allow
		FROM lighthouse_compression_settings
		WHERE cloud_type = ? AND region = ? AND resource_id = ?`,
		string(c.CloudType), c.Region, c.ResourceID,
	).Scan(&out.CloudType, &out.Region, &out.ResourceID, &enabled, &out.ExtraIPv4Allow)
	if errors.Is(err, sql.ErrNoRows) {
		return LighthouseCompression{}, false, nil
	}
	if err != nil {
		return LighthouseCompression{}, false, err
	}
	out.Enabled = enabled != 0
	normalized, err := NormalizeLighthouseCompression(out)
	if err != nil {
		return LighthouseCompression{}, false, err
	}
	return normalized, true, nil
}

// LoadLighthouseCompressionSettingTx 在事务内读取一条实例压缩配置。
func (s *Store) LoadLighthouseCompressionSettingTx(ctx context.Context, tx *sql.Tx, c LighthouseCompression) (LighthouseCompression, bool, error) {
	return loadLighthouseCompressionSetting(ctx, tx, c)
}

// LoadLighthouseCompressionStateTx 在事务内读取实例运行观察。
func (s *Store) LoadLighthouseCompressionStateTx(ctx context.Context, tx *sql.Tx, c LighthouseCompression) (LighthouseCompressionState, bool, error) {
	return loadLighthouseCompressionState(ctx, tx, c)
}
