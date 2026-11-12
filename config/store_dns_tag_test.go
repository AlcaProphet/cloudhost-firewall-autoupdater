package config

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/tag"

	_ "modernc.org/sqlite"
)

// 本文件是 Build8 Step 1 的判别性回归：
//   - 新建库/迁移标记已存在的库使用 auto-ddns；
//   - 已初始化旧库缺键或空白 TAG 在首次升级事务内显式写入 auto-dns；
//   - 显式 TAG 原样保留，旧规则描述仍可判定所有权；
//   - 迁移只执行一次，失败时设置值与迁移标记一起回滚；
//   - reset 保留迁移标记，重启后仍使用 auto-ddns。

// legacySettingsSchema 是旧库最小可识别形态：只要 settings 表已存在，
// 建表前就视为「已初始化旧库」。
const legacySettingsSchema = `
CREATE TABLE settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

// newLegacySettingsDB 建立只含 settings 表的旧库并写入给定键值，返回数据库路径。
func newLegacySettingsDB(t *testing.T, values map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy-dns-tag.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开旧库失败: %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("关闭旧库失败: %v", cerr)
		}
	}()
	if _, err := db.Exec(legacySettingsSchema); err != nil {
		t.Fatalf("建立旧库 settings 表失败: %v", err)
	}
	for k, v := range values {
		if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", k, v); err != nil {
			t.Fatalf("预置旧库设置 %s 失败: %v", k, err)
		}
	}
	return path
}

// openStoreAt 打开指定路径的 Store，调用方负责关闭。
func openStoreAt(t *testing.T, path string) *Store {
	t.Helper()
	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("打开 Store 失败: %v", err)
	}
	return store
}

// closeStore 关闭 Store 并断言成功。
func closeStore(t *testing.T, store *Store) {
	t.Helper()
	if err := store.Close(); err != nil {
		t.Fatalf("关闭 Store 失败: %v", err)
	}
}

// rawDB 打开独立连接用于检查实际数据库值；关闭由调用方负责。
func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开检查连接失败: %v", err)
	}
	return db
}

// rawSetting 返回设置键的实际值；found=false 表示键不存在。
func rawSetting(t *testing.T, path, key string) (string, bool) {
	t.Helper()
	db := rawDB(t, path)
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("关闭检查连接失败: %v", cerr)
		}
	}()
	var value string
	err := db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("读取设置 %s 失败: %v", key, err)
	}
	return value, true
}

// rawUserVersion 读取 PRAGMA user_version。
func rawUserVersion(t *testing.T, path string) int {
	t.Helper()
	db := rawDB(t, path)
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("关闭检查连接失败: %v", cerr)
		}
	}()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("读取 user_version 失败: %v", err)
	}
	return version
}

// rawTableExists 判断实际数据库中是否存在指定表。
func rawTableExists(t *testing.T, path, table string) bool {
	t.Helper()
	db := rawDB(t, path)
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("关闭检查连接失败: %v", cerr)
		}
	}()
	var name string
	err := db.QueryRow("SELECT name FROM sqlite_schema WHERE type='table' AND name=?", table).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		t.Fatalf("检查表 %s 失败: %v", table, err)
	}
	return true
}

// snapshotDNSTag 读取完整业务快照中的 DNS TAG（即运行快照来源）。
func snapshotDNSTag(t *testing.T, store *Store) string {
	t.Helper()
	snapshot, err := store.LoadBusinessSnapshot()
	if err != nil {
		t.Fatalf("LoadBusinessSnapshot 失败: %v", err)
	}
	return snapshot.Settings["tag"]
}

func TestNewStoreUsesAutoDDNSAndMarksMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.db")
	store := openStoreAt(t, path)
	if got := snapshotDNSTag(t, store); got != DefaultDNSTag {
		t.Fatalf("新库运行快照 DNS TAG = %q, want %q", got, DefaultDNSTag)
	}
	if got := rawUserVersion(t, path); got != schemaMigrationVersion {
		t.Errorf("新库 user_version = %d, want %d", got, schemaMigrationVersion)
	}
	if _, found := rawSetting(t, path, "tag"); found {
		t.Errorf("新库不应显式写入默认 TAG（应由默认值解析）")
	}
	closeStore(t, store)

	// 重启仍必须使用 auto-ddns，而不是把空设置重新识别为旧库。
	store = openStoreAt(t, path)
	defer closeStore(t, store)
	if got := snapshotDNSTag(t, store); got != DefaultDNSTag {
		t.Errorf("重启后 DNS TAG = %q, want %q", got, DefaultDNSTag)
	}
}

func TestLegacyMissingOrBlankDNSTagMigratesOnce(t *testing.T) {
	for name, seed := range map[string]map[string]string{
		"缺键": {"interval": "5m"},
		"空白": {"tag": "   ", "interval": "5m"},
	} {
		t.Run(name, func(t *testing.T) {
			path := newLegacySettingsDB(t, seed)
			store := openStoreAt(t, path)
			if got := snapshotDNSTag(t, store); got != LegacyDefaultDNSTag {
				t.Fatalf("首次升级后运行快照 DNS TAG = %q, want %q", got, LegacyDefaultDNSTag)
			}
			if got, found := rawSetting(t, path, "tag"); !found || got != LegacyDefaultDNSTag {
				t.Fatalf("旧库迁移应显式写入 %q，实际 value=%q found=%v", LegacyDefaultDNSTag, got, found)
			}
			if got := rawUserVersion(t, path); got != schemaMigrationVersion {
				t.Fatalf("迁移后 user_version = %d, want %d", got, schemaMigrationVersion)
			}
			closeStore(t, store)

			// 第二次启动不重复迁移：先确认保持旧有效值。
			store = openStoreAt(t, path)
			if got := snapshotDNSTag(t, store); got != LegacyDefaultDNSTag {
				t.Fatalf("第二次启动 DNS TAG = %q, want %q", got, LegacyDefaultDNSTag)
			}
			closeStore(t, store)

			// 删除显式值后再次启动：迁移标记阻止重放旧值，应解析为新默认值。
			db := rawDB(t, path)
			if _, err := db.Exec("DELETE FROM settings WHERE key = 'tag'"); err != nil {
				t.Fatalf("删除迁移后的 TAG 失败: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatalf("关闭检查连接失败: %v", err)
			}
			store = openStoreAt(t, path)
			defer closeStore(t, store)
			if got := snapshotDNSTag(t, store); got != DefaultDNSTag {
				t.Errorf("已迁移库缺键时应使用新默认值 %q，实际 %q（迁移被重复执行？）", DefaultDNSTag, got)
			}
		})
	}
}

func TestLegacyExplicitDNSTagPreserved(t *testing.T) {
	path := newLegacySettingsDB(t, map[string]string{
		"tag":      " my-dns ",
		"interval": "5m",
	})
	store := openStoreAt(t, path)
	if got := snapshotDNSTag(t, store); got != "my-dns" {
		t.Fatalf("旧库显式 TAG 归一化后 = %q, want my-dns", got)
	}
	// 显式值原样保留在数据库中，迁移不得改写。
	if got, found := rawSetting(t, path, "tag"); !found || got != " my-dns " {
		t.Errorf("显式 TAG 不得被迁移改写：value=%q found=%v", got, found)
	}
	closeStore(t, store)

	// 旧规则描述仍可按该 TAG 判定所有权。
	if comment, ok := tag.Parse("[my-dns] 生产API", "my-dns"); !ok || comment != "生产API" {
		t.Errorf("旧 [my-dns] 规则所有权判定失败: comment=%q ok=%v", comment, ok)
	}

	store = openStoreAt(t, path)
	defer closeStore(t, store)
	if got := snapshotDNSTag(t, store); got != "my-dns" {
		t.Errorf("重启后显式 TAG = %q, want my-dns", got)
	}
}

func TestLegacyDNSTagMigrationFailureRollsBack(t *testing.T) {
	path := newLegacySettingsDB(t, map[string]string{"interval": "5m"})
	db := rawDB(t, path)
	// AFTER INSERT 让旧 TAG 实际写入成功后再中止语句，等价于「写旧 TAG 后、提交前」失败。
	if _, err := db.Exec(`CREATE TRIGGER fail_dns_tag_migration AFTER INSERT ON settings
		WHEN NEW.key = 'tag' BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`); err != nil {
		t.Fatalf("创建失败注入 trigger 失败: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("关闭检查连接失败: %v", err)
	}

	if store, err := OpenStore(path); err == nil {
		_ = store.Close()
		t.Fatal("迁移写入失败时 OpenStore 必须返回错误")
	}

	if got, found := rawSetting(t, path, "tag"); found {
		t.Errorf("迁移失败不得部分写入 TAG: value=%q", got)
	}
	if got := rawUserVersion(t, path); got != 0 {
		t.Errorf("迁移失败不得提交迁移标记：user_version=%d", got)
	}
	if rawTableExists(t, path, "alert_email") || rawTableExists(t, path, "targets") {
		t.Errorf("迁移失败不得留下部分 Schema：alert_email/targets 应随事务回滚")
	}
	if !rawTableExists(t, path, "settings") {
		t.Errorf("原有 settings 表不得被迁移失败删除")
	}

	// 移除故障后可在同一旧库上正常完成一次性迁移。
	db = rawDB(t, path)
	if _, err := db.Exec("DROP TRIGGER fail_dns_tag_migration"); err != nil {
		t.Fatalf("移除失败注入 trigger 失败: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("关闭检查连接失败: %v", err)
	}
	store := openStoreAt(t, path)
	defer closeStore(t, store)
	if got := snapshotDNSTag(t, store); got != LegacyDefaultDNSTag {
		t.Errorf("恢复后迁移 TAG = %q, want %q", got, LegacyDefaultDNSTag)
	}
	if got := rawUserVersion(t, path); got != schemaMigrationVersion {
		t.Errorf("恢复后 user_version = %d, want %d", got, schemaMigrationVersion)
	}
}

func TestResetKeepsMigrationMarkerAndUsesAutoDDNS(t *testing.T) {
	path := newLegacySettingsDB(t, map[string]string{"interval": "5m"})
	store := openStoreAt(t, path)
	if got := snapshotDNSTag(t, store); got != LegacyDefaultDNSTag {
		t.Fatalf("迁移后 TAG = %q, want %q", got, LegacyDefaultDNSTag)
	}
	if err := store.WithTransaction(func(tx *sql.Tx) error {
		return store.ResetAllTx(context.Background(), tx)
	}); err != nil {
		t.Fatalf("ResetAllTx 失败: %v", err)
	}
	if got := snapshotDNSTag(t, store); got != DefaultDNSTag {
		t.Errorf("reset 后运行快照 TAG = %q, want %q", got, DefaultDNSTag)
	}
	if got := rawUserVersion(t, path); got != schemaMigrationVersion {
		t.Errorf("reset 不得清除迁移标记：user_version=%d, want %d", got, schemaMigrationVersion)
	}
	closeStore(t, store)

	store = openStoreAt(t, path)
	defer closeStore(t, store)
	if got := snapshotDNSTag(t, store); got != DefaultDNSTag {
		t.Errorf("reset 后重启 TAG = %q, want %q", got, DefaultDNSTag)
	}
}
