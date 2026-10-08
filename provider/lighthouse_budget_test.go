package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tcerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
)

// 仅使用本地真实 SDK 端点；不构成真实云或默认 120 秒墙钟验收。
func i810FullPage() []string {
	a := make([]string, 100)
	for i := range a {
		a[i] = lighthouseRuleJSON(fmt.Sprintf("10.0.0.%d/32", i+1))
	}
	return a
}
func TestI810SnapshotCounts(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		cap                  int
		pages                []int
		versions             []int
		wantCalls, wantRules int
		fail                 bool
	}{
		{"continuous_full", 3, []int{100, 100, 100, 1}, []int{7, 7, 7, 7}, 3, 0, true},
		{"last_allowed_short", 3, []int{100, 100, 1}, []int{7, 7, 7}, 3, 201, false},
		{"last_allowed_empty", 2, []int{100, 0}, []int{7, 7}, 2, 100, false},
		{"empty_first", 1, []int{0}, []int{7}, 1, 0, false},
		{"shared_across_reread", 3, []int{100, 1, 100, 1}, []int{7, 8, 9, 9}, 3, 0, true},
		{"reread_completes_at_boundary", 4, []int{100, 1, 100, 1}, []int{7, 8, 9, 9}, 4, 101, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, h := newMockCloudAPI(t)
			full := i810FullPage()
			m.reply = func(i int, r recordedRequest) (int, string) {
				if i >= len(tc.pages) {
					return 200, `{"Response":{"Error":{"Code":"FailedOperation","Message":"fixture stop"}}}`
				}
				return 200, lighthousePage(full[:tc.pages[i]], tc.versions[i])
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			snap, err := mockLighthouse(t, h).getSnapshotBounded(ctx, tc.cap)
			if tc.fail {
				if !errors.Is(err, ErrSnapshotIncomplete) || errors.Is(err, context.DeadlineExceeded) || len(snap.Rules) != 0 {
					t.Fatalf("wrong cap failure: rules=%d err=%v", len(snap.Rules), err)
				}
			} else if err != nil || len(snap.Rules) != tc.wantRules {
				t.Fatalf("rules=%d err=%v", len(snap.Rules), err)
			}
			if n := len(m.recorded()); n != tc.wantCalls {
				t.Fatalf("calls=%d want=%d", n, tc.wantCalls)
			}
			offset := 0
			for i, req := range m.recorded() {
				body := bodyJSON(t, req)
				if body["Offset"] != float64(offset) || body["Limit"] != float64(100) {
					t.Fatalf("分页请求参数不一致: %v", body)
				}
				offset += 100
				if tc.pages[i] < 100 {
					offset = 0
				}
			}

		})
	}
}
func TestI810SnapshotDefaultCount(t *testing.T) {
	m, h := newMockCloudAPI(t)
	full := i810FullPage()
	m.reply = func(i int, r recordedRequest) (int, string) {
		if i >= 100 {
			return 200, `{"Response":{"Error":{"Code":"FailedOperation","Message":"fixture stop"}}}`
		}
		return 200, lighthousePage(full, 7)
	}
	snap, err := mockLighthouse(t, h).GetSnapshot()
	if !errors.Is(err, ErrSnapshotIncomplete) || len(snap.Rules) != 0 || len(m.recorded()) != 100 {
		t.Fatalf("rules=%d calls=%d err=%v", len(snap.Rules), len(m.recorded()), err)
	}
}
func TestI810SnapshotDeadline(t *testing.T) {
	for _, kind := range []string{"headers", "body", "reread"} {
		t.Run(kind, func(t *testing.T) {
			release := make(chan struct{})
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ncall := int(calls.Add(1))
				if kind == "reread" && ncall < 3 {
					time.Sleep(180 * time.Millisecond)
					n := 100
					if ncall == 2 {
						n = 1
					}
					i810WriteFixture(t, w, r, lighthousePage(i810FullPage()[:n], ncall+6))
					return
				}
				if kind == "body" {
					w.Header().Set("Content-Type", "application/json")
					i810WriteFixture(t, w, r, `{"Response":`)
					w.(http.Flusher).Flush()
				}
				select {
				case <-release:
				case <-r.Context().Done():
				case <-time.After(time.Second):
					if kind == "body" {
						i810WriteFixture(t, w, r, `{"FirewallRuleSet":[],"FirewallVersion":7}}`)
					} else {
						i810WriteFixture(t, w, r, lighthousePage(nil, 7))
					}
				}
			}))
			defer func() { close(release); srv.CloseClientConnections(); srv.Close() }()
			budget := 200 * time.Millisecond
			if kind == "reread" {
				budget = 500 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			start := time.Now()
			snap, err := mockLighthouse(t, strings.TrimPrefix(srv.URL, "http://")).getSnapshotBounded(ctx, 100)
			elapsed := time.Since(start)
			if !errors.Is(err, ErrSnapshotIncomplete) || !errors.Is(err, context.DeadlineExceeded) || len(snap.Rules) != 0 || (kind != "reread" && elapsed > 600*time.Millisecond) || (kind == "reread" && elapsed > 700*time.Millisecond) {
				t.Fatalf("elapsed=%s rules=%d err=%v", elapsed, len(snap.Rules), err)
			}
			if kind == "reread" && calls.Load() != 3 {
				t.Fatalf("reread calls=%d", calls.Load())
			}
		})
	}
}

func TestI810SnapshotSDKError(t *testing.T) {
	m, h := newMockCloudAPI(t)
	m.reply = func(i int, r recordedRequest) (int, string) {
		return 200, `{"Response":{"Error":{"Code":"FailedOperation","Message":"sentinel"},"RequestId":"mock"}}`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snap, err := mockLighthouse(t, h).getSnapshotBounded(ctx, 3)
	var sdkErr *tcerrors.TencentCloudSDKError
	if !errors.As(err, &sdkErr) || sdkErr.GetCode() != "FailedOperation" || errors.Is(err, ErrSnapshotIncomplete) || !strings.Contains(err.Error(), "FailedOperation") || len(snap.Rules) != 0 || len(m.recorded()) != 1 {
		t.Fatalf("snap=%+v err=%v", snap, err)
	}
}

func TestI810SnapshotSharedClient(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body["InstanceId"] == "slow" {
			select {
			case <-release:
			case <-r.Context().Done():
			case <-time.After(time.Second):
			}
			return
		}
		time.Sleep(70 * time.Millisecond)
		i810WriteFixture(t, w, r, lighthousePage(nil, 7))
	}))
	defer func() { close(release); srv.CloseClientConnections(); srv.Close() }()
	slow := mockLighthouse(t, strings.TrimPrefix(srv.URL, "http://"))
	slow.instanceID = "slow"
	fast := &TCLighthouse{client: slow.client, instanceID: "fast"}
	c1, stop1 := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop1()
	c2, stop2 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer stop2()
	done := make(chan error, 1)
	go func() { _, err := slow.getSnapshotBounded(c1, 3); done <- err }()
	snap, err := fast.getSnapshotBounded(c2, 3)
	if err != nil || snap.Revision != "7" {
		t.Fatalf("other request affected: snap=%+v err=%v", snap, err)
	}
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("slow err=%v", err)
	}
}

// 已取消的请求允许响应写入失败；其他夹具错误必须使测试失败。
func i810WriteFixture(t *testing.T, w http.ResponseWriter, r *http.Request, payload string) {
	t.Helper()
	if _, err := fmt.Fprint(w, payload); err != nil && r.Context().Err() == nil {
		t.Errorf("写入本地响应失败: %v", err)
	}
}

func TestI810SnapshotExpiredBeforeRequest(t *testing.T) {
	m, host := newMockCloudAPI(t)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	snap, err := mockLighthouse(t, host).getSnapshotBounded(ctx, 100)
	if !errors.Is(err, ErrSnapshotIncomplete) || !errors.Is(err, context.DeadlineExceeded) || len(snap.Rules) != 0 || len(m.recorded()) != 0 {
		t.Fatalf("过期预算不得发起查询: rules=%d calls=%d err=%v", len(snap.Rules), len(m.recorded()), err)
	}
}
