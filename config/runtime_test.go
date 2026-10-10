package config

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newSnapshotStore 打开一个临时 SQLite（测试夹具，每个用例独立目录）
func newSnapshotStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "snapshot-test.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if cerr := store.Close(); cerr != nil {
			t.Errorf("关闭测试数据库失败: %v", cerr)
		}
	})
	return store
}

// TestLoadBusinessSnapshotTxDefaultsOnEmptyDB 空库快照补齐全部业务设置默认值，
// 且不包含数据库中的未知键（Build6 §3.1、§12.10）。
func TestLoadBusinessSnapshotTxDefaultsOnEmptyDB(t *testing.T) {
	store := newSnapshotStore(t)
	ctx := context.Background()

	tx, err := store.BeginReadOnlyTx(ctx)
	if err != nil {
		t.Fatalf("BeginReadOnlyTx 失败: %v", err)
	}
	snapshot, err := store.LoadBusinessSnapshotTx(ctx, tx)
	if err != nil {
		t.Fatalf("LoadBusinessSnapshotTx 失败: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("提交只读事务失败: %v", err)
	}

	want := map[string]string{
		"tag": "auto-dns", "interval": "5m", "dns": "223.5.5.5", "dns_timeout": "10s",
		"dns_fail_threshold": "5", "log_level": "info", "theme": "light", "sync_enabled": "true",
		"tc_access_id": "", "tc_access_key": "", "ali_access_id": "", "ali_access_key": "",
	}
	for k, v := range want {
		if got := snapshot.Settings[k]; got != v {
			t.Errorf("默认设置 %s = %q, want %q", k, got, v)
		}
	}
	if len(snapshot.Settings) != len(settingsKeysV3) {
		t.Errorf("设置键数 = %d, want %d: %+v", len(snapshot.Settings), len(settingsKeysV3), snapshot.Settings)
	}
	if snapshot.Targets == nil || snapshot.Rules == nil {
		t.Errorf("空库快照的 targets/rules 应为空切片而不是 nil: %+v", snapshot)
	}
	if snapshot.Email.Enabled || snapshot.Webhook.Enabled {
		t.Errorf("空库告警应为禁用: %+v", snapshot)
	}
}

// TestLoadBusinessSnapshotTxDropsUnknownKeys 快照只保留业务设置的完整键集合，
// 数据库中的 webui_port 等残留/未知键不得进入快照。
func TestLoadBusinessSnapshotTxDropsUnknownKeys(t *testing.T) {
	store := newSnapshotStore(t)
	ctx := context.Background()

	for k, v := range map[string]string{
		"webui_port": "61234", "legacy_key": "legacy-value", "tag": "exported",
	} {
		if err := store.SetSetting(k, v); err != nil {
			t.Fatalf("预置设置失败: %v", err)
		}
	}

	tx, err := store.BeginReadOnlyTx(ctx)
	if err != nil {
		t.Fatalf("BeginReadOnlyTx 失败: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	snapshot, err := store.LoadBusinessSnapshotTx(ctx, tx)
	if err != nil {
		t.Fatalf("LoadBusinessSnapshotTx 失败: %v", err)
	}
	if _, ok := snapshot.Settings["webui_port"]; ok {
		t.Errorf("快照不应含 webui_port: %+v", snapshot.Settings)
	}
	if _, ok := snapshot.Settings["legacy_key"]; ok {
		t.Errorf("快照不应含未知键 legacy_key: %+v", snapshot.Settings)
	}
	if snapshot.Settings["tag"] != "exported" {
		t.Errorf("已有合法设置应保留: %+v", snapshot.Settings)
	}
}

// TestLoadBusinessSnapshotTxRejectsInvalidExistingValue 已有非空但非法的设置值
// 必须返回带键名（不含值）的错误，不得静默回退默认值（Build6 §12.10）。
func TestLoadBusinessSnapshotTxRejectsInvalidExistingValue(t *testing.T) {
	cases := []struct {
		key, value, wantField string
	}{
		{"tag", "bad[tag]", "tag"},
		{"interval", "not-a-duration", "interval"},
		{"dns", "[not-ipv6", "dns"},
		{"dns_timeout", "-5s", "dns_timeout"},
		{"dns_fail_threshold", "0", "dns_fail_threshold"},
		{"log_level", "verbose", "log_level"},
		{"theme", "blue", "theme"},
		{"sync_enabled", "yes", "sync_enabled"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			store := newSnapshotStore(t)
			ctx := context.Background()
			if err := store.SetSetting(tc.key, tc.value); err != nil {
				t.Fatalf("预置设置失败: %v", err)
			}

			tx, err := store.BeginReadOnlyTx(ctx)
			if err != nil {
				t.Fatalf("BeginReadOnlyTx 失败: %v", err)
			}
			defer func() { _ = tx.Rollback() }()

			_, err = store.LoadBusinessSnapshotTx(ctx, tx)
			if err == nil {
				t.Fatalf("非法值 %s=%q 必须报错", tc.key, tc.value)
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("错误应为 ValidationError，实际 %T: %v", err, err)
			}
			if ve.Field != tc.wantField {
				t.Errorf("错误字段 = %q, want %q", ve.Field, tc.wantField)
			}
			// 不回显原值
			if len(tc.value) > 3 && strings.Contains(err.Error(), tc.value) {
				t.Errorf("错误不得回显原值: %s", err.Error())
			}
		})
	}
}

// TestLoadBusinessSnapshotTxConsistentInOneTransaction 同一事务内的快照必须内部一致：
// 事务打开后另一连接写入的新目标不得出现在本次快照中。
func TestLoadBusinessSnapshotTxConsistentInOneTransaction(t *testing.T) {
	store := newSnapshotStore(t)
	ctx := context.Background()

	addTargetTxForTest(t, store, TargetConfig{CloudType: CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "lhins-a"})

	tx, err := store.BeginReadOnlyTx(ctx)
	if err != nil {
		t.Fatalf("BeginReadOnlyTx 失败: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	first, err := store.LoadBusinessSnapshotTx(ctx, tx)
	if err != nil {
		t.Fatalf("首次快照失败: %v", err)
	}

	// 事务外（另一连接）写入新目标
	other, err := OpenStore(filepath.Join(t.TempDir(), "other.db"))
	if err != nil {
		t.Fatalf("打开另一数据库失败: %v", err)
	}
	if err := other.Close(); err != nil {
		t.Errorf("关闭另一数据库失败: %v", err)
	}
	addTargetTxForTest(t, store, TargetConfig{CloudType: CloudTCCVM, Region: "ap-beijing", ResourceID: "sg-b"})

	second, err := store.LoadBusinessSnapshotTx(ctx, tx)
	if err != nil {
		t.Fatalf("二次快照失败: %v", err)
	}
	if len(first.Targets) != 1 || len(second.Targets) != 1 {
		t.Errorf("同一事务内两次读取应看到同一快照，实际 %d / %d", len(first.Targets), len(second.Targets))
	}
}

// TestBeginReadOnlyTxSupportsSnapshotReads 只读事务可正常读取完整业务快照。
//
// 说明：modernc.org/sqlite 驱动不强制 sql.TxOptions.ReadOnly 的写入拒绝，
// 本用例只锁定「导出走独立只读事务且能读到一致快照」这一产品契约；
// 导出路径本身不执行任何写入语句（由 handler 与代码审查保证）。
func TestBeginReadOnlyTxSupportsSnapshotReads(t *testing.T) {
	store := newSnapshotStore(t)
	ctx := context.Background()

	addTargetTxForTest(t, store, TargetConfig{CloudType: CloudAliECS, Region: "cn-hangzhou", ResourceID: "sg-x"})

	tx, err := store.BeginReadOnlyTx(ctx)
	if err != nil {
		t.Fatalf("BeginReadOnlyTx 失败: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	snapshot, err := store.LoadBusinessSnapshotTx(ctx, tx)
	if err != nil {
		t.Fatalf("只读事务内读取快照失败: %v", err)
	}
	if len(snapshot.Targets) != 1 || snapshot.Targets[0].ResourceID != "sg-x" {
		t.Errorf("只读事务未读到目标: %+v", snapshot.Targets)
	}
}

// TestToRuntimeConfigCarriesThemeAndCredentials 运行时配置必须携带主题、同步开关与四个凭据。
func TestToRuntimeConfigCarriesThemeAndCredentials(t *testing.T) {
	snapshot := &BusinessSnapshot{
		Settings: map[string]string{
			"tag": "auto-dns", "interval": "30s", "dns": "1.1.1.1", "dns_timeout": "3s",
			"dns_fail_threshold": "7", "log_level": "warn", "theme": "dark", "sync_enabled": "false",
			"tc_access_id": "AKIDx", "tc_access_key": "sk", "ali_access_id": "LTAI", "ali_access_key": "aks",
		},
	}
	rc := snapshot.ToRuntimeConfig()

	if rc.Theme != "dark" || rc.SyncEnabled || rc.DNSFailThreshold != 7 {
		t.Errorf("运行时配置字段错误: %+v", rc)
	}
	if rc.Interval != 30*time.Second || rc.DNSTimeout != 3*time.Second {
		t.Errorf("时长解析错误: interval=%v dnsTimeout=%v", rc.Interval, rc.DNSTimeout)
	}
	if rc.Credentials.TencentSecretID != "AKIDx" || rc.Credentials.AliyunAccessKeySecret != "aks" {
		t.Errorf("凭据未进入运行时配置: %+v", rc.Credentials)
	}
}

// TestRuntimeConfigDeepCopyIsolatesSlices 发布前深拷贝必须隔离切片与内层引用数组。
func TestRuntimeConfigDeepCopyIsolatesSlices(t *testing.T) {
	original := RuntimeConfig{
		Targets:     []TargetConfig{{ID: 1, Region: "r"}},
		DomainRules: []DomainRule{{ID: 1, Host: "a.com", Targets: []int{1, 2}}},
	}
	copied := original.DeepCopy()

	original.Targets[0].Region = "changed"
	original.DomainRules[0].Targets[0] = 99
	original.DomainRules[0].Host = "changed"

	if copied.Targets[0].Region != "r" {
		t.Errorf("目标切片未深拷贝: %+v", copied.Targets)
	}
	if copied.DomainRules[0].Targets[0] != 1 || copied.DomainRules[0].Host != "a.com" {
		t.Errorf("规则内层切片未深拷贝: %+v", copied.DomainRules)
	}
}

// TestReplaceBusinessSettingsTxRequiresFullKeySet 导入写设置必须提供完整键集合。
func TestReplaceBusinessSettingsTxRequiresFullKeySet(t *testing.T) {
	store := newSnapshotStore(t)
	ctx := context.Background()

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 失败: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := store.ReplaceBusinessSettingsTx(ctx, tx, map[string]string{"tag": "x"}); err == nil {
		t.Fatalf("缺少键时必须报错")
	}

	full := make(map[string]string, len(settingsKeysV3))
	for _, k := range settingsKeysV3 {
		full[k] = "v-" + k
	}
	if err := store.ReplaceBusinessSettingsTx(ctx, tx, full); err != nil {
		t.Fatalf("完整键集合写入失败: %v", err)
	}
	got, err := store.GetSettingsTx(ctx, tx)
	if err != nil {
		t.Fatalf("GetSettingsTx 失败: %v", err)
	}
	if got["tag"] != "v-tag" || len(got) != len(settingsKeysV3) {
		t.Errorf("设置写入不完整: %+v", got)
	}
}

// TestDeleteImportOwnedTablesTxClearsFixedSet 导入清表只清固定业务表，
// 保留 sync_logs 与 sqlite_sequence（Build6 §12.12）。
func TestDeleteImportOwnedTablesTxClearsFixedSet(t *testing.T) {
	store := newSnapshotStore(t)
	ctx := context.Background()

	addTargetTxForTest(t, store, TargetConfig{CloudType: CloudTCLighthouse, Region: "r", ResourceID: "i"})
	addRuleTxForTest(t, store, DomainRule{Host: "a.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT"})
	if err := store.SetSetting("tag", "t"); err != nil {
		t.Fatalf("预置设置失败: %v", err)
	}
	if err := store.SaveAlertEmail(&AlertEmailConfig{Security: "auto_starttls", Host: "smtp.example.com", Port: "587"}); err != nil {
		t.Fatalf("预置邮件告警失败: %v", err)
	}
	if err := store.AddSyncLog(SyncLog{Timestamp: time.Now(), Target: "t", Domain: "a.com", Result: "success"}); err != nil {
		t.Fatalf("预置同步日志失败: %v", err)
	}

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 失败: %v", err)
	}
	if err := store.DeleteImportOwnedTablesTx(ctx, tx); err != nil {
		t.Fatalf("DeleteImportOwnedTablesTx 失败: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("提交失败: %v", err)
	}

	targets, err := store.GetTargets()
	if err != nil {
		t.Fatalf("GetTargets 失败: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("目标未清空: %+v", targets)
	}
	settings, err := store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if len(settings) != 0 {
		t.Errorf("设置未清空: %+v", settings)
	}
	logs, err := store.GetSyncLogs(10)
	if err != nil {
		t.Fatalf("GetSyncLogs 失败: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("sync_logs 必须保留，实际 %d 条", len(logs))
	}
	if _, err := store.GetAlertEmail(); err != nil {
		t.Fatalf("GetAlertEmail 失败: %v", err)
	}
}
