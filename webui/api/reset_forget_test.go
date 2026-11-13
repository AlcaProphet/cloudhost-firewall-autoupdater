package api

import (
	"net/http"
	"sync/atomic"
	"testing"
)

type forgetStub struct {
	*stubSyncer
	forgets     atomic.Int32
	edgeForgets atomic.Int32
}

func (s *forgetStub) ForgetAddressConflict() { s.forgets.Add(1) }
func (s *forgetStub) ForgetEdgeOneRuntime()  { s.edgeForgets.Add(1) }

func TestConfigResetForgetsAddressConflictAfterCommit(t *testing.T) {
	e := newTestEnv(t)
	spy := &forgetStub{stubSyncer: &stubSyncer{enabled: true, runtime: e.runtime}}
	e.deps.Syncer = spy
	w := e.do(t, http.MethodPost, "/api/config/reset", "{}")
	if w.Code != http.StatusOK {
		t.Fatalf("reset 状态码 = %d, body=%s", w.Code, w.Body.String())
	}
	if got := spy.forgets.Load(); got != 1 {
		t.Fatalf("reset 成功后 Forget 次数 = %d, want 1", got)
	}
	if got := spy.edgeForgets.Load(); got != 1 {
		t.Fatalf("reset 成功后 EdgeOne 运行态清理次数 = %d, want 1", got)
	}
}
