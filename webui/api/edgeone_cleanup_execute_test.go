package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// 支持异步 HTTP 查询的物理云替身；回归仍通过真实 Run 队列和配置协调器。
type s5Cloud struct {
	mu             sync.Mutex
	ct             config.CloudType
	id             int
	snapshot       provider.RuleSnapshot
	reads, creates int
	batches        [][]config.RuleInfo
	readFn         func(int, *provider.RuleSnapshot) error
	deleteFn       func(int, []config.RuleInfo, *provider.RuleSnapshot) (provider.DeleteResult, error)
}

func (p *s5Cloud) Name() string                { return fmt.Sprintf("%s#%d", p.ct, p.id) }
func (p *s5Cloud) CloudType() config.CloudType { return p.ct }
func (p *s5Cloud) TargetIndex() int            { return p.id }
func (p *s5Cloud) GetSnapshot() (provider.RuleSnapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads++
	if p.readFn != nil {
		if err := p.readFn(p.reads, &p.snapshot); err != nil {
			return provider.RuleSnapshot{}, err
		}
	}
	s := p.snapshot
	s.Rules = append([]config.RuleInfo{}, s.Rules...)
	return s, nil
}
func (p *s5Cloud) GetRules() ([]config.RuleInfo, error) { s, e := p.GetSnapshot(); return s.Rules, e }
func (p *s5Cloud) CreateRules(provider.RuleSnapshot, []config.RuleAction) (provider.CreateResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.creates++
	return provider.CreateResult{}, errors.New("清理不得新增")
}
func (p *s5Cloud) ConvertPorts(s string) []string { return provider.ExpandPorts(p.ct, s) }
func (p *s5Cloud) DeleteRules(s provider.RuleSnapshot, rows []config.RuleInfo) (provider.DeleteResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if (p.ct == config.CloudTCCVM || p.ct == config.CloudTCLighthouse) && s.Revision != p.snapshot.Revision {
		return provider.DeleteResult{}, errors.New("version conflict")
	}
	p.batches = append(p.batches, append([]config.RuleInfo{}, rows...))
	if p.deleteFn != nil {
		return p.deleteFn(len(p.batches), rows, &p.snapshot)
	}
	for _, r := range rows {
		p.snapshot.Rules = removeS5Row(p.snapshot.Rules, r)
	}
	return provider.DeleteResult{Deleted: len(rows), Resolved: len(rows)}, nil
}
func removeS5Row(rows []config.RuleInfo, r config.RuleInfo) []config.RuleInfo {
	out := []config.RuleInfo{}
	for _, x := range rows {
		if x != r {
			out = append(out, x)
		}
	}
	return out
}
func s5Row(i int) config.RuleInfo {
	return config.RuleInfo{Protocol: "TCP", Port: "443", CidrBlock: fmt.Sprintf("192.0.%d.%d/32", i/256, i%256), Action: "ACCEPT", Description: "[edgeone]", RuleID: fmt.Sprintf("id-%d", i), PolicyIndex: fmt.Sprint(i)}
}

func s5Setup(t *testing.T, cts ...config.CloudType) (*testEnv, *syncer.Syncer, []*s5Cloud) {
	t.Helper()
	e := newTestEnv(t)
	ps := []*s5Cloud{}
	providers := []provider.Provider{}
	for i, ct := range cts {
		id := e.seedTarget(t, ct, "ap-guangzhou", fmt.Sprintf("resource-%d", i))
		p := &s5Cloud{ct: ct, id: id, snapshot: provider.RuleSnapshot{Revision: "7", Rules: []config.RuleInfo{s5Row(i + 1)}}}
		ps = append(ps, p)
		providers = append(providers, p)
	}
	s4ApplyRuntime(t, e, providers, &s4FakeTEO{}, func(rc *config.RuntimeConfig) {
		rc.SyncEnabled = false
		rc.EdgeOneEnabled = true
		rc.EdgeOneTag = "edgeone"
		rc.Interval = time.Hour
		rc.EdgeOneInterval = time.Hour
	})
	s := syncer.New(e.runtime)
	e.deps.Syncer = s
	go s.Run()
	<-s.Started()
	t.Cleanup(func() { s.Stop(); s.Wait() })
	return e, s, ps
}
func s5Confirm(t *testing.T, e *testEnv, preview string, disable bool) (int, CleanupOperation) {
	t.Helper()
	w := e.do(t, http.MethodPost, "/api/edgeone/firewall-cleanup", fmt.Sprintf(`{"preview_id":%q,"disable_edgeone":%t}`, preview, disable))
	var op CleanupOperation
	if w.Code == 200 || w.Code == 202 {
		if err := json.Unmarshal(w.Body.Bytes(), &op); err != nil {
			t.Fatal(err)
		}
	}
	return w.Code, op
}
func s5Done(t *testing.T, e *testEnv, id string) CleanupOperation {
	t.Helper()
	for i := 0; i < 300; i++ {
		w := e.do(t, http.MethodGet, "/api/edgeone/firewall-cleanup/"+id, "")
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var op CleanupOperation
		if err := json.Unmarshal(w.Body.Bytes(), &op); err != nil {
			t.Fatal(err)
		}
		if op.FinishedAt != nil {
			return op
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("任务未结束")
	return CleanupOperation{}
}

func TestBuild9S5ConfirmFourPlatformsAndDuplicates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e, _, ps := s5Setup(t, config.CloudTCLighthouse, config.CloudTCCVM, config.CloudAliSWAS, config.CloudAliECS)
		for _, p := range ps {
			manual := s5Row(100)
			manual.Description = "[edgeone]foo"
			p.snapshot.Rules = append(p.snapshot.Rules, manual)
		}
		_, preview := s4Preview(t, e, `{"all":true}`)
		code, op := s5Confirm(t, e, preview.PreviewID, false)
		if code != 202 {
			t.Fatalf("code=%d op=%+v", code, op)
		}
		duplicateCode, duplicate := s5Confirm(t, e, preview.PreviewID, true)
		if duplicateCode != 200 || duplicate.OperationID != op.OperationID || duplicate.DisableEdgeOne {
			t.Fatalf("duplicate=%+v", duplicate)
		}
		final := s5Done(t, e, op.OperationID)
		if !final.Cleared || len(final.Targets) != 4 || final.SwitchAction != "unchanged" {
			t.Fatalf("final=%+v", final)
		}
		for i, result := range final.Targets {
			if result.ConfirmedDeleted != 1 || result.AbsentRows == nil || *result.AbsentRows != 1 || result.RemainingRows == nil || *result.RemainingRows != 0 || result.RequestsSent != 1 {
				t.Fatalf("target=%+v", result)
			}
			ps[i].mu.Lock()
			if ps[i].creates != 0 || len(ps[i].batches) != 1 || len(ps[i].snapshot.Rules) != 1 {
				t.Errorf("unexpected cloud writes")
			}
			ps[i].mu.Unlock()
		}
		if teo := e.snapshot().TEO.(*s4FakeTEO); teo.confirmCalls != 0 || teo.describeCalls != 0 {
			t.Fatal("清理访问 TEO")
		}
	})
}

func TestBuild9S5SnapshotChangesAndNewRules(t *testing.T) {
	for _, mode := range []string{"comment", "version", "index-drift", "id-missing", "id-duplicate", "lh-second-key", "new-row", "absent", "read-failed"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ct := config.CloudAliECS
				if strings.HasPrefix(mode, "lh-") {
					ct = config.CloudTCLighthouse
				}
				if mode == "version" || mode == "index-drift" {
					ct = config.CloudTCCVM
				}
				e, _, ps := s5Setup(t, ct)
				p := ps[0]
				_, preview := s4Preview(t, e, `{"all":true}`)
				p.mu.Lock()
				switch mode {
				case "comment":
					p.snapshot.Rules[0].Description = "[edgeone] 修改备注"
				case "version":
					p.snapshot.Revision = "8"
				case "index-drift":
					p.snapshot.Rules[0].PolicyIndex = "2"
				case "id-missing":
					p.snapshot.Rules[0].RuleID = ""
				case "id-duplicate":
					p.snapshot.Rules = append(p.snapshot.Rules, p.snapshot.Rules[0])
				case "lh-second-key":
					extra := p.snapshot.Rules[0]
					extra.Description = "manual"
					p.snapshot.Rules = append(p.snapshot.Rules, extra)
				case "new-row":
					p.snapshot.Rules = append(p.snapshot.Rules, s5Row(55))
				case "absent":
					p.snapshot.Rules = nil
				case "read-failed":
					p.readFn = func(int, *provider.RuleSnapshot) error { return provider.ErrSnapshotIncomplete }
				}
				p.mu.Unlock()
				code, op := s5Confirm(t, e, preview.PreviewID, false)
				if code != 202 {
					t.Fatal(code)
				}
				final := s5Done(t, e, op.OperationID)
				p.mu.Lock()
				deletes := len(p.batches)
				p.mu.Unlock()
				if mode == "new-row" {
					if deletes != 1 || final.Cleared || *final.Targets[0].RemainingRows != 1 {
						t.Fatalf("new rules included %+v", final)
					}
				} else if mode == "comment" {
					if deletes != 1 || !final.Cleared {
						t.Fatalf("备注不应参与删除定位 %+v", final)
					}
				} else if deletes != 0 {
					t.Fatalf("unsafe deletion %s %+v", mode, final)
				}
				if mode == "index-drift" && final.Targets[0].ReasonCode != "snapshot_changed" {
					t.Fatalf("index drift=%+v", final)
				}
				if mode == "absent" && (!final.Cleared || final.Targets[0].ConfirmedDeleted != 0 || *final.Targets[0].AbsentRows != 1) {
					t.Fatalf("absent=%+v", final)
				}
			})
		})
	}
}

func TestBuild9S5PartialTimeoutReadbackAndBatch(t *testing.T) {
	for _, mode := range []string{"permission", "timeout-remains", "timeout-gone", "readback-failed", "batch-partial", "batch-changed", "batch-idempotent"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e, _, ps := s5Setup(t, config.CloudAliECS, config.CloudTCCVM)
				p := ps[0]
				if strings.HasPrefix(mode, "batch-") {
					p.snapshot.Rules = nil
					for i := 1; i <= 101; i++ {
						p.snapshot.Rules = append(p.snapshot.Rules, s5Row(i))
					}
				}
				p.deleteFn = func(n int, rows []config.RuleInfo, s *provider.RuleSnapshot) (provider.DeleteResult, error) {
					if mode == "permission" {
						return provider.DeleteResult{}, errors.New("UnauthorizedOperation")
					}
					if mode == "timeout-remains" {
						return provider.DeleteResult{}, context.DeadlineExceeded
					}
					if mode == "batch-partial" && n == 2 {
						return provider.DeleteResult{}, errors.New("UnauthorizedOperation")
					}
					for _, r := range rows {
						s.Rules = removeS5Row(s.Rules, r)
					}
					if mode == "timeout-gone" {
						return provider.DeleteResult{}, context.DeadlineExceeded
					}
					if mode == "batch-changed" {
						s.Rules[0].Description = "manual"
					}
					if mode == "batch-idempotent" && n == 2 {
						return provider.DeleteResult{}, errors.New("InvalidSecurityGroupRuleId.NotFound")
					}
					return provider.DeleteResult{Deleted: len(rows), Resolved: len(rows)}, nil
				}
				if mode == "readback-failed" {
					p.readFn = func(n int, _ *provider.RuleSnapshot) error {
						if n == 3 {
							return provider.ErrSnapshotIncomplete
						}
						return nil
					}
				}
				_, preview := s4Preview(t, e, `{"all":true}`)
				_, op := s5Confirm(t, e, preview.PreviewID, false)
				final := s5Done(t, e, op.OperationID)
				first := final.Targets[0]
				if len(final.Targets) != 2 || final.Targets[1].ConfirmedDeleted != 1 {
					t.Fatalf("independent target did not finish %+v", final)
				}
				switch mode {
				case "permission":
					if final.Cleared || first.ConfirmedDeleted != 0 || *first.RemainingRows != 1 || first.OutcomeUnknown {
						t.Fatalf("permission=%+v", first)
					}
				case "timeout-remains":
					if final.Cleared || first.Status != "unknown" || !first.OutcomeUnknown || *first.RemainingRows != 1 {
						t.Fatalf("timeout=%+v", first)
					}
				case "timeout-gone":
					if !final.Cleared || first.ConfirmedDeleted != 0 || first.OutcomeUnknown || *first.AbsentRows != 1 {
						t.Fatalf("timeout-gone=%+v", first)
					}
				case "readback-failed":
					if final.Cleared || first.ReadbackVerified || first.RemainingRows != nil || first.ConfirmedDeleted != 1 {
						t.Fatalf("readback=%+v", first)
					}
				case "batch-partial":
					if first.ConfirmedDeleted != 100 || *first.RemainingRows != 1 || first.RequestsSent != 2 {
						t.Fatalf("partial=%+v", first)
					}
				case "batch-changed":
					if first.ConfirmedDeleted != 100 || first.RequestsSent != 1 || first.ReasonCode != "snapshot_changed" {
						t.Fatalf("changed=%+v", first)
					}
				case "batch-idempotent":
					if !final.Cleared || first.ConfirmedDeleted != 100 || *first.AbsentRows != 101 {
						t.Fatalf("idempotent=%+v", first)
					}
				}
			})
		})
	}
}

func TestBuild9S5QueuedConfigStopAndHTTPDetach(t *testing.T) {
	for _, mode := range []string{"tag", "credentials", "targets", "expired", "stop", "shutdown", "detached"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e, s, ps := s5Setup(t, config.CloudAliECS)
				entered, release := make(chan struct{}), make(chan struct{})
				if err := s.QueueMaintenance(func() { close(entered); <-release }); err != nil {
					t.Fatal(err)
				}
				<-entered
				_, preview := s4Preview(t, e, `{"all":true}`)
				ctx, cancel := context.WithCancel(context.Background())
				req := httptest.NewRequest(http.MethodPost, "/api/edgeone/firewall-cleanup", strings.NewReader(fmt.Sprintf(`{"preview_id":%q,"disable_edgeone":false}`, preview.PreviewID))).WithContext(ctx)
				mux := http.NewServeMux()
				e.deps.Register(mux)
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)
				cancel()
				code := w.Code
				var op CleanupOperation
				if err := json.Unmarshal(w.Body.Bytes(), &op); err != nil {
					t.Fatal(err)
				}
				if code != 202 {
					t.Fatal(code)
				}
				next := *e.snapshot()
				next.Config = next.Config.DeepCopy()
				switch mode {
				case "tag":
					next.Config.EdgeOneTag = "other"
					s.ApplyState(&next)
				case "credentials":
					next.Pool = provider.NewClientPool(provider.Credentials{TencentSecretID: "different"})
					s.ApplyState(&next)
				case "targets":
					next.Config.Targets = nil
					s.ApplyState(&next)
				case "expired":
					time.Sleep(11 * time.Minute)
				case "stop":
					s.Stop()
				case "shutdown":
					ch := make(chan struct{})
					e.deps.ShutdownCh = ch
					close(ch)
				case "detached":
					if ctx.Err() == nil {
						t.Fatal("受理请求 context 未取消")
					}
				}
				close(release)
				final := s5Done(t, e, op.OperationID)
				ps[0].mu.Lock()
				deletes := len(ps[0].batches)
				ps[0].mu.Unlock()
				if mode == "detached" {
					if deletes != 1 || !final.Cleared {
						t.Fatalf("detached=%+v", final)
					}
				} else if deletes != 0 || final.Cleared || final.ReasonCode == "" {
					t.Fatalf("queued check=%+v deletes=%d", final, deletes)
				}
			})
		})
	}
}

func TestBuild9S5DisableSwitchCoordinator(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e, s, ps := s5Setup(t, config.CloudAliECS)
				// 同正式构造路径发布配置，但保留本地物理云替身。
				e.deps.Coord = NewConfigCoordinator(e.store, func(snapshot *config.BusinessSnapshot, policy syncer.BreakerPolicy) (Candidate, error) {
					c, err := e.deps.buildCandidate(snapshot, policy)
					if err == nil {
						c.State.Providers = []provider.Provider{ps[0]}
						c.State.Config.SyncEnabled = false
						c.State.Config.EdgeOneTag = "edgeone"
						c.State.Config.Credentials = e.snapshot().Config.Credentials
						c.State.Pool = e.snapshot().Pool
					}
					return c, err
				}, func(c Candidate) { s.ApplyState(c.State) })
				if fail {
					e.execRaw(t, `CREATE TRIGGER s5_fail BEFORE INSERT ON settings WHEN NEW.key='edgeone_enabled' BEGIN SELECT RAISE(ABORT,'injected'); END`)
				}
				_, preview := s4Preview(t, e, `{"all":true}`)
				_, op := s5Confirm(t, e, preview.PreviewID, true)
				final := s5Done(t, e, op.OperationID)
				ps[0].mu.Lock()
				deletes := len(ps[0].batches)
				ps[0].mu.Unlock()
				if fail {
					if deletes != 0 || final.SwitchAction != "failed" || final.Cleared {
						t.Fatalf("failed=%+v", final)
					}
				} else {
					if deletes != 1 || !final.Cleared || final.SwitchAction != "disabled" || e.snapshot().Config.EdgeOneEnabled {
						t.Fatalf("disable=%+v", final)
					}
				}
			})
		})
	}
}

func TestBuild9S5ConfirmValidation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e, _, _ := s5Setup(t, config.CloudAliECS)
		for _, body := range []string{`{}`, `{"preview_id":"x"}`, `{"preview_id":"x","disable_edgeone":null}`, `{"preview_id":"x","disable_edgeone":false,"tag":"other"}`} {
			if w := e.do(t, http.MethodPost, "/api/edgeone/firewall-cleanup", body); w.Code != 400 {
				t.Fatalf("%s -> %d", body, w.Code)
			}
		}
		if w := e.do(t, http.MethodGet, "/api/edgeone/firewall-cleanup/missing", ""); w.Code != 404 {
			t.Fatal(w.Code)
		}
		if w := e.do(t, http.MethodPost, "/api/edgeone/firewall-cleanup", `{"preview_id":"missing","disable_edgeone":false}`); w.Code != 409 || !strings.Contains(w.Body.String(), CleanupPreviewNotFound) {
			t.Fatalf("missing %s", w.Body.String())
		}
		_, preview := s4Preview(t, e, `{"all":true}`)
		next := *e.snapshot()
		next.Config = next.Config.DeepCopy()
		next.Config.EdgeOneTag = "changed"
		e.runtime.Apply(&next)
		if w := e.do(t, http.MethodPost, "/api/edgeone/firewall-cleanup", fmt.Sprintf(`{"preview_id":%q,"disable_edgeone":false}`, preview.PreviewID)); w.Code != 409 {
			t.Fatal(w.Code)
		}
	})
}

func TestBuild9S5AdmissionAfterReadAndBetweenBatches(t *testing.T) {
	for _, mode := range []string{"after-read-config", "between-batches-config", "between-batches-stop"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e, s, ps := s5Setup(t, config.CloudAliECS)
				p := ps[0]
				if mode != "after-read-config" {
					p.snapshot.Rules = nil
					for i := 1; i <= 101; i++ {
						p.snapshot.Rules = append(p.snapshot.Rules, s5Row(i))
					}
				}
				change := func() {
					next := *e.snapshot()
					next.Config = next.Config.DeepCopy()
					next.Config.EdgeOneTag = "changed"
					s.ApplyState(&next)
				}
				p.readFn = func(n int, _ *provider.RuleSnapshot) error {
					if n == 2 && mode == "after-read-config" {
						change()
					}
					return nil
				}
				if mode != "after-read-config" {
					p.deleteFn = func(n int, rows []config.RuleInfo, snapshot *provider.RuleSnapshot) (provider.DeleteResult, error) {
						for _, r := range rows {
							snapshot.Rules = removeS5Row(snapshot.Rules, r)
						}
						if mode == "between-batches-stop" {
							s.Stop()
						} else {
							change()
						}
						return provider.DeleteResult{Deleted: len(rows), Resolved: len(rows)}, nil
					}
				}
				_, preview := s4Preview(t, e, `{"all":true}`)
				_, op := s5Confirm(t, e, preview.PreviewID, false)
				final := s5Done(t, e, op.OperationID)
				p.mu.Lock()
				calls := len(p.batches)
				p.mu.Unlock()
				want := 1
				if mode == "after-read-config" {
					want = 0
				}
				if calls != want || final.Cleared || final.Targets[0].RequestsSent != want || final.Targets[0].ReasonCode == "" {
					t.Fatalf("guard %s calls=%d result=%+v", mode, calls, final)
				}
				if mode != "after-read-config" && final.Targets[0].ConfirmedDeleted != 100 {
					t.Fatal("丢失第一批确认计数")
				}
			})
		})
	}
}

type s5CoverageSpy struct {
	*syncer.Syncer
	mu          sync.Mutex
	invalidated []string
}

func (s *s5CoverageSpy) InvalidateTargetCoverage(identity string) {
	s.mu.Lock()
	s.invalidated = append(s.invalidated, identity)
	s.mu.Unlock()
	s.Syncer.InvalidateTargetCoverage(identity)
}
func TestBuild9S5InvalidationPrecedesDeleteAndUnreadBlocksClear(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e, s, ps := s5Setup(t, config.CloudAliECS, config.CloudTCCVM)
		spy := &s5CoverageSpy{Syncer: s}
		e.deps.Syncer = spy
		ps[1].readFn = func(int, *provider.RuleSnapshot) error { return provider.ErrSnapshotIncomplete }
		p := ps[0]
		p.deleteFn = func(_ int, rows []config.RuleInfo, snapshot *provider.RuleSnapshot) (provider.DeleteResult, error) {
			spy.mu.Lock()
			n := len(spy.invalidated)
			spy.mu.Unlock()
			if n != 1 {
				t.Error("删除交 SDK 前没有撤销覆盖证据")
			}
			for _, r := range rows {
				snapshot.Rules = removeS5Row(snapshot.Rules, r)
			}
			return provider.DeleteResult{Deleted: len(rows), Resolved: len(rows)}, nil
		}
		_, preview := s4Preview(t, e, `{"all":true}`)
		_, op := s5Confirm(t, e, preview.PreviewID, false)
		final := s5Done(t, e, op.OperationID)
		if final.Cleared || len(final.Targets) != 2 || final.Targets[0].Status != "cleared" || final.Targets[1].ReasonCode != "preview_target_unread" {
			t.Fatalf("unread=%+v", final)
		}
		spy.mu.Lock()
		defer spy.mu.Unlock()
		if len(spy.invalidated) != 1 {
			t.Fatal("未提交删除的目标也被清除证据")
		}
	})
}

func TestBuild9S5ObservedConfigChangeNeverRevives(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e, s, ps := s5Setup(t, config.CloudAliECS, config.CloudTCCVM)
		original := e.snapshot()
		ps[0].readFn = func(n int, _ *provider.RuleSnapshot) error {
			if n == 2 {
				next := *original
				next.Config = next.Config.DeepCopy()
				next.Config.EdgeOneTag = "temporary"
				s.ApplyState(&next)
			}
			if n == 3 {
				s.ApplyState(original)
			}
			return nil
		}
		_, preview := s4Preview(t, e, `{"all":true}`)
		_, op := s5Confirm(t, e, preview.PreviewID, false)
		final := s5Done(t, e, op.OperationID)
		for _, p := range ps {
			p.mu.Lock()
			n := len(p.batches)
			p.mu.Unlock()
			if n != 0 {
				t.Fatal("配置恢复后旧授权复活")
			}
		}
		if final.Targets[0].ReasonCode != CleanupPreviewConfigChanged || final.Targets[1].ReasonCode != CleanupPreviewInvalidated {
			t.Fatalf("latched invalidation=%+v", final)
		}
	})
}
