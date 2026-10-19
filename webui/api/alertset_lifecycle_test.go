package api

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

func r19AlertReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("I8R19: 告警阶段信号超时")
		var zero T
		return zero
	}
}

func TestI8R19WebhookPoolAcrossReload(t *testing.T) {
	remotes, closed := make(chan string, 8), make(chan string, 8)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remotes <- r.RemoteAddr
		if _, err := w.Write([]byte(`{"errcode":0,"code":0}`)); err != nil {
			t.Error(err)
		}
	}))
	srv.Config.ConnState = func(c net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			closed <- c.RemoteAddr().String()
		}
	}
	srv.Start()
	defer srv.Close()
	manager := NewAlertManager(notifier.NewEventBus())
	defer manager.CloseResources()
	cfg := alertConfig(false, true)
	cfg.Webhook.URL = srv.URL
	var first string
	for _, channel := range []string{"dingtalk", "feishu", "dingtalk"} {
		cfg.Webhook.Channel = channel
		set := manager.Build(cfg)
		manager.Apply(set)
		if err := set.webhook.OnEvent(notifier.Event{Type: notifier.EventSyncError}); err != nil {
			t.Fatal(err)
		}
		remote := r19AlertReceive(t, remotes)
		if first == "" {
			first = remote
		} else if remote != first {
			t.Error("I8R19: 热重载未复用管理器私有池")
		}
	}
	var stops sync.WaitGroup
	for range 4 {
		stops.Go(manager.CloseResources)
	}
	stops.Wait()
	select {
	case remote := <-closed:
		if remote != first {
			t.Error("I8R19: 未关闭管理器私有连接")
		}
	case <-time.After(3 * time.Second):
		t.Error("I8R19_BEHAVIOR: Webhook retained idle socket")
	}
}

func TestI8R19WebhookOldTaskAcrossReload(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			close(entered)
			<-release
		}
		if _, err := w.Write([]byte(`{"errcode":0,"code":0}`)); err != nil {
			t.Error(err)
		}
	}))
	defer func() { once.Do(func() { close(release) }); srv.Close() }()
	bus := notifier.NewEventBus()
	manager := NewAlertManager(bus)
	defer manager.CloseResources()
	cfg := alertConfig(false, true)
	cfg.Webhook.URL = srv.URL + "/old"
	old := manager.Build(cfg)
	manager.Apply(old)
	bus.Publish(notifier.Event{Type: notifier.EventSyncError})
	r19AlertReceive(t, entered)
	// 发布新配置时不等待、不取消旧发送，两个实例仍使用同一长期 limiter。
	cfg.Webhook.URL = srv.URL + "/new"
	cfg.Webhook.Channel = "feishu"
	fresh := manager.Build(cfg)
	manager.Apply(fresh)
	if err := fresh.webhook.OnEvent(notifier.Event{Type: notifier.EventSyncError}); err != nil {
		t.Fatal(err)
	}
	if manager.webhookLimiter.InFlight() != 1 {
		t.Error("I8R19: 热重载取消了旧任务或丢失长期计数")
	}
	once.Do(func() { close(release) })
	bus.DrainSubscribers()
	if manager.webhookLimiter.InFlight() != 0 {
		t.Error("I8R19: 告警 drain 后仍有发送")
	}
}

func TestI8R19CandidateRollbackKeepsPool(t *testing.T) {
	e := newTestEnv(t)
	defer e.alerts.CloseResources()
	remotes := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remotes <- r.RemoteAddr
		if _, err := w.Write([]byte(`{"errcode":0}`)); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	cfg := alertConfig(false, true)
	cfg.Webhook.URL = srv.URL
	set := e.alerts.Build(cfg)
	e.alerts.Apply(set)
	if err := set.webhook.OnEvent(notifier.Event{Type: notifier.EventSyncError}); err != nil {
		t.Fatal(err)
	}
	first := r19AlertReceive(t, remotes)
	// 在真实事务里构造候选后拒绝，证明失败候选不会拥有或关闭长期池。
	rejected := errors.New("fixture rejects candidate")
	coordinator := NewConfigCoordinator(e.store, func(snapshot *config.BusinessSnapshot, policy syncer.BreakerPolicy) (Candidate, error) {
		candidate, err := e.deps.buildCandidate(snapshot, policy)
		if err != nil {
			return Candidate{}, err
		}
		if candidate.State == nil {
			t.Error("候选未实际构造")
		}
		return Candidate{}, rejected
	}, func(Candidate) { t.Error("失败候选不应发布") })
	err := coordinator.Mutate(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		return e.store.SetSettingTx(ctx, tx, "tag", "rejected-tag")
	})
	if !errors.Is(err, rejected) || e.alerts.Current().webhook != set.webhook {
		t.Fatalf("失败候选改变了已发布告警: %v", err)
	}
	if err := set.webhook.OnEvent(notifier.Event{Type: notifier.EventSyncError}); err != nil {
		t.Fatal(err)
	}
	if r19AlertReceive(t, remotes) != first {
		t.Error("I8R19: 候选回滚关闭了管理器池")
	}
}
