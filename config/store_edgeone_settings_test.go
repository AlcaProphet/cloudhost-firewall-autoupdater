package config

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// 本文件是 Build8 Step 2 的领域回归：新增双功能开关、EdgeOne TAG/间隔的
// 默认值、旧库一次性初始化、运行时快照和最终快照 TAG 互斥校验。

func newEmptyStoreAt(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "edgeone-settings.db")
	return openStoreAt(t, path), path
}

func TestNewStoreEdgeOneDefaultsAndRuntime(t *testing.T) {
	store, path := newEmptyStoreAt(t)
	defer closeStore(t, store)

	snapshot, err := store.LoadBusinessSnapshot()
	if err != nil {
		t.Fatalf("LoadBusinessSnapshot 失败: %v", err)
	}
	for k, want := range map[string]string{
		"dns_enabled":      "true",
		"edgeone_enabled":  "false",
		"edgeone_tag":      DefaultEdgeOneTag,
		"edgeone_interval": DefaultEdgeOneIntervalText,
	} {
		if got := snapshot.Settings[k]; got != want {
			t.Errorf("新库设置 %s = %q, want %q", k, got, want)
		}
	}
	if _, found := rawSetting(t, path, "edgeone_tag"); found {
		t.Errorf("新库不应显式写入 EdgeOne TAG 默认值")
	}
	if got := rawUserVersion(t, path); got != schemaMigrationVersion {
		t.Errorf("新库 user_version = %d, want %d", got, schemaMigrationVersion)
	}

	rc := snapshot.ToRuntimeConfig()
	if !rc.DNSEnabled || rc.EdgeOneEnabled {
		t.Errorf("运行时开关 = dns:%v edgeone:%v, want true/false", rc.DNSEnabled, rc.EdgeOneEnabled)
	}
	if rc.EdgeOneTag != DefaultEdgeOneTag {
		t.Errorf("运行时 EdgeOne TAG = %q, want %q", rc.EdgeOneTag, DefaultEdgeOneTag)
	}
	if rc.EdgeOneInterval != DefaultEdgeOneInterval {
		t.Errorf("运行时 EdgeOne 间隔 = %v, want %v", rc.EdgeOneInterval, DefaultEdgeOneInterval)
	}
}

func TestLegacyDNSConflictBootstrapsEdgeOneTagOnce(t *testing.T) {
	path := newLegacySettingsDB(t, map[string]string{
		"tag":      "edgeone",
		"interval": "5m",
	})
	store := openStoreAt(t, path)
	snapshot, err := store.LoadBusinessSnapshot()
	if err != nil {
		t.Fatalf("首次升级快照失败: %v", err)
	}
	if got := snapshot.Settings["edgeone_tag"]; got != LegacyEdgeOneTag {
		t.Fatalf("DNS TAG 为 edgeone 时 EdgeOne TAG = %q, want %q", got, LegacyEdgeOneTag)
	}
	if got := snapshot.Settings["tag"]; got != DefaultEdgeOneTag {
		t.Errorf("DNS TAG 应保留 edgeone，实际 %q", got)
	}
	if got, found := rawSetting(t, path, "edgeone_tag"); !found || got != LegacyEdgeOneTag {
		t.Errorf("旧库应显式写入 EdgeOne TAG：value=%q found=%v", got, found)
	}
	if got := rawUserVersion(t, path); got != schemaMigrationVersion {
		t.Errorf("迁移后 user_version = %d, want %d", got, schemaMigrationVersion)
	}
	closeStore(t, store)

	// 第二次启动保持 edgeone-origin，不重复初始化。
	store = openStoreAt(t, path)
	if got := snapshotDNSTag(t, store); got != DefaultEdgeOneTag {
		t.Errorf("重启后 DNS TAG = %q, want edgeone", got)
	}
	snapshot, err = store.LoadBusinessSnapshot()
	if err != nil || snapshot.Settings["edgeone_tag"] != LegacyEdgeOneTag {
		t.Errorf("重启后 EdgeOne TAG = %q, err=%v", snapshot.Settings["edgeone_tag"], err)
	}
	closeStore(t, store)

	// 显式改值后重启必须保留，迁移不得覆盖。
	db := rawDB(t, path)
	if _, err := db.Exec("UPDATE settings SET value = 'my-edge' WHERE key = 'edgeone_tag'"); err != nil {
		t.Fatalf("改写 EdgeOne TAG 失败: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("关闭检查连接失败: %v", err)
	}
	store = openStoreAt(t, path)
	defer closeStore(t, store)
	snapshot, err = store.LoadBusinessSnapshot()
	if err != nil {
		t.Fatalf("重启快照失败: %v", err)
	}
	if got := snapshot.Settings["edgeone_tag"]; got != "my-edge" {
		t.Errorf("显式 EdgeOne TAG 被迁移覆盖：%q", got)
	}
}

func TestLegacyEdgeOneTagExplicitPreserved(t *testing.T) {
	path := newLegacySettingsDB(t, map[string]string{
		"tag":              "edgeone",
		"edgeone_tag":      "custom-edge",
		"edgeone_interval": "36h",
	})
	store := openStoreAt(t, path)
	defer closeStore(t, store)
	snapshot, err := store.LoadBusinessSnapshot()
	if err != nil {
		t.Fatalf("快照失败: %v", err)
	}
	if got := snapshot.Settings["edgeone_tag"]; got != "custom-edge" {
		t.Errorf("显式 EdgeOne TAG = %q, want custom-edge", got)
	}
	if got := snapshot.Settings["edgeone_interval"]; got != "36h" {
		t.Errorf("显式 EdgeOne 间隔 = %q, want 36h", got)
	}
	if rc := snapshot.ToRuntimeConfig(); rc.EdgeOneInterval != 36*time.Hour {
		t.Errorf("运行时 EdgeOne 间隔 = %v, want 36h", rc.EdgeOneInterval)
	}
	// DNS TAG 与显式 EdgeOne TAG 不同，快照可正常校验。
	if _, found := rawSetting(t, path, "edgeone_tag"); !found {
		t.Errorf("显式 EdgeOne TAG 应保留在数据库中")
	}
}

func TestEdgeOneTagConflictRejectedBySnapshot(t *testing.T) {
	store, _ := newEmptyStoreAt(t)
	defer closeStore(t, store)
	if err := store.SetSetting("tag", "edgeone"); err != nil {
		t.Fatalf("写入 DNS TAG 失败: %v", err)
	}
	if err := store.SetSetting("edgeone_tag", "edgeone"); err != nil {
		t.Fatalf("写入 EdgeOne TAG 失败: %v", err)
	}
	_, err := store.LoadBusinessSnapshot()
	if err == nil {
		t.Fatal("两个 TAG 相同时完整快照必须拒绝")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Field != "edgeone_tag" {
		t.Fatalf("期望 edgeone_tag 校验错误，实际 %v", err)
	}
}

func TestNormalizeSettingsRejectsInvalidFeatureValues(t *testing.T) {
	cases := map[string]map[string]string{
		"dns_enabled 非布尔":       {"dns_enabled": "yes"},
		"edgeone_enabled 非布尔":   {"edgeone_enabled": "1"},
		"edgeone_tag 方括号":       {"edgeone_tag": "[edge]"},
		"edgeone_interval 零":    {"edgeone_interval": "0s"},
		"edgeone_interval 负数":   {"edgeone_interval": "-1h"},
		"edgeone_interval 不可解析": {"edgeone_interval": "abc"},
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			store, _ := newEmptyStoreAt(t)
			defer closeStore(t, store)
			for k, v := range raw {
				if err := store.SetSetting(k, v); err != nil {
					t.Fatalf("预置 %s 失败: %v", k, err)
				}
			}
			if _, err := store.LoadBusinessSnapshot(); err == nil {
				t.Fatalf("非法值 %v 必须被完整快照拒绝", raw)
			} else {
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("期望领域校验错误，实际 %v", err)
				}
			}
		})
	}
}

func TestDefaultEdgeOneTagForDNS(t *testing.T) {
	cases := map[string]string{
		"edgeone":     LegacyEdgeOneTag,
		" edgeone ":   LegacyEdgeOneTag,
		"EdgeOne":     DefaultEdgeOneTag,
		"auto-ddns":   DefaultEdgeOneTag,
		"edgeone-old": DefaultEdgeOneTag,
	}
	for dnsTag, want := range cases {
		if got := DefaultEdgeOneTagForDNS(dnsTag); got != want {
			t.Errorf("DefaultEdgeOneTagForDNS(%q) = %q, want %q", dnsTag, got, want)
		}
	}
}

func TestLegacyV1DBBootstrapsEdgeOneTag(t *testing.T) {
	cases := map[string]struct {
		values   map[string]string
		wantTag  string
		wantEdge string
	}{
		"DNS TAG 为 edgeone 且缺 EdgeOne TAG": {
			values: map[string]string{"tag": "edgeone"}, wantTag: "edgeone", wantEdge: LegacyEdgeOneTag,
		},
		"DNS TAG 为 edgeone 且显式 EdgeOne TAG": {
			values: map[string]string{"tag": "edgeone", "edgeone_tag": "custom"}, wantTag: "edgeone", wantEdge: "custom",
		},
		"DNS TAG 非 edgeone": {
			values: map[string]string{"tag": "my-dns"}, wantTag: "my-dns", wantEdge: DefaultEdgeOneTag,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := newLegacySettingsDB(t, tc.values)
			db := rawDB(t, path)
			if _, err := db.Exec("PRAGMA user_version = 1"); err != nil {
				t.Fatalf("设置 user_version=1 失败: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatalf("关闭检查连接失败: %v", err)
			}

			store := openStoreAt(t, path)
			defer closeStore(t, store)
			snapshot, err := store.LoadBusinessSnapshot()
			if err != nil {
				t.Fatalf("v1 库升级失败: %v", err)
			}
			if got := snapshot.Settings["tag"]; got != tc.wantTag {
				t.Errorf("DNS TAG = %q, want %q", got, tc.wantTag)
			}
			if got := snapshot.Settings["edgeone_tag"]; got != tc.wantEdge {
				t.Errorf("EdgeOne TAG = %q, want %q", got, tc.wantEdge)
			}
			if got := rawUserVersion(t, path); got != schemaMigrationVersion {
				t.Errorf("升级后 user_version = %d, want %d", got, schemaMigrationVersion)
			}
		})
	}
}
