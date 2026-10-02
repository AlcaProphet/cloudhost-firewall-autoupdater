package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

// BeginTx 开启写事务（配置变更协调器与配置导入共用）。
// DSN 的 _txlock=immediate 在读取前预留写锁，避免 WAL 读快照过期后无法升级。
func (s *Store) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, nil)
}

// BeginReadOnlyTx 开启 SQLite 只读事务（version 3 导出使用，Build6 §12.7）。
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
	Result    string    `json:"result"` // success / failed / skipped / partial
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

// sqliteDSN 把数据库文件路径转换为带连接级 PRAGMA 的 SQLite file URI（Issue6 A4）。
//
// 背景：`PRAGMA busy_timeout` 是**连接级**设置，打开后执行一次只影响当时那一条
// 物理连接；database/sql 之后新建的连接 busy_timeout 为 0，竞争时立即 SQLITE_BUSY。
// 驱动支持 `_pragma=` 查询参数，并在**每条新连接**上执行，因此这里把它写进 DSN。
//
// 两个必须遵守的细节：
//  1. 只对 `#`、`?`、`%` 做百分号转义，并保留 `(`/`)` 字面量（SQLite 的 URI 解析器
//     不会把括号当分隔符，而它们对 `busy_timeout(5000)` 可读性重要）。若走
//     `net/url` 的查询编码会把括号编码成 %28/%29，不必要地降低可读性。
//  2. 必须先 `filepath.Abs`：相对路径会在 file URI 里变成 authority，
//     SQLite 报 `invalid uri authority`；同时 `file:` 前缀让驱动不再按 `?` 截断路径
//     （修复前 `/data/a?b.db` 会被静默打开成 `/data/a`）。
//
// 刻意**不**把 `journal_mode(WAL)` 放进 _pragma：WAL 按契约只在打开后设置一次，
// 每条连接重复切换没有必要。P3-09 使用独立参数 `_txlock=immediate` 预留写事务；
// 当前驱动对 ReadOnly=true 跳过该参数，导出与启动加载仍使用普通读事务。
func sqliteDSN(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("解析数据库绝对路径失败: %w", err)
	}

	var b strings.Builder
	b.WriteString("file:")
	// 转义为路径安全形式（Windows 反斜杠统一为斜杠，SQLite URI 要求）
	for _, r := range filepath.ToSlash(abs) {
		switch r {
		case '%':
			b.WriteString("%25")
		case '#':
			b.WriteString("%23")
		case '?':
			b.WriteString("%3F")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString("?_pragma=busy_timeout(5000)&_txlock=immediate")
	return b.String(), nil
}

// OpenStore 打开或创建 SQLite 数据库
func OpenStore(path string) (*Store, error) {
	dsn, err := sqliteDSN(path)
	if err != nil {
		return nil, err
	}

	// 在 SQLite 首次访问前保护主库；不截断已有配置，创建模式也不能代替权限迁移。
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("预创建数据库失败: %w", err)
	}
	permissionErr := f.Chmod(0600)
	if permissionErr != nil {
		permissionErr = fmt.Errorf("收敛数据库权限失败: %w", permissionErr)
	}
	closeErr := f.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("关闭数据库预创建文件失败: %w", closeErr)
	}
	if err := errors.Join(permissionErr, closeErr); err != nil {
		return nil, err
	}
	if err := chmodStoreAuxFiles(path); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	return openStoreDB(db, path)
}

// chmodStoreAuxFiles 迁移已存在的 WAL/SHM；缺失正常，不预创建或删除辅助文件。
// 固定 Unix VFS 从主库权限创建新辅助文件，重建行为由实际 SQLite 回归验证。
func chmodStoreAuxFiles(path string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Chmod(path+suffix, 0600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("收敛数据库辅助文件 %s 权限失败: %w", suffix, err)
		}
	}
	return nil
}

// openStoreDB 在初始化失败时关闭数据库，成功时将所有权交给 Store。
func openStoreDB(db *sql.DB, path string) (_ *Store, err error) {
	defer func() {
		if err != nil {
			if closeErr := db.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("初始化失败后关闭数据库失败: %w", closeErr))
			}
		}
	}()
	// WAL 模式：按契约只在打开后设置一次（每条连接重复切换没有必要）
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("设置 WAL 模式失败: %w", err)
	}

	s := &Store{db: db}
	if err := s.initTables(); err != nil {
		return nil, err
	}
	// 初始化可能刚创建辅助文件；权限失败仍走统一关闭，不能带不安全状态启动。
	if err := chmodStoreAuxFiles(path); err != nil {
		return nil, err
	}
	return s, nil
}

// Close 关闭数据库
func (s *Store) Close() error {
	return s.db.Close()
}

// schemaSQL 是 Build7 目标 Schema（Build7 §4.2）。
//
// 四张单行表（alert_email / alert_webhook / alert_policy / uptime_kuma_push）的业务
// ID 固定为 1，默认行由 migrateSchemaTx 用 INSERT OR IGNORE 保证存在；新建库的
// 告警启用状态因此天然为「全部关闭」。
const schemaSQL = `
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
	enabled INTEGER NOT NULL DEFAULT 0,
	host TEXT NOT NULL DEFAULT '',
	port TEXT NOT NULL DEFAULT '587',
	username TEXT NOT NULL DEFAULT '',
	password TEXT NOT NULL DEFAULT '',
	from_addr TEXT NOT NULL DEFAULT '',
	to_addr TEXT NOT NULL DEFAULT '',
	subject TEXT NOT NULL DEFAULT '` + DefaultEmailSubject + `',
	body TEXT NOT NULL DEFAULT '` + DefaultEmailBody + `'
);
CREATE TABLE IF NOT EXISTS alert_webhook (
	id INTEGER PRIMARY KEY DEFAULT 1,
	enabled INTEGER NOT NULL DEFAULT 0,
	url TEXT NOT NULL DEFAULT '',
	channel TEXT NOT NULL DEFAULT 'dingtalk'
);
CREATE TABLE IF NOT EXISTS alert_policy (
	id INTEGER PRIMARY KEY DEFAULT 1,
	dns_failed_enabled INTEGER NOT NULL DEFAULT 0,
	sync_error_enabled INTEGER NOT NULL DEFAULT 0,
	operational_error_enabled INTEGER NOT NULL DEFAULT 0,
	health_timeout TEXT NOT NULL DEFAULT '10m'
);
CREATE TABLE IF NOT EXISTS uptime_kuma_push (
	id INTEGER PRIMARY KEY DEFAULT 1,
	enabled INTEGER NOT NULL DEFAULT 0,
	url TEXT NOT NULL DEFAULT '',
	interval TEXT NOT NULL DEFAULT '60s'
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

// singleRowTables 是四张单行表：业务 ID 固定为 1，至多一行。
var singleRowTables = []string{"alert_email", "alert_webhook", "alert_policy", "uptime_kuma_push"}

// initTables 在**单个事务**内建立/迁移 Schema（Build7 §4.2）。
//
// 事务性很关键：`ALTER TABLE` 中途失败不能留下「补了一半列」的库，否则
// 「新增 subject/body 列」这个一次性迁移信号会在重启时失效。
func (s *Store) initTables() error {
	ctx := context.Background()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开始 Schema 迁移事务失败: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			slog.Error("回滚 Schema 迁移事务失败", "error", rbErr)
		}
	}()

	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("初始化表结构失败: %w", err)
	}
	if err := migrateSchemaTx(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交 Schema 迁移事务失败: %w", err)
	}
	committed = true
	return nil
}

// migrateSchemaTx 执行显式 Schema 迁移（Build7 §4.2），不依赖
// `CREATE TABLE IF NOT EXISTS` 自动补列：
//
//  1. 为老库补列（rules.enable_ipv6、alert_webhook.channel、alert_email.subject/body）；
//  2. **只有** alert_email 的主题/正文列是本次新增时，才把邮件与 Webhook 的启用状态
//     统一归零。这是「一次性」语义：列已存在说明该库已经迁移过（或本来就是新库），
//     绝不能在每次启动重复重置用户配置；
//  3. 单行表归一化为「至多一行且业务 ID=1」，并保证默认行存在；
//     alert_policy / uptime_kuma_push 是新表，默认行即全部关闭 + 10m/60s。
//
// 任何一步失败都返回错误并中止启动（Issue6 A17 口径），绝不带着不完整 Schema 运行。
func migrateSchemaTx(ctx context.Context, tx *sql.Tx) error {
	if _, err := ensureColumnTx(ctx, tx, "rules", "enable_ipv6",
		"ALTER TABLE rules ADD COLUMN enable_ipv6 INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if _, err := ensureColumnTx(ctx, tx, "alert_webhook", "channel",
		"ALTER TABLE alert_webhook ADD COLUMN channel TEXT DEFAULT '"+DefaultWebhookChannel+"'"); err != nil {
		return err
	}

	subjectAdded, err := ensureColumnTx(ctx, tx, "alert_email", "subject",
		"ALTER TABLE alert_email ADD COLUMN subject TEXT NOT NULL DEFAULT '"+DefaultEmailSubject+"'")
	if err != nil {
		return err
	}
	bodyAdded, err := ensureColumnTx(ctx, tx, "alert_email", "body",
		"ALTER TABLE alert_email ADD COLUMN body TEXT NOT NULL DEFAULT '"+DefaultEmailBody+"'")
	if err != nil {
		return err
	}

	if subjectAdded || bodyAdded {
		if _, err := tx.ExecContext(ctx, "UPDATE alert_email SET enabled = 0"); err != nil {
			return fmt.Errorf("迁移归零 alert_email 失败: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE alert_webhook SET enabled = 0"); err != nil {
			return fmt.Errorf("迁移归零 alert_webhook 失败: %w", err)
		}
	}

	for _, table := range singleRowTables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE id <> 1"); err != nil {
			return fmt.Errorf("归一化单行表 %s 失败: %w", table, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO "+table+" (id) VALUES (1)"); err != nil {
			return fmt.Errorf("写入 %s 默认行失败: %w", table, err)
		}
	}
	return nil
}

// ensureColumnTx 在事务内检查列是否存在，缺失时执行给定的 ALTER TABLE。
//
// 返回 true 表示本次真的新增了该列，供「一次性迁移」判定使用；用 PRAGMA 探测而不是
// 匹配 "duplicate column" 文本，避免把其他错误误判为已迁移。
func ensureColumnTx(ctx context.Context, tx *sql.Tx, table, column, alter string) (bool, error) {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false, fmt.Errorf("读取 %s 列信息失败: %w", table, err)
	}
	found := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			if cerr := rows.Close(); cerr != nil {
				slog.Warn("关闭列信息游标失败", "table", table, "error", cerr)
			}
			return false, fmt.Errorf("扫描 %s 列信息失败: %w", table, err)
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		if cerr := rows.Close(); cerr != nil {
			slog.Warn("关闭列信息游标失败", "table", table, "error", cerr)
		}
		return false, fmt.Errorf("遍历 %s 列信息失败: %w", table, err)
	}
	if err := rows.Close(); err != nil {
		return false, fmt.Errorf("关闭 %s 列信息失败: %w", table, err)
	}
	if found {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, alter); err != nil {
		return false, fmt.Errorf("迁移列 %s.%s 失败: %w", table, column, err)
	}
	return true, nil
}

// PingContext 执行一次有界的 SQLite 探活（Build7 §7.2 的运行健康检查入口）。
//
// 使用 SELECT 1 而不是 database/sql 的 Ping：后者在某些驱动下只验证连接对象，
// 不会真正打到数据库。调用方负责用 context 限制总时长（健康检查固定 2 秒）。
func (s *Store) PingContext(ctx context.Context) error {
	var one int
	if err := s.db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("SQLite 探活失败: %w", err)
	}
	if one != 1 {
		return fmt.Errorf("SQLite 探活返回异常结果")
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
//
// rules.targets 采用**四态严格口径**（Issue6 A9，2026-09-27 用户裁决 F1）：
//   - 历史空串 ""   → 兼容为「适用于全部目标」（空 Targets 的既有语义）
//   - "[]"          → 合法的「全部目标」（json 解出非 nil 空切片）
//   - "null"        → **内部错误**（json 解出 nil 切片，与 "[]" 可区分）；
//     历史（c289744 之前）写路径用 json.Marshal(nil slice) 会写出字面量 null，
//     因此存量库可能包含该值：升级后 fail-closed 是**预期行为**，
//     发布说明给出修复方式（SQL 或重新导入 version 3 配置包）。
//   - 对象/标量/非整数数组/解析失败/物理 NULL → 内部错误
//
// 错误文本只带规则 `#id`，绝不回显原始损坏值。
func loadRules(ctx context.Context, q DBTX) ([]DomainRule, error) {
	rows, err := q.QueryContext(ctx, "SELECT id, host, protocol, ports, action, targets, comment, enable_ipv6 FROM rules ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := make([]DomainRule, 0)
	for rows.Next() {
		var r DomainRule
		// 用 sql.NullString 覆盖物理 SQL NULL：直接 Scan 进 string 会报
		// `unsupported Scan … into type *string`，且错误里不含规则 ID
		var targets sql.NullString
		var enableIPv6 int
		if err := rows.Scan(&r.ID, &r.Host, &r.Protocol, &r.Ports, &r.Action, &targets, &r.Comment, &enableIPv6); err != nil {
			return nil, err
		}
		parsed, err := parseRuleTargets(r.ID, targets)
		if err != nil {
			return nil, err
		}
		r.Targets = parsed
		r.EnableIPv6 = enableIPv6 != 0
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// parseRuleTargets 严格解析单条规则的 targets 列（见 loadRules 的四态口径）。
func parseRuleTargets(ruleID int, raw sql.NullString) ([]int, error) {
	// 物理 NULL 视为损坏：与 "" 不同，它不是历史兼容形态
	if !raw.Valid {
		return nil, fmt.Errorf("规则 #%d 的 targets 为 SQL NULL，数据已损坏", ruleID)
	}

	text := raw.String
	// 历史空串兼容为「全部目标」（空 Targets 的既有语义）
	if text == "" {
		return nil, nil
	}

	var nums []int
	if err := json.Unmarshal([]byte(text), &nums); err != nil {
		// 不回显原值：只给规则 ID 与损坏事实
		return nil, fmt.Errorf("规则 #%d 的 targets 不是合法的整数数组（数据已损坏）", ruleID)
	}
	if nums == nil {
		// JSON null：json 解出 nil 切片，而 [] 解出非 nil 空切片，两者可区分。
		// null 不得视为 []，否则损坏会被静默扩大为「适用于全部目标」。
		return nil, fmt.Errorf("规则 #%d 的 targets 为 JSON null（数据已损坏，不得视为全部目标）", ruleID)
	}
	return nums, nil
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

// resetAllSQL 清空全部业务表的语句（ResetAllTx 使用）
// resetAllSQL 清空全部业务表，并重新写入四张单行表的默认行：
// 邮件、Webhook、三个触发开关与 Push 全部回到关闭，health_timeout 回 10m、
// Push interval 回 60s（Build7 §2.1、§4.2）。
const resetAllSQL = "DELETE FROM targets; DELETE FROM rules; DELETE FROM settings; DELETE FROM sync_logs;" +
	"DELETE FROM alert_email; DELETE FROM alert_webhook; DELETE FROM alert_policy; DELETE FROM uptime_kuma_push;" +
	"DELETE FROM scanned_resources;" +
	"INSERT OR IGNORE INTO alert_email (id) VALUES (1);" +
	"INSERT OR IGNORE INTO alert_webhook (id) VALUES (1);" +
	"INSERT OR IGNORE INTO alert_policy (id) VALUES (1);" +
	"INSERT OR IGNORE INTO uptime_kuma_push (id) VALUES (1);"

// ResetAllTx 在事务中清空全部业务数据（「清空所有数据」经协调器调用）
func (s *Store) ResetAllTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, resetAllSQL)
	return err
}

// WithTransaction 在事务中执行操作，失败自动回滚。
//
// Issue6 A14：修复前忽略 Rollback 返回值、且没有 defer——fn panic 时事务与连接
// 不会被回滚（*sql.Tx 没有 finalizer，WAL 下泄漏的写事务会让后续写入持续 BUSY）。
// 现在用 committed 标志 + defer 回滚：正常提交后不再回滚；已结束事务的
// sql.ErrTxDone 属预期，忽略；其他回滚失败记安全错误日志（不回显业务数据）。
func (s *Store) WithTransaction(fn func(tx *sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			slog.Error("回滚事务失败", "error", rbErr)
		}
	}()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
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
		"SELECT enabled, host, port, username, password, from_addr, to_addr, subject, body FROM alert_email WHERE id = 1").
		Scan(&enabled, &cfg.Host, &cfg.Port, &cfg.Username, &cfg.Password, &cfg.FromAddr, &cfg.ToAddr,
			&cfg.Subject, &cfg.Body)
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
const saveAlertEmailSQL = `INSERT OR REPLACE INTO alert_email (id, enabled, host, port, username, password, from_addr, to_addr, subject, body)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// alertEmailArgs 把邮件告警配置转换为 SQL 参数
func alertEmailArgs(cfg *AlertEmailConfig) []any {
	enabled := 0
	if cfg.Enabled {
		enabled = 1
	}
	return []any{enabled, cfg.Host, cfg.Port, cfg.Username, cfg.Password, cfg.FromAddr, cfg.ToAddr, cfg.Subject, cfg.Body}
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

// boolToInt 把布尔值转换为 SQLite 的 0/1。
func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// GetAlertPolicy 获取告警触发策略（GET /api/alerts 与测试使用）。
func (s *Store) GetAlertPolicy() (*AlertPolicyConfig, error) {
	cfg, err := loadAlertPolicy(context.Background(), s.db)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// loadAlertPolicy 读取告警触发策略（事务内可复用；无行或空时长使用固定默认值，
// 非空但非法的存量值返回错误而不静默回退，与设置键的既有口径一致）。
func loadAlertPolicy(ctx context.Context, q DBTX) (AlertPolicyConfig, error) {
	var dnsFailed, syncError, operationalError int
	var healthTimeout string
	err := q.QueryRowContext(ctx,
		"SELECT dns_failed_enabled, sync_error_enabled, operational_error_enabled, health_timeout FROM alert_policy WHERE id = 1").
		Scan(&dnsFailed, &syncError, &operationalError, &healthTimeout)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultAlertPolicy(), nil
	}
	if err != nil {
		return AlertPolicyConfig{}, err
	}

	cfg := AlertPolicyConfig{
		DNSFailedEnabled:        dnsFailed != 0,
		SyncErrorEnabled:        syncError != 0,
		OperationalErrorEnabled: operationalError != 0,
	}
	if strings.TrimSpace(healthTimeout) == "" {
		cfg.HealthTimeoutText = "10m"
		cfg.HealthTimeout = DefaultHealthTimeout
		return cfg, nil
	}
	text, d, err := ParsePositiveDuration("policy.health_timeout", healthTimeout)
	if err != nil {
		return AlertPolicyConfig{}, err
	}
	cfg.HealthTimeoutText = text
	cfg.HealthTimeout = d
	return cfg, nil
}

// saveAlertPolicySQL 保存告警策略的语句（单条写入与事务内写入共用）
const saveAlertPolicySQL = `INSERT OR REPLACE INTO alert_policy
	(id, dns_failed_enabled, sync_error_enabled, operational_error_enabled, health_timeout)
	VALUES (1, ?, ?, ?, ?)`

// alertPolicyArgs 把告警策略转换为 SQL 参数（时长按已校验文本持久化）
func alertPolicyArgs(cfg *AlertPolicyConfig) []any {
	return []any{
		boolToInt(cfg.DNSFailedEnabled),
		boolToInt(cfg.SyncErrorEnabled),
		boolToInt(cfg.OperationalErrorEnabled),
		cfg.HealthTimeoutText,
	}
}

// SaveAlertPolicyTx 在事务中保存告警触发策略（PUT /api/alerts 与配置导入共用）
func (s *Store) SaveAlertPolicyTx(ctx context.Context, tx *sql.Tx, cfg *AlertPolicyConfig) error {
	_, err := tx.ExecContext(ctx, saveAlertPolicySQL, alertPolicyArgs(cfg)...)
	return err
}

// GetUptimeKumaPush 获取 Uptime Kuma Push 配置（GET /api/alerts 与测试使用）。
func (s *Store) GetUptimeKumaPush() (*UptimeKumaPushConfig, error) {
	cfg, err := loadUptimeKumaPush(context.Background(), s.db)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// loadUptimeKumaPush 读取 Push 配置（事务内可复用；无行或空间隔使用固定默认值）。
func loadUptimeKumaPush(ctx context.Context, q DBTX) (UptimeKumaPushConfig, error) {
	var enabled int
	var url, interval string
	err := q.QueryRowContext(ctx,
		"SELECT enabled, url, interval FROM uptime_kuma_push WHERE id = 1").
		Scan(&enabled, &url, &interval)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultUptimeKumaPush(), nil
	}
	if err != nil {
		return UptimeKumaPushConfig{}, err
	}

	cfg := UptimeKumaPushConfig{Enabled: enabled != 0, URL: url}
	if strings.TrimSpace(interval) == "" {
		cfg.IntervalText = "60s"
		cfg.Interval = DefaultPushInterval
		return cfg, nil
	}
	text, d, err := ParsePositiveDuration("uptime_kuma_push.interval", interval)
	if err != nil {
		return UptimeKumaPushConfig{}, err
	}
	cfg.IntervalText = text
	cfg.Interval = d
	return cfg, nil
}

// saveUptimeKumaPushSQL 保存 Push 配置的语句（单条写入与事务内写入共用）
const saveUptimeKumaPushSQL = `INSERT OR REPLACE INTO uptime_kuma_push (id, enabled, url, interval) VALUES (1, ?, ?, ?)`

// uptimeKumaPushArgs 把 Push 配置转换为 SQL 参数（间隔按已校验文本持久化）
func uptimeKumaPushArgs(cfg *UptimeKumaPushConfig) []any {
	return []any{boolToInt(cfg.Enabled), cfg.URL, cfg.IntervalText}
}

// SaveUptimeKumaPushTx 在事务中保存 Push 配置（PUT /api/alerts 与配置导入共用）
func (s *Store) SaveUptimeKumaPushTx(ctx context.Context, tx *sql.Tx, cfg *UptimeKumaPushConfig) error {
	_, err := tx.ExecContext(ctx, saveUptimeKumaPushSQL, uptimeKumaPushArgs(cfg)...)
	return err
}

// GetSettingsTx 在事务内按固定键集合读取设置（不返回数据库中的未知键）。
func (s *Store) GetSettingsTx(ctx context.Context, q DBTX) (map[string]string, error) {
	return loadSettingsByKeys(ctx, q, settingsKeysV3)
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
	//
	// Issue6 A14：COUNT 查询失败也必须处理。此处保留「查询失败就不裁剪」的保守
	// 语义（不改变裁剪阈值与行为），但记录安全错误日志，不再静默吞掉。
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM sync_logs").Scan(&count); err != nil {
		slog.Warn("统计同步日志条数失败，跳过本次裁剪", "error", err)
		return nil
	}
	if count > 1000 {
		if _, err := s.db.Exec("DELETE FROM sync_logs WHERE id NOT IN (SELECT id FROM sync_logs ORDER BY id DESC LIMIT 1000)"); err != nil {
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
	policy, err := loadAlertPolicy(ctx, q)
	if err != nil {
		return nil, err
	}
	push, err := loadUptimeKumaPush(ctx, q)
	if err != nil {
		return nil, err
	}

	normalized, err := normalizeSettings(settings)
	if err != nil {
		return nil, err
	}

	return &BusinessSnapshot{
		Targets:        targets,
		Rules:          rules,
		Settings:       normalized,
		Policy:         policy,
		Email:          email,
		Webhook:        webhook,
		UptimeKumaPush: push,
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
//   - 返回的 map 只包含 version 3 的完整设置键集合，未知键被丢弃。
func normalizeSettings(raw map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(settingsKeysV3))
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
	for _, key := range settingsKeysV3 {
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

// ReplaceBusinessSettingsTx 在事务内显式写入 version 3 的完整设置键集合
// （导入使用：调用方必须提供全部 12 个键，缺失键视为调用方错误）。
func (s *Store) ReplaceBusinessSettingsTx(ctx context.Context, tx *sql.Tx, settings map[string]string) error {
	for _, key := range settingsKeysV3 {
		if _, ok := settings[key]; !ok {
			return fmt.Errorf("缺少设置键 %s", key)
		}
	}
	return s.writeSettingsTx(ctx, tx, settings)
}

// ReplaceBusinessAlertsTx 在事务内覆盖保存完整的告警配置：
// 触发策略、邮件（含主题/正文）、Webhook 与 Uptime Kuma Push（Build7 §4.3）。
func (s *Store) ReplaceBusinessAlertsTx(
	ctx context.Context,
	tx *sql.Tx,
	policy AlertPolicyConfig,
	email AlertEmailConfig,
	webhook AlertWebhookConfig,
	push UptimeKumaPushConfig,
) error {
	if err := s.SaveAlertPolicyTx(ctx, tx, &policy); err != nil {
		return err
	}
	if err := s.SaveAlertEmailTx(ctx, tx, &email); err != nil {
		return err
	}
	if err := s.SaveAlertWebhookTx(ctx, tx, &webhook); err != nil {
		return err
	}
	return s.SaveUptimeKumaPushTx(ctx, tx, &push)
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
		"DELETE FROM alert_policy",
		"DELETE FROM uptime_kuma_push",
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
