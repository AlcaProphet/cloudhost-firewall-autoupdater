package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store SQLite 配置持久化
type Store struct {
	db *sql.DB
}

// DBTX Store 底层查询所需的最小接口：*sql.DB 与 *sql.Tx 都满足。
//
// 事务内调用必须传 tx，避免语句跳出事务（Build6 §12.7）。
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// BeginTx 开启写事务（配置变更协调器与配置导入共用）
func (s *Store) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, nil)
}

// SyncLog 同步日志记录
type SyncLog struct {
	Timestamp time.Time `json:"timestamp"`
	Target    string    `json:"target"`
	Domain    string    `json:"domain"`
	Result    string    `json:"result"` // success / failed / skipped
	Added     int       `json:"added"`
	Deleted   int       `json:"deleted"`
	Error     string    `json:"error"`
}

// ScannedResource 扫描到的云资源（用于添加目标时资源 ID 自动补全）
type ScannedResource struct {
	ID           int    `json:"id"`
	CloudType    string `json:"cloud_type"`
	Region       string `json:"region"`
	ResourceID   string `json:"resource_id"`
	ResourceName string `json:"resource_name"`
}

// OpenStore 打开或创建 SQLite 数据库
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// WAL 模式 + busy_timeout
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("设置 WAL 模式失败: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("设置 busy_timeout 失败: %w", err)
	}

	s := &Store{db: db}
	if err := s.initTables(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close 关闭数据库
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) initTables() error {
	schema := `
CREATE TABLE IF NOT EXISTS targets (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	cloud_type TEXT NOT NULL,
	region TEXT NOT NULL,
	resource_id TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS rules (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	host TEXT NOT NULL,
	protocol TEXT NOT NULL,
	ports TEXT NOT NULL,
	action TEXT NOT NULL DEFAULT 'ACCEPT',
	targets TEXT DEFAULT '',
	comment TEXT DEFAULT '',
	enable_ipv6 INTEGER DEFAULT 0
);
CREATE TABLE IF NOT EXISTS settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sync_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
	target TEXT,
	domain TEXT,
	result TEXT,
	added INTEGER DEFAULT 0,
	deleted INTEGER DEFAULT 0,
	error TEXT DEFAULT ''
);
CREATE TABLE IF NOT EXISTS alert_email (
	id INTEGER PRIMARY KEY DEFAULT 1,
	enabled INTEGER DEFAULT 0,
	host TEXT DEFAULT '',
	port TEXT DEFAULT '587',
	username TEXT DEFAULT '',
	password TEXT DEFAULT '',
	from_addr TEXT DEFAULT '',
	to_addr TEXT DEFAULT ''
);
CREATE TABLE IF NOT EXISTS alert_webhook (
	id INTEGER PRIMARY KEY DEFAULT 1,
	enabled INTEGER DEFAULT 0,
	url TEXT DEFAULT '',
	channel TEXT DEFAULT 'dingtalk'
);
CREATE TABLE IF NOT EXISTS scanned_resources (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	cloud_type TEXT NOT NULL,
	region TEXT NOT NULL,
	resource_id TEXT NOT NULL,
	resource_name TEXT DEFAULT '',
	scanned_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("初始化表结构失败: %w", err)
	}
	// 迁移：为已有表补充列（"列已存在"属于正常迁移场景，仅忽略该错误；其他错误记录 WARN）
	if _, err := s.db.Exec("ALTER TABLE rules ADD COLUMN enable_ipv6 INTEGER DEFAULT 0"); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		slog.Warn("迁移 rules 表失败", "error", err)
	}
	if _, err := s.db.Exec("ALTER TABLE alert_webhook ADD COLUMN channel TEXT DEFAULT 'dingtalk'"); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		slog.Warn("迁移 alert_webhook 表失败", "error", err)
	}
	return nil
}

// GetSettings 获取全局设置
func (s *Store) GetSettings() (map[string]string, error) {
	rows, err := s.db.Query("SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		settings[k] = v
	}
	return settings, rows.Err()
}

// SetSetting 设置单项配置
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		"INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)",
		key, value,
	)
	return err
}

// GetTargets 获取所有目标
func (s *Store) GetTargets() ([]TargetConfig, error) {
	return loadTargets(context.Background(), s.db)
}

// loadTargets 读取全部目标；传入 *sql.Tx 即可在事务内复用（Build6 §12.7）
func loadTargets(ctx context.Context, q DBTX) ([]TargetConfig, error) {
	rows, err := q.QueryContext(ctx, "SELECT id, cloud_type, region, resource_id FROM targets ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	targets := make([]TargetConfig, 0)
	for rows.Next() {
		var t TargetConfig
		var ct string
		if err := rows.Scan(&t.ID, &ct, &t.Region, &t.ResourceID); err != nil {
			return nil, err
		}
		t.CloudType = CloudType(ct)
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// AddTarget 添加目标
func (s *Store) AddTarget(t TargetConfig) error {
	_, err := s.db.Exec(
		"INSERT INTO targets (cloud_type, region, resource_id) VALUES (?, ?, ?)",
		string(t.CloudType), t.Region, t.ResourceID,
	)
	return err
}

// DeleteTarget 删除目标
func (s *Store) DeleteTarget(id int) error {
	_, err := s.db.Exec("DELETE FROM targets WHERE id = ?", id)
	return err
}

// GetRules 获取所有域名规则
func (s *Store) GetRules() ([]DomainRule, error) {
	return loadRules(context.Background(), s.db)
}

// loadRules 读取全部域名规则；传入 *sql.Tx 即可在事务内复用（Build6 §12.7）
func loadRules(ctx context.Context, q DBTX) ([]DomainRule, error) {
	rows, err := q.QueryContext(ctx, "SELECT id, host, protocol, ports, action, targets, comment, enable_ipv6 FROM rules ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := make([]DomainRule, 0)
	for rows.Next() {
		var r DomainRule
		var targets string
		var enableIPv6 int
		if err := rows.Scan(&r.ID, &r.Host, &r.Protocol, &r.Ports, &r.Action, &targets, &r.Comment, &enableIPv6); err != nil {
			return nil, err
		}
		if targets != "" {
			var nums []int
			if err := json.Unmarshal([]byte(targets), &nums); err == nil {
				r.Targets = nums
			}
		}
		r.EnableIPv6 = enableIPv6 != 0
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// ruleColumns 把域名规则转换为数据库列值（targets JSON 文本、enable_ipv6 0/1）
func ruleColumns(r DomainRule) (targetsJSON string, enableIPv6 int, err error) {
	targets := r.Targets
	if targets == nil {
		targets = []int{}
	}
	raw, err := json.Marshal(targets)
	if err != nil {
		return "", 0, fmt.Errorf("序列化规则目标失败: %w", err)
	}
	if r.EnableIPv6 {
		enableIPv6 = 1
	}
	return string(raw), enableIPv6, nil
}

// AddRule 添加域名规则
func (s *Store) AddRule(r DomainRule) error {
	targetsJSON, enableIPv6, err := ruleColumns(r)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		"INSERT INTO rules (host, protocol, ports, action, targets, comment, enable_ipv6) VALUES (?, ?, ?, ?, ?, ?, ?)",
		r.Host, r.Protocol, r.Ports, r.Action, targetsJSON, r.Comment, enableIPv6,
	)
	return err
}

// DeleteRule 删除域名规则
func (s *Store) DeleteRule(id int) error {
	_, err := s.db.Exec("DELETE FROM rules WHERE id = ?", id)
	return err
}

// UpdateTarget 更新目标
func (s *Store) UpdateTarget(id int, t TargetConfig) error {
	_, err := s.db.Exec(
		"UPDATE targets SET cloud_type = ?, region = ?, resource_id = ? WHERE id = ?",
		string(t.CloudType), t.Region, t.ResourceID, id,
	)
	return err
}

// UpdateRule 更新域名规则
func (s *Store) UpdateRule(id int, r DomainRule) error {
	targetsJSON, enableIPv6, err := ruleColumns(r)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		"UPDATE rules SET host = ?, protocol = ?, ports = ?, action = ?, targets = ?, comment = ?, enable_ipv6 = ? WHERE id = ?",
		r.Host, r.Protocol, r.Ports, r.Action, targetsJSON, r.Comment, enableIPv6, id,
	)
	return err
}

// resetAllSQL 清空全部业务表的语句（ResetAll 与事务内 ResetAllTx 共用）
const resetAllSQL = "DELETE FROM targets; DELETE FROM rules; DELETE FROM settings; DELETE FROM sync_logs;" +
	"DELETE FROM alert_email; DELETE FROM alert_webhook; DELETE FROM scanned_resources;"

// ResetAll 清空全部业务数据（目标、规则、凭据、日志、告警、扫描结果），等效重新初始化
func (s *Store) ResetAll() error {
	_, err := s.db.Exec(resetAllSQL)
	return err
}

// ResetAllTx 在事务中清空全部业务数据（「清空所有数据」经协调器调用）
func (s *Store) ResetAllTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, resetAllSQL)
	return err
}

// WithTransaction 在事务中执行操作，失败自动回滚
func (s *Store) WithTransaction(fn func(tx *sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ReplaceScannedResources 覆盖式保存某云厂商+地域的扫描结果（先删后插）
func (s *Store) ReplaceScannedResources(cloudType, region string, resources []ScannedResource) error {
	return s.WithTransaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM scanned_resources WHERE cloud_type = ? AND region = ?", cloudType, region); err != nil {
			return fmt.Errorf("清理旧扫描结果失败: %w", err)
		}
		for _, r := range resources {
			if _, err := tx.Exec(
				"INSERT INTO scanned_resources (cloud_type, region, resource_id, resource_name) VALUES (?, ?, ?, ?)",
				cloudType, region, r.ResourceID, r.ResourceName,
			); err != nil {
				return fmt.Errorf("写入扫描结果失败: %w", err)
			}
		}
		return nil
	})
}

// GetScannedResources 获取某云厂商的扫描结果（跨地域汇总，按 resource_id 排序）
func (s *Store) GetScannedResources(cloudType string) ([]ScannedResource, error) {
	rows, err := s.db.Query(
		"SELECT id, cloud_type, region, resource_id, resource_name FROM scanned_resources WHERE cloud_type = ? ORDER BY resource_id",
		cloudType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	resources := make([]ScannedResource, 0)
	for rows.Next() {
		var r ScannedResource
		if err := rows.Scan(&r.ID, &r.CloudType, &r.Region, &r.ResourceID, &r.ResourceName); err != nil {
			return nil, err
		}
		resources = append(resources, r)
	}
	return resources, rows.Err()
}

// DeleteScannedResources 清理某云厂商的全部扫描结果
func (s *Store) DeleteScannedResources(cloudType string) error {
	_, err := s.db.Exec("DELETE FROM scanned_resources WHERE cloud_type = ?", cloudType)
	return err
}

// ClearAllTx 在事务中清空目标、规则与设置（配置导入的覆盖式替换第一部分）
func (s *Store) ClearAllTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM targets; DELETE FROM rules; DELETE FROM settings;")
	return err
}

// AddTargetTx 在事务中插入目标并返回数据库分配的 ID（Build6 §12.7）
func (s *Store) AddTargetTx(ctx context.Context, tx *sql.Tx, t TargetConfig) (int64, error) {
	res, err := tx.ExecContext(
		ctx,
		"INSERT INTO targets (cloud_type, region, resource_id) VALUES (?, ?, ?)",
		string(t.CloudType), t.Region, t.ResourceID,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("获取新增目标 ID 失败: %w", err)
	}
	return id, nil
}

// UpdateTargetTx 在事务中更新目标，返回受影响行数（0 表示目标不存在）
func (s *Store) UpdateTargetTx(ctx context.Context, tx *sql.Tx, id int, t TargetConfig) (int64, error) {
	res, err := tx.ExecContext(
		ctx,
		"UPDATE targets SET cloud_type = ?, region = ?, resource_id = ? WHERE id = ?",
		string(t.CloudType), t.Region, t.ResourceID, id,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteTargetTx 在事务中删除目标，返回受影响行数（0 表示目标不存在）
func (s *Store) DeleteTargetTx(ctx context.Context, tx *sql.Tx, id int) (int64, error) {
	res, err := tx.ExecContext(ctx, "DELETE FROM targets WHERE id = ?", id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// TargetExistsTx 在同一事务内判断目标是否存在
func (s *Store) TargetExistsTx(ctx context.Context, tx *sql.Tx, id int) (bool, error) {
	return rowExists(ctx, tx, "SELECT 1 FROM targets WHERE id = ?", id)
}

// RuleExistsTx 在同一事务内判断规则是否存在
func (s *Store) RuleExistsTx(ctx context.Context, tx *sql.Tx, id int) (bool, error) {
	return rowExists(ctx, tx, "SELECT 1 FROM rules WHERE id = ?", id)
}

// rowExists 执行存在性查询
func rowExists(ctx context.Context, tx *sql.Tx, query string, args ...any) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx, query, args...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ReferencingRuleIDsTx 返回同一事务内引用了指定目标的规则 ID（按 id 升序）。
//
// rules.targets 以 JSON 数组文本保存，这里逐条解析后精确比较，
// 避免 LIKE 子串匹配把目标 1 误判为命中目标 12（Build6 §12.7）。
func (s *Store) ReferencingRuleIDsTx(ctx context.Context, tx *sql.Tx, targetID int) ([]int, error) {
	rules, err := loadRules(ctx, tx)
	if err != nil {
		return nil, err
	}
	var ids []int
	for _, r := range rules {
		for _, t := range r.Targets {
			if t == targetID {
				ids = append(ids, r.ID)
				break
			}
		}
	}
	return ids, nil
}

// ValidateRuleTargetsTx 在同一事务内确认规则引用的目标都存在。
//
// 空数组继续表示“适用于全部目标”；正数与去重检查由调用方在领域层完成。
func (s *Store) ValidateRuleTargetsTx(ctx context.Context, tx *sql.Tx, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	targets, err := loadTargets(ctx, tx)
	if err != nil {
		return err
	}
	existing := make(map[int]bool, len(targets))
	for _, t := range targets {
		existing[t.ID] = true
	}
	for _, id := range ids {
		if !existing[id] {
			return invalidField("targets", fmt.Sprintf("引用了不存在的目标 #%d", id))
		}
	}
	return nil
}

// AddRuleTx 在事务中添加域名规则
func (s *Store) AddRuleTx(ctx context.Context, tx *sql.Tx, r DomainRule) error {
	targetsJSON, enableIPv6, err := ruleColumns(r)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(
		ctx,
		"INSERT INTO rules (host, protocol, ports, action, targets, comment, enable_ipv6) VALUES (?, ?, ?, ?, ?, ?, ?)",
		r.Host, r.Protocol, r.Ports, r.Action, targetsJSON, r.Comment, enableIPv6,
	)
	return err
}

// UpdateRuleTx 在事务中更新规则，返回受影响行数（0 表示规则不存在）
func (s *Store) UpdateRuleTx(ctx context.Context, tx *sql.Tx, id int, r DomainRule) (int64, error) {
	targetsJSON, enableIPv6, err := ruleColumns(r)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(
		ctx,
		"UPDATE rules SET host = ?, protocol = ?, ports = ?, action = ?, targets = ?, comment = ?, enable_ipv6 = ? WHERE id = ?",
		r.Host, r.Protocol, r.Ports, r.Action, targetsJSON, r.Comment, enableIPv6, id,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteRuleTx 在事务中删除规则，返回受影响行数（0 表示规则不存在）
func (s *Store) DeleteRuleTx(ctx context.Context, tx *sql.Tx, id int) (int64, error) {
	res, err := tx.ExecContext(ctx, "DELETE FROM rules WHERE id = ?", id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SetSettingTx 在事务中写入单项配置
func (s *Store) SetSettingTx(ctx context.Context, tx *sql.Tx, key, value string) error {
	_, err := tx.ExecContext(
		ctx,
		"INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)",
		key, value,
	)
	return err
}

// BatchAddTargetsTx 在事务中批量插入目标，返回“插入顺序 → 数据库 ID”的切片
// （Step 5 的 export_id 映射会使用该结果）
func (s *Store) BatchAddTargetsTx(ctx context.Context, tx *sql.Tx, targets []TargetConfig) ([]int64, error) {
	ids := make([]int64, 0, len(targets))
	for _, t := range targets {
		id, err := s.AddTargetTx(ctx, tx, t)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// BatchAddRulesTx 在事务中批量添加规则
func (s *Store) BatchAddRulesTx(ctx context.Context, tx *sql.Tx, rules []DomainRule) error {
	for _, r := range rules {
		if err := s.AddRuleTx(ctx, tx, r); err != nil {
			return err
		}
	}
	return nil
}

// GetAlertEmail 获取邮件告警配置
func (s *Store) GetAlertEmail() (*AlertEmailConfig, error) {
	var cfg AlertEmailConfig
	var enabled int
	err := s.db.QueryRow("SELECT enabled, host, port, username, password, from_addr, to_addr FROM alert_email WHERE id = 1").
		Scan(&enabled, &cfg.Host, &cfg.Port, &cfg.Username, &cfg.Password, &cfg.FromAddr, &cfg.ToAddr)
	if err == sql.ErrNoRows {
		return &AlertEmailConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg.Enabled = enabled != 0
	return &cfg, nil
}

// saveAlertEmailSQL 保存邮件告警配置的语句（单条写入与事务内写入共用）
const saveAlertEmailSQL = `INSERT OR REPLACE INTO alert_email (id, enabled, host, port, username, password, from_addr, to_addr)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?)`

// alertEmailArgs 把邮件告警配置转换为 SQL 参数
func alertEmailArgs(cfg *AlertEmailConfig) []any {
	enabled := 0
	if cfg.Enabled {
		enabled = 1
	}
	return []any{enabled, cfg.Host, cfg.Port, cfg.Username, cfg.Password, cfg.FromAddr, cfg.ToAddr}
}

// SaveAlertEmail 保存邮件告警配置
func (s *Store) SaveAlertEmail(cfg *AlertEmailConfig) error {
	_, err := s.db.Exec(saveAlertEmailSQL, alertEmailArgs(cfg)...)
	return err
}

// SaveAlertEmailTx 在事务中保存邮件告警配置（PUT /api/alerts 与配置导入共用）
func (s *Store) SaveAlertEmailTx(ctx context.Context, tx *sql.Tx, cfg *AlertEmailConfig) error {
	_, err := tx.ExecContext(ctx, saveAlertEmailSQL, alertEmailArgs(cfg)...)
	return err
}

// GetAlertWebhook 获取 Webhook 告警配置
func (s *Store) GetAlertWebhook() (*AlertWebhookConfig, error) {
	var cfg AlertWebhookConfig
	var enabled int
	err := s.db.QueryRow("SELECT enabled, url, channel FROM alert_webhook WHERE id = 1").
		Scan(&enabled, &cfg.URL, &cfg.Channel)
	if err == sql.ErrNoRows {
		return &AlertWebhookConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg.Enabled = enabled != 0
	return &cfg, nil
}

// saveAlertWebhookSQL 保存 Webhook 告警配置的语句（单条写入与事务内写入共用）
const saveAlertWebhookSQL = `INSERT OR REPLACE INTO alert_webhook (id, enabled, url, channel) VALUES (1, ?, ?, ?)`

// alertWebhookArgs 把 Webhook 告警配置转换为 SQL 参数
func alertWebhookArgs(cfg *AlertWebhookConfig) []any {
	enabled := 0
	if cfg.Enabled {
		enabled = 1
	}
	return []any{enabled, cfg.URL, cfg.Channel}
}

// SaveAlertWebhook 保存 Webhook 告警配置
func (s *Store) SaveAlertWebhook(cfg *AlertWebhookConfig) error {
	_, err := s.db.Exec(saveAlertWebhookSQL, alertWebhookArgs(cfg)...)
	return err
}

// SaveAlertWebhookTx 在事务中保存 Webhook 告警配置（PUT /api/alerts 与配置导入共用）
func (s *Store) SaveAlertWebhookTx(ctx context.Context, tx *sql.Tx, cfg *AlertWebhookConfig) error {
	_, err := tx.ExecContext(ctx, saveAlertWebhookSQL, alertWebhookArgs(cfg)...)
	return err
}

// AddSyncLog 添加同步日志
func (s *Store) AddSyncLog(log SyncLog) error {
	_, err := s.db.Exec(
		"INSERT INTO sync_logs (timestamp, target, domain, result, added, deleted, error) VALUES (?, ?, ?, ?, ?, ?, ?)",
		log.Timestamp, log.Target, log.Domain, log.Result, log.Added, log.Deleted, log.Error,
	)
	if err != nil {
		return err
	}
	// 仅当超过保留上限（1000 条）时执行清理，避免每次写入全表扫描（O(n)）
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM sync_logs").Scan(&count); err == nil && count > 1000 {
		_, err = s.db.Exec("DELETE FROM sync_logs WHERE id NOT IN (SELECT id FROM sync_logs ORDER BY id DESC LIMIT 1000)")
		if err != nil {
			return err
		}
	}
	return nil
}

// GetSyncLogs 获取最近 N 条同步日志
func (s *Store) GetSyncLogs(limit int) ([]SyncLog, error) {
	rows, err := s.db.Query(
		"SELECT timestamp, target, domain, result, added, deleted, error FROM sync_logs ORDER BY id DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]SyncLog, 0)
	for rows.Next() {
		var l SyncLog
		if err := rows.Scan(&l.Timestamp, &l.Target, &l.Domain, &l.Result, &l.Added, &l.Deleted, &l.Error); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// ClearSyncLogs 清空全部同步历史记录（仅 sync_logs 表，不影响 targets/rules/settings）
func (s *Store) ClearSyncLogs() error {
	_, err := s.db.Exec("DELETE FROM sync_logs")
	return err
}

// LoadConfig 从 SQLite 构建 Config
func (s *Store) LoadConfig() (*Config, error) {
	targets, err := s.GetTargets()
	if err != nil {
		return nil, err
	}
	rules, err := s.GetRules()
	if err != nil {
		return nil, err
	}
	settings, err := s.GetSettings()
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Targets:          targets,
		DomainRules:      rules,
		Tag:              "auto-dns",
		Interval:         5 * time.Minute,
		DNS:              "223.5.5.5",
		DNSTimeout:       10 * time.Second,
		DNSFailThreshold: 5,
		LogLevel:         "info",
		SyncEnabled:      true, // 默认开启（向后兼容：老用户无该键时保持启动即同步）
		TCAccessID:       settings["tc_access_id"],
		TCAccessKey:      settings["tc_access_key"],
		AliAccessID:      settings["ali_access_id"],
		AliAccessKey:     settings["ali_access_key"],
	}

	// 设置校验（Build6 §12.10）：空白值按“缺失”处理并使用默认值；
	// 已有非空但非法的值不得静默回退默认值，而是返回带键名（不含值）的错误。
	if v := strings.TrimSpace(settings["tag"]); v != "" {
		tag, err := NormalizeTag(v)
		if err != nil {
			return nil, err
		}
		cfg.Tag = tag
	}
	if v := strings.TrimSpace(settings["interval"]); v != "" {
		_, d, err := ParsePositiveDuration("interval", v)
		if err != nil {
			return nil, err
		}
		cfg.Interval = d
	}
	if v := strings.TrimSpace(settings["dns"]); v != "" {
		dnsAddr, err := NormalizeDNSAddress(v)
		if err != nil {
			return nil, err
		}
		cfg.DNS = dnsAddr
	}
	if v := strings.TrimSpace(settings["log_level"]); v != "" {
		level, err := NormalizeLogLevel(v)
		if err != nil {
			return nil, err
		}
		cfg.LogLevel = level
	}
	// webui_port 已不是业务设置：数据库中的残留键一律忽略，不做迁移或清理
	if v := strings.TrimSpace(settings["dns_fail_threshold"]); v != "" {
		_, n, err := NormalizeDNSFailThreshold(v)
		if err != nil {
			return nil, err
		}
		cfg.DNSFailThreshold = n
	}
	if v := strings.TrimSpace(settings["dns_timeout"]); v != "" {
		_, d, err := ParsePositiveDuration("dns_timeout", v)
		if err != nil {
			return nil, err
		}
		cfg.DNSTimeout = d
	}
	// theme 是业务设置但不进入同步器运行时配置（前端直接读 GET /api/settings），
	// 这里只做合法性校验，避免非法值长期留在数据库中静默生效
	if v := strings.TrimSpace(settings["theme"]); v != "" {
		if _, err := NormalizeTheme(v); err != nil {
			return nil, err
		}
	}
	if v := strings.TrimSpace(settings["sync_enabled"]); v != "" {
		enabled, err := NormalizeSyncEnabled(v)
		if err != nil {
			return nil, err
		}
		cfg.SyncEnabled = enabled
	}

	return cfg, nil
}
