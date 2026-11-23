//go:build step16

package process

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/webui/api"
)

// Build9 Step 7 沿用独立 step16 构建标签与正式入口二进制；无真实云请求。
func step7JSON(t *testing.T, f *step16Fixture, method, path string, payload any, want int, out any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, f.url(path), strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s %s = %d want %d: %s", method, path, resp.StatusCode, want, body)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			t.Fatalf("decode %s: %v: %s", path, err, body)
		}
	}
}

func step7Compression(t *testing.T, f *step16Fixture, enabled bool, allowance int64) {
	t.Helper()
	step7JSON(t, f, "PUT", fmt.Sprintf("/api/targets/%d/lighthouse-compression", f.targetIDs[0]), map[string]any{"enabled": enabled, "extra_ipv4_allow": allowance}, 200, nil)
}

func step7ManualRows(n int) []step16LHRule {
	rows := make([]step16LHRule, n)
	for i := range rows {
		rows[i] = step16LHRule{Protocol: "TCP", Port: fmt.Sprint(10000 + i), Cidr: "192.0.2.0/24", Action: "ACCEPT", Description: "manual"}
	}
	return rows
}

func step7MutateInstance(f *step16Fixture, fn func(*step16Instance)) {
	f.cloud.mu.Lock()
	defer f.cloud.mu.Unlock()
	inst := f.cloud.instances["lhins-step16-a"]
	fn(inst)
	inst.Version++
}

func step7Round(t *testing.T, f *step16Fixture, resume bool) syncer.SyncStatus {
	t.Helper()
	before, err := f.status()
	if err != nil {
		t.Fatal(err)
	}
	if resume {
		if code := f.resume(); code != 200 {
			t.Fatalf("resume=%d", code)
		}
	} else if code := f.trigger(); code != 202 {
		t.Fatalf("trigger=%d", code)
	}
	f.waitRoundAfter(before.LastSync, 35*time.Second)
	// 目标汇总先于 TEO 确认发布；等待来源完成信号才断言确认/过渡状态。
	return f.waitSourceCompletedAfter(before.EdgeOne.LastCompletedAt, 35*time.Second)
}

func step7Pending(t *testing.T, f *step16Fixture, want bool) {
	t.Helper()
	var c struct {
		Enabled bool `json:"enabled"`
		Pending bool `json:"pending_review"`
	}
	step7JSON(t, f, "GET", fmt.Sprintf("/api/targets/%d/lighthouse-compression", f.targetIDs[0]), nil, 200, &c)
	if c.Enabled || c.Pending != want {
		t.Fatalf("compression disabled pending=%+v want %v", c, want)
	}
	store, err := config.OpenStore(filepath.Join(f.dataDir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state, found, err := store.LoadLighthouseCompressionState(config.LighthouseCompression{CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "lhins-step16-a"})
	if err != nil || (found && state.PendingReview) != want {
		t.Fatalf("SQLite pending=%+v found=%v err=%v", state, found, err)
	}
}

func TestBuild9S7CompressionLifecycle(t *testing.T) {
	f := newStep16Fixture(t)
	f.cloud.setZone("zone-step7", step16Zone{CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/32", "203.0.113.2/32"}})
	f.cloud.setInstance("lhins-step16-a", step16Instance{Rules: step7ManualRows(95)})
	f.setup(1, func(t *testing.T, s *config.Store, ids []int) {
		if err := s.SetSetting("dns_enabled", "false"); err != nil {
			t.Fatal(err)
		}
	})
	f.start()
	// 绑定经正式 HTTP 提交，保持默认关闭。
	step7JSON(t, f, "POST", "/api/edgeone/sites", map[string]any{"zone_id": "zone-step7", "target_ids": f.targetIDs, "protocol": "TCP", "ports": "80,443", "address_family": "ipv4"}, 201, nil)
	var initial struct {
		Enabled bool `json:"enabled"`
	}
	step7JSON(t, f, "GET", fmt.Sprintf("/api/targets/%d/lighthouse-compression", f.targetIDs[0]), nil, 200, &initial)
	if initial.Enabled {
		t.Fatal("binding enabled compression")
	}
	step7Compression(t, f, true, 2)
	before, _ := f.status()
	preview, code := f.dryRun()
	if code != 200 || len(preview.ExecutionUnits) != 1 {
		t.Fatalf("dryrun=%d %+v", code, preview)
	}
	c := preview.ExecutionUnits[0].Compression
	if !c.Enabled || c.Assumed || c.AddRows != 1 || c.Peak == nil || *c.Peak != 96 || c.ExtraIPv4 == nil || *c.ExtraIPv4 != 2 {
		t.Fatalf("lossy plan=%+v", c)
	}
	if _, create, del := f.cloud.totalLHCounts(); create+del != 0 {
		t.Fatal("dryrun wrote cloud")
	}
	if n, _ := f.countRows("sync_logs"); n != 0 {
		t.Fatal("dryrun wrote sync_logs")
	}
	after, _ := f.status()
	if before.LastSync != nil || after.LastSync != nil {
		t.Fatal("dryrun advanced formal round")
	}
	round := step7Round(t, f, true)
	if round.LastRound.Added != 1 {
		t.Fatalf("formal=%+v", round.LastRound)
	}
	inst, _ := f.cloud.instanceSnapshot("lhins-step16-a")
	if len(inst.Rules) != 96 || inst.CreateCalls != 1 || inst.DeleteCalls != 0 {
		t.Fatalf("formal snapshot=%+v", inst)
	}
	owned := inst.Rules[len(inst.Rules)-1]
	if owned.Cidr != "203.0.113.0/30" || owned.Port != "80,443" || !strings.HasPrefix(owned.Description, "[edgeone]") {
		t.Fatalf("wire row=%+v", owned)
	}
	if logs := f.waitSyncLogCount(1, 5*time.Second); len(logs) != 1 || logs[0].Added != 1 {
		t.Fatalf("logs=%+v", logs)
	}
	// 同一二进制/SQLite 重启保留实例设置，无新增重复物理行。
	f.pause()
	f.restart()
	var saved struct {
		Enabled   bool  `json:"enabled"`
		Allowance int64 `json:"extra_ipv4_allow"`
	}
	step7JSON(t, f, "GET", fmt.Sprintf("/api/targets/%d/lighthouse-compression", f.targetIDs[0]), nil, 200, &saved)
	if !saved.Enabled || saved.Allowance != 2 {
		t.Fatalf("restart=%+v", saved)
	}
	step7MutateInstance(f, func(i *step16Instance) { i.Rules = append(i.Rules, step7ManualRows(99)[95:]...) })
	step7Compression(t, f, false, 0)
	step7Pending(t, f, true)
	step7Round(t, f, true)
	inst, _ = f.cloud.instanceSnapshot("lhins-step16-a")
	if len(inst.Rules) != 100 || inst.CreateCalls != 1 || inst.DeleteCalls != 0 {
		t.Fatalf("full quota must keep old=%+v", inst)
	}
	step7Pending(t, f, true)
	residual, code := f.dryRun()
	if code != 200 || !residual.ExecutionUnits[0].Compression.PendingReview || residual.ExecutionUnits[0].Compression.ResidualRows == 0 {
		t.Fatalf("residual=%+v", residual)
	}
	// 外部释放七条手工行；仍须先增加精确覆盖再撤除旧 /30。
	step7MutateInstance(f, func(i *step16Instance) { i.Rules = i.Rules[7:] })
	round = step7Round(t, f, false)
	inst, _ = f.cloud.instanceSnapshot("lhins-step16-a")
	if len(inst.Rules) != 96 || inst.CreateCalls != 2 || inst.DeleteCalls != 1 || round.LastRound.Added != 4 || round.LastRound.Deleted != 1 {
		t.Fatalf("narrow=%+v counts=%d/%d rows=%d", round.LastRound, inst.CreateCalls, inst.DeleteCalls, len(inst.Rules))
	}
	for _, r := range inst.Rules {
		if r.Cidr == "203.0.113.0/30" {
			t.Fatal("lossy residue after narrowing")
		}
	}
	step7Pending(t, f, false)
	// 同一二进制/数据目录继续走手动清理及后续正式同步，闭合全流程。
	if f.pause() != 200 {
		t.Fatal("pause before cleanup failed")
	}
	id := step7Preview(t, f)
	op := step7Confirm(t, f, id, false)
	if !op.Cleared || len(op.Targets) != 1 || op.Targets[0].ConfirmedDeleted != 4 {
		t.Fatalf("same-process cleanup=%+v", op)
	}
	inst, _ = f.cloud.instanceSnapshot("lhins-step16-a")
	if len(inst.Rules) != 92 || inst.DeleteCalls != 2 {
		t.Fatalf("same-process cleanup rows=%d deletes=%d", len(inst.Rules), inst.DeleteCalls)
	}
	round = step7Round(t, f, true)
	inst, _ = f.cloud.instanceSnapshot("lhins-step16-a")
	if round.LastRound.Added != 4 || len(inst.Rules) != 96 || inst.CreateCalls != 3 || inst.DeleteCalls != 2 {
		t.Fatalf("same-process rebuild=%+v rows=%d writes=%d/%d", round.LastRound, len(inst.Rules), inst.CreateCalls, inst.DeleteCalls)
	}
	t.Logf("B9S7 compression: dry peak=96 extra=2; full100 kept old; release7 -> add4/delete1 final96; manual cleanup4 ->92; subsequent rebuild4 ->96; actions=%v", f.cloud.actionLog())
	f.stop()
}

func step7Preview(t *testing.T, f *step16Fixture) string {
	t.Helper()
	var p struct {
		ID string `json:"preview_id"`
	}
	step7JSON(t, f, "POST", "/api/edgeone/firewall-cleanup/preview", map[string]any{"all": true}, 200, &p)
	if p.ID == "" {
		t.Fatal("missing preview id")
	}
	return p.ID
}

func step7Confirm(t *testing.T, f *step16Fixture, id string, disable bool) api.CleanupOperation {
	t.Helper()
	var op api.CleanupOperation
	step7JSON(t, f, "POST", "/api/edgeone/firewall-cleanup", map[string]any{"preview_id": id, "disable_edgeone": disable}, 202, &op)
	var again api.CleanupOperation
	step7JSON(t, f, "POST", "/api/edgeone/firewall-cleanup", map[string]any{"preview_id": id, "disable_edgeone": !disable}, 200, &again)
	if again.OperationID != op.OperationID || again.DisableEdgeOne != disable {
		t.Fatal("duplicate confirmation changed authorization")
	}
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		step7JSON(t, f, "GET", "/api/edgeone/firewall-cleanup/"+op.OperationID, nil, 200, &op)
		if op.Status == "completed" || op.Status == "rejected" {
			return op
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("cleanup not finished: %+v", op)
	return op
}

func TestBuild9S7CleanupAndRebuild(t *testing.T) {
	f := newStep16Fixture(t)
	f.cloud.setZone("zone-cleanup7", step16Zone{CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24"}})
	f.setup(1, func(t *testing.T, s *config.Store, ids []int) {
		if err := s.SetSetting("dns_enabled", "false"); err != nil {
			t.Fatal(err)
		}
		step16AddSite(t, s, config.EdgeOneSite{ZoneID: "zone-cleanup7", TargetIDs: ids, Protocol: "TCP", Ports: "80,443", AddressFamily: "ipv4"})
	})
	f.start()
	step7Compression(t, f, true, 0)
	step7Round(t, f, true)
	f.pause()
	// 相似 TAG、DNS TAG 与手工规则始终保留。
	step7MutateInstance(f, func(i *step16Instance) {
		for n, desc := range []string{"[edgeone]foo", "[auto-ddns] old", "manual"} {
			i.Rules = append(i.Rules, step16LHRule{Protocol: "TCP", Port: fmt.Sprint(9000 + n), Cidr: "198.51.100.0/24", Action: "ACCEPT", Description: desc})
		}
	})
	_, create, del := f.cloud.totalLHCounts()
	td, cf := f.cloud.teoCounts("zone-cleanup7")
	id := step7Preview(t, f)
	if _, c, d := f.cloud.totalLHCounts(); c != create || d != del {
		t.Fatal("preview writes")
	}
	op := step7Confirm(t, f, id, false)
	if !op.Cleared || len(op.Targets) != 1 || op.Targets[0].ConfirmedDeleted != 1 || !op.Targets[0].ReadbackVerified || *op.Targets[0].RemainingRows != 0 {
		t.Fatalf("cleanup=%+v", op)
	}
	if d, c := f.cloud.teoCounts("zone-cleanup7"); d != td || c != cf {
		t.Fatal("cleanup queried or confirmed TEO")
	}
	inst, _ := f.cloud.instanceSnapshot("lhins-step16-a")
	if len(inst.Rules) != 3 || inst.CreateCalls != create || inst.DeleteCalls != del+1 {
		t.Fatalf("cleanup snapshot=%+v", inst)
	}
	step7Round(t, f, true)
	inst, _ = f.cloud.instanceSnapshot("lhins-step16-a")
	if len(inst.Rules) != 4 || inst.CreateCalls != create+1 {
		t.Fatal("enabled sync did not rebuild")
	}
	f.pause()
	id = step7Preview(t, f)
	op = step7Confirm(t, f, id, true)
	if !op.Cleared || op.SwitchAction != "disabled" {
		t.Fatalf("disable cleanup=%+v", op)
	}
	settings, _ := f.settings()
	if settings["edgeone_enabled"] != "false" {
		t.Fatal("switch not persisted")
	}
	_, create, del = f.cloud.totalLHCounts()
	if f.resume() != 200 || f.trigger() != 202 {
		t.Fatal("disabled source trigger rejected")
	}
	status, err := f.status()
	if err != nil || status.DNS.Effective || status.EdgeOne.Effective {
		t.Fatalf("disabled sources=%+v %v", status, err)
	}
	f.waitIdle(5 * time.Second)
	if _, c, d := f.cloud.totalLHCounts(); c != create || d != del {
		t.Fatal("disabled module rebuilt rules")
	}
	t.Logf("B9S7 cleanup: one confirmation, mixed TAG retained, enabled rebuild once, disabled zero rebuild; operations=%+v actions=%v", op, f.cloud.actionLog())
	f.stop()
}

func TestBuild9S7S1PortReadbackGate(t *testing.T) {
	f := newStep16Fixture(t)
	f.cloud.setZone("zone-port7", step16Zone{CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24"}, NextVer: "v2", NextV4: []string{"198.51.100.0/24"}})
	old := step16LHRule{Protocol: "TCP", Port: "80,443", Cidr: "203.0.113.0/24", Action: "ACCEPT", Description: "[edgeone] old"}
	f.cloud.setInstance("lhins-step16-a", step16Instance{Rules: []step16LHRule{old}, OmitPortOnDescribe: map[int]string{2: "80"}})
	f.setup(1, func(t *testing.T, s *config.Store, ids []int) {
		if err := s.SetSetting("dns_enabled", "false"); err != nil {
			t.Fatal(err)
		}
		step16AddSite(t, s, config.EdgeOneSite{ZoneID: "zone-port7", TargetIDs: ids, Protocol: "TCP", Ports: "80,443", AddressFamily: "ipv4"})
	})
	f.start()
	step7Compression(t, f, true, 0)
	round := step7Round(t, f, true)
	inst, _ := f.cloud.instanceSnapshot("lhins-step16-a")
	if round.LastRound.Outcome != syncer.RoundFailed || inst.CreateCalls != 1 || inst.DeleteCalls != 0 || !step16ContainsRule(inst.Rules, old) {
		t.Fatalf("S1 gate: round=%+v inst=%+v", round.LastRound, inst)
	}
	if _, cf := f.cloud.teoCounts("zone-port7"); cf != 0 {
		t.Fatal("missing port authorized TEO confirm")
	}
	if row, err := f.transition("zone-port7"); err != nil || (row != nil && row.ConfirmationState == config.EdgeOneConfirmationAccepted) {
		t.Fatalf("transition=%+v %v", row, err)
	}
	// 下轮完整回读恢复后才可确认，accepted 同指纹不重复发出。
	step7Round(t, f, false)
	if _, cf := f.cloud.teoCounts("zone-port7"); cf != 1 {
		t.Fatalf("recovery confirm=%d logs=%s", cf, f.out.String())
	}
	step7Round(t, f, false)
	if _, cf := f.cloud.teoCounts("zone-port7"); cf != 1 {
		t.Fatalf("duplicate confirm=%d", cf)
	}
	if _, _, del := f.cloud.totalLHCounts(); del != 0 {
		t.Fatal("transition protection removed old rules")
	}
	t.Logf("B9S7 S1 missing443: add once, delete0/confirm0; complete retry confirms once; accepted deduped")
	f.stop()
}

func TestBuild9S7CleanupWaitsForConfirmationAndPartialFailure(t *testing.T) {
	f := newStep16Fixture(t)
	f.cloud.setZone("zone-serial7", step16Zone{CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24"}, NextVer: "v2", NextV4: []string{"198.51.100.0/24"}, ConfirmMode: "block"})
	f.cloud.setInstance("lhins-step16-b", step16Instance{DeleteError: true})
	f.setup(2, func(t *testing.T, s *config.Store, ids []int) {
		if err := s.SetSetting("dns_enabled", "false"); err != nil {
			t.Fatal(err)
		}
		step16AddSite(t, s, config.EdgeOneSite{ZoneID: "zone-serial7", TargetIDs: ids, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4"})
	})
	f.start()
	// 两个目标完整覆盖后 TEO 确认在途，维护任务只排队。
	if code := f.resume(); code != 200 {
		t.Fatalf("resume=%d", code)
	}
	if !f.cloud.waitConfirmStarted("zone-serial7", 20*time.Second) {
		t.Fatal("confirmation not started")
	}
	t.Cleanup(func() { f.cloud.releaseConfirm("zone-serial7") })
	id := step7Preview(t, f)
	var op api.CleanupOperation
	step7JSON(t, f, "POST", "/api/edgeone/firewall-cleanup", map[string]any{"preview_id": id, "disable_edgeone": false}, 202, &op)
	step7JSON(t, f, "GET", "/api/edgeone/firewall-cleanup/"+op.OperationID, nil, 200, &op)
	if op.Status != "queued" {
		t.Fatalf("maintenance overlapped confirm: %+v", op)
	}
	if _, _, del := f.cloud.totalLHCounts(); del != 0 {
		t.Fatal("delete during TEO confirm")
	}
	f.cloud.releaseConfirm("zone-serial7")
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		step7JSON(t, f, "GET", "/api/edgeone/firewall-cleanup/"+op.OperationID, nil, 200, &op)
		if op.Status == "completed" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if op.Status != "completed" || op.Cleared || len(op.Targets) != 2 {
		t.Fatalf("partial=%+v", op)
	}
	first, second := op.Targets[0], op.Targets[1]
	if first.ConfirmedDeleted != 2 || !first.ReadbackVerified || first.RemainingRows == nil || *first.RemainingRows != 0 || second.ConfirmedDeleted != 0 || !second.ReadbackVerified || second.RemainingRows == nil || *second.RemainingRows != 2 {
		t.Fatalf("partial counts=%+v", op.Targets)
	}
	if _, cf := f.cloud.teoCounts("zone-serial7"); cf != 1 {
		t.Fatalf("cleanup confirmed again=%d", cf)
	}
	for _, resource := range []string{"lhins-step16-a", "lhins-step16-b"} {
		_, c, d := f.cloud.lhCounts(resource)
		if c != 1 || d != 1 {
			t.Fatalf("%s writes=%d/%d", resource, c, d)
		}
	}
	row, err := f.transition("zone-serial7")
	if err != nil || row == nil || row.ConfirmationState != config.EdgeOneConfirmationAccepted {
		t.Fatalf("cleanup lost accepted transition: %+v %v", row, err)
	}
	t.Logf("B9S7 serial: queued during confirm; first deleted2 second denied and remains2; accepted preserved; actions=%v", f.cloud.actionLog())
	f.stop()
}

func TestBuild9S7BorrowAndActivation(t *testing.T) {
	for _, manualCount := range []int{92, 93} {
		t.Run(fmt.Sprintf("manual%d", manualCount), func(t *testing.T) {
			f := newStep16Fixture(t)
			current := []string{"10.0.0.0/32", "10.0.0.2/32", "10.0.0.4/32", "10.0.0.6/32"}
			next := []string{"10.0.0.8/32", "10.0.0.10/32", "10.0.0.12/32", "10.0.0.14/32"}
			if manualCount == 93 {
				current = current[:3]
			}
			f.cloud.setZone("zone-borrow7", step16Zone{CurrentVer: "v1", CurrentV4: current, NextVer: "v2", NextV4: next})
			rows := step7ManualRows(manualCount)
			for _, cidr := range current {
				rows = append(rows, step16LHRule{Protocol: "TCP", Port: "ALL", Cidr: cidr, Action: "ACCEPT", Description: "[edgeone] current"})
			}
			f.cloud.setInstance("lhins-step16-a", step16Instance{Rules: rows})
			f.setup(1, func(t *testing.T, s *config.Store, ids []int) {
				if err := s.SetSetting("dns_enabled", "false"); err != nil {
					t.Fatal(err)
				}
				step16AddSite(t, s, config.EdgeOneSite{ZoneID: "zone-borrow7", TargetIDs: ids, Protocol: "TCP", Ports: "1-65535", AddressFamily: "ipv4"})
			})
			f.start()
			step7Compression(t, f, true, 0)
			preview, code := f.dryRun()
			if code != 200 || len(preview.ExecutionUnits) != 1 {
				t.Fatalf("preview=%+v", preview)
			}
			c := preview.ExecutionUnits[0].Compression
			round := step7Round(t, f, true)
			inst, _ := f.cloud.instanceSnapshot("lhins-step16-a")
			if manualCount == 93 {
				if inst.CreateCalls != 0 || inst.DeleteCalls != 0 || len(inst.Rules) != 96 || round.LastRound.Outcome != syncer.RoundPartial {
					t.Fatalf("insufficient capacity=%+v %+v", round.LastRound, inst)
				}
				if _, cf := f.cloud.teoCounts("zone-borrow7"); cf != 0 {
					t.Fatal("blocked borrow confirmed")
				}
				if c.Peak == nil || *c.Peak != 100 || c.ExpectedAfter == nil || *c.ExpectedAfter != 97 {
					t.Fatalf("blocked comparison must have peak100 and expected97: %+v", c)
				}
				t.Logf("B9S7 borrow blocked: initial96 + next4 =100, final97 cannot restore reserve; plan=%+v", c)
				f.stop()
				return
			}
			if c.Peak == nil || *c.Peak != 100 || c.ExpectedAfter == nil || *c.ExpectedAfter != 96 || c.Borrowed == nil || *c.Borrowed != 4 {
				t.Fatalf("borrow preview=%+v", c)
			}
			if len(inst.Rules) != 100 || inst.CreateCalls != 1 || inst.DeleteCalls != 0 || round.LastRound.Added != 4 {
				t.Fatalf("borrow=%+v %+v", round.LastRound, inst)
			}
			if _, cf := f.cloud.teoCounts("zone-borrow7"); cf != 1 {
				t.Fatalf("confirm=%d", cf)
			}
			f.stop()
			// 重启以后，可信查询证明新版实际生效才释放旧版四条。
			f.cloud.setZone("zone-borrow7", step16Zone{CurrentVer: "v2", CurrentV4: next})
			f.start()
			round = f.waitSourceCompletedAfter(nil, 35*time.Second)
			inst, _ = f.cloud.instanceSnapshot("lhins-step16-a")
			if len(inst.Rules) != 96 || inst.CreateCalls != 1 || inst.DeleteCalls != 1 || round.LastRound.Added != 0 || round.LastRound.Deleted != 4 {
				t.Fatalf("activation=%+v %+v", round.LastRound, inst)
			}
			if row, err := f.transition("zone-borrow7"); err != nil || row != nil {
				t.Fatalf("transition not released=%+v %v", row, err)
			}
			if _, cf := f.cloud.teoCounts("zone-borrow7"); cf != 1 {
				t.Fatal("restart repeated confirm")
			}
			preview, _ = f.dryRun()
			if restored := preview.ExecutionUnits[0].Compression.ReservedRestored; restored == nil || !*restored {
				t.Fatalf("reserve not restored: %+v", preview)
			}
			t.Logf("B9S7 borrow: 96->100, add4/delete0/confirm1; restart effective ->96 add0/delete4; actions=%v", f.cloud.actionLog())
			f.stop()
		})
	}
}
