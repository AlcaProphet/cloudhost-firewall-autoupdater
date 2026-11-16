package syncer

import (
	"errors"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
)

// TestStep16ConflictReadFailureFormalEntry 验证正式 Run 入口在冲突表读取失败时
// 零目标快照/零写入，读取恢复后保存证据，完整无重叠且解除提交成功后恢复目标。
//
// 该用例补足 Step 16 跨步矩阵中“正式入口调用计数待验”的缺口；状态层已有
// TestAddressConflictStateRetainsOverlapAcrossReadFailures 覆盖。
func TestStep16ConflictReadFailureFormalEntry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newProbeProvider(config.CloudTCCVM, 0)
		teo := &step8FakeTEO{snapshot: step8Snapshot("zone-a", "203.0.113.0/24")}
		st := step8State(t, true, true, time.Hour, time.Hour, p, []config.EdgeOneSite{step8Site("zone-a")})
		st.TEO = teo
		store := &step8MemConflictStore{getErr: errors.New("database read unavailable")}
		s := New(NewRuntimeManager(st))
		s.SetConflictState(NewAddressConflictState(store))
		overlap := true
		s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
			if overlap {
				return []dns.ResolvedIP{{IP: net.ParseIP("203.0.113.7")}}, nil
			}
			return []dns.ResolvedIP{{IP: net.ParseIP("192.0.2.10")}}, nil
		}
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()

		if got := probeSnapshotCalls(p); got != 0 {
			t.Fatalf("读取失败时目标快照/写入 = %d, want 0", got)
		}
		if !s.Status().Preflight.KnownConflict && s.Status().Preflight.State != PreflightStateOverlap {
			t.Fatalf("读取失败后未保留本轮重叠证据: %+v", s.Status().Preflight)
		}
		if s.Status().Preflight.Error == "" {
			t.Fatalf("读取失败未暴露安全诊断: %+v", s.Status().Preflight)
		}

		// 下一轮 DNS 数据不足：读取恢复后保存内存证据，仍零写入。
		store.mu.Lock()
		store.getErr = nil
		store.mu.Unlock()
		overlap = false
		s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
			return nil, errors.New("dns insufficient")
		}
		s.TriggerSync()
		synctest.Wait()
		if got := probeSnapshotCalls(p); got != 0 {
			t.Fatalf("DNS 不足轮目标快照/写入 = %d, want 0", got)
		}
		store.mu.Lock()
		saved := store.conflict
		store.mu.Unlock()
		if saved == nil {
			t.Fatal("读取恢复后未保存冲突证据")
		}
		if s.Status().Preflight.State != PreflightStateOverlap {
			t.Fatalf("DNS 不足轮预检 = %+v", s.Status().Preflight)
		}

		// 完整无重叠且解除提交成功：目标恢复执行，冲突记录清除。
		s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
			return []dns.ResolvedIP{{IP: net.ParseIP("192.0.2.10")}}, nil
		}
		s.TriggerSync()
		synctest.Wait()
		if got := probeSnapshotCalls(p); got == 0 {
			t.Fatal("完整无重叠且解除成功后仍未执行目标")
		}
		store.mu.Lock()
		remaining := store.conflict
		store.mu.Unlock()
		if remaining != nil {
			t.Fatalf("解除后冲突记录残留: %+v", remaining)
		}
		if s.Status().Preflight.State != PreflightStateClear {
			t.Fatalf("恢复后预检 = %+v", s.Status().Preflight)
		}
	})
}
