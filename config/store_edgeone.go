package config

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// 本文件承载 EdgeOne 站点与目标绑定的 Store 读写（Build8 Step 3）。
// 站点参数存主表，目标绑定单独存行；引用闭包和别名冲突在完整快照内统一校验。

// loadEdgeOneSites 读取全部站点及其绑定目标（事务内可复用）。
//
// 绑定按 target_id 升序返回，站点按 zone_id 升序返回，保证 API 与导出稳定；
// 即使站点没有绑定行，也会返回 TargetIDs 为 nil 的站点，交由完整快照判定损坏。
func loadEdgeOneSites(ctx context.Context, q DBTX) ([]EdgeOneSite, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT s.zone_id, s.protocol, s.ports, s.address_family, b.target_id
		FROM edgeone_sites s
		LEFT JOIN edgeone_site_targets b ON b.zone_id = s.zone_id
		ORDER BY s.zone_id, b.target_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sites := make([]EdgeOneSite, 0)
	index := make(map[string]int)
	for rows.Next() {
		var site EdgeOneSite
		var targetID sql.NullInt64
		if err := rows.Scan(&site.ZoneID, &site.Protocol, &site.Ports, &site.AddressFamily, &targetID); err != nil {
			return nil, err
		}
		idx, ok := index[site.ZoneID]
		if !ok {
			index[site.ZoneID] = len(sites)
			sites = append(sites, site)
			idx = len(sites) - 1
		}
		if targetID.Valid {
			sites[idx].TargetIDs = append(sites[idx].TargetIDs, int(targetID.Int64))
		}
	}
	return sites, rows.Err()
}

// LoadEdgeOneSites 读取站点配置（非事务路径；完整快照与导出请使用 LoadBusinessSnapshotTx）。
func (s *Store) LoadEdgeOneSites() ([]EdgeOneSite, error) {
	return loadEdgeOneSites(context.Background(), s.db)
}

// EdgeOneSiteExistsTx 在同一事务内判断站点是否存在。
func (s *Store) EdgeOneSiteExistsTx(ctx context.Context, tx *sql.Tx, zoneID string) (bool, error) {
	return rowExists(ctx, tx, "SELECT 1 FROM edgeone_sites WHERE zone_id = ?", zoneID)
}

// AddEdgeOneSiteTx 在事务中插入站点及其绑定目标。
func (s *Store) AddEdgeOneSiteTx(ctx context.Context, tx *sql.Tx, site EdgeOneSite) error {
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO edgeone_sites (zone_id, protocol, ports, address_family) VALUES (?, ?, ?, ?)",
		site.ZoneID, site.Protocol, site.Ports, site.AddressFamily,
	); err != nil {
		return err
	}
	return replaceEdgeOneSiteTargetsTx(ctx, tx, site)
}

// UpdateEdgeOneSiteTx 在事务中全量替换站点参数与目标绑定，返回受影响站点行数。
func (s *Store) UpdateEdgeOneSiteTx(ctx context.Context, tx *sql.Tx, site EdgeOneSite) (int64, error) {
	res, err := tx.ExecContext(ctx,
		"UPDATE edgeone_sites SET protocol = ?, ports = ?, address_family = ? WHERE zone_id = ?",
		site.Protocol, site.Ports, site.AddressFamily, site.ZoneID,
	)
	if err != nil {
		return 0, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rows == 0 {
		return 0, nil
	}
	if err := replaceEdgeOneSiteTargetsTx(ctx, tx, site); err != nil {
		return 0, err
	}
	return rows, nil
}

// replaceEdgeOneSiteTargetsTx 删除并重建一个站点的全部绑定行。
func replaceEdgeOneSiteTargetsTx(ctx context.Context, tx *sql.Tx, site EdgeOneSite) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM edgeone_site_targets WHERE zone_id = ?", site.ZoneID); err != nil {
		return err
	}
	for _, targetID := range site.TargetIDs {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO edgeone_site_targets (zone_id, target_id) VALUES (?, ?)",
			site.ZoneID, targetID,
		); err != nil {
			return err
		}
	}
	return nil
}

// DeleteEdgeOneSiteTx 在事务中删除站点及其绑定，返回删除的站点行数。
func (s *Store) DeleteEdgeOneSiteTx(ctx context.Context, tx *sql.Tx, zoneID string) (int64, error) {
	if _, err := tx.ExecContext(ctx, "DELETE FROM edgeone_site_targets WHERE zone_id = ?", zoneID); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM edgeone_sites WHERE zone_id = ?", zoneID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// EdgeOneSitesReferencingTargetTx 返回同一事务内绑定了指定目标的站点 ZoneId。
func (s *Store) EdgeOneSitesReferencingTargetTx(ctx context.Context, tx *sql.Tx, targetID int) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		"SELECT zone_id FROM edgeone_site_targets WHERE target_id = ? ORDER BY zone_id", targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	zones := make([]string, 0)
	for rows.Next() {
		var zoneID string
		if err := rows.Scan(&zoneID); err != nil {
			return nil, err
		}
		zones = append(zones, zoneID)
	}
	return zones, rows.Err()
}

// ValidateEdgeOneSiteTargetsTx 在同一事务内校验站点目标存在性与站内别名冲突。
//
// 空目标列表由领域校验提前拒绝；这里的错误属于请求侧 400。
func (s *Store) ValidateEdgeOneSiteTargetsTx(ctx context.Context, tx *sql.Tx, site EdgeOneSite) error {
	targets, err := loadTargets(ctx, tx)
	if err != nil {
		return err
	}
	byID := make(map[int]TargetConfig, len(targets))
	for _, target := range targets {
		byID[target.ID] = target
	}
	seen := make(map[string]int, len(site.TargetIDs))
	for _, id := range site.TargetIDs {
		target, ok := byID[id]
		if !ok {
			return invalidField("target_ids", fmt.Sprintf("引用了不存在的目标 #%d", id))
		}
		key := TargetIdentity(target)
		if previous, exists := seen[key]; exists {
			return invalidField("target_ids",
				fmt.Sprintf("目标 #%d 与 #%d 指向同一实际资源", previous, id))
		}
		seen[key] = id
	}
	return nil
}

// normalizeAndValidateEdgeOneSites 在完整快照内归一化站点配置并校验引用闭包。
//
// 空绑定、悬空引用属于持久化完整性错误（普通 error，导出/启动安全失败）；
// 同一站点的实际资源别名属于领域冲突（ValidationError，普通目标更新返回 400）。
func normalizeAndValidateEdgeOneSites(sites []EdgeOneSite, targets []TargetConfig) ([]EdgeOneSite, error) {
	normalized := make([]EdgeOneSite, 0, len(sites))
	for _, site := range sites {
		// 空绑定是持久化完整性错误，必须在归一化前判定：NormalizeEdgeOneSite
		// 会返回 ValidationError，若继续用 %w 包装会被 HTTP 层误分类为 400。
		if len(site.TargetIDs) == 0 {
			return nil, fmt.Errorf("站点 %s 没有绑定目标，数据已损坏", site.ZoneID)
		}
		siteValue, err := NormalizeEdgeOneSite(site)
		if err != nil {
			// 持久化站点的字段/引用错误属于内部完整性错误，不能包装成请求侧
			// ValidationError；站内实际资源别名在下方单独保留 400 语义。
			var ve *ValidationError
			if errors.As(err, &ve) {
				return nil, fmt.Errorf("站点 %s 配置无效: %s", site.ZoneID, ve.Error())
			}
			return nil, fmt.Errorf("站点 %s 配置无效: %w", site.ZoneID, err)
		}
		normalized = append(normalized, siteValue)
	}

	byID := make(map[int]TargetConfig, len(targets))
	for _, target := range targets {
		byID[target.ID] = target
	}
	for _, site := range normalized {
		seen := make(map[string]int, len(site.TargetIDs))
		for _, id := range site.TargetIDs {
			target, ok := byID[id]
			if !ok {
				return nil, fmt.Errorf("站点 %s 引用了不存在的目标 #%d，数据已损坏", site.ZoneID, id)
			}
			key := TargetIdentity(target)
			if previous, exists := seen[key]; exists {
				return nil, invalidField("edgeone_sites",
					fmt.Sprintf("站点 %s 的目标 #%d 与 #%d 指向同一实际资源", site.ZoneID, previous, id))
			}
			seen[key] = id
		}
	}
	return normalized, nil
}
