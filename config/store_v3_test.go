package config

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// 本文件是 Build7 Step 1 的判别性测试：目标 Schema（alert_policy / uptime_kuma_push /
// alert_email.subject|body）、一次性显式迁移、单行归一化与「迁移只归零一次」。
//
// 首轮刻意只使用 OpenStore + 原始 SQL 断言：即使新领域 API 尚未实现，测试也必须
// 能够编译，并在迁移/建表缺失时因**目标原因**失败（no such table / no such column）。

// legacyV2Schema 是 Build6 末期的 alert 相关表形态（冻结副本，用于构造真实旧库）。
//
// 刻意不引用生产 DDL：迁移测试必须针对「历史库长什么样」，而不是针对当前 DDL。
const legacyV2Schema = `
CREATE TABLE targets (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	cloud_type TEXT NOT NULL,
	region TEXT NOT NULL,
	resource_id TEXT NOT NULL
);
CREATE TABLE rules (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	host TEXT NOT NULL,
	protocol TEXT NOT NULL,
	ports TEXT NOT NULL,
	action TEXT NOT NULL DEFAULT 'ACCEPT',
	targets TEXT DEFAULT '',
	comment TEXT DEFAULT '',
	enable_ipv6 INTEGER DEFAULT 0
);
CREATE TABLE settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE sync_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
	target TEXT,
	domain TEXT,
	result TEXT,
	added INTEGER DEFAULT 0,
	deleted INTEGER DEFAULT 0,
	error TEXT DEFAULT ''
);
CREATE TABLE alert_email (
	id INTEGER PRIMARY KEY DEFAULT 1,
	enabled INTEGER DEFAULT 0,
	host TEXT DEFAULT '',
	port TEXT DEFAULT '587',
	username TEXT DEFAULT '',
	password TEXT DEFAULT '',
	from_addr TEXT DEFAULT '',
	to_addr TEXT DEFAULT ''
);
CREATE TABLE alert_webhook (
	id INTEGER PRIMARY KEY DEFAULT 1,
	enabled INTEGER DEFAULT 0,
	url TEXT DEFAULT '',
	channel TEXT DEFAULT 'dingtalk'
);
`

// newLegacyStore 构造一个 Build6 形态的旧库（含启用中的告警、凭据与业务数据），返回数据库路径。
func newLegacyStore(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开旧库失败: %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("关闭旧库失败: %v", cerr)
		}
	}()

	if _, err := db.Exec(legacyV2Schema); err != nil {
		t.Fatalf("建立旧库 Schema 失败: %v", err)
	}

	stmts := []string{
		// 启用中的邮件告警（含凭据）——迁移后必须 enabled=0 但凭据保留
		`INSERT INTO alert_email (id, enabled, host, port, username, password, from_addr, to_addr)
		 VALUES (1, 1, 'smtp.legacy', '2525', 'legacy-user', 'legacy-pass', 'from@legacy', 'to@legacy')`,
		// 越界的第二行：单行表必须被归一化为「至多一行且 id=1」
		`INSERT INTO alert_email (id, enabled, host) VALUES (2, 1, 'stray')`,
		`INSERT INTO alert_webhook (id, enabled, url, channel)
		 VALUES (1, 1, 'https://legacy.invalid/hook', 'feishu')`,
		`INSERT INTO alert_webhook (id, enabled, url) VALUES (2, 1, 'https://stray.invalid/hook')`,
		// 业务数据：迁移不得删除目标/规则/设置/同步日志
		`INSERT INTO targets (cloud_type, region, resource_id) VALUES ('tc_lighthouse', 'ap-guangzhou', 'lhins-legacy')`,
		`INSERT INTO rules (host, protocol, ports, action, targets, comment, enable_ipv6)
		 VALUES ('legacy.example.com', 'TCP', '443', 'ACCEPT', '[]', 'legacy', 0)`,
		`INSERT INTO settings (key, value) VALUES ('tag', 'legacy-tag')`,
		`INSERT INTO settings (key, value) VALUES ('tc_access_id', 'legacy-akid')`,
		`INSERT INTO sync_logs (target, domain, result, added, deleted) VALUES ('lhins-legacy', 'legacy.example.com', 'success', 1, 0)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("预置旧库数据失败: %v; stmt=%s", err, stmt)
		}
	}
	return path
}

// openRaw 打开一个独立的只读/写入连接（测试夹具）。
func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开夹具连接失败: %v", err)
	}
	t.Cleanup(func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("关闭夹具连接失败: %v", cerr)
		}
	})
	return db
}

// tableColumns 返回表的列名集合。
func tableColumns(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("读取 %s 列信息失败: %v", table, err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			t.Errorf("关闭 rows 失败: %v", cerr)
		}
	}()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("扫描 %s 列信息失败: %v", table, err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历 %s 列信息失败: %v", table, err)
	}
	return cols
}

// tableExists 判断表是否存在。
func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("查询表 %s 失败: %v", table, err)
	}
	return true
}

// countRows 统计表的行数。
func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("统计 %s 行数失败: %v", table, err)
	}
	return n
}

// scalarString 读取单个字符串列。
func scalarString(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	var v string
	if err := db.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatalf("查询失败: %v; query=%s", err, query)
	}
	return v
}

// scalarInt 读取单个整数列。
func scalarInt(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var v int
	if err := db.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatalf("查询失败: %v; query=%s", err, query)
	}
	return v
}

// TestSchemaV3FreshDatabaseShape 新库必须直接具备 Build7 目标 Schema 与「默认全部关闭」。
func TestSchemaV3FreshDatabaseShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("打开新库失败: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("关闭新库失败: %v", err)
	}

	db := openRaw(t, path)

	for _, table := range []string{"alert_policy", "uptime_kuma_push"} {
		if !tableExists(t, db, table) {
			t.Fatalf("新库缺少表 %s（Build7 §4.2 目标 Schema）", table)
		}
	}

	cols := tableColumns(t, db, "alert_email")
	for _, col := range []string{"subject", "body"} {
		if !cols[col] {
			t.Errorf("alert_email 缺少列 %s（Build7 §4.2 目标 Schema）", col)
		}
	}

	// 单行表：恰好一行且业务 ID 固定为 1
	for _, table := range []string{"alert_email", "alert_webhook", "alert_policy", "uptime_kuma_push"} {
		if n := countRows(t, db, table); n != 1 {
			t.Errorf("%s 行数 = %d, want 1（单行表）", table, n)
		}
		if id := scalarInt(t, db, "SELECT id FROM "+table+" WHERE id=1"); id != 1 {
			t.Errorf("%s 缺少 id=1 的默认行", table)
		}
	}

	// 默认全部关闭
	if v := scalarInt(t, db, "SELECT enabled FROM alert_email WHERE id=1"); v != 0 {
		t.Errorf("alert_email.enabled = %d, want 0", v)
	}
	if v := scalarInt(t, db, "SELECT enabled FROM alert_webhook WHERE id=1"); v != 0 {
		t.Errorf("alert_webhook.enabled = %d, want 0", v)
	}
	if v := scalarInt(t, db, "SELECT enabled FROM uptime_kuma_push WHERE id=1"); v != 0 {
		t.Errorf("uptime_kuma_push.enabled = %d, want 0", v)
	}
	if v := scalarInt(t, db, "SELECT dns_failed_enabled FROM alert_policy WHERE id=1"); v != 0 {
		t.Errorf("alert_policy.dns_failed_enabled = %d, want 0", v)
	}
	if v := scalarInt(t, db, "SELECT sync_error_enabled FROM alert_policy WHERE id=1"); v != 0 {
		t.Errorf("alert_policy.sync_error_enabled = %d, want 0", v)
	}
	if v := scalarInt(t, db, "SELECT operational_error_enabled FROM alert_policy WHERE id=1"); v != 0 {
		t.Errorf("alert_policy.operational_error_enabled = %d, want 0", v)
	}

	// 默认值
	if v := scalarString(t, db, "SELECT health_timeout FROM alert_policy WHERE id=1"); v != "10m" {
		t.Errorf("alert_policy.health_timeout = %q, want 10m", v)
	}
	if v := scalarString(t, db, "SELECT interval FROM uptime_kuma_push WHERE id=1"); v != "60s" {
		t.Errorf("uptime_kuma_push.interval = %q, want 60s", v)
	}
	if v := scalarString(t, db, "SELECT port FROM alert_email WHERE id=1"); v != "587" {
		t.Errorf("alert_email.port = %q, want 587", v)
	}
	if v := scalarString(t, db, "SELECT subject FROM alert_email WHERE id=1"); v != "[FWAlizer] 告警通知" {
		t.Errorf("alert_email.subject = %q, want 默认主题", v)
	}
	if v := scalarString(t, db, "SELECT body FROM alert_email WHERE id=1"); v != "FWAlizer 检测到运行异常，请检查同步日志。" {
		t.Errorf("alert_email.body = %q, want 默认正文", v)
	}
	if v := scalarString(t, db, "SELECT channel FROM alert_webhook WHERE id=1"); v != "" {
		t.Errorf("alert_webhook.channel = %q, want 未选择", v)
	}
}

// TestMigrationFromLegacySchemaResetsAlertState 旧库必须被显式迁移：
// 补列、建新表、单行归一化、告警启用状态归零，但凭据与业务数据保留。
func TestMigrationFromLegacySchemaResetsAlertState(t *testing.T) {
	path := newLegacyStore(t)

	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("打开旧库失败（迁移必须成功）: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("关闭旧库失败: %v", err)
	}

	db := openRaw(t, path)

	cols := tableColumns(t, db, "alert_email")
	for _, col := range []string{"subject", "body"} {
		if !cols[col] {
			t.Fatalf("迁移后 alert_email 仍缺少列 %s", col)
		}
	}
	for _, table := range []string{"alert_policy", "uptime_kuma_push"} {
		if !tableExists(t, db, table) {
			t.Fatalf("迁移后仍缺少表 %s", table)
		}
	}

	// 单行归一化：越界行被删除，只剩 id=1
	for _, table := range []string{"alert_email", "alert_webhook", "alert_policy", "uptime_kuma_push"} {
		if n := countRows(t, db, table); n != 1 {
			t.Errorf("迁移后 %s 行数 = %d, want 1", table, n)
		}
		if n := scalarInt(t, db, "SELECT COUNT(*) FROM "+table+" WHERE id<>1"); n != 0 {
			t.Errorf("迁移后 %s 仍存在 id<>1 的行", table)
		}
	}

	// 启用状态统一归零（邮件、Webhook、三触发、Push）
	if v := scalarInt(t, db, "SELECT enabled FROM alert_email WHERE id=1"); v != 0 {
		t.Errorf("迁移后 alert_email.enabled = %d, want 0", v)
	}
	if v := scalarInt(t, db, "SELECT enabled FROM alert_webhook WHERE id=1"); v != 0 {
		t.Errorf("迁移后 alert_webhook.enabled = %d, want 0", v)
	}
	if v := scalarInt(t, db, "SELECT enabled FROM uptime_kuma_push WHERE id=1"); v != 0 {
		t.Errorf("迁移后 uptime_kuma_push.enabled = %d, want 0", v)
	}
	for _, col := range []string{"dns_failed_enabled", "sync_error_enabled", "operational_error_enabled"} {
		if v := scalarInt(t, db, "SELECT "+col+" FROM alert_policy WHERE id=1"); v != 0 {
			t.Errorf("迁移后 alert_policy.%s = %d, want 0", col, v)
		}
	}

	// 凭据与渠道配置必须保留（只归零启用状态，不删数据）
	if v := scalarString(t, db, "SELECT host FROM alert_email WHERE id=1"); v != "smtp.legacy" {
		t.Errorf("迁移后 alert_email.host = %q, want smtp.legacy", v)
	}
	if v := scalarString(t, db, "SELECT port FROM alert_email WHERE id=1"); v != "2525" {
		t.Errorf("迁移后 alert_email.port = %q, want 2525", v)
	}
	if v := scalarString(t, db, "SELECT password FROM alert_email WHERE id=1"); v != "legacy-pass" {
		t.Errorf("迁移后 alert_email.password = %q, want legacy-pass", v)
	}
	if v := scalarString(t, db, "SELECT url FROM alert_webhook WHERE id=1"); v != "https://legacy.invalid/hook" {
		t.Errorf("迁移后 alert_webhook.url = %q, want 旧值", v)
	}
	if v := scalarString(t, db, "SELECT channel FROM alert_webhook WHERE id=1"); v != "feishu" {
		t.Errorf("迁移后 alert_webhook.channel = %q, want feishu", v)
	}

	// 既有业务数据保留
	if n := countRows(t, db, "targets"); n != 1 {
		t.Errorf("迁移后 targets 行数 = %d, want 1", n)
	}
	if n := countRows(t, db, "rules"); n != 1 {
		t.Errorf("迁移后 rules 行数 = %d, want 1", n)
	}
	if n := countRows(t, db, "sync_logs"); n != 1 {
		t.Errorf("迁移后 sync_logs 行数 = %d, want 1", n)
	}
	if v := scalarString(t, db, "SELECT value FROM settings WHERE key='tag'"); v != "legacy-tag" {
		t.Errorf("迁移后 settings.tag = %q, want legacy-tag", v)
	}
	if v := scalarString(t, db, "SELECT value FROM settings WHERE key='tc_access_id'"); v != "legacy-akid" {
		t.Errorf("迁移后 settings.tc_access_id = %q, want legacy-akid", v)
	}
}

// TestMigrationResetsOnlyOnce 迁移只能归零一次：用户随后写入的启用状态、
// 主题正文与策略值在重启后必须保持，不得被再次重置。
func TestMigrationResetsOnlyOnce(t *testing.T) {
	path := newLegacyStore(t)

	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("首次迁移失败: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	// 模拟用户迁移后的配置写入
	db := openRaw(t, path)
	stmts := []string{
		`UPDATE alert_email SET enabled=1, subject='自定义主题', body='自定义正文' WHERE id=1`,
		`UPDATE alert_webhook SET enabled=1 WHERE id=1`,
		`UPDATE alert_policy SET dns_failed_enabled=1, health_timeout='25m' WHERE id=1`,
		`UPDATE uptime_kuma_push SET enabled=1, url='https://kuma.invalid/push/token1', interval='45s' WHERE id=1`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("写入迁移后配置失败: %v; stmt=%s", err, stmt)
		}
	}

	// 重启（再次 OpenStore）不得重置用户配置
	store2, err := OpenStore(path)
	if err != nil {
		t.Fatalf("重启打开失败: %v", err)
	}
	if err := store2.Close(); err != nil {
		t.Fatalf("重启关闭失败: %v", err)
	}

	if v := scalarInt(t, db, "SELECT enabled FROM alert_email WHERE id=1"); v != 1 {
		t.Errorf("重启后 alert_email.enabled = %d, want 1（迁移不得重复归零）", v)
	}
	if v := scalarString(t, db, "SELECT subject FROM alert_email WHERE id=1"); v != "自定义主题" {
		t.Errorf("重启后 subject = %q, want 自定义主题", v)
	}
	if v := scalarInt(t, db, "SELECT enabled FROM alert_webhook WHERE id=1"); v != 1 {
		t.Errorf("重启后 alert_webhook.enabled = %d, want 1", v)
	}
	if v := scalarInt(t, db, "SELECT dns_failed_enabled FROM alert_policy WHERE id=1"); v != 1 {
		t.Errorf("重启后 dns_failed_enabled = %d, want 1", v)
	}
	if v := scalarString(t, db, "SELECT health_timeout FROM alert_policy WHERE id=1"); v != "25m" {
		t.Errorf("重启后 health_timeout = %q, want 25m", v)
	}
	if v := scalarInt(t, db, "SELECT enabled FROM uptime_kuma_push WHERE id=1"); v != 1 {
		t.Errorf("重启后 uptime_kuma_push.enabled = %d, want 1", v)
	}
	if v := scalarString(t, db, "SELECT interval FROM uptime_kuma_push WHERE id=1"); v != "45s" {
		t.Errorf("重启后 uptime_kuma_push.interval = %q, want 45s", v)
	}
}

// TestV3DomainDefaultsAndLimits 固定默认值与边界（Build7 §4.1、§4.2、§4.5）。
func TestV3DomainDefaultsAndLimits(t *testing.T) {
	if DefaultHealthTimeout != 10*time.Minute || DefaultPushInterval != 60*time.Second || MinPushInterval != 20*time.Second {
		t.Errorf("时长默认值/下限错误: health=%v push=%v min=%v", DefaultHealthTimeout, DefaultPushInterval, MinPushInterval)
	}
	if DefaultEmailSubject != "[FWAlizer] 告警通知" || DefaultEmailBody != "FWAlizer 检测到运行异常，请检查同步日志。" {
		t.Errorf("邮件默认主题/正文错误: %q / %q", DefaultEmailSubject, DefaultEmailBody)
	}

	policy := DefaultAlertPolicy()
	if policy.DNSFailedEnabled || policy.SyncErrorEnabled || policy.OperationalErrorEnabled {
		t.Errorf("默认策略必须三个触发开关全部关闭: %+v", policy)
	}
	if policy.HealthTimeout != 10*time.Minute || policy.HealthTimeoutText != "10m" {
		t.Errorf("默认策略 health_timeout 错误: %+v", policy)
	}

	push := DefaultUptimeKumaPush()
	if push.Enabled || push.URL != "" {
		t.Errorf("默认 Push 必须关闭且 URL 为空: %+v", push)
	}
	if push.Interval != 60*time.Second || push.IntervalText != "60s" {
		t.Errorf("默认 Push interval 错误: %+v", push)
	}
}

// TestStoreRoundTripPolicyAndPush 策略与 Push 必须能事务写入、读回，并进入业务快照与运行时配置。
func TestStoreRoundTripPolicyAndPush(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "roundtrip.db"))
	if err != nil {
		t.Fatalf("打开库失败: %v", err)
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			t.Errorf("关闭库失败: %v", cerr)
		}
	}()

	policy, err := NormalizeAlertPolicy(AlertPolicyConfig{
		DNSFailedEnabled: true, SyncErrorEnabled: true, HealthTimeoutText: "25m",
	})
	if err != nil {
		t.Fatalf("归一化策略失败: %v", err)
	}
	push, err := NormalizeUptimeKumaPush(UptimeKumaPushConfig{
		Enabled: true, URL: "https://kuma.invalid/api/push/tok1", IntervalText: "45s",
	})
	if err != nil {
		t.Fatalf("归一化 Push 失败: %v", err)
	}
	email, err := NormalizeAlertEmail(AlertEmailConfig{
		Enabled: true, Host: "smtp.x", Port: "587", FromAddr: "f@x", ToAddr: "t@x",
		Subject: "主题X", Body: "正文X",
	})
	if err != nil {
		t.Fatalf("归一化邮件失败: %v", err)
	}
	webhook, err := NormalizeAlertWebhook(AlertWebhookConfig{Enabled: true, URL: "https://hook.invalid/x", Channel: "slack"})
	if err != nil {
		t.Fatalf("归一化 Webhook 失败: %v", err)
	}

	ctx := context.Background()
	if err := s.WithTransaction(func(tx *sql.Tx) error {
		return s.ReplaceBusinessAlertsTx(ctx, tx, policy, email, webhook, push)
	}); err != nil {
		t.Fatalf("写入告警配置失败: %v", err)
	}

	gotPolicy, err := s.GetAlertPolicy()
	if err != nil {
		t.Fatalf("读取策略失败: %v", err)
	}
	if !gotPolicy.DNSFailedEnabled || !gotPolicy.SyncErrorEnabled || gotPolicy.OperationalErrorEnabled {
		t.Errorf("策略开关读回错误: %+v", gotPolicy)
	}
	if gotPolicy.HealthTimeoutText != "25m" || gotPolicy.HealthTimeout != 25*time.Minute {
		t.Errorf("策略 health_timeout 读回错误: %+v", gotPolicy)
	}

	gotPush, err := s.GetUptimeKumaPush()
	if err != nil {
		t.Fatalf("读取 Push 失败: %v", err)
	}
	if !gotPush.Enabled || gotPush.URL != "https://kuma.invalid/api/push/tok1" ||
		gotPush.IntervalText != "45s" || gotPush.Interval != 45*time.Second {
		t.Errorf("Push 读回错误: %+v", gotPush)
	}

	// 快照与运行时配置必须携带新字段（协调器候选依赖它）
	snapshot, err := s.LoadBusinessSnapshot()
	if err != nil {
		t.Fatalf("读取业务快照失败: %v", err)
	}
	if snapshot.Policy.HealthTimeoutText != "25m" || !snapshot.Policy.SyncErrorEnabled {
		t.Errorf("快照未携带策略: %+v", snapshot.Policy)
	}
	if snapshot.UptimeKumaPush.IntervalText != "45s" || !snapshot.UptimeKumaPush.Enabled {
		t.Errorf("快照未携带 Push: %+v", snapshot.UptimeKumaPush)
	}
	if snapshot.Email.Subject != "主题X" || snapshot.Email.Body != "正文X" {
		t.Errorf("快照未携带邮件主题/正文: %+v", snapshot.Email)
	}
	rc := snapshot.ToRuntimeConfig()
	if rc.Policy.HealthTimeout != 25*time.Minute || rc.UptimeKumaPush.Interval != 45*time.Second {
		t.Errorf("RuntimeConfig 未携带策略/Push: %+v", rc)
	}

	// 主题/正文必须能往返（SQLite 持久化）
	gotEmail, err := s.GetAlertEmail()
	if err != nil {
		t.Fatalf("读取邮件告警失败: %v", err)
	}
	if gotEmail.Subject != "主题X" || gotEmail.Body != "正文X" {
		t.Errorf("邮件主题/正文读回错误: %+v", gotEmail)
	}
}

// TestResetRestoresAlertDefaults reset 必须清空新表并回到「全部关闭 + 10m/60s + 默认主题正文」。
func TestResetRestoresAlertDefaults(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "reset.db"))
	if err != nil {
		t.Fatalf("打开库失败: %v", err)
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			t.Errorf("关闭库失败: %v", cerr)
		}
	}()

	ctx := context.Background()
	if err := s.WithTransaction(func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE alert_email SET enabled=1, subject='X', body='Y', password='secret' WHERE id=1`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE alert_webhook SET enabled=1, url='https://hook.invalid' WHERE id=1`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE alert_policy SET dns_failed_enabled=1, health_timeout='25m' WHERE id=1`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE uptime_kuma_push SET enabled=1, url='https://kuma.invalid/push/t', interval='45s' WHERE id=1`); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("预置告警配置失败: %v", err)
	}

	if err := s.WithTransaction(func(tx *sql.Tx) error { return s.ResetAllTx(ctx, tx) }); err != nil {
		t.Fatalf("reset 失败: %v", err)
	}

	policy, err := s.GetAlertPolicy()
	if err != nil {
		t.Fatalf("reset 后读取策略失败: %v", err)
	}
	if policy.DNSFailedEnabled || policy.SyncErrorEnabled || policy.OperationalErrorEnabled {
		t.Errorf("reset 后策略必须全部关闭: %+v", policy)
	}
	if policy.HealthTimeoutText != "10m" {
		t.Errorf("reset 后 health_timeout = %q, want 10m", policy.HealthTimeoutText)
	}
	push, err := s.GetUptimeKumaPush()
	if err != nil {
		t.Fatalf("reset 后读取 Push 失败: %v", err)
	}
	if push.Enabled || push.URL != "" || push.IntervalText != "60s" {
		t.Errorf("reset 后 Push 必须为默认关闭/空/60s: %+v", push)
	}
	email, err := s.GetAlertEmail()
	if err != nil {
		t.Fatalf("reset 后读取邮件失败: %v", err)
	}
	if email.Enabled || email.Password != "" || email.Host != "" {
		t.Errorf("reset 后邮件必须回到默认关闭且清空凭据: %+v", email)
	}
	if email.Subject != DefaultEmailSubject || email.Body != DefaultEmailBody {
		t.Errorf("reset 后邮件主题/正文必须回到默认: %+v", email)
	}
}

// TestLoadBusinessSnapshotRejectsInvalidStoredDuration 存量非法时长不得静默回退为默认值。
func TestLoadBusinessSnapshotRejectsInvalidStoredDuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatalf("打开库失败: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("关闭库失败: %v", err)
	}

	db := openRaw(t, path)
	if _, err := db.Exec(`UPDATE alert_policy SET health_timeout='not-a-duration' WHERE id=1`); err != nil {
		t.Fatalf("写入非法时长失败: %v", err)
	}

	s2, err := OpenStore(path)
	if err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	defer func() {
		if cerr := s2.Close(); cerr != nil {
			t.Errorf("关闭库失败: %v", cerr)
		}
	}()

	_, err = s2.LoadBusinessSnapshot()
	if err == nil {
		t.Fatal("存量非法 health_timeout 必须返回错误，不得静默回退为 10m")
	}
	if !strings.Contains(err.Error(), "policy.health_timeout") {
		t.Errorf("错误必须指出字段名: %v", err)
	}
	if strings.Contains(err.Error(), "not-a-duration") {
		t.Errorf("错误不得回显原始值: %v", err)
	}
}
