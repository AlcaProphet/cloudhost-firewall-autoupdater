package api

import (
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// 本文件是 Build9 Step 4 预览标识生命周期与 Step 5 Take/Lookup 接口回归。

func s4RegistryConfig() config.RuntimeConfig {
	rc := config.RuntimeConfig{
		Targets: []config.TargetConfig{
			{ID: 1, CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "lhins-a"},
			{ID: 2, CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "lhins-a"},
			{ID: 3, CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-a"},
		},
		EdgeOneSites: []config.EdgeOneSite{
			{ZoneID: "zone-a", TargetIDs: []int{1}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4"},
		},
		DomainRules: []config.DomainRule{
			{ID: 1, Host: "a.example", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{1}},
		},
		Tag:        "auto-ddns",
		EdgeOneTag: "edgeone",
	}
	rc.SetDNSFeatureEnabled(true)
	return rc
}

func s4RegistryCredentials() provider.Credentials {
	return provider.Credentials{
		TencentSecretID:       "tc-id",
		TencentSecretKey:      "tc-key",
		AliyunAccessKeyID:     "ali-id",
		AliyunAccessKeySecret: "ali-key",
	}
}

func s4RegistryState(rc config.RuntimeConfig, creds provider.Credentials) *syncer.RuntimeState {
	return &syncer.RuntimeState{
		Config: rc,
		Pool:   provider.NewClientPool(creds),
	}
}

func s4CreateRecord(t *testing.T, registry *CleanupPreviewRegistry, state *syncer.RuntimeState, scope string, ids []int) CleanupPreviewRecord {
	t.Helper()
	digest, fingerprint, err := cleanupScopeDigest(state, ids)
	if err != nil {
		t.Fatalf("计算范围摘要失败: %v", err)
	}
	record, err := registry.Create(CleanupPreviewRecord{
		Tag:                   state.Config.EdgeOneTag,
		Scope:                 scope,
		SelectedTargetIDs:     append([]int(nil), ids...),
		Digest:                digest,
		CredentialFingerprint: fingerprint,
		Targets: []CleanupPreviewTarget{{
			TargetIDs: []int{1},
			Rows: []CleanupPreviewRow{{
				Status:         "deletable",
				FunctionalKeys: []string{"key-a"},
			}},
		}},
		Warnings: []string{"warning-a"},
	})
	if err != nil {
		t.Fatalf("创建预览记录失败: %v", err)
	}
	return record
}

func TestBuild9S4PreviewTakeConfigChanges(t *testing.T) {
	baseRC := s4RegistryConfig()
	base := s4RegistryState(baseRC, s4RegistryCredentials())

	cases := []struct {
		name   string
		mutate func(*config.RuntimeConfig, *provider.Credentials)
	}{
		{
			name: "tag",
			mutate: func(rc *config.RuntimeConfig, _ *provider.Credentials) {
				rc.EdgeOneTag = "edgeone2"
			},
		},
		{
			name: "credentials",
			mutate: func(_ *config.RuntimeConfig, creds *provider.Credentials) {
				creds.TencentSecretKey = "changed"
			},
		},
		{
			name: "site-binding",
			mutate: func(rc *config.RuntimeConfig, _ *provider.Credentials) {
				rc.EdgeOneSites[0].Ports = "80"
			},
		},
		{
			name: "dns-rule",
			mutate: func(rc *config.RuntimeConfig, _ *provider.Credentials) {
				rc.DomainRules[0].Ports = "8443"
			},
		},
		{
			name: "target-identity",
			mutate: func(rc *config.RuntimeConfig, _ *provider.Credentials) {
				rc.Targets[0].ResourceID = "lhins-b"
			},
		},
		{
			name: "alias-added",
			mutate: func(rc *config.RuntimeConfig, _ *provider.Credentials) {
				rc.Targets = append(rc.Targets, config.TargetConfig{
					ID: 4, CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "lhins-a",
				})
			},
		},
		{
			name: "selected-target-removed",
			mutate: func(rc *config.RuntimeConfig, _ *provider.Credentials) {
				rc.Targets = []config.TargetConfig{rc.Targets[1], rc.Targets[2]}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry := NewCleanupPreviewRegistry()
			record := s4CreateRecord(t, registry, base, cleanupScopeSelected, []int{1})
			rc := baseRC.DeepCopy()
			creds := s4RegistryCredentials()
			tc.mutate(&rc, &creds)
			changed := s4RegistryState(rc, creds)

			got, reason := registry.Take(record.ID, changed)
			if reason != CleanupPreviewConfigChanged {
				t.Fatalf("Take reason = %q, want %q", reason, CleanupPreviewConfigChanged)
			}
			if got.Status != cleanupPreviewInvalidated || got.ConsumedAt != nil {
				t.Fatalf("配置变化必须标为失效且不消费: %+v", got)
			}
			if _, reason := registry.Take(record.ID, base); reason != CleanupPreviewInvalidated {
				t.Fatalf("失效预览不得因配置恢复而复活，实际 %q", reason)
			}
			if _, reason := registry.Lookup(record.ID); reason != CleanupPreviewInvalidated {
				t.Fatalf("Lookup 必须返回 invalidated，实际 %q", reason)
			}
		})
	}
}

func TestBuild9S4PreviewTakeAllModeTargetSet(t *testing.T) {
	baseRC := s4RegistryConfig()
	base := s4RegistryState(baseRC, s4RegistryCredentials())
	allIDs := []int{1, 2, 3}

	registry := NewCleanupPreviewRegistry()
	record := s4CreateRecord(t, registry, base, cleanupScopeAll, allIDs)

	changedRC := baseRC.DeepCopy()
	changedRC.Targets = append(changedRC.Targets, config.TargetConfig{
		ID: 4, CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "lhins-new",
	})
	changed := s4RegistryState(changedRC, s4RegistryCredentials())
	if _, reason := registry.Take(record.ID, changed); reason != CleanupPreviewConfigChanged {
		t.Fatalf("all 模式新增目标必须失效，实际 %q", reason)
	}

	// 明确选择一个目标时，新增无关目标不应使预览失效。
	selectedRegistry := NewCleanupPreviewRegistry()
	selected := s4CreateRecord(t, selectedRegistry, base, cleanupScopeSelected, []int{1})
	if _, reason := selectedRegistry.Take(selected.ID, changed); reason != "" {
		t.Fatalf("selected 模式下无关目标变化不应失效，实际 %q", reason)
	}
}

func TestBuild9S4PreviewTakeExpiryAndConsume(t *testing.T) {
	registry := NewCleanupPreviewRegistry()
	base := time.Date(2026, 11, 18, 10, 0, 0, 0, time.Local)
	registry.now = func() time.Time { return base }
	state := s4RegistryState(s4RegistryConfig(), s4RegistryCredentials())

	record := s4CreateRecord(t, registry, state, cleanupScopeSelected, []int{1})
	if want := base.Add(cleanupPreviewTTL); !record.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", record.ExpiresAt, want)
	}
	if got, reason := registry.Lookup(record.ID); reason != "" || got.Status != cleanupPreviewActive {
		t.Fatalf("未过期 Lookup 应 active，实际 reason=%q status=%q", reason, got.Status)
	}

	registry.now = func() time.Time { return base.Add(cleanupPreviewTTL + time.Second) }
	if _, reason := registry.Take(record.ID, state); reason != CleanupPreviewExpired {
		t.Fatalf("过期 Take reason = %q, want %q", reason, CleanupPreviewExpired)
	}
	if _, reason := registry.Lookup(record.ID); reason != CleanupPreviewExpired {
		t.Fatalf("过期 Lookup reason = %q", reason)
	}

	// 新预览：Lookup 不消费，Take 只成功一次。
	registry.now = func() time.Time { return base }
	second := s4CreateRecord(t, registry, state, cleanupScopeSelected, []int{1})
	if _, reason := registry.Lookup(second.ID); reason != "" {
		t.Fatalf("Lookup 不得消费: %q", reason)
	}
	got, reason := registry.Take(second.ID, state)
	if reason != "" {
		t.Fatalf("首次 Take reason = %q", reason)
	}
	if got.Status != cleanupPreviewConsumed || got.ConsumedAt == nil {
		t.Fatalf("首次 Take 必须消费: %+v", got)
	}
	if _, reason := registry.Take(second.ID, state); reason != CleanupPreviewConsumed {
		t.Fatalf("重复 Take reason = %q, want %q", reason, CleanupPreviewConsumed)
	}
	if _, reason := registry.Lookup(second.ID); reason != CleanupPreviewConsumed {
		t.Fatalf("重复 Lookup reason = %q", reason)
	}
}

func TestBuild9S4PreviewTakeUnavailableAndUnknown(t *testing.T) {
	registry := NewCleanupPreviewRegistry()
	state := s4RegistryState(s4RegistryConfig(), s4RegistryCredentials())
	record := s4CreateRecord(t, registry, state, cleanupScopeSelected, []int{1})

	if _, reason := registry.Take(record.ID, nil); reason != CleanupPreviewUnavailable {
		t.Fatalf("nil state reason = %q, want %q", reason, CleanupPreviewUnavailable)
	}
	if got, reason := registry.Lookup(record.ID); reason != "" || got.Status != cleanupPreviewActive {
		t.Fatalf("不可用不得消费记录: reason=%q status=%q", reason, got.Status)
	}
	if _, reason := registry.Take(record.ID, &syncer.RuntimeState{Config: state.Config}); reason != CleanupPreviewUnavailable {
		t.Fatalf("nil Pool reason = %q, want %q", reason, CleanupPreviewUnavailable)
	}
	if _, reason := registry.Take(record.ID, state); reason != "" {
		t.Fatalf("恢复可用后应成功，实际 %q", reason)
	}
	if _, reason := registry.Take("missing", state); reason != CleanupPreviewNotFound {
		t.Fatalf("未知 ID reason = %q, want %q", reason, CleanupPreviewNotFound)
	}
}

func TestBuild9S4PreviewTakeReturnsDeepCopy(t *testing.T) {
	registry := NewCleanupPreviewRegistry()
	state := s4RegistryState(s4RegistryConfig(), s4RegistryCredentials())
	record := s4CreateRecord(t, registry, state, cleanupScopeSelected, []int{1})

	taken, reason := registry.Take(record.ID, state)
	if reason != "" {
		t.Fatalf("Take reason = %q", reason)
	}
	taken.Targets[0].TargetIDs[0] = 99
	taken.Targets[0].Rows[0].FunctionalKeys[0] = "mutated"
	taken.Warnings[0] = "mutated"

	stored, reason := registry.Lookup(record.ID)
	if reason != CleanupPreviewConsumed {
		t.Fatalf("Lookup reason = %q", reason)
	}
	if stored.Targets[0].TargetIDs[0] != 1 || stored.Targets[0].Rows[0].FunctionalKeys[0] != "key-a" || stored.Warnings[0] != "warning-a" {
		t.Fatalf("Take 必须返回深拷贝，注册表被外部修改: %+v", stored)
	}
}

func TestBuild9S4PreviewDigestStableAndAliasesIncluded(t *testing.T) {
	baseRC := s4RegistryConfig()
	base := s4RegistryState(baseRC, s4RegistryCredentials())
	baseDigest, _, err := cleanupScopeDigest(base, []int{1})
	if err != nil {
		t.Fatalf("base digest: %v", err)
	}
	reorderedRC := baseRC.DeepCopy()
	reorderedRC.Targets = []config.TargetConfig{reorderedRC.Targets[2], reorderedRC.Targets[1], reorderedRC.Targets[0]}
	reorderedRC.DomainRules[0].Targets = []int{1}
	reordered := s4RegistryState(reorderedRC, s4RegistryCredentials())
	reorderedDigest, _, err := cleanupScopeDigest(reordered, []int{1, 1})
	if err != nil {
		t.Fatalf("reordered digest: %v", err)
	}
	if baseDigest != reorderedDigest {
		t.Fatal("目标顺序、重复选择与别名顺序不得改变范围摘要")
	}

	scope, err := cleanupScopeConfig(base, []int{1})
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	if len(scope.Targets) != 1 || !slicesEqualInt(scope.Targets[0].TargetIDs, []int{1, 2}) {
		t.Fatalf("选择任一边名都必须纳入全部别名: %+v", scope.Targets)
	}

	changedRC := baseRC.DeepCopy()
	changedRC.EdgeOneSites[0].Ports = "80"
	changed := s4RegistryState(changedRC, s4RegistryCredentials())
	changedDigest, _, err := cleanupScopeDigest(changed, []int{1})
	if err != nil {
		t.Fatalf("changed digest: %v", err)
	}
	if changedDigest == baseDigest {
		t.Fatal("站点变化必须改变范围摘要")
	}

	otherCreds := s4RegistryCredentials()
	otherCreds.AliyunAccessKeySecret = "changed"
	other := s4RegistryState(baseRC, otherCreds)
	credDigest, _, err := cleanupScopeDigest(other, []int{1})
	if err != nil {
		t.Fatalf("cred digest: %v", err)
	}
	if credDigest == baseDigest {
		t.Fatal("凭据变化必须改变范围摘要")
	}

	if _, _, err := cleanupScopeDigest(base, []int{999}); err == nil {
		t.Fatal("不存在的目标必须使摘要计算失败")
	}
}
