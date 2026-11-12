package api

import (
	"net/http"
	"sync/atomic"
	"testing"
)

type forgetStub struct {
	*stubSyncer
	forgets atomic.Int32
}

func (s *forgetStub) ForgetAddressConflict() { s.forgets.Add(1) }

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
}
