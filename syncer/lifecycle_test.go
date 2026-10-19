package syncer

import (
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

type r19GroupProvider struct {
	*targetProbeProvider
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *r19GroupProvider) GetSnapshot() (provider.RuleSnapshot, error) {
	p.once.Do(func() { close(p.entered); <-p.release })
	return p.targetProbeProvider.GetSnapshot()
}

// Wait 必须覆盖全部云分组，并广播释放多个等待者；另外观察实际 Run 返回。
func TestI8R19SyncerJoinsCloudGroups(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		state := gatedState(t, true, time.Hour)
		groups := []*r19GroupProvider{}
		for _, ct := range []config.CloudType{config.CloudTCCVM, config.CloudAliECS} {
			groups = append(groups, &r19GroupProvider{targetProbeProvider: newProbeProvider(ct, 0), entered: make(chan struct{}), release: make(chan struct{})})
		}
		state.Providers = []provider.Provider{groups[0], groups[1]}
		s := New(NewRuntimeManager(state))
		s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
			return []dns.ResolvedIP{{IP: net.ParseIP("192.0.2.1")}}, nil
		}
		runDone := make(chan struct{})
		go func() { s.Run(); close(runDone) }()
		defer func() {
			for _, p := range groups {
				select {
				case <-p.release:
				default:
					close(p.release)
				}
			}
			s.Stop()
			s.Wait()
		}()
		for _, p := range groups {
			<-p.entered
		}
		s.Stop()
		waited := make(chan struct{}, 4)
		for range 4 {
			go func() { s.Wait(); waited <- struct{}{} }()
		}
		synctest.Wait()
		if len(waited) != 0 {
			t.Error("I8R19: Wait 未等待在途云分组")
		}
		close(groups[0].release)
		synctest.Wait()
		if len(waited) != 0 {
			t.Error("I8R19: Wait 漏等第二个云分组")
		}
		close(groups[1].release)
		<-runDone
		for range 4 {
			<-waited
		}
		if s.Status().Running {
			t.Error("I8R19: Run 返回后仍标记运行")
		}
		for _, p := range groups {
			p.mu.Lock()
			calls := p.snapshotCalls
			p.mu.Unlock()
			if calls != 2 {
				t.Errorf("I8R19: 当轮 S0/S1 未完成: %s calls=%d", p.CloudType(), calls)
			}
		}
	})
}
