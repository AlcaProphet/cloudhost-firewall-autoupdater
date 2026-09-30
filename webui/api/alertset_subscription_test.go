package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// TestAlertManagerRepeatedApplyDoesNotDuplicateSubscriptions 判别「重复 Apply 不累积订阅」。
//
// 背景（2026-09-30 第二轮只读核验）：审计报告 §3 曾以 `alertset_test.go` 的
// TestAlertManagerApplyIsIdempotent 作为「重复 Apply 5 次验证无增长」的证据，但该用例只断言
// `current.email` / `current.webhook` 非 nil，**不统计订阅数量**，对「订阅是否累积」没有判别力。
// 本用例补齐该断言，且**不修改生产代码**（用户裁决 A2：只用行为断言，不新增计数接缝）。
//
// 判别原理：`notifier.EventBus.Publish` 会**为每个接口订阅者各起一个 goroutine**。正常语义下
// `AlertManager.Apply` 先按**旧集合**退订、再按**新集合**订阅，因此同一渠道在一次 Publish 中
// 只应收到**一次**投递；若 Apply 未完整退订，投递次数会随 Apply 次数增长。
// 这里用本地 HTTP 服务器**实际收到的请求数**作为投递次数的观测点。
//
// 时序确定性：只启用 Webhook 渠道，并以「服务器已收到并应答」作为该轮投递完成的同步点，
// 随后才进入下一轮 Apply。这样既避免与既有未同步字段（`notifier` 的 `limiter` 由
// SetInFlightLimiter 写、OnEvent 读，二者无同步）产生数据竞争，也不需要靠 sleep 猜时序。
func TestAlertManagerRepeatedApplyDoesNotDuplicateSubscriptions(t *testing.T) {
	var (
		mu    sync.Mutex
		hits  int
		ready = make(chan struct{}, 16)
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		// 钉钉成功响应需为 HTTP 2xx + 整数 errcode=0（P2-05 合同）。
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"errcode":0,"errmsg":"ok"}`)
		ready <- struct{}{}
	}))
	defer srv.Close()

	bus := notifier.NewEventBus()
	manager := NewAlertManager(bus)

	// 只启用 Webhook：单一渠道便于把「投递次数」与「订阅份数」一一对应。
	// 同时开启两个触发开关，使其在两个事件类型上各安装一份订阅。
	rc := alertConfig(false, true)
	rc.Webhook.URL = srv.URL + "/hook"
	set := BuildAlertSet(rc)
	if set.webhook == nil {
		t.Fatal("前置条件失败：Webhook 应已构造")
	}
	if set.email != nil {
		t.Fatal("前置条件失败：本用例只启用 Webhook，email 不应构造")
	}
	if len(set.events) < 2 {
		t.Fatalf("前置条件失败：策略应产生至少两个事件订阅，实际 %v", set.events)
	}
	first, second := set.events[0], set.events[1]

	totalHits := func() int {
		mu.Lock()
		defer mu.Unlock()
		return hits
	}
	// awaitHit 等待本地服务器收到一次请求；超时即判定未投递。
	awaitHit := func(what string) {
		t.Helper()
		select {
		case <-ready:
		case <-time.After(3 * time.Second):
			t.Fatalf("%s：3 秒内未收到 Webhook 投递", what)
		}
	}

	// ---- 第 1 轮：Apply 两次（重复 Apply 必须收敛为一份订阅）----
	manager.Apply(set)
	manager.Apply(set)

	bus.Publish(notifier.Event{Type: first, Timestamp: time.Now()})
	awaitHit("第 1 轮 Publish")
	// 给可能的重复投递一个短窗口：若订阅累积，第二份会带来第二个请求。
	time.Sleep(300 * time.Millisecond)
	if got := totalHits(); got != 1 {
		t.Fatalf("重复 Apply 两次后，一次 Publish(%s) 应恰好投递 1 次，实际 %d 次；"+
			"投递次数随 Apply 次数增长说明 Apply 未按旧集合完整退订（订阅累积）", first, got)
	}

	// 第 1 轮的投递已在该同步点完成（服务器已应答），此时再次 Apply 不会与在途
	// 通知 goroutine 竞争既有未同步字段。
	manager.Apply(set)

	// ---- 第 2 轮：换用另一事件类型，确认同一 Apply 安装的多个事件类型各自只一份订阅 ----
	bus.Publish(notifier.Event{Type: second, Timestamp: time.Now()})
	awaitHit("第 2 轮 Publish")
	time.Sleep(300 * time.Millisecond)
	if got := totalHits(); got != 2 {
		t.Fatalf("再次 Apply 后，两次 Publish 合计应恰好投递 2 次（每个事件各一次），实际 %d 次", got)
	}

	// ---- 第 3 轮：切换到**新的**集合实例，旧集合必须被完整退订 ----
	second2 := BuildAlertSet(rc)
	if second2.webhook == nil {
		t.Fatal("前置条件失败：新集合未构造出 Webhook")
	}
	if second2.webhook == set.webhook {
		t.Fatal("前置条件失败：两个集合应持有不同的通知器实例（否则无法区分旧集合退订）")
	}
	manager.Apply(second2)

	bus.Publish(notifier.Event{Type: first, Timestamp: time.Now()})
	awaitHit("第 3 轮 Publish")
	time.Sleep(300 * time.Millisecond)
	if got := totalHits(); got != 3 {
		t.Fatalf("切换到新集合后一次 Publish(%s) 应恰好投递 1 次（合计 3 次），实际 %d 次；"+
			"多余投递说明旧集合未被退订", first, got)
	}
}
