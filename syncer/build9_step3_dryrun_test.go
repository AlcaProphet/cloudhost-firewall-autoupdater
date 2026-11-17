package syncer

import (
	"errors"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 本文件验证 Build9 Step 3 的只读模拟：正式同一计划、未知证据、假设启用和
// 零云写/零状态写。

func TestBuild9S3DryRunCompressionMatchesFormalNoSideEffects(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0)
	site := step3Site("zone-a", "TCP", "80,443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	store := newStep3CompressionStore()
	st := step3State(t, p, site, step3Settings(true, 0), teo)
	s := step3Syncer(t, st, store)

	resp, err := s.DryRun()
	if err != nil {
		t.Fatalf("DryRun 失败: %v", err)
	}
	if len(resp.ExecutionUnits) != 1 {
		t.Fatalf("执行单元数量 = %d", len(resp.ExecutionUnits))
	}
	unit := resp.ExecutionUnits[0]
	if !unit.Compression.Applicable || !unit.Compression.Enabled || unit.Compression.Assumed {
		t.Fatalf("压缩预览标志错误: %+v", unit.Compression)
	}
	if unit.Compression.Mode != provider.CompressionModeNormal || unit.Compression.AddRows != 1 ||
		unit.PlannedAdd != 1 || unit.TransitionPeak != 1 || unit.CoverageReady {
		t.Fatalf("压缩预览容量错误: %+v unit=%+v", unit.Compression, unit)
	}
	if len(unit.Compression.Rows) != 1 || unit.Compression.Rows[0].Port != "80,443" {
		t.Fatalf("压缩预览行错误: %+v", unit.Compression.Rows)
	}
	if _, creates, deletes := p.counts(); creates != 0 || deletes != 0 {
		t.Fatalf("DryRun 不得写云: create=%d delete=%d", creates, deletes)
	}
	if _, confirms := teo.counts(); confirms != 0 {
		t.Fatalf("DryRun 不得确认: %d", confirms)
	}
	if store.saves != 0 || store.deletes != 0 {
		t.Fatalf("DryRun 不得写状态: saves=%d deletes=%d", store.saves, store.deletes)
	}

	// 同一 S0/站点/额度下正式 attempt 必须产生相同物理行。
	p2 := newProbeProvider(config.CloudTCLighthouse, 0)
	st2 := step3State(t, p2, site, step3Settings(true, 0), teo)
	s2 := step3Syncer(t, st2, newStep3CompressionStore())
	s2.runScheduledRound(st2, false, true)
	if len(p2.rules) != 1 {
		t.Fatalf("正式新增行数 = %d", len(p2.rules))
	}
	got := p2.rules[0]
	want := unit.Compression.Rows[0]
	if got.Protocol != want.Protocol || got.Port != want.Port || got.CidrBlock != want.CIDR {
		t.Fatalf("模拟与正式计划不一致: formal=%+v dryrun=%+v", got, want)
	}
}

func TestBuild9S3DryRunDisabledShowsAssumed(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0)
	site := step3Site("zone-a", "TCP", "80,443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step3State(t, p, site, step3Settings(false, 0), teo)
	s := step3Syncer(t, st, newStep3CompressionStore())

	resp, err := s.DryRun()
	if err != nil {
		t.Fatalf("DryRun 失败: %v", err)
	}
	unit := resp.ExecutionUnits[0]
	if unit.Compression.Enabled || !unit.Compression.Assumed {
		t.Fatalf("关闭态应显示假设启用: %+v", unit.Compression)
	}
	if len(unit.Compression.Rows) == 0 || unit.Compression.AddRows == 0 {
		t.Fatalf("假设启用测算应包含压缩行: %+v", unit.Compression)
	}
	if unit.PlannedAdd != 2 || unit.TransitionPeak != 2 || unit.Compression.OtherNewRows != 0 || unit.Compression.Peak == nil || *unit.Compression.Peak != 1 {
		t.Fatalf("现行计划仍应按关闭态展示精确新增: %+v", unit)
	}
	if _, creates, deletes := p.counts(); creates != 0 || deletes != 0 {
		t.Fatalf("DryRun 不得写云: create=%d delete=%d", creates, deletes)
	}
}

func TestBuild9S3DryRunUnknownState(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0)
	site := step3Site("zone-a", "TCP", "443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	store := newStep3CompressionStore()
	store.loadErr = errors.New("fixture")
	st := step3State(t, p, site, step3Settings(true, 0), teo)
	s := step3Syncer(t, st, store)

	resp, err := s.DryRun()
	if err != nil {
		t.Fatalf("DryRun 失败: %v", err)
	}
	unit := resp.ExecutionUnits[0]
	if unit.Compression.PendingKnown || unit.DataComplete {
		t.Fatalf("状态读取失败应显示未知: compression=%+v data_complete=%v", unit.Compression, unit.DataComplete)
	}
	if _, creates, deletes := p.counts(); creates != 0 || deletes != 0 {
		t.Fatalf("DryRun 不得写云: create=%d delete=%d", creates, deletes)
	}
}

func TestBuild9S3DryRunSameRoundDDNSCompetesForBudget(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0)
	site := step3Site("zone-a", "TCP", "80")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step8State(t, true, true, time.Hour, time.Hour, p, []config.EdgeOneSite{site})
	st.Config.SyncEnabled = false
	st.Config.LighthouseCompression = []config.LighthouseCompression{step3Settings(true, 0)}
	st.TEO = teo
	s := step3Syncer(t, st, newStep3CompressionStore())

	resp, err := s.DryRun()
	if err != nil {
		t.Fatalf("DryRun 失败: %v", err)
	}
	unit := resp.ExecutionUnits[0]
	if unit.Compression.OtherNewRows != 1 || unit.PlannedAdd != 2 || unit.TransitionPeak != 2 {
		t.Fatalf("同轮 DDNS 必须计入常态预算与峰值: %+v", unit.Compression)
	}
}

func TestBuild9S3DryRunNarrowedResidualIsVisible(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0,
		config.RuleInfo{Protocol: "TCP", Port: "443", CidrBlock: "203.0.0.0/16", Action: "ACCEPT", Description: "[edgeone]"},
	)
	site := step3Site("zone-a", "TCP", "443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step3State(t, p, site, step3Settings(true, 0), teo)
	s := step3Syncer(t, st, newStep3CompressionStore())

	resp, err := s.DryRun()
	if err != nil {
		t.Fatalf("DryRun 失败: %v", err)
	}
	unit := resp.ExecutionUnits[0]
	if unit.Compression.Mode != provider.CompressionModeNormal || unit.Compression.AddRows != 1 || unit.CoverageReady {
		t.Fatalf("收窄应允许精确替换并显示当前未达标: %+v", unit.Compression)
	}
	if unit.Compression.ExtraIPv4 == nil || *unit.Compression.ExtraIPv4 == 0 {
		t.Fatalf("应显示实际额外放行残留: %+v", unit.Compression)
	}
	if unit.Compression.ResidualRows != 1 {
		t.Fatalf("应显示压缩残留行: %+v", unit.Compression)
	}
	if _, creates, deletes := p.counts(); creates != 0 || deletes != 0 {
		t.Fatalf("DryRun 不得写云: create=%d delete=%d", creates, deletes)
	}
}

func TestBuild9S3DryRunUnknownPeakForUnavailableSite(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0)
	teo := &step10TEO{describeFn: func() (provider.OriginACLSnapshot, error) {
		return provider.OriginACLSnapshot{}, errors.New("fixture: site unavailable")
	}}
	for _, enabled := range []bool{true, false} {
		st := step3State(t, p, step3Site("zone-a", "TCP", "443"), step3Settings(enabled, 0), teo)
		resp, err := step3Syncer(t, st, newStep3CompressionStore()).DryRun()
		if err != nil {
			t.Fatal(err)
		}
		unit := resp.ExecutionUnits[0]
		if unit.DataComplete || unit.CoverageReady || unit.Compression.Peak != nil || unit.Compression.ExtraIPv4 != nil ||
			len(unit.Compression.Blockers) == 0 {
			t.Fatalf("enabled=%v: 站点输入不足时峰值和风险均未知: %+v", enabled, unit)
		}
	}
}
