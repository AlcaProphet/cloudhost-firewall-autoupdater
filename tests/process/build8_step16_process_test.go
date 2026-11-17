//go:build step16

package process

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// 本文件只由 `go test -tags step16` 执行。每个用例使用独立临时库、独立
// 回环替身和真实二进制；释放/清理项按实际 cloud 计数器与 SQLite 记录断言。

func TestStep16StartupAndConfig(t *testing.T) {
	t.Run("新库启动与空配置", func(t *testing.T) {
		f := newStep16Fixture(t)
		f.start()
		settings, err := f.settings()
		if err != nil {
			t.Fatal(err)
		}
		if settings["tag"] != "auto-ddns" {
			t.Fatalf("新库 DNS TAG = %q, want auto-ddns", settings["tag"])
		}
		code, body, err := f.health()
		if err != nil || code != http.StatusOK {
			t.Fatalf("operational health = %d %v body=%v", code, err, body)
		}
		targets := step16RawRequest(t, http.MethodGet, f.url("/api/targets"), "")
		if strings.TrimSpace(string(targets)) != "[]" && strings.TrimSpace(string(targets)) != "null" {
			t.Fatalf("空库 targets = %s", targets)
		}
		f.stop()
		if desc, create, del := f.cloud.totalLHCounts(); desc+create+del != 0 {
			t.Fatalf("空库出现 Lighthouse 调用: %d/%d/%d", desc, create, del)
		}
	})

	t.Run("旧库迁移一次且重启不重复", func(t *testing.T) {
		f := newStep16Fixture(t)
		step16CreateLegacyDB(t, f.dataDir, map[string]string{"tag": "auto-dns", "interval": "5m"})
		f.start()
		settings, err := f.settings()
		if err != nil {
			t.Fatal(err)
		}
		if settings["tag"] != "auto-dns" {
			t.Fatalf("旧库迁移后 tag = %q, want auto-dns", settings["tag"])
		}
		version1 := step16UserVersion(t, f.dataDir)
		f.stop()
		f.restart()
		settings, err = f.settings()
		if err != nil {
			t.Fatal(err)
		}
		if settings["tag"] != "auto-dns" {
			t.Fatalf("重启后 tag = %q, want auto-dns", settings["tag"])
		}
		if version2 := step16UserVersion(t, f.dataDir); version2 != version1 {
			t.Fatalf("重启后 user_version = %d, want %d", version2, version1)
		}
		f.stop()
		if desc, create, del := f.cloud.totalLHCounts(); desc+create+del != 0 {
			t.Fatalf("旧库启动出现 Lighthouse 调用: %d/%d/%d", desc, create, del)
		}
	})

	t.Run("间隔热更新与等价保存", func(t *testing.T) {
		f := newStep16Fixture(t)
		f.cloud.setZone("zone-interval", step16Zone{CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24"}})
		f.setup(1, func(t *testing.T, store *config.Store, ids []int) {
			step16AddRule(t, store, config.DomainRule{
				Host: "app.example.test", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: ids,
			})
			step16AddSite(t, store, config.EdgeOneSite{
				ZoneID: "zone-interval", TargetIDs: ids, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
			})
		})
		f.start()
		f.resume()
		before := f.waitRoundAfter(nil, 20*time.Second)
		if before.LastRound == nil || before.LastRound.Outcome != syncer.RoundSuccess {
			t.Fatalf("首轮未成功: %+v", before.LastRound)
		}
		if before.DNS.NextDueAt == nil || before.EdgeOne.NextDueAt == nil {
			t.Fatalf("首轮后缺少 next_due: %+v", before)
		}
		teoBefore, confirmBefore := f.cloud.teoCounts("zone-interval")
		if code := f.putSettings(map[string]string{"edgeone_interval": "36h"}); code != http.StatusOK {
			t.Fatalf("保存 36h = %d", code)
		}
		var after syncer.SyncStatus
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			after, _ = f.status()
			if after.EdgeOne.NextDueAt != nil && before.EdgeOne.NextDueAt != nil &&
				!after.EdgeOne.NextDueAt.Equal(*before.EdgeOne.NextDueAt) {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if after.EdgeOne.NextDueAt == nil || before.EdgeOne.NextDueAt.Equal(*after.EdgeOne.NextDueAt) {
			t.Fatalf("EdgeOne 间隔变化未重排: before=%v after=%v", before.EdgeOne.NextDueAt, after.EdgeOne.NextDueAt)
		}
		if after.DNS.NextDueAt == nil || !after.DNS.NextDueAt.Equal(*before.DNS.NextDueAt) {
			t.Fatalf("DNS 到期被 EdgeOne 间隔变更重置: before=%v after=%v", before.DNS.NextDueAt, after.DNS.NextDueAt)
		}
		if !after.LastSync.Equal(*before.LastSync) {
			t.Fatalf("间隔变更触发了额外正式轮次")
		}
		if desc, confirm := f.cloud.teoCounts("zone-interval"); desc != teoBefore || confirm != confirmBefore {
			t.Fatalf("间隔变更触发云调用: TEO %d/%d -> %d/%d", teoBefore, confirmBefore, desc, confirm)
		}
		edgeDue := *after.EdgeOne.NextDueAt
		dnsDue := *after.DNS.NextDueAt
		if code := f.putSettings(map[string]string{"edgeone_interval": "36h"}); code != http.StatusOK {
			t.Fatalf("等价保存 = %d", code)
		}
		time.Sleep(300 * time.Millisecond)
		after2, _ := f.status()
		if after2.EdgeOne.NextDueAt == nil || !after2.EdgeOne.NextDueAt.Equal(edgeDue) {
			t.Fatalf("等价保存重置了 EdgeOne 计时: %v -> %v", edgeDue, after2.EdgeOne.NextDueAt)
		}
		if after2.DNS.NextDueAt == nil || !after2.DNS.NextDueAt.Equal(dnsDue) {
			t.Fatalf("等价保存重置了 DNS 计时: %v -> %v", dnsDue, after2.DNS.NextDueAt)
		}
		f.stop()
	})
}

func TestStep16ConfigBundleImportExport(t *testing.T) {
	source := newStep16Fixture(t)
	source.setup(2, func(t *testing.T, store *config.Store, ids []int) {
		step16AddRule(t, store, config.DomainRule{
			Host: "roundtrip.example.test", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{ids[1]}, Comment: "跨实例",
		})
		step16AddSite(t, store, config.EdgeOneSite{
			ZoneID: "zone-roundtrip", TargetIDs: ids, Protocol: "TCP", Ports: "80,443", AddressFamily: "dual",
		})
	})
	source.start()
	exportBody := step16RawRequest(t, http.MethodPost, source.url("/api/config/export"), "")
	for _, fragment := range []string{`"version": 4`, `"edgeone_sites"`, `"zone-roundtrip"`, `"roundtrip.example.test"`} {
		if !strings.Contains(string(exportBody), fragment) {
			t.Fatalf("v4 导出缺少 %s", fragment)
		}
	}
	source.stop()

	// v4 跨实例导入：目标 ID 重映射、站点双绑定保留。
	dest := newStep16Fixture(t)
	dest.setup(5, nil)
	dest.start()
	if code := step16Request(t, http.MethodPost, dest.url("/api/config/import"), string(exportBody)); code != http.StatusOK {
		t.Fatalf("v4 导入 = %d", code)
	}
	var sites struct {
		Sites []config.EdgeOneSite `json:"sites"`
	}
	if err := step16GetJSON(dest.url("/api/edgeone/sites"), &sites); err != nil {
		t.Fatal(err)
	}
	if len(sites.Sites) != 1 || sites.Sites[0].ZoneID != "zone-roundtrip" || len(sites.Sites[0].TargetIDs) != 2 {
		t.Fatalf("v4 导入站点 = %+v", sites.Sites)
	}
	var targets []config.TargetConfig
	if err := step16GetJSON(dest.url("/api/targets"), &targets); err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("v4 导入目标数 = %d", len(targets))
	}
	resources := map[string]bool{}
	for _, target := range targets {
		resources[target.ResourceID] = true
	}
	if !resources["lhins-step16-a"] || !resources["lhins-step16-b"] {
		t.Fatalf("v4 导入目标资源 = %v", resources)
	}
	dest.stop()

	// v3：删除 v4 增量字段后严格导入；站点被清空、EdgeOne 关闭、DNS 设置保留。
	var bundle map[string]any
	if err := json.Unmarshal(exportBody, &bundle); err != nil {
		t.Fatal(err)
	}
	bundle["version"] = 3
	delete(bundle, "edgeone_sites")
	settings := bundle["settings"].(map[string]any)
	for _, key := range []string{"dns_enabled", "edgeone_enabled", "edgeone_tag", "edgeone_interval"} {
		delete(settings, key)
	}
	v3Body, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}

	v3dest := newStep16Fixture(t)
	v3dest.setup(2, nil)
	v3dest.start()
	if code := step16Request(t, http.MethodPost, v3dest.url("/api/config/import"), string(v3Body)); code != http.StatusOK {
		t.Fatalf("v3 导入 = %d", code)
	}
	var v3sites struct {
		Sites []config.EdgeOneSite `json:"sites"`
	}
	if err := step16GetJSON(v3dest.url("/api/edgeone/sites"), &v3sites); err != nil {
		t.Fatal(err)
	}
	if len(v3sites.Sites) != 0 {
		t.Fatalf("v3 导入后站点 = %+v", v3sites.Sites)
	}
	v3settings, err := v3dest.settings()
	if err != nil {
		t.Fatal(err)
	}
	if v3settings["edgeone_enabled"] != "false" || v3settings["dns_enabled"] != "true" {
		t.Fatalf("v3 导入后功能开关 = %+v", v3settings)
	}
	if v3settings["tag"] != "auto-ddns" || v3settings["interval"] != "5m" {
		t.Fatalf("v3 导入后 DNS 设置 = %+v", v3settings)
	}
	v3dest.stop()
	if desc, create, del := source.cloud.totalLHCounts(); desc+create+del != 0 {
		t.Fatalf("导入导出出现 Lighthouse 调用: %d/%d/%d", desc, create, del)
	}
}

func TestStep16ConflictOverlapAndRecovery(t *testing.T) {
	f := newStep16Fixture(t)
	f.cloud.setZone("zone-overlap", step16Zone{CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24"}})
	f.dns.setIP("203.0.113.7")
	f.setup(2, func(t *testing.T, store *config.Store, ids []int) {
		step16AddSite(t, store, config.EdgeOneSite{
			ZoneID: "zone-overlap", TargetIDs: []int{ids[0]}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
		})
		step16AddRule(t, store, config.DomainRule{
			Host: "overlap.example.test", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{ids[1]},
		})
	})
	f.start()
	if code := f.resume(); code != http.StatusOK {
		t.Fatalf("resume = %d", code)
	}
	first := f.waitSourceCompletedAfter(nil, 20*time.Second)
	row, err := f.conflict()
	if err != nil || row == nil {
		t.Fatalf("重叠后冲突记录 row=%+v err=%v", row, err)
	}
	if first.Preflight.State != syncer.PreflightStateOverlap || !first.Preflight.KnownConflict {
		t.Fatalf("预检状态 = %+v", first.Preflight)
	}
	code, health, err := f.health()
	if err != nil || code != http.StatusServiceUnavailable {
		t.Fatalf("重叠 health = %d %v %v", code, health, err)
	}
	td, cf := f.cloud.teoCounts("zone-overlap")
	lhD, lhC, lhX := f.cloud.totalLHCounts()
	if td != 1 || cf != 0 || lhD+lhC+lhX != 0 {
		t.Fatalf("resume 重叠计数 TEO=%d/%d LH=%d/%d/%d", td, cf, lhD, lhC, lhX)
	}
	t.Logf("STEP16 overlap resume: conflict=%+v health=%d reasons=%v counters TEO=%d/%d LH=%d/%d/%d", row, code, health["reasons"], td, cf, lhD, lhC, lhX)

	// 手动入口：仍为零云写入/确认。
	lastEdge := *first.EdgeOne.LastCompletedAt
	if code := f.trigger(); code != http.StatusAccepted {
		t.Fatalf("manual trigger = %d", code)
	}
	manual := f.waitSourceCompletedAfter(&lastEdge, 20*time.Second)
	td2, cf2 := f.cloud.teoCounts("zone-overlap")
	lhD2, lhC2, lhX2 := f.cloud.totalLHCounts()
	if td2 != td+1 || cf2 != 0 || lhD2+lhC2+lhX2 != 0 {
		t.Fatalf("manual 重叠计数 TEO=%d/%d LH=%d/%d/%d", td2, cf2, lhD2, lhC2, lhX2)
	}
	if manual.Preflight.State != syncer.PreflightStateOverlap {
		t.Fatalf("manual 预检 = %+v", manual.Preflight)
	}

	// 定时入口：缩短间隔后等待 ticker，不调用 trigger。
	if code := f.putSettings(map[string]string{"interval": "2s"}); code != http.StatusOK {
		t.Fatalf("保存短间隔 = %d", code)
	}
	lastDNS := *manual.DNS.LastCompletedAt
	scheduled := f.waitDNSCompletedAfter(&lastDNS, 10*time.Second)
	td3, cf3 := f.cloud.teoCounts("zone-overlap")
	lhD3, lhC3, lhX3 := f.cloud.totalLHCounts()
	// 仅 DNS 到期时复用有效 EdgeOne 缓存做预检，不强制新增 TEO 查询。
	if td3 != td2 || cf3 != 0 || lhD3+lhC3+lhX3 != 0 {
		t.Fatalf("scheduled 重叠计数 TEO=%d/%d LH=%d/%d/%d", td3, cf3, lhD3, lhC3, lhX3)
	}
	if scheduled.Preflight.State != syncer.PreflightStateOverlap {
		t.Fatalf("scheduled 预检 = %+v", scheduled.Preflight)
	}
	if logs := f.waitSyncLogCount(0, 300*time.Millisecond); len(logs) != 0 {
		t.Fatalf("阻断轮不应写目标同步日志: %+v", logs)
	}

	// 重启：同一临时库，冲突记录保留，启动轮仍零写入。
	f.restart()
	restarted := f.waitSourceCompletedAfter(nil, 20*time.Second)
	row, err = f.conflict()
	if err != nil || row == nil {
		t.Fatalf("重启后冲突记录丢失: %+v err=%v", row, err)
	}
	td4, cf4 := f.cloud.teoCounts("zone-overlap")
	lhD4, lhC4, lhX4 := f.cloud.totalLHCounts()
	if td4 != td3+1 || cf4 != 0 || lhD4+lhC4+lhX4 != 0 {
		t.Fatalf("重启重叠计数 TEO=%d/%d LH=%d/%d/%d", td4, cf4, lhD4, lhC4, lhX4)
	}
	if restarted.Preflight.State != syncer.PreflightStateOverlap {
		t.Fatalf("重启预检 = %+v", restarted.Preflight)
	}

	// 只改变 DNS 输入，等待定时轮利用有效的 EdgeOne 快照查实解除。
	if restarted.DNS.LastCompletedAt == nil {
		t.Fatal("重启轮缺少 DNS 完成时间")
	}
	f.dns.setIP("192.0.2.10")
	recovered := f.waitRoundAfter(restarted.DNS.LastCompletedAt, 20*time.Second)
	row, err = f.conflict()
	if err != nil || row != nil {
		t.Fatalf("定时恢复后冲突记录 = %+v err=%v", row, err)
	}
	td5, cf5 := f.cloud.teoCounts("zone-overlap")
	if td5 != td4 || cf5 != 0 {
		t.Fatalf("DNS 定时恢复应复用 EdgeOne 快照且零确认，TEO=%d/%d", td5, cf5)
	}
	if d, c, x := f.cloud.lhCounts("lhins-step16-a"); d+c+x != 0 {
		t.Fatalf("EdgeOne 未到期，定时轮不应执行该目标，LH=%d/%d/%d", d, c, x)
	}
	if d, c, x := f.cloud.lhCounts("lhins-step16-b"); d != 2 || c != 1 || x != 0 {
		t.Fatalf("DNS 定时恢复计数 LH=%d/%d/%d，期望 2/1/0", d, c, x)
	}
	if recovered.Preflight.State != syncer.PreflightStateClear {
		t.Fatalf("定时恢复预检 = %+v", recovered.Preflight)
	}
	code, health, _ = f.health()
	if code != http.StatusOK {
		t.Fatalf("定时恢复 health = %d %v", code, health)
	}
	t.Logf("STEP16 ticker recovery: conflict cleared, DNS LH=2/1/0, Edge LH=0/0/0, TEO=%d/%d, health=%d", td5, cf5, code)

	// 手动轮让两侧到期，验证 EdgeOne 也恢复写入，已恢复的 DNS 不重复新增。
	if code := f.putSettings(map[string]string{"interval": "5m"}); code != http.StatusOK {
		t.Fatalf("恢复 DNS 间隔 = %d", code)
	}
	if code := f.trigger(); code != http.StatusAccepted {
		t.Fatalf("恢复后手动触发 = %d", code)
	}
	manualRecovered := f.waitRoundAfter(recovered.LastSync, 30*time.Second)
	if manualRecovered.Preflight.State != syncer.PreflightStateClear {
		t.Fatalf("恢复后手动预检 = %+v", manualRecovered.Preflight)
	}
	if td, cf := f.cloud.teoCounts("zone-overlap"); td != td5+1 || cf != 0 {
		t.Fatalf("恢复后手动轮 TEO=%d/%d，期望 %d/0", td, cf, td5+1)
	}
	if d, c, x := f.cloud.lhCounts("lhins-step16-a"); d != 2 || c != 1 || x != 0 {
		t.Fatalf("EdgeOne 恢复计数 LH=%d/%d/%d，期望 2/1/0", d, c, x)
	}
	if _, c, x := f.cloud.lhCounts("lhins-step16-b"); c != 1 || x != 0 {
		t.Fatalf("DNS 不应重复新增或删除，LH create/delete=%d/%d", c, x)
	}
	f.stop()
}

func TestStep16EdgeOneConfirmPositive(t *testing.T) {
	f := newStep16Fixture(t)
	f.cloud.setZone("zone-confirm", step16Zone{
		CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24"},
		NextVer: "v2", NextV4: []string{"198.51.100.0/24"}, PlannedAt: "2030-01-02T03:04:05+08:00",
	})
	f.cloud.patchZone("zone-confirm", func(zone *step16Zone) { zone.ConfirmMode = "block" })
	f.setup(2, func(t *testing.T, store *config.Store, ids []int) {
		if err := store.SetSetting("dns_enabled", "false"); err != nil {
			t.Fatal(err)
		}
		step16AddSite(t, store, config.EdgeOneSite{
			ZoneID: "zone-confirm", TargetIDs: ids, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
		})
	})
	f.start()
	if code := f.resume(); code != http.StatusOK {
		t.Fatalf("resume = %d", code)
	}
	if !f.cloud.waitConfirmStarted("zone-confirm", 20*time.Second) {
		t.Fatalf("确认请求未进入替身")
	}
	var unknown *config.EdgeOneTransition
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		row, err := f.transition("zone-confirm")
		if err == nil && row != nil && row.ConfirmationState == config.EdgeOneConfirmationUnknown {
			unknown = row
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if unknown == nil {
		t.Fatalf("确认前未观察到 unknown 提交")
	}
	if unknown.ConfirmationRequestID != "" {
		t.Fatalf("unknown 不应有 RequestId: %+v", unknown)
	}
	td, cf := f.cloud.teoCounts("zone-confirm")
	if td != 2 || cf != 1 {
		t.Fatalf("确认阻塞时 TEO=%d/%d, want 2/1", td, cf)
	}
	edgeStatus, err := f.edgeStatus()
	if err != nil || len(edgeStatus.Sites) != 1 || edgeStatus.Sites[0].ConfirmationState != config.EdgeOneConfirmationUnknown {
		t.Fatalf("unknown 站点状态 = %+v err=%v", edgeStatus, err)
	}
	f.cloud.releaseConfirm("zone-confirm")
	status := f.waitRoundAfter(nil, 30*time.Second)
	row, err := f.transition("zone-confirm")
	if err != nil || row == nil || row.ConfirmationState != config.EdgeOneConfirmationAccepted || row.ConfirmationRequestID == "" {
		t.Fatalf("accepted 记录 = %+v err=%v", row, err)
	}
	td, cf = f.cloud.teoCounts("zone-confirm")
	if td != 2 || cf != 1 {
		t.Fatalf("accepted 后 TEO=%d/%d, want 2/1", td, cf)
	}
	for _, instance := range []string{"lhins-step16-a", "lhins-step16-b"} {
		d, c, x := f.cloud.lhCounts(instance)
		if d != 2 || c != 1 || x != 0 {
			t.Fatalf("%s counts = %d/%d/%d, want 2/1/0", instance, d, c, x)
		}
	}
	if status.LastRound == nil || status.LastRound.Outcome != syncer.RoundSuccess {
		t.Fatalf("确认轮结果 = %+v", status.LastRound)
	}
	logs := f.waitSyncLogCount(2, 5*time.Second)
	if len(logs) != 2 || logs[0].Result != "success" || logs[1].Result != "success" ||
		logs[0].Added+logs[1].Added != status.LastRound.Added || logs[0].Deleted+logs[1].Deleted != status.LastRound.Deleted {
		t.Fatalf("同步日志与轮次汇总不一致: logs=%+v round=%+v", logs, status.LastRound)
	}
	edgeStatus, err = f.edgeStatus()
	if err != nil || len(edgeStatus.Sites) != 1 || len(edgeStatus.Sites[0].Coverage) != 2 {
		t.Fatalf("站点覆盖记录 = %+v err=%v", edgeStatus, err)
	}
	for _, coverage := range edgeStatus.Sites[0].Coverage {
		if !coverage.Complete {
			t.Fatalf("站点覆盖不完整: %+v", coverage)
		}
	}
	code, health, _ := f.health()
	if code != http.StatusOK {
		t.Fatalf("确认后 health = %d %v", code, health)
	}
	t.Logf("STEP16 confirm accepted: request_id=%s zone=%s version=%s round_added=%d logs=%+v", row.ConfirmationRequestID, row.ZoneID, row.PendingVersion, status.LastRound.Added, logs)

	// 同指纹下一轮：accepted 跳过，不重发。
	last := *status.LastSync
	if code := f.trigger(); code != http.StatusAccepted {
		t.Fatalf("dedupe trigger = %d", code)
	}
	_ = f.waitRoundAfter(&last, 30*time.Second)
	td, cf = f.cloud.teoCounts("zone-confirm")
	if td != 3 || cf != 1 {
		t.Fatalf("accepted 跨轮 TEO=%d/%d, want 3/1", td, cf)
	}
	for _, instance := range []string{"lhins-step16-a", "lhins-step16-b"} {
		d, c, x := f.cloud.lhCounts(instance)
		if d != 4 || c != 1 || x != 0 {
			t.Fatalf("%s 跨轮 counts = %d/%d/%d, want 4/1/0", instance, d, c, x)
		}
	}

	// 重启：accepted 持久化，同指纹仍不重发。
	f.restart()
	status = f.waitRoundAfter(nil, 30*time.Second)
	row, err = f.transition("zone-confirm")
	if err != nil || row == nil || row.ConfirmationState != config.EdgeOneConfirmationAccepted || row.ConfirmationRequestID == "" {
		t.Fatalf("重启 accepted 记录 = %+v err=%v", row, err)
	}
	td, cf = f.cloud.teoCounts("zone-confirm")
	if td != 4 || cf != 1 {
		t.Fatalf("重启后 TEO=%d/%d, want 4/1", td, cf)
	}
	edgeStatus, err = f.edgeStatus()
	if err != nil || len(edgeStatus.Sites) != 1 || edgeStatus.Sites[0].ConfirmationState != config.EdgeOneConfirmationAccepted {
		t.Fatalf("重启 accepted 站点状态 = %+v err=%v", edgeStatus, err)
	}
	f.stop()
}

func TestStep16EdgeOneConfirmRejections(t *testing.T) {
	newRejectFixture := func(t *testing.T, mutate func(*step16Zone)) *step16Fixture {
		f := newStep16Fixture(t)
		zone := step16Zone{
			CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24"},
			NextVer: "v2", NextV4: []string{"198.51.100.0/24"},
		}
		mutate(&zone)
		f.cloud.setZone("zone-reject", zone)
		f.setup(2, func(t *testing.T, store *config.Store, ids []int) {
			if err := store.SetSetting("dns_enabled", "false"); err != nil {
				t.Fatal(err)
			}
			step16AddSite(t, store, config.EdgeOneSite{
				ZoneID: "zone-reject", TargetIDs: ids, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
			})
		})
		return f
	}

	t.Run("一个目标S1缺项零确认", func(t *testing.T) {
		f := newRejectFixture(t, func(*step16Zone) {})
		f.cloud.setInstance("lhins-step16-b", step16Instance{
			Version: 1, IncompleteDescribeCalls: map[int]bool{2: true, 4: true, 6: true},
		})
		f.start()
		f.resume()
		status := f.waitSourceCompletedAfter(nil, 40*time.Second)
		_, cf := f.cloud.teoCounts("zone-reject")
		td, _ := f.cloud.teoCounts("zone-reject")
		if cf != 0 {
			t.Fatalf("S1 缺项仍确认 %d 次", cf)
		}
		if td != 1 {
			t.Fatalf("S1 缺项 TEO Describe = %d, want 1", td)
		}
		row, _ := f.transition("zone-reject")
		if row != nil && row.ConfirmationState == config.EdgeOneConfirmationAccepted {
			t.Fatalf("S1 缺项留下 accepted: %+v", row)
		}
		if status.LastRound == nil || status.LastRound.Failed != 1 || status.LastRound.Outcome != syncer.RoundFailed {
			t.Fatalf("S1 缺项轮次 = %+v", status.LastRound)
		}
		f.stop()
	})

	t.Run("确认前版本变化零确认", func(t *testing.T) {
		f := newRejectFixture(t, func(zone *step16Zone) {
			zone.MutateOnDescribe = 2
			zone.MutateNextVer = "v3"
		})
		f.start()
		f.resume()
		f.waitSourceCompletedAfter(nil, 40*time.Second)
		td, cf := f.cloud.teoCounts("zone-reject")
		if td != 2 || cf != 0 {
			t.Fatalf("版本变化 TEO=%d/%d, want 2/0", td, cf)
		}
		row, _ := f.transition("zone-reject")
		if row != nil && row.ConfirmationState == config.EdgeOneConfirmationAccepted {
			t.Fatalf("版本变化留下 accepted: %+v", row)
		}
		f.stop()
	})

	t.Run("同版本网段变化零确认", func(t *testing.T) {
		f := newRejectFixture(t, func(zone *step16Zone) {
			zone.MutateOnDescribe = 2
			zone.MutateNextV4 = []string{"198.51.101.0/24"}
		})
		f.start()
		f.resume()
		f.waitSourceCompletedAfter(nil, 40*time.Second)
		td, cf := f.cloud.teoCounts("zone-reject")
		if td != 2 || cf != 0 {
			t.Fatalf("网段变化 TEO=%d/%d, want 2/0", td, cf)
		}
		row, _ := f.transition("zone-reject")
		if row != nil && row.ConfirmationState == config.EdgeOneConfirmationAccepted {
			t.Fatalf("网段变化留下 accepted: %+v", row)
		}
		f.stop()
	})
}

func TestStep16EdgeOneUnknownReadOnly(t *testing.T) {
	f := newStep16Fixture(t)
	f.cloud.setZone("zone-unknown", step16Zone{
		CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24"},
		NextVer: "v2", NextV4: []string{"198.51.100.0/24"}, ConfirmMode: "error",
	})
	f.setup(2, func(t *testing.T, store *config.Store, ids []int) {
		if err := store.SetSetting("dns_enabled", "false"); err != nil {
			t.Fatal(err)
		}
		step16AddSite(t, store, config.EdgeOneSite{
			ZoneID: "zone-unknown", TargetIDs: ids, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
		})
	})
	f.start()
	f.resume()
	f.waitTEOConfirmCount("zone-unknown", 1, 40*time.Second)
	status := f.waitSourceCompletedAfter(nil, 40*time.Second)
	row, err := f.transition("zone-unknown")
	if err != nil || row == nil || row.ConfirmationState != config.EdgeOneConfirmationUnknown {
		t.Fatalf("首次 unknown 记录 = %+v err=%v status=%+v", row, err, status.LastRound)
	}
	td, cf := f.cloud.teoCounts("zone-unknown")
	t.Logf("unknown status=%+v row=%+v action=%v", status, row, f.cloud.actionLog())
	if td != 2 || cf != 1 {
		t.Fatalf("首次 unknown TEO=%d/%d, want 2/1", td, cf)
	}
	// 下一轮只读复查：多一次 Describe，不重发确认。
	lastCompleted := *status.EdgeOne.LastCompletedAt
	if code := f.trigger(); code != http.StatusAccepted {
		t.Fatalf("unknown trigger = %d", code)
	}
	f.waitSourceCompletedAfter(&lastCompleted, 40*time.Second)
	td2, cf2 := f.cloud.teoCounts("zone-unknown")
	if td2 != td+2 || cf2 != 1 {
		t.Fatalf("unknown 复查 TEO=%d/%d, want %d/1", td2, cf2, td+2)
	}
	row, err = f.transition("zone-unknown")
	if err != nil || row == nil || row.ConfirmationState != config.EdgeOneConfirmationUnknown {
		t.Fatalf("复查后 unknown 记录 = %+v err=%v", row, err)
	}

	// 重启后未知记录仍保留；启动轮只读复查，不重发确认。
	f.restart()
	status = f.waitSourceCompletedAfter(nil, 40*time.Second)
	td3, cf3 := f.cloud.teoCounts("zone-unknown")
	if td3 != td2+2 || cf3 != 1 {
		t.Fatalf("重启 unknown 复查 TEO=%d/%d, want %d/1", td3, cf3, td2+2)
	}
	row, err = f.transition("zone-unknown")
	if err != nil || row == nil || row.ConfirmationState != config.EdgeOneConfirmationUnknown {
		t.Fatalf("重启后 unknown 记录 = %+v err=%v", row, err)
	}
	f.stop()
}

func TestStep16ProtectionAndCleanup(t *testing.T) {
	f := newStep16Fixture(t)
	f.cloud.setZone("zone-protect", step16Zone{
		CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24", "192.0.2.0/24"},
		NextVer: "v2", NextV4: []string{"198.51.100.0/24", "192.0.2.0/24"},
	})
	f.dns.setIP("198.18.0.10")
	f.setup(2, func(t *testing.T, store *config.Store, ids []int) {
		step16AddSite(t, store, config.EdgeOneSite{
			ZoneID: "zone-protect", TargetIDs: ids, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
		})
		step16AddRule(t, store, config.DomainRule{
			Host: "protect.example.test", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{ids[0]},
		})
	})
	f.start()
	f.resume()
	f.waitTEOConfirmCount("zone-protect", 1, 40*time.Second)
	_ = f.waitSourceCompletedAfter(nil, 40*time.Second)
	row, err := f.transition("zone-protect")
	if err != nil || row == nil || row.ConfirmationState != config.EdgeOneConfirmationAccepted {
		t.Fatalf("保护前置确认 = %+v err=%v", row, err)
	}
	baseTD, baseCF := f.cloud.teoCounts("zone-protect")
	baseLHD, baseLHC, baseLHX := f.cloud.totalLHCounts()

	// 重启后只看到当前 A，Next 消失；DNS 命中受保护的 B，两侧零写入。
	f.stop()
	f.cloud.setZone("zone-protect", step16Zone{
		CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24", "192.0.2.0/24"},
	})
	f.dns.setIP("198.51.100.7")
	f.start()
	protected := f.waitSourceCompletedAfter(nil, 30*time.Second)
	row, err = f.transition("zone-protect")
	if err != nil || row == nil || len(row.ProtectedCIDRs) == 0 {
		t.Fatalf("重启保护记录 = %+v err=%v", row, err)
	}
	td, cf := f.cloud.teoCounts("zone-protect")
	lhD, lhC, lhX := f.cloud.totalLHCounts()
	if td <= baseTD || cf != baseCF || lhD+lhC+lhX != baseLHD+baseLHC+baseLHX {
		t.Fatalf("受保护轮新增计数 TEO=%d/%d (base %d/%d) LH=%d/%d/%d (base %d/%d/%d)",
			td, cf, baseTD, baseCF, lhD, lhC, lhX, baseLHD, baseLHC, baseLHX)
	}
	if protected.Preflight.State != syncer.PreflightStateOverlap {
		t.Fatalf("受保护轮预检 = %+v", protected.Preflight)
	}
	edgeStatus, err := f.edgeStatus()
	if err != nil || len(edgeStatus.Sites) != 1 || edgeStatus.Sites[0].TransitionPendingVersion != "v2" {
		t.Fatalf("受保护站点状态 = %+v err=%v", edgeStatus, err)
	}

	// B 实际生效：当前版与规范网段同时匹配，保护释放并只清理 A。
	effBase := map[string][3]int{}
	for _, instance := range []string{"lhins-step16-a", "lhins-step16-b"} {
		d, c, x := f.cloud.lhCounts(instance)
		effBase[instance] = [3]int{d, c, x}
	}
	effBaseTD, effBaseCF := f.cloud.teoCounts("zone-protect")
	f.stop()
	f.cloud.setZone("zone-protect", step16Zone{
		CurrentVer: "v2", CurrentV4: []string{"198.51.100.0/24", "192.0.2.0/24"},
	})
	f.dns.setIP("198.18.0.10")
	f.start()
	effective := f.waitRoundAfter(nil, 40*time.Second)
	if effective.Preflight.State != syncer.PreflightStateClear {
		t.Fatalf("生效轮预检 = %+v", effective.Preflight)
	}
	row, err = f.transition("zone-protect")
	if err != nil || row != nil {
		t.Fatalf("生效后过渡记录应清除: %+v err=%v", row, err)
	}
	for _, instance := range []string{"lhins-step16-a", "lhins-step16-b"} {
		d, c, x := f.cloud.lhCounts(instance)
		base := effBase[instance]
		if d != base[0]+3 || c != base[1] || x != base[2]+1 {
			t.Fatalf("%s 生效轮新增 counts = %d/%d/%d (base %v), want +3/0/+1", instance, d, c, x, base)
		}
		inst, _ := f.cloud.instanceSnapshot(instance)
		var cidrs []string
		for _, rule := range inst.Rules {
			cidrs = append(cidrs, rule.Cidr)
		}
		joined := strings.Join(cidrs, ",")
		if strings.Contains(joined, "203.0.113.0/24") {
			t.Fatalf("%s 仍保留 A 网段: %v", instance, cidrs)
		}
		if !strings.Contains(joined, "198.51.100.0/24") || !strings.Contains(joined, "192.0.2.0/24") {
			t.Fatalf("%s 未保留 B/共享网段: %v", instance, cidrs)
		}
	}
	td2, cf2 := f.cloud.teoCounts("zone-protect")
	if cf2 != effBaseCF || td2 <= effBaseTD {
		t.Fatalf("生效轮新增 TEO=%d/%d (base %d/%d)", td2, cf2, effBaseTD, effBaseCF)
	}
	edgeStatus, err = f.edgeStatus()
	if err != nil || len(edgeStatus.Sites) != 1 || !edgeStatus.Sites[0].ActualEffective || edgeStatus.Sites[0].CurrentVersion != "v2" || edgeStatus.Sites[0].PendingVersion != "" {
		t.Fatalf("生效站点状态 = %+v err=%v", edgeStatus, err)
	}
	logs := f.waitSyncLogCount(2, 5*time.Second)
	if len(logs) < 2 || logs[0].Deleted+logs[1].Deleted != 2 {
		t.Fatalf("清理同步日志 = %+v", logs)
	}
	f.stop()
}

func TestStep16DryRunReadOnly(t *testing.T) {
	f := newStep16Fixture(t)
	f.cloud.setZone("zone-dry", step16Zone{CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24"}})
	f.dns.setIP("192.0.2.10")
	f.setup(2, func(t *testing.T, store *config.Store, ids []int) {
		step16AddSite(t, store, config.EdgeOneSite{
			ZoneID: "zone-dry", TargetIDs: []int{ids[0]}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
		})
		step16AddRule(t, store, config.DomainRule{
			Host: "dry.example.test", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{ids[1]},
		})
	})
	f.start()
	before, _ := f.status()
	edgeBefore, _ := f.edgeStatus()
	conflictBefore, _ := f.conflict()
	transitionBefore, _ := f.transition("zone-dry")
	logsBefore, _ := f.countRows("sync_logs")
	tdBefore, cfBefore := f.cloud.teoCounts("zone-dry")
	lhDBefore, lhCBefore, lhXBefore := f.cloud.totalLHCounts()

	resp, code := f.dryRun()
	if code != http.StatusOK {
		t.Fatalf("Dry Run = %d", code)
	}
	if len(resp.Results) != 2 || len(resp.ExecutionUnits) != 2 {
		t.Fatalf("Dry Run 结果数 = %d units=%d", len(resp.Results), len(resp.ExecutionUnits))
	}
	if resp.Preflight.State != syncer.PreflightStateClear || resp.Preflight.ProtectedCIDRs == nil {
		t.Fatalf("Dry Run 预检 = %+v", resp.Preflight)
	}
	after, _ := f.status()
	edgeAfter, _ := f.edgeStatus()
	conflictAfter, _ := f.conflict()
	transitionAfter, _ := f.transition("zone-dry")
	logsAfter, _ := f.countRows("sync_logs")
	tdAfter, cfAfter := f.cloud.teoCounts("zone-dry")
	lhDAfter, lhCAfter, lhXAfter := f.cloud.totalLHCounts()
	if cfAfter != cfBefore || lhCAfter != lhCBefore || lhXAfter != lhXBefore {
		t.Fatalf("Dry Run 产生云写入/确认: TEO %d/%d->%d/%d LH %d/%d/%d->%d/%d/%d",
			tdBefore, cfBefore, tdAfter, cfAfter, lhDBefore, lhCBefore, lhXBefore, lhDAfter, lhCAfter, lhXAfter)
	}
	if conflictBefore != nil || conflictAfter != nil {
		t.Fatalf("Dry Run 冲突记录变化: %+v -> %+v", conflictBefore, conflictAfter)
	}
	if (transitionBefore == nil) != (transitionAfter == nil) {
		t.Fatalf("Dry Run 过渡记录变化: %+v -> %+v", transitionBefore, transitionAfter)
	}
	if logsAfter != logsBefore {
		t.Fatalf("Dry Run 写了同步运行表: %d -> %d", logsBefore, logsAfter)
	}
	if before.LastSync != nil && after.LastSync != nil && !after.LastSync.Equal(*before.LastSync) {
		t.Fatalf("Dry Run 推进了正式 last_sync")
	}
	if before.EdgeOne.NextDueAt != nil || after.EdgeOne.NextDueAt != nil {
		if before.EdgeOne.NextDueAt == nil || after.EdgeOne.NextDueAt == nil || !after.EdgeOne.NextDueAt.Equal(*before.EdgeOne.NextDueAt) {
			t.Fatalf("Dry Run 改变 EdgeOne 调度: %v -> %v", before.EdgeOne.NextDueAt, after.EdgeOne.NextDueAt)
		}
	}
	if before.DNS.NextDueAt != nil || after.DNS.NextDueAt != nil {
		if before.DNS.NextDueAt == nil || after.DNS.NextDueAt == nil || !after.DNS.NextDueAt.Equal(*before.DNS.NextDueAt) {
			t.Fatalf("Dry Run 改变 DNS 调度: %v -> %v", before.DNS.NextDueAt, after.DNS.NextDueAt)
		}
	}
	if before.EdgeOne.LastOutcome != after.EdgeOne.LastOutcome ||
		(before.EdgeOne.LastCompletedAt == nil) != (after.EdgeOne.LastCompletedAt == nil) ||
		(before.EdgeOne.LastCompletedAt != nil && !after.EdgeOne.LastCompletedAt.Equal(*before.EdgeOne.LastCompletedAt)) {
		t.Fatalf("Dry Run 改变 EdgeOne 来源状态: before=%+v after=%+v", before.EdgeOne, after.EdgeOne)
	}
	if len(edgeBefore.Sites) != len(edgeAfter.Sites) {
		t.Fatalf("Dry Run 写入站点状态: %d -> %d", len(edgeBefore.Sites), len(edgeAfter.Sites))
	}
	if len(edgeBefore.Sites) == 1 && len(edgeAfter.Sites) == 1 {
		b, a := edgeBefore.Sites[0], edgeAfter.Sites[0]
		if (b.LastQueryAt == nil) != (a.LastQueryAt == nil) ||
			(b.LastQueryAt != nil && !a.LastQueryAt.Equal(*b.LastQueryAt)) ||
			b.LastQueryRequestID != a.LastQueryRequestID {
			t.Fatalf("Dry Run 刷新站点查询缓存/状态: before=%+v after=%+v", b, a)
		}
	}
	if tdAfter == tdBefore || lhDAfter == lhDBefore {
		t.Fatalf("Dry Run 未进行只读快照/解析: TEO %d->%d LH %d->%d", tdBefore, tdAfter, lhDBefore, lhDAfter)
	}
	if code := f.trigger(); code != http.StatusConflict {
		t.Fatalf("暂停时 trigger = %d, want 409", code)
	}
	// 恢复后正式轮与 Dry Run 的预计新增一致。
	if code := f.resume(); code != http.StatusOK {
		t.Fatalf("resume = %d", code)
	}
	status := f.waitRoundAfter(nil, 30*time.Second)
	planned := 0
	for _, unit := range resp.ExecutionUnits {
		planned += unit.PlannedAdd
	}
	if status.LastRound == nil || status.LastRound.Added != planned {
		t.Fatalf("正式新增 = %+v, Dry Run planned=%d", status.LastRound, planned)
	}
	if status.LastRound.CleanupCandidates != 0 || status.LastRound.CleanupDeleted != 0 {
		t.Fatalf("正式清理候选/删除 = %+v", status.LastRound)
	}
	f.stop()
}

func TestStep16SourceIsolation(t *testing.T) {
	t.Run("仅DNS", func(t *testing.T) {
		f := newStep16Fixture(t)
		f.setup(1, func(t *testing.T, store *config.Store, ids []int) {
			if err := store.SetSetting("dns_enabled", "true"); err != nil {
				t.Fatal(err)
			}
			if err := store.SetSetting("edgeone_enabled", "false"); err != nil {
				t.Fatal(err)
			}
			step16AddRule(t, store, config.DomainRule{
				Host: "dns-only.example.test", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: ids,
			})
		})
		f.dns.setIP("192.0.2.10")
		f.start()
		f.resume()
		status := f.waitRoundAfter(nil, 20*time.Second)
		if status.LastRound == nil || status.LastRound.Outcome != syncer.RoundSuccess {
			t.Fatalf("DNS-only 轮次 = %+v", status.LastRound)
		}
		for _, action := range f.cloud.actionLog() {
			if strings.Contains(action, " teo ") {
				t.Fatalf("DNS-only 出现 TEO 调用: %s", action)
			}
		}
		if d, c, x := f.cloud.lhCounts("lhins-step16-a"); d != 2 || c != 1 || x != 0 {
			t.Fatalf("DNS-only Lighthouse = %d/%d/%d", d, c, x)
		}
		f.stop()
	})

	t.Run("EdgeOne不足DNS继续", func(t *testing.T) {
		f := newStep16Fixture(t)
		f.cloud.setZone("zone-incomplete", step16Zone{CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24"}, DescribeMode: "incomplete"})
		f.dns.setIP("192.0.2.10")
		f.setup(2, func(t *testing.T, store *config.Store, ids []int) {
			step16AddSite(t, store, config.EdgeOneSite{
				ZoneID: "zone-incomplete", TargetIDs: []int{ids[0]}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
			})
			step16AddRule(t, store, config.DomainRule{
				Host: "dns-continue.example.test", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{ids[1]},
			})
		})
		f.start()
		f.resume()
		status := f.waitSourceCompletedAfter(nil, 30*time.Second)
		if status.Preflight.State != syncer.PreflightStateDataUnavailable || status.Preflight.DNSComplete != true || status.Preflight.EdgeOneComplete != false {
			t.Fatalf("EdgeOne 不足预检 = %+v", status.Preflight)
		}
		if status.EdgeOne.LastOutcome != syncer.SourceOutcomeIncomplete {
			t.Fatalf("EdgeOne 来源结果 = %q, want incomplete", status.EdgeOne.LastOutcome)
		}
		if status.DNS.LastOutcome != syncer.SourceOutcomeSuccess {
			t.Fatalf("DNS 来源结果 = %q, want success", status.DNS.LastOutcome)
		}
		if _, cf := f.cloud.teoCounts("zone-incomplete"); cf != 0 {
			t.Fatalf("EdgeOne 不足仍确认 %d 次", cf)
		}
		if _, c, x := f.cloud.lhCounts("lhins-step16-a"); c != 0 || x != 0 {
			t.Fatalf("EdgeOne 不足目标 A 仍写入: %d/%d", c, x)
		}
		if _, c, _ := f.cloud.lhCounts("lhins-step16-b"); c != 1 {
			t.Fatalf("DNS 侧未继续写入: %d", c)
		}
		code, health, _ := f.health()
		if code != http.StatusServiceUnavailable {
			t.Fatalf("EdgeOne 不足 health = %d %v", code, health)
		}
		if !strings.Contains(fmt.Sprint(health["reasons"]), "单侧继续") {
			t.Fatalf("EdgeOne 不足健康原因 = %v", health["reasons"])
		}
		f.stop()
	})

	t.Run("DNS失败EdgeOne完整正向", func(t *testing.T) {
		f := newStep16Fixture(t)
		f.cloud.setZone("zone-forward", step16Zone{
			CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24"},
			NextVer: "v2", NextV4: []string{"198.51.100.0/24"},
		})
		f.dns.setMode("nxdomain")
		f.setup(2, func(t *testing.T, store *config.Store, ids []int) {
			step16AddSite(t, store, config.EdgeOneSite{
				ZoneID: "zone-forward", TargetIDs: ids, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
			})
			step16AddRule(t, store, config.DomainRule{
				Host: "dns-failed.example.test", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{ids[0]},
			})
		})
		f.start()
		f.resume()
		f.waitTEOConfirmCount("zone-forward", 1, 40*time.Second)
		status := f.waitSourceCompletedAfter(nil, 40*time.Second)
		if status.Preflight.DNSComplete != false || status.Preflight.EdgeOneComplete != true {
			t.Fatalf("DNS 失败预检 = %+v", status.Preflight)
		}
		if status.DNS.LastOutcome != syncer.SourceOutcomeIncomplete {
			t.Fatalf("DNS 失败来源结果 = %q, want incomplete", status.DNS.LastOutcome)
		}
		if status.EdgeOne.LastOutcome != syncer.SourceOutcomeSuccess {
			t.Fatalf("EdgeOne 正向来源结果 = %q, want success", status.EdgeOne.LastOutcome)
		}
		td, cf := f.cloud.teoCounts("zone-forward")
		if td != 2 || cf != 1 {
			t.Fatalf("DNS 失败正向 TEO=%d/%d, want 2/1", td, cf)
		}
		for _, instance := range []string{"lhins-step16-a", "lhins-step16-b"} {
			d, c, x := f.cloud.lhCounts(instance)
			if d != 2 || c != 1 || x != 0 {
				t.Fatalf("%s 正向计数 = %d/%d/%d", instance, d, c, x)
			}
		}
		f.stop()
	})
}

func step16UserVersion(t *testing.T, dataDir string) int {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "config.db"))
	if err != nil {
		t.Fatalf("打开 user_version 连接失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("读取 user_version 失败: %v", err)
	}
	return version
}

// TestStep16SIGTERMInFlight 在真实目标轮次在途时发送 SIGTERM，验证进程等待
// 当前轮完成后有界退出、退出码 0、端口与 pidfile 释放。
func TestStep16SIGTERMInFlight(t *testing.T) {
	f := newStep16Fixture(t)
	f.cloud.setZone("zone-inflight", step16Zone{CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24"}})
	f.setup(2, func(t *testing.T, store *config.Store, ids []int) {
		if err := store.SetSetting("dns_enabled", "false"); err != nil {
			t.Fatal(err)
		}
		step16AddSite(t, store, config.EdgeOneSite{
			ZoneID: "zone-inflight", TargetIDs: ids, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
		})
	})
	f.start()
	if code := f.resume(); code != http.StatusOK {
		t.Fatalf("resume = %d", code)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		status, err := f.status()
		if err == nil && status.RoundStartedAt != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	status, err := f.status()
	if err != nil || status.RoundStartedAt == nil {
		t.Fatalf("未观察到在途轮次: %+v err=%v", status, err)
	}
	start := time.Now()
	f.stop()
	elapsed := time.Since(start)
	if elapsed > 25*time.Second {
		t.Fatalf("SIGTERM 在途退出耗时 %s，超过有界观察窗口", elapsed)
	}
	t.Logf("STEP16 SIGTERM in-flight elapsed=%s", elapsed)
}
