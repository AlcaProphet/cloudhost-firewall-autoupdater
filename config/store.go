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

// BeginReadOnlyTx 开启 SQLite 只读事务（version 2 导出使用，Build6 §12.7）。
//
// 导出全部读取都传该 tx，保证配置包快照内部一致，且不进入变更锁。
func (s *Store) BeginReadOnlyTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
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

// GetSettings 获取全局设置（非事务路径；完整快照读取请用 LoadBusinessSnapshotTx）
func (s *Store) GetSettings() (map[string]string, error) {
	return loadSettings(context.Background(), s.db)
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

// resetAllSQL 清空全部业务表的语句（ResetAllTx 使用）
const resetAllSQL = "DELETE FROM targets; DELETE FROM rules; DELETE FROM settings; DELETE FROM sync_logs;" +
	"DELETE FROM alert_email; DELETE FROM alert_webhook; DELETE FROM scanned_resources;"

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

// GetAlertEmail 获取邮件告警配置
func (s *Store) GetAlertEmail() (*AlertEmailConfig, error) {
	cfg, err := loadAlertEmail(context.Background(), s.db)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// loadAlertEmail 读取邮件告警配置（事务内可复用；无行时返回零值配置）
func loadAlertEmail(ctx context.Context, q DBTX) (AlertEmailConfig, error) {
	var cfg AlertEmailConfig
	var enabled int
	err := q.QueryRowContext(ctx,
		"SELECT enabled, host, port, username, password, from_addr, to_addr FROM alert_email WHERE id = 1").
		Scan(&enabled, &cfg.Host, &cfg.Port, &cfg.Username, &cfg.Password, &cfg.FromAddr, &cfg.ToAddr)
	if errors.Is(err, sql.ErrNoRows) {
		return AlertEmailConfig{}, nil
	}
	if err != nil {
		return AlertEmailConfig{}, err
	}
	cfg.Enabled = enabled != 0
	return cfg, nil
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
	cfg, err := loadAlertWebhook(context.Background(), s.db)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// loadAlertWebhook 读取 Webhook 告警配置（事务内可复用；无行时返回零值配置）
func loadAlertWebhook(ctx context.Context, q DBTX) (AlertWebhookConfig, error) {
	var cfg AlertWebhookConfig
	var enabled int
	err := q.QueryRowContext(ctx, "SELECT enabled, url, channel FROM alert_webhook WHERE id = 1").
		Scan(&enabled, &cfg.URL, &cfg.Channel)
	if errors.Is(err, sql.ErrNoRows) {
		return AlertWebhookConfig{}, nil
	}
	if err != nil {
		return AlertWebhookConfig{}, err
	}
	cfg.Enabled = enabled != 0
	return cfg, nil
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

// GetAlertEmailConfigTx 在事务内读取邮件告警配置（配置导入的候选构造使用）。
func (s *Store) GetAlertEmailConfigTx(ctx context.Context, q DBTX) (AlertEmailConfig, error) {
	return loadAlertEmail(ctx, q)
}

// GetAlertWebhookConfigTx 在事务内读取 Webhook 告警配置（配置导入的候选构造使用）。
func (s *Store) GetAlertWebhookConfigTx(ctx context.Context, q DBTX) (AlertWebhookConfig, error) {
	return loadAlertWebhook(ctx, q)
}

// GetSettingsTx 在事务内按固定键集合读取设置（不返回数据库中的未知键）。
func (s *Store) GetSettingsTx(ctx context.Context, q DBTX) (map[string]string, error) {
	return loadSettingsByKeys(ctx, q, settingsKeysV2)
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

// LoadConfig 从 SQLite 构建启动期 Config。
//
// 走与导出、导入、协调器完全相同的「事务内完整业务快照」路径（Build6 §12.10）：
// 同一个 LoadBusinessSnapshotTx 负责读出 targets/rules/settings/alerts 并复用
// validate.go 的归一化与校验，避免出现「API 能写入但重启加载失败」的双口径。
func (s *Store) LoadConfig() (*Config, error) {
	ctx := context.Background()
	tx, err := s.BeginReadOnlyTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("开始只读事务失败: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			slog.Error("回滚只读事务失败", "error", rbErr)
		}
	}()

	snapshot, err := s.LoadBusinessSnapshotTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交只读事务失败: %w", err)
	}
	return snapshot.ToConfig(), nil
}

// LoadBusinessSnapshot 读取一次完整业务配置快照（启动路径使用；内部走只读事务）。
//
// 与导出、导入、协调器共用同一读取与校验路径，避免出现第二套配置加载语义。
func (s *Store) LoadBusinessSnapshot() (*BusinessSnapshot, error) {
	ctx := context.Background()
	tx, err := s.BeginReadOnlyTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("开始只读事务失败: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			slog.Error("回滚只读事务失败", "error", rbErr)
		}
	}()

	snapshot, err := s.LoadBusinessSnapshotTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交只读事务失败: %w", err)
	}
	return snapshot, nil
}

// LoadBusinessSnapshotTx 在调用方给定的事务内读取完整业务配置快照
// （targets / rules / settings / alert_email / alert_webhook），并完成归一化与校验。
//
// 只读导出、导入候选构造、协调器候选构造与启动加载共用本方法：
// 所有读取都走同一个 tx，绝不回退到 s.db，保证快照内部一致（Build6 §12.7、§12.11）。
func (s *Store) LoadBusinessSnapshotTx(ctx context.Context, q DBTX) (*BusinessSnapshot, error) {
	targets, err := loadTargets(ctx, q)
	if err != nil {
		return nil, err
	}
	rules, err := loadRules(ctx, q)
	if err != nil {
		return nil, err
	}
	settings, err := loadSettings(ctx, q)
	if err != nil {
		return nil, err
	}
	email, err := loadAlertEmail(ctx, q)
	if err != nil {
		return nil, err
	}
	webhook, err := loadAlertWebhook(ctx, q)
	if err != nil {
		return nil, err
	}

	normalized, err := normalizeSettings(settings)
	if err != nil {
		return nil, err
	}

	return &BusinessSnapshot{
		Targets:  targets,
		Rules:    rules,
		Settings: normalized,
		Email:    email,
		Webhook:  webhook,
	}, nil
}

// loadSettings 读取全部设置键值（事务内可复用）。
func loadSettings(ctx context.Context, q DBTX) (map[string]string, error) {
	rows, err := q.QueryContext(ctx, "SELECT key, value FROM settings")
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

// loadSettingsByKeys 按固定键集合读取设置值（事务内可复用）。
//
// 显式指定键名，不依赖数据库里存在哪些未知键，因此导入与快照都不会把
// 旧数据库的未知键带回新状态。
func loadSettingsByKeys(ctx context.Context, q DBTX, keys []string) (map[string]string, error) {
	settings := make(map[string]string, len(keys))
	if len(keys) == 0 {
		return settings, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
	args := make([]any, 0, len(keys))
	for _, k := range keys {
		args = append(args, k)
	}
	rows, err := q.QueryContext(ctx, "SELECT key, value FROM settings WHERE key IN ("+placeholders+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		settings[k] = v
	}
	return settings, rows.Err()
}

// normalizeSettings 对原始设置 map 执行统一校验与归一化（Build6 §12.10）：
//
//   - 缺失或空白的非凭据默认键按「缺失」处理并使用固定默认值；
//   - 已有非空但非法的值不静默回退，而是返回带键名（不含值）的错误；
//   - webui_port 等已不是业务设置的残留键一律忽略，不做迁移或清理；
//   - 返回的 map 只包含 version 2 的完整设置键集合，未知键被丢弃。
func normalizeSettings(raw map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(settingsKeysV2))
	// 默认值（Build6 §3.1）
	out["tag"] = "auto-dns"
	out["interval"] = "5m"
	out["dns"] = "223.5.5.5"
	out["dns_timeout"] = "10s"
	out["dns_fail_threshold"] = "5"
	out["log_level"] = "info"
	out["theme"] = "light"
	out["sync_enabled"] = "true"
	// 四个云凭据允许缺失/空值，按原值保存
	out["tc_access_id"] = raw["tc_access_id"]
	out["tc_access_key"] = raw["tc_access_key"]
	out["ali_access_id"] = raw["ali_access_id"]
	out["ali_access_key"] = raw["ali_access_key"]

	if v := strings.TrimSpace(raw["tag"]); v != "" {
		normalized, err := NormalizeTag(v)
		if err != nil {
			return nil, err
		}
		out["tag"] = normalized
	}
	if v := strings.TrimSpace(raw["interval"]); v != "" {
		normalized, _, err := ParsePositiveDuration("interval", v)
		if err != nil {
			return nil, err
		}
		out["interval"] = normalized
	}
	if v := strings.TrimSpace(raw["dns"]); v != "" {
		normalized, err := NormalizeDNSAddress(v)
		if err != nil {
			return nil, err
		}
		out["dns"] = normalized
	}
	if v := strings.TrimSpace(raw["dns_timeout"]); v != "" {
		normalized, _, err := ParsePositiveDuration("dns_timeout", v)
		if err != nil {
			return nil, err
		}
		out["dns_timeout"] = normalized
	}
	if v := strings.TrimSpace(raw["dns_fail_threshold"]); v != "" {
		normalized, _, err := NormalizeDNSFailThreshold(v)
		if err != nil {
			return nil, err
		}
		out["dns_fail_threshold"] = normalized
	}
	if v := strings.TrimSpace(raw["log_level"]); v != "" {
		normalized, err := NormalizeLogLevel(v)
		if err != nil {
			return nil, err
		}
		out["log_level"] = normalized
	}
	if v := strings.TrimSpace(raw["theme"]); v != "" {
		normalized, err := NormalizeTheme(v)
		if err != nil {
			return nil, err
		}
		out["theme"] = normalized
	}
	if v := strings.TrimSpace(raw["sync_enabled"]); v != "" {
		enabled, err := NormalizeSyncEnabled(v)
		if err != nil {
			return nil, err
		}
		if enabled {
			out["sync_enabled"] = "true"
		} else {
			out["sync_enabled"] = "false"
		}
	}
	return out, nil
}

// writeSettingsTx 在事务内显式逐键写入给定设置（导入与候选写入共用）。
//
// 只写入调用方提供的键，不遍历数据库或请求中的任意 map。
func (s *Store) writeSettingsTx(ctx context.Context, tx *sql.Tx, settings map[string]string) error {
	for _, key := range settingsKeysV2 {
		value, ok := settings[key]
		if !ok {
			continue
		}
		if err := s.SetSettingTx(ctx, tx, key, value); err != nil {
			return err
		}
	}
	return nil
}

// ReplaceBusinessSettingsTx 在事务内显式写入 version 2 的完整设置键集合
// （导入使用：调用方必须提供全部 12 个键，缺失键视为调用方错误）。
func (s *Store) ReplaceBusinessSettingsTx(ctx context.Context, tx *sql.Tx, settings map[string]string) error {
	for _, key := range settingsKeysV2 {
		if _, ok := settings[key]; !ok {
			return fmt.Errorf("缺少设置键 %s", key)
		}
	}
	return s.writeSettingsTx(ctx, tx, settings)
}

// ReplaceBusinessAlertsTx 在事务内覆盖保存完整邮件与 Webhook 告警配置。
func (s *Store) ReplaceBusinessAlertsTx(ctx context.Context, tx *sql.Tx, email AlertEmailConfig, webhook AlertWebhookConfig) error {
	if err := s.SaveAlertEmailTx(ctx, tx, &email); err != nil {
		return err
	}
	return s.SaveAlertWebhookTx(ctx, tx, &webhook)
}

// ClearScannedResourcesTx 在事务内清空扫描缓存（配置导入的保留/清空边界）。
func (s *Store) ClearScannedResourcesTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM scanned_resources")
	return err
}

// DeleteImportOwnedTablesTx 按依赖顺序清空导入覆盖的业务表（Build6 §12.12 固定顺序）：
// rules → targets → settings → alert_email → alert_webhook。
//
// 即使当前 Schema 没有外键，也按依赖顺序实现；不触碰 sync_logs 与 sqlite_sequence。
func (s *Store) DeleteImportOwnedTablesTx(ctx context.Context, tx *sql.Tx) error {
	stmts := []string{
		"DELETE FROM rules",
		"DELETE FROM targets",
		"DELETE FROM settings",
		"DELETE FROM alert_email",
		"DELETE FROM alert_webhook",
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
