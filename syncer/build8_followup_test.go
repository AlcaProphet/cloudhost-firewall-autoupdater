package syncer

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

func TestStep8CacheExpiresDuringFailedRefresh(t *testing.T) {
	for _, cutoff := range []string{"interval", "planned_activation"} {
		t.Run(cutoff, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := newProbeProvider(config.CloudTCCVM, 0)
				snapshot := step10Snapshot()
				snapshot.Next = nil
				teo := &step10TEO{}
				state := step10State(t, p, teo)
				if cutoff == "planned_activation" {
					planned := time.Now().Add(30 * time.Second)
					snapshot.PlannedActiveAt = &planned
				} else {
					state.Config.EdgeOneInterval = 30 * time.Second
				}
				teo.describeFn = func() (provider.OriginACLSnapshot, error) {
					if teo.describes == 1 {
						return snapshot, nil
					}
					time.Sleep(40 * time.Second)
					return provider.OriginACLSnapshot{}, errors.New("fixture query timed out")
				}
				s := New(NewRuntimeManager(state))
				epoch := s.Generation()
				s.prepareEdgeOneData(state, epoch, true)
				out := s.runScheduledRound(state, false, true)
				if out.EdgeRan || p.createCalls != 0 {
					t.Fatalf("expired %s input admitted target execution: edge_ran=%v create=%d", cutoff, out.EdgeRan, p.createCalls)
				}
			})
		})
	}
}

type blockingStep10Confirm struct {
	step10TEO
	started chan struct{}
	release chan struct{}
	err     error
}

func (c *blockingStep10Confirm) ConfirmOriginACLUpdate(context.Context, string) (string, error) {
	close(c.started)
	<-c.release
	if c.err != nil {
		return "", c.err
	}
	return "old-config-request", nil
}

func TestStep10TwoBoundTargets(t *testing.T) {
	for _, failS1 := range []bool{false, true} {
		name := "both_targets_confirm_once"
		if failS1 {
			name = "one_target_missing_sends_zero"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p0 := newProbeProvider(config.CloudTCCVM, 0)
				p1 := newProbeProvider(config.CloudTCCVM, 1)
				p1.createVisible = !failS1
				teo := &step10TEO{}
				state := step10State(t, p0, teo)
				state.Config.Targets = append(state.Config.Targets, config.TargetConfig{ID: 1, CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-second"})
				state.Config.EdgeOneSites[0].TargetIDs = []int{0, 1}
				state.Providers = append(state.Providers, p1)
				s := New(NewRuntimeManager(state))
				s.SetEdgeOneTransitionStore(newStep10Store())
				go s.Run()
				defer func() { s.Stop(); s.Wait() }()
				synctest.Wait()
				synctest.Sleep(time.Second)
				synctest.Wait()
				want := 1
				if failS1 {
					want = 0
				}
				_, confirms := teo.counts()
				if confirms != want || p0.snapshotCalls != 2 || p1.snapshotCalls != 2 {
					t.Fatalf("snapshot calls=%d/%d confirmation calls=%d want=%d", p0.snapshotCalls, p1.snapshotCalls, confirms, want)
				}
			})
		})
	}
}

func TestStep8RecheckCachedInputAtPreflight(t *testing.T) {
	for _, cutoff := range []time.Duration{20 * time.Second, 30 * time.Second} {
		t.Run(cutoff.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := newProbeProvider(config.CloudTCCVM, 0)
				teo := &step8FakeTEO{snapshot: fixSnapshot("zone-a", "203.0.113.0/24", false)}
				site := fixSite("ipv4")
				state := fixState(t, config.CloudTCCVM, true, true, p, []config.DomainRule{fixRule(1, "home.example")}, []config.EdgeOneSite{site}, teo)
				state.Config.EdgeOneInterval = 30 * time.Second
				s := New(NewRuntimeManager(state))
				s.SetConflictState(NewAddressConflictState(&step8MemConflictStore{}))
				s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
					time.Sleep(cutoff)
					return []dns.ResolvedIP{{IP: net.ParseIP("192.0.2.10")}}, nil
				}
				epoch := s.Generation()
				s.prepareEdgeOneData(state, epoch, true)
				teo.err = errors.New("fixture forced refresh failed")
				out := s.runScheduledRound(state, true, true)
				wantEdge := cutoff < 30*time.Second
				wantRules := 1
				if wantEdge {
					wantRules = 2
				}
				if !out.EdgeRetry || !out.DNSRan || out.EdgeRan != wantEdge || len(p.rules) != wantRules {
					t.Fatalf("dispatch=%+v rules=%+v wantEdge=%v", out, p.rules, wantEdge)
				}
				status := s.EdgeOneRuntimeStatus()[0]
				if status.LastQueryError == "" || status.LastQueryAt == nil || time.Since(*status.LastQueryAt) != cutoff {
					t.Fatalf("failed refresh must preserve last successful query time and diagnostic: %+v", status)
				}
			})
		})
	}
}

func TestStep10LateConfirmWithSQLite(t *testing.T) {
	for _, mutation := range []string{"current", "reset", "delete", "binding_change", "stale_timeout", "stale_not_sent", "stale_latest"} {
		t.Run(mutation, func(t *testing.T) {
			ctx := context.Background()
			store, err := config.OpenStore(filepath.Join(t.TempDir(), "state.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			p := newProbeProvider(config.CloudTCCVM, 0)
			teo := &blockingStep10Confirm{started: make(chan struct{}), release: make(chan struct{})}
			switch mutation {
			case "stale_timeout":
				teo.err = context.DeadlineExceeded
			case "stale_not_sent":
				teo.err = &provider.TEOError{Kind: provider.TEOErrorTimeout, NotSent: true}
			case "stale_latest":
				teo.err = &provider.TEOError{Kind: provider.TEOErrorAlreadyLatest}
			}
			state := step10State(t, p, teo)
			if err := store.WithTransactionContext(ctx, func(tx *sql.Tx) error {
				id, err := store.AddTargetTx(ctx, tx, state.Config.Targets[0])
				if err != nil {
					return err
				}
				state.Config.EdgeOneSites[0].TargetIDs = []int{int(id)}
				state.Config.Targets[0].ID = int(id)
				return store.AddEdgeOneSiteTx(ctx, tx, state.Config.EdgeOneSites[0])
			}); err != nil {
				t.Fatal(err)
			}
			s := New(NewRuntimeManager(state))
			s.SetEdgeOneTransitionStore(store)
			var stable sync.Mutex
			s.SetStableConfig(func(fn func() bool) bool {
				stable.Lock()
				defer stable.Unlock()
				return fn()
			})
			s.storeSiteProofs(step10Proofs(t, s, state))
			edgeData := map[string]edgeOneSiteData{"zone-a": {site: state.Config.EdgeOneSites[0], snapshot: step10Snapshot()}}
			done := make(chan struct{})
			var retry bool
			go func() {
				retry = s.ConfirmEdgeOneSites(state, edgeData, s.Generation())
				close(done)
			}()
			select {
			case <-teo.started:
			case <-time.After(3 * time.Second):
				t.Fatal("fixture confirmation did not send")
			}
			defer func() {
				select {
				case <-teo.release:
				default:
					close(teo.release)
				}
				<-done
			}()
			row, err := store.GetEdgeOneTransition(ctx, "zone-a")
			if err != nil || row == nil || row.ConfirmationState != config.EdgeOneConfirmationUnknown {
				t.Fatalf("unknown not committed before request: %+v / %v", row, err)
			}
			if mutation != "current" {
				stable.Lock()
				next := *state
				switch mutation {
				case "reset", "delete":
					next.Config.EdgeOneSites = nil
					if mutation == "reset" {
						err = store.WithTransactionContext(ctx, func(tx *sql.Tx) error { return store.ResetAllTx(ctx, tx) })
					} else {
						err = store.WithTransactionContext(ctx, func(tx *sql.Tx) error {
							_, err := store.DeleteEdgeOneSiteTx(ctx, tx, "zone-a")
							return err
						})
					}
				}
				s.ApplyState(&next)
				if mutation == "reset" {
					s.ForgetEdgeOneRuntime()
				}
				stable.Unlock()
				if err != nil {
					t.Fatal(err)
				}
			}
			close(teo.release)
			<-done
			row, err = store.GetEdgeOneTransition(ctx, "zone-a")
			if err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "current":
				if row == nil || row.ConfirmationState != config.EdgeOneConfirmationAccepted || row.ConfirmationRequestID != "old-config-request" {
					t.Fatalf("current request should record accepted: %+v", row)
				}
			case "retained_by_import", "binding_change", "stale_timeout", "stale_not_sent", "stale_latest":
				if row == nil || row.ConfirmationState != config.EdgeOneConfirmationUnknown || row.ConfirmationRequestID != "" {
					t.Fatalf("stale request must leave retained record unknown: %+v", row)
				}
			default:
				if row != nil {
					t.Fatalf("stale request restored deleted row: %+v", row)
				}
			}
			if mutation != "current" {
				if retry {
					t.Fatal("stale confirmation outcome requested a retry")
				}
				for _, status := range s.EdgeOneRuntimeStatus() {
					if status.ConfirmationID != "" || status.LastConfirmationErr != "" {
						t.Fatalf("stale request wrote runtime state: %+v", status)
					}
				}
				if s.confirmedMemory("zone-a") != ([32]byte{}) {
					t.Fatal("stale request restored confirmed fingerprint")
				}
			}
		})
	}
}

func TestStep8DNSOnlyRoundDoesNotAdvanceEdgeRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newProbeProvider(config.CloudTCCVM, 0)
		teo := &step8FakeTEO{snapshot: fixSnapshot("zone-a", "203.0.113.0/24", false)}
		state := fixState(t, config.CloudTCCVM, true, true, p, []config.DomainRule{fixRule(1, "home.example")}, []config.EdgeOneSite{fixSite("ipv4")}, teo)
		state.Config.EdgeOneInterval = 30 * time.Second
		s := New(NewRuntimeManager(state))
		s.SetConflictState(NewAddressConflictState(&step8MemConflictStore{}))
		s.resolveHostFn = step8Resolve(state)
		s.prepareEdgeOneData(state, s.Generation(), true)
		synctest.Sleep(30 * time.Second)
		teo.err = errors.New("fixture refresh failed")
		dispatch := s.runScheduledRound(state, true, false)
		if !dispatch.DNSRan || dispatch.EdgeRan || dispatch.EdgeRetry || len(p.rules) != 1 || p.rules[0].CidrBlock != "192.0.2.10/32" {
			t.Fatalf("DNS-only scheduling should not write EdgeOne or request its retry: %+v %+v", dispatch, p.rules)
		}
	})
}
