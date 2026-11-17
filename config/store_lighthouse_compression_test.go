package config

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func saveCompressionForTest(t *testing.T, s *Store, c LighthouseCompression) {
	t.Helper()
	if err := s.WithTransaction(func(tx *sql.Tx) error {
		return s.SaveLighthouseCompressionTx(context.Background(), tx, c)
	}); err != nil {
		t.Fatalf("保存压缩配置失败: %v", err)
	}
}

func TestBuild9S1NewStoreCompressionDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.db")
	store := openStoreAt(t, path)
	defer closeStore(t, store)

	if !rawTableExists(t, path, "lighthouse_compression_settings") ||
		!rawTableExists(t, path, "lighthouse_compression_state") {
		t.Fatal("新库必须创建压缩配置与状态表")
	}
	if got := rawUserVersion(t, path); got != 3 {
		t.Fatalf("user_version = %d, want 3", got)
	}
	settings, err := store.LoadLighthouseCompressionSettings()
	if err != nil || len(settings) != 0 {
		t.Fatalf("新库压缩配置 = %+v err=%v", settings, err)
	}
	key := LighthouseCompression{CloudType: CloudTCLighthouse, Region: "gz", ResourceID: "lhins-a"}
	if _, found, err := store.LoadLighthouseCompressionState(key); err != nil || found {
		t.Fatalf("新库不应有压缩状态: found=%v err=%v", found, err)
	}
}

func TestBuild9S1CompressionPersistsAndResets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persist.db")
	store := openStoreAt(t, path)
	targetID := int64(0)
	if err := store.WithTransaction(func(tx *sql.Tx) error {
		id, err := store.AddTargetTx(context.Background(), tx, TargetConfig{CloudType: CloudTCLighthouse, Region: "gz", ResourceID: "lhins-a"})
		if err != nil {
			return err
		}
		targetID = id
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if targetID == 0 {
		t.Fatal("目标 ID 未分配")
	}
	item := LighthouseCompression{CloudType: CloudTCLighthouse, Region: "gz", ResourceID: "lhins-a", Enabled: true, ExtraIPv4Allow: 123}
	saveCompressionForTest(t, store, item)
	if err := store.WithTransaction(func(tx *sql.Tx) error {
		return store.SaveLighthouseCompressionStateTx(context.Background(), tx, LighthouseCompressionState{
			CloudType: CloudTCLighthouse, Region: "gz", ResourceID: "lhins-a",
			PendingReview: true, PendingReason: "disabled", PendingSince: "2026-11-17T00:00:00Z",
		})
	}); err != nil {
		t.Fatal(err)
	}
	closeStore(t, store)

	store = openStoreAt(t, path)
	defer closeStore(t, store)
	got, err := store.LoadLighthouseCompressionSettings()
	if err != nil || len(got) != 1 || !got[0].Enabled || got[0].ExtraIPv4Allow != 123 {
		t.Fatalf("重启后压缩配置 = %+v err=%v", got, err)
	}
	state, found, err := store.LoadLighthouseCompressionState(item)
	if err != nil || !found || !state.PendingReview || state.PendingReason != "disabled" {
		t.Fatalf("重启后压缩状态 = %+v found=%v err=%v", state, found, err)
	}

	if err := store.WithTransaction(func(tx *sql.Tx) error { return store.ResetAllTx(context.Background(), tx) }); err != nil {
		t.Fatal(err)
	}
	settings, _ := store.LoadLighthouseCompressionSettings()
	if len(settings) != 0 {
		t.Fatalf("reset 后配置残留: %+v", settings)
	}
	if _, found, _ := store.LoadLighthouseCompressionState(item); found {
		t.Fatal("reset 后状态残留")
	}
	if rawUserVersion(t, path) != 3 {
		t.Fatal("reset 不得清除 user_version")
	}
}

func TestBuild9S1SnapshotRejectsOrphanCompression(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orphan.db")
	store := openStoreAt(t, path)
	defer closeStore(t, store)
	saveCompressionForTest(t, store, LighthouseCompression{
		CloudType: CloudTCLighthouse, Region: "gz", ResourceID: "missing", Enabled: true, ExtraIPv4Allow: 1,
	})
	if _, err := store.LoadBusinessSnapshot(); err == nil {
		t.Fatal("悬空压缩配置必须使完整快照失败")
	}
}

func TestBuild9S1V2DatabaseDefaultsOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	db := rawDB(t, path)
	if _, err := db.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE targets (id INTEGER PRIMARY KEY AUTOINCREMENT, cloud_type TEXT NOT NULL, region TEXT NOT NULL, resource_id TEXT NOT NULL);
		INSERT INTO targets (cloud_type, region, resource_id) VALUES ('tc_lighthouse','gz','lhins-old');
		PRAGMA user_version = 2;`); err != nil {
		t.Fatalf("建立 v2 夹具失败: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store := openStoreAt(t, path)
	defer closeStore(t, store)
	if got := rawUserVersion(t, path); got != 3 {
		t.Fatalf("升级后 user_version = %d, want 3", got)
	}
	settings, err := store.LoadLighthouseCompressionSettings()
	if err != nil || len(settings) != 0 {
		t.Fatalf("旧库升级后压缩配置 = %+v err=%v", settings, err)
	}
	if !rawTableExists(t, path, "lighthouse_compression_settings") {
		t.Fatal("升级后必须创建压缩配置表")
	}
}

func TestBuild9S1MigrationFailureLeavesNoCompressionTables(t *testing.T) {
	path := newLegacySettingsDB(t, map[string]string{"interval": "5m"})
	db := rawDB(t, path)
	if _, err := db.Exec(`CREATE TRIGGER fail_dns_tag_migration AFTER INSERT ON settings
		WHEN NEW.key = 'tag' BEGIN SELECT RAISE(ABORT, 'fixture'); END;`); err != nil {
		t.Fatalf("创建失败注入 trigger 失败: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if store, err := OpenStore(path); err == nil {
		_ = store.Close()
		t.Fatal("迁移失败时 OpenStore 必须返回错误")
	}
	if rawTableExists(t, path, "lighthouse_compression_settings") ||
		rawTableExists(t, path, "lighthouse_compression_state") {
		t.Fatal("迁移失败不得留下压缩表")
	}
	if got := rawUserVersion(t, path); got != 0 {
		t.Fatalf("迁移失败不得提交 user_version: %d", got)
	}
}
