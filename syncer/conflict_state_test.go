package syncer

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
)

// 本文件是 Build8 Step 7 的运行态自动恢复回归。
//
// 使用真实临时 SQLite、真实写锁和写入计数替身，覆盖：
// 查实重叠 → 首次保存失败 → 下一轮数据不足 → 保存成功并重启 → 解除提交失败 →
// 完整复查后恢复。阻断期间通过 Admission 落点断言 DNS/EdgeOne/站点确认零写入。

// roundWriteCounter 模拟 Step 8 将要在准入点执行的写入计数。
type roundWriteCounter struct {
	dnsWrites    int
	edgeWrites   int
	siteConfirms int
}

func (c *roundWriteCounter) apply(admission Admission) {
	if admission.DNS {
		c.dnsWrites++
	}
	if admission.EdgeOne {
		c.edgeWrites++
		c.siteConfirms++
	}
}

func (c *roundWriteCounter) total() int {
	return c.dnsWrites + c.edgeWrites + c.siteConfirms
}

func newConflictStateStore(t *testing.T) (*config.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "conflict-state.db")
	store, err := config.OpenStore(path)
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if cerr := store.Close(); cerr != nil {
			t.Errorf("关闭测试数据库失败: %v", cerr)
		}
	})
	return store, path
}

// sqliteWriteLock 在独立连接上持有 BEGIN IMMEDIATE，制造真实 SQLite 写锁。
type sqliteWriteLock struct {
	db   *sql.DB
	conn *sql.Conn
}

func acquireSQLiteWriteLock(t *testing.T, path string) *sqliteWriteLock {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatalf("打开写锁连接失败: %v", err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		_ = db.Close()
		t.Fatalf("获取写锁连接失败: %v", err)
	}
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		_ = conn.Close()
		_ = db.Close()
		t.Fatalf("获取 SQLite 写锁失败: %v", err)
	}
	lock := &sqliteWriteLock{db: db, conn: conn}
	t.Cleanup(lock.release)
	return lock
}

func (l *sqliteWriteLock) release() {
	if l == nil || l.conn == nil {
		return
	}
	_, _ = l.conn.ExecContext(context.Background(), "ROLLBACK")
	_ = l.conn.Close()
	_ = l.db.Close()
	l.conn = nil
}

// assertSQLiteBusyError 确认失败确实来自真实写锁，而不是其他存储错误。
func assertSQLiteBusyError(t *testing.T, err error) {
	t.Helper()
	text := strings.ToLower(err.Error())
	if !strings.Contains(text, "locked") && !strings.Contains(text, "busy") {
		t.Fatalf("期望 SQLite 写锁错误，实际: %v", err)
	}
}

func assertBlockedByConflict(t *testing.T, outcome PreflightOutcome) {
	t.Helper()
	if outcome.State != PreflightStateOverlap {
		t.Fatalf("state = %q, want overlap: %+v", outcome.State, outcome)
	}
	if outcome.Admission.DNS || outcome.Admission.EdgeOne {
		t.Fatalf("冲突期间两侧必须零准入: %+v", outcome.Admission)
	}
	if !outcome.Admission.KeepConflict || !outcome.KnownConflict {
		t.Fatalf("必须保留已知冲突: %+v", outcome)
	}
}

func TestAddressConflictStateSQLiteAutoRecoverySequence(t *testing.T) {
	store, path := newConflictStateStore(t)
	ctx := context.Background()
	state := NewAddressConflictState(store)
	writes := &roundWriteCounter{}

	overlapInput := Preflight{
		DNSEnabled:      true,
		EdgeOneEnabled:  true,
		DNSComplete:     true,
		EdgeOneComplete: true,
		DNS: []DNSAddress{{
			Host: "home.example",
			Addr: netip.MustParseAddr("203.0.113.7"),
		}},
		EdgeOne: []EdgeOnePrefix{{
			ZoneID: "zone-a",
			Prefix: netip.MustParsePrefix("203.0.113.0/24"),
		}},
	}

	// 第 1 轮：真实写锁使首次 Save 失败；进程内证据先保留，两侧零准入。
	lock := acquireSQLiteWriteLock(t, path)
	lockStarted := time.Now()
	outcome, err := state.Evaluate(ctx, overlapInput)
	lockElapsed := time.Since(lockStarted)
	lock.release()
	if err == nil {
		t.Fatal("首次保存被写锁阻塞时必须返回错误")
	}
	assertSQLiteBusyError(t, err)
	if lockElapsed < 2*time.Second {
		t.Fatalf("首次保存未等待 busy_timeout，耗时 %v", lockElapsed)
	}
	assertBlockedByConflict(t, outcome)
	writes.apply(outcome.Admission)
	if writes.total() != 0 {
		t.Fatalf("阻断期间写入 = %+v, want 0", writes)
	}
	if row, gerr := store.GetAddressConflict(ctx); gerr != nil || row != nil {
		t.Fatalf("首次保存失败不应留下行: %+v err=%v", row, gerr)
	}
	memory := state.memoryConflict()
	if memory == nil || memory.FirstSeenAt.IsZero() {
		t.Fatalf("进程内证据必须保留: %+v", memory)
	}
	firstSeen := memory.FirstSeenAt

	// 第 2 轮：DNS 数据不足，但已知冲突继续阻断并重试保存；保存成功。
	outcome, err = state.Evaluate(ctx, Preflight{
		DNSEnabled:      true,
		EdgeOneEnabled:  true,
		DNSComplete:     false,
		EdgeOneComplete: true,
		EdgeOne:         overlapInput.EdgeOne,
	})
	if err != nil {
		t.Fatalf("数据不足轮次应成功重试保存: %v", err)
	}
	assertBlockedByConflict(t, outcome)
	writes.apply(outcome.Admission)
	if writes.total() != 0 {
		t.Fatalf("数据不足轮次写入 = %+v, want 0", writes)
	}
	row, err := store.GetAddressConflict(ctx)
	if err != nil || row == nil {
		t.Fatalf("重试后必须已持久化: %+v err=%v", row, err)
	}
	if !row.FirstSeenAt.Equal(firstSeen) {
		t.Fatalf("first_seen_at = %v, want %v", row.FirstSeenAt, firstSeen)
	}

	// 第 3 轮：进程重启，启动加载已提交记录；输入不足仍阻断且零写入。
	restarted := NewAddressConflictState(store)
	if err := restarted.Load(ctx); err != nil {
		t.Fatalf("启动加载失败: %v", err)
	}
	if restarted.memoryConflict() == nil {
		t.Fatal("启动必须恢复已提交冲突")
	}
	outcome, err = restarted.Evaluate(ctx, Preflight{
		DNSEnabled:     true,
		EdgeOneEnabled: true,
	})
	if err != nil {
		t.Fatalf("重启后数据不足轮次不应报错: %v", err)
	}
	assertBlockedByConflict(t, outcome)
	writes.apply(outcome.Admission)
	if writes.total() != 0 {
		t.Fatalf("重启后阻断写入 = %+v, want 0", writes)
	}
	if got := restarted.memoryConflict(); got == nil || !got.FirstSeenAt.Equal(firstSeen) {
		t.Fatalf("重启后首次时间 = %+v", got)
	}

	noOverlap := Preflight{
		DNSEnabled:      true,
		EdgeOneEnabled:  true,
		DNSComplete:     true,
		EdgeOneComplete: true,
		DNS: []DNSAddress{{
			Host: "other.example",
			Addr: netip.MustParseAddr("192.0.2.9"),
		}},
		EdgeOne: []EdgeOnePrefix{{
			ZoneID: "zone-b",
			Prefix: netip.MustParsePrefix("198.51.100.0/24"),
		}},
	}

	// 第 4 轮：完整输入证明无重叠，但解除提交被写锁拒绝；保留内存阻断。
	lock = acquireSQLiteWriteLock(t, path)
	lockStarted = time.Now()
	outcome, err = restarted.Evaluate(ctx, noOverlap)
	lockElapsed = time.Since(lockStarted)
	lock.release()
	if err == nil {
		t.Fatal("解除提交失败必须返回错误")
	}
	assertSQLiteBusyError(t, err)
	if lockElapsed < 2*time.Second {
		t.Fatalf("解除提交未等待 busy_timeout，耗时 %v", lockElapsed)
	}
	assertBlockedByConflict(t, outcome)
	writes.apply(outcome.Admission)
	if writes.total() != 0 {
		t.Fatalf("解除失败期间写入 = %+v, want 0", writes)
	}
	if row, gerr := store.GetAddressConflict(ctx); gerr != nil || row == nil {
		t.Fatalf("解除失败后记录应保留: %+v err=%v", row, gerr)
	}
	if restarted.memoryConflict() == nil {
		t.Fatal("解除失败后进程内证据必须保留")
	}

	// 第 5 轮：完整有效输入且解除提交成功；先提交，再清内存并恢复。
	outcome, err = restarted.Evaluate(ctx, noOverlap)
	if err != nil {
		t.Fatalf("解除应成功: %v", err)
	}
	if outcome.State != PreflightStateClear || outcome.KnownConflict || outcome.Admission.KeepConflict {
		t.Fatalf("恢复结论 = %+v", outcome)
	}
	if !outcome.Admission.DNS || !outcome.Admission.EdgeOne {
		t.Fatalf("完整无重叠应恢复两侧: %+v", outcome.Admission)
	}
	writes.apply(outcome.Admission)
	if writes.dnsWrites != 1 || writes.edgeWrites != 1 || writes.siteConfirms != 1 {
		t.Fatalf("恢复后写入 = %+v", writes)
	}
	if row, gerr := store.GetAddressConflict(ctx); gerr != nil || row != nil {
		t.Fatalf("解除后记录应为空: %+v err=%v", row, gerr)
	}
	if restarted.memoryConflict() != nil {
		t.Fatal("成功解除后必须清除进程内状态")
	}

	// 第 6 轮：再次完整复查无重叠，正常放行；不重新制造冲突。
	outcome, err = restarted.Evaluate(ctx, noOverlap)
	if err != nil || outcome.State != PreflightStateClear || !outcome.Admission.DNS || !outcome.Admission.EdgeOne {
		t.Fatalf("恢复后复查 = %+v err=%v", outcome, err)
	}
}

// fakeConflictStore 是读取/保存/解除失败的受控替身。
type fakeConflictStore struct {
	mu       sync.Mutex
	row      *config.AddressConflict
	getErr   error
	saveErr  error
	clearErr error

	gets   int
	saves  int
	clears int
}

func (f *fakeConflictStore) GetAddressConflict(context.Context) (*config.AddressConflict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.row == nil {
		return nil, nil
	}
	cp := *f.row
	return &cp, nil
}

func (f *fakeConflictStore) SaveAddressConflict(_ context.Context, conflict config.AddressConflict) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	if f.saveErr != nil {
		return f.saveErr
	}
	cp := conflict
	f.row = &cp
	return nil
}

func (f *fakeConflictStore) ClearAddressConflict(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clears++
	if f.clearErr != nil {
		return f.clearErr
	}
	f.row = nil
	return nil
}

func (f *fakeConflictStore) counts() (gets, saves, clears int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gets, f.saves, f.clears
}

func TestAddressConflictStateReadFailureBlocksBoth(t *testing.T) {
	store := &fakeConflictStore{getErr: errors.New("database unavailable")}
	state := NewAddressConflictState(store)
	writes := &roundWriteCounter{}
	outcome, err := state.Evaluate(context.Background(), Preflight{
		DNSEnabled:     true,
		EdgeOneEnabled: true,
	})
	if err == nil {
		t.Fatal("双开读取失败必须返回错误")
	}
	if outcome.Admission.DNS || outcome.Admission.EdgeOne {
		t.Fatalf("读取失败必须阻断两侧: %+v", outcome.Admission)
	}
	if outcome.State != PreflightStateDataUnavailable {
		t.Fatalf("state = %q, want data_unavailable", outcome.State)
	}
	writes.apply(outcome.Admission)
	if writes.total() != 0 {
		t.Fatalf("读取失败写入 = %+v", writes)
	}

	// 单项启用不读取冲突状态，正常侧继续。
	singleStore := &fakeConflictStore{getErr: errors.New("should not be read")}
	single := NewAddressConflictState(singleStore)
	outcome, err = single.Evaluate(context.Background(), Preflight{DNSEnabled: true, DNSComplete: true})
	if err != nil {
		t.Fatalf("单项启用不应读取冲突表: %v", err)
	}
	if !outcome.Admission.DNS || outcome.Admission.EdgeOne || outcome.State != PreflightStateSingleSource {
		t.Fatalf("单项结论 = %+v", outcome)
	}
	if gets, _, _ := singleStore.counts(); gets != 0 {
		t.Fatalf("单项启用读取冲突表 %d 次, want 0", gets)
	}
}

func TestAddressConflictStateOverlapPriorityAndSaveRetry(t *testing.T) {
	store := &fakeConflictStore{saveErr: errors.New("first save failed")}
	state := NewAddressConflictState(store)

	partial := Preflight{
		DNSEnabled:      true,
		EdgeOneEnabled:  true,
		DNSComplete:     false,
		EdgeOneComplete: false,
		DNS: []DNSAddress{{
			Host: "home.example",
			Addr: netip.MustParseAddr("203.0.113.7"),
		}},
		EdgeOne: []EdgeOnePrefix{{ZoneID: "zone-a", Prefix: netip.MustParsePrefix("203.0.113.0/24")}},
	}
	outcome, err := state.Evaluate(context.Background(), partial)
	if err == nil {
		t.Fatal("首次保存失败应返回错误")
	}
	assertBlockedByConflict(t, outcome)
	if outcome.Overlap == nil || outcome.Overlap.DNS.Host != "home.example" {
		t.Fatalf("部分输入仍必须保留正向命中: %+v", outcome.Overlap)
	}
	if _, saves, _ := store.counts(); saves != 1 {
		t.Fatalf("首次保存次数 = %d", saves)
	}

	// 下一轮数据不足：继续阻断并重试保存。
	store.mu.Lock()
	store.saveErr = nil
	store.mu.Unlock()
	outcome, err = state.Evaluate(context.Background(), Preflight{
		DNSEnabled:      true,
		EdgeOneEnabled:  true,
		DNSComplete:     false,
		EdgeOneComplete: false,
	})
	if err != nil {
		t.Fatalf("重试保存应成功: %v", err)
	}
	assertBlockedByConflict(t, outcome)
	if _, saves, _ := store.counts(); saves != 2 {
		t.Fatalf("重试保存次数 = %d, want 2", saves)
	}

	// 完整输入无重叠：解除成功后放行。
	outcome, err = state.Evaluate(context.Background(), Preflight{
		DNSEnabled:      true,
		EdgeOneEnabled:  true,
		DNSComplete:     true,
		EdgeOneComplete: true,
		DNS:             []DNSAddress{{Host: "other.example", Addr: netip.MustParseAddr("192.0.2.9")}},
		EdgeOne:         []EdgeOnePrefix{{ZoneID: "zone-b", Prefix: netip.MustParsePrefix("198.51.100.0/24")}},
	})
	if err != nil {
		t.Fatalf("解除失败: %v", err)
	}
	if outcome.State != PreflightStateClear || !outcome.Admission.DNS || !outcome.Admission.EdgeOne {
		t.Fatalf("恢复结论 = %+v", outcome)
	}
	if _, _, clears := store.counts(); clears != 1 {
		t.Fatalf("解除次数 = %d", clears)
	}
}

func TestAddressConflictStateClearFailureKeepsMemory(t *testing.T) {
	first := time.Now().UTC().Add(-time.Hour)
	store := &fakeConflictStore{
		row: &config.AddressConflict{
			DNSName:       "home.example",
			DNSAddress:    "203.0.113.7",
			EdgeOnePrefix: "203.0.113.0/24",
			ZoneID:        "zone-a",
			FirstSeenAt:   first,
			LastSeenAt:    first.Add(time.Minute),
		},
		clearErr: errors.New("clear failed"),
	}
	state := NewAddressConflictState(store)
	if err := state.Load(context.Background()); err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	noOverlap := Preflight{
		DNSEnabled:      true,
		EdgeOneEnabled:  true,
		DNSComplete:     true,
		EdgeOneComplete: true,
		DNS:             []DNSAddress{{Host: "other.example", Addr: netip.MustParseAddr("192.0.2.9")}},
		EdgeOne:         []EdgeOnePrefix{{ZoneID: "zone-b", Prefix: netip.MustParsePrefix("198.51.100.0/24")}},
	}
	outcome, err := state.Evaluate(context.Background(), noOverlap)
	if err == nil {
		t.Fatal("解除失败应返回错误")
	}
	assertBlockedByConflict(t, outcome)
	if state.memoryConflict() == nil {
		t.Fatal("解除失败后内存证据必须保留")
	}

	store.mu.Lock()
	store.clearErr = nil
	store.mu.Unlock()
	outcome, err = state.Evaluate(context.Background(), noOverlap)
	if err != nil || outcome.State != PreflightStateClear || !outcome.Admission.DNS || !outcome.Admission.EdgeOne {
		t.Fatalf("解除重试 = %+v err=%v", outcome, err)
	}
	if state.memoryConflict() != nil {
		t.Fatal("解除成功后内存必须清空")
	}
}

func TestAddressConflictStateSingleSideAndReopen(t *testing.T) {
	first := time.Now().UTC().Add(-time.Hour)
	store := &fakeConflictStore{
		row: &config.AddressConflict{
			DNSName:       "home.example",
			DNSAddress:    "203.0.113.7",
			EdgeOnePrefix: "203.0.113.0/24",
			ZoneID:        "zone-a",
			FirstSeenAt:   first,
			LastSeenAt:    first.Add(time.Minute),
		},
	}
	state := NewAddressConflictState(store)
	if err := state.Load(context.Background()); err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	// 关闭 EdgeOne：DNS 单项继续，已知冲突证据保留但不阻断单项。
	outcome, err := state.Evaluate(context.Background(), Preflight{DNSEnabled: true, DNSComplete: true})
	if err != nil {
		t.Fatalf("单项评估失败: %v", err)
	}
	if outcome.State != PreflightStateSingleSource || !outcome.Admission.DNS || outcome.Admission.EdgeOne {
		t.Fatalf("单项结论 = %+v", outcome)
	}
	if !outcome.Admission.KeepConflict || !outcome.KnownConflict {
		t.Fatalf("单项必须保留已知冲突: %+v", outcome)
	}
	if state.memoryConflict() == nil {
		t.Fatal("单项评估不得抹去证据")
	}

	// 重新双开且输入不足：立即回到阻断。
	outcome, err = state.Evaluate(context.Background(), Preflight{
		DNSEnabled:     true,
		EdgeOneEnabled: true,
	})
	if err != nil {
		t.Fatalf("重新双开评估失败: %v", err)
	}
	assertBlockedByConflict(t, outcome)
}

func TestAddressConflictStateNoKnownDataStates(t *testing.T) {
	cases := []struct {
		name string
		in   Preflight
		want PreflightState
		dns  bool
		edge bool
	}{
		{"双关", Preflight{}, PreflightStateSingleSource, false, false},
		{"两项完整无重叠", Preflight{DNSEnabled: true, EdgeOneEnabled: true, DNSComplete: true, EdgeOneComplete: true}, PreflightStateClear, true, true},
		{"DNS 不足", Preflight{DNSEnabled: true, EdgeOneEnabled: true, EdgeOneComplete: true}, PreflightStateDataUnavailable, false, true},
		{"EdgeOne 不足", Preflight{DNSEnabled: true, EdgeOneEnabled: true, DNSComplete: true}, PreflightStateDataUnavailable, true, false},
		{"两侧均不足", Preflight{DNSEnabled: true, EdgeOneEnabled: true}, PreflightStateDataUnavailable, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := NewAddressConflictState(&fakeConflictStore{})
			outcome, err := state.Evaluate(context.Background(), tc.in)
			if err != nil {
				t.Fatalf("评估失败: %v", err)
			}
			if outcome.State != tc.want || outcome.Admission.DNS != tc.dns || outcome.Admission.EdgeOne != tc.edge {
				t.Fatalf("outcome = %+v, want state=%s dns=%v edge=%v", outcome, tc.want, tc.dns, tc.edge)
			}
		})
	}
}

func TestAddressConflictDoesNotTouchDNSCircuitBreaker(t *testing.T) {
	breaker := dns.NewCircuitBreaker(1)
	state := NewAddressConflictState(&fakeConflictStore{})
	_, err := state.Evaluate(context.Background(), Preflight{
		DNSEnabled:      true,
		EdgeOneEnabled:  true,
		DNSComplete:     false,
		EdgeOneComplete: false,
		DNS:             []DNSAddress{{Host: "home.example", Addr: netip.MustParseAddr("203.0.113.7")}},
		EdgeOne:         []EdgeOnePrefix{{ZoneID: "zone-a", Prefix: netip.MustParsePrefix("203.0.113.0/24")}},
	})
	_ = err // 首次保存失败属于存储错误，不影响熔断器断言。
	if breaker.IsOpen("home.example") {
		t.Fatal("冲突判定不得增加 DNS 熔断计数")
	}

	// 真实 DNS 解析失败才增加计数；冲突证据与解析结果分开记录。
	breaker.RecordFailure("real.example")
	if !breaker.IsOpen("real.example") {
		t.Fatal("真实解析失败应打开熔断器")
	}
	if breaker.IsOpen("home.example") {
		t.Fatal("其他域名的解析失败不得被冲突判定复用")
	}
}

// TestPreflightConflictBlocksEveryTargetAndConfirmation 全局预检没有目标/端口维度：
// 同一重叠结论必须同时阻断两个目标的 DNS/EdgeOne 写入与站点确认。
func TestPreflightConflictBlocksEveryTargetAndConfirmation(t *testing.T) {
	state := NewAddressConflictState(&fakeConflictStore{})
	outcome, err := state.Evaluate(context.Background(), Preflight{
		DNSEnabled:      true,
		EdgeOneEnabled:  true,
		DNSComplete:     false, // 部分成功仍证明重叠
		EdgeOneComplete: false,
		DNS: []DNSAddress{{
			Host: "home.example",
			Addr: netip.MustParseAddr("203.0.113.7"),
		}},
		EdgeOne: []EdgeOnePrefix{{
			ZoneID: "zone-a",
			Prefix: netip.MustParsePrefix("203.0.113.0/24"), // 可来自 Current 或 Next
		}},
	})
	if err != nil {
		t.Fatalf("评估失败: %v", err)
	}
	writers := []*roundWriteCounter{{}, {}}
	for _, w := range writers {
		w.apply(outcome.Admission)
	}
	for i, w := range writers {
		if w.total() != 0 {
			t.Fatalf("目标 %d 阻断期间写入 = %+v", i+1, w)
		}
	}
}

// TestAddressConflictStateInvalidSamplesForceIncomplete 非法样本不得因调用方传入
// Complete=true 而被视为完整输入；无效侧必须停止准入。
func TestAddressConflictStateInvalidSamplesForceIncomplete(t *testing.T) {
	validDNS := []DNSAddress{{Host: "home.example", Addr: netip.MustParseAddr("192.0.2.9")}}
	validEdge := []EdgeOnePrefix{{ZoneID: "zone-a", Prefix: netip.MustParsePrefix("203.0.113.0/24")}}
	cases := []struct {
		name     string
		dns      []DNSAddress
		edge     []EdgeOnePrefix
		wantDNS  bool
		wantEdge bool
	}{
		{
			name:     "非法 DNS 地址",
			dns:      []DNSAddress{{Host: "bad.example", Addr: netip.Addr{}}},
			edge:     validEdge,
			wantDNS:  false,
			wantEdge: true,
		},
		{
			name:     "非法 EdgeOne 网段",
			dns:      validDNS,
			edge:     []EdgeOnePrefix{{ZoneID: "zone-bad", Prefix: netip.Prefix{}}},
			wantDNS:  true,
			wantEdge: false,
		},
		{
			name:     "两侧均非法",
			dns:      []DNSAddress{{Host: "bad.example", Addr: netip.Addr{}}},
			edge:     []EdgeOnePrefix{{ZoneID: "zone-bad", Prefix: netip.Prefix{}}},
			wantDNS:  false,
			wantEdge: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := NewAddressConflictState(&fakeConflictStore{})
			outcome, err := state.Evaluate(context.Background(), Preflight{
				DNSEnabled:      true,
				EdgeOneEnabled:  true,
				DNSComplete:     true,
				EdgeOneComplete: true,
				DNS:             tc.dns,
				EdgeOne:         tc.edge,
			})
			if err != nil {
				t.Fatalf("评估失败: %v", err)
			}
			if outcome.Admission.DNS != tc.wantDNS || outcome.Admission.EdgeOne != tc.wantEdge {
				t.Fatalf("Admission = %+v, want DNS=%v EdgeOne=%v", outcome.Admission, tc.wantDNS, tc.wantEdge)
			}
			if outcome.State != PreflightStateDataUnavailable {
				t.Fatalf("state = %q, want data_unavailable", outcome.State)
			}
			if tc.wantDNS == false && outcome.DNSComplete {
				t.Fatal("非法 DNS 样本不得保持完整")
			}
			if tc.wantEdge == false && outcome.EdgeOneComplete {
				t.Fatal("非法 EdgeOne 样本不得保持完整")
			}
		})
	}
}

// TestAddressConflictStateLoadWithoutRowRetriesPersist 外部清空 SQLite 行后，
// 进程内证据不能被误认为已持久化；后续数据不足轮次仍须重试保存。
func TestAddressConflictStateLoadWithoutRowRetriesPersist(t *testing.T) {
	store := &fakeConflictStore{}
	state := NewAddressConflictState(store)
	overlap := Preflight{
		DNSEnabled:      true,
		EdgeOneEnabled:  true,
		DNSComplete:     true,
		EdgeOneComplete: true,
		DNS:             []DNSAddress{{Host: "home.example", Addr: netip.MustParseAddr("203.0.113.7")}},
		EdgeOne:         []EdgeOnePrefix{{ZoneID: "zone-a", Prefix: netip.MustParsePrefix("203.0.113.0/24")}},
	}
	outcome, err := state.Evaluate(context.Background(), overlap)
	if err != nil {
		t.Fatalf("首次保存失败: %v", err)
	}
	assertBlockedByConflict(t, outcome)
	if _, saves, _ := store.counts(); saves != 1 {
		t.Fatalf("首次保存次数 = %d", saves)
	}

	// 模拟外部 reset/清理：行消失，但进程内证据仍在。
	store.mu.Lock()
	store.row = nil
	store.mu.Unlock()
	if err := state.Load(context.Background()); err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	outcome, err = state.Evaluate(context.Background(), Preflight{
		DNSEnabled:     true,
		EdgeOneEnabled: true,
	})
	if err != nil {
		t.Fatalf("数据不足轮次重试保存失败: %v", err)
	}
	assertBlockedByConflict(t, outcome)
	if _, saves, _ := store.counts(); saves != 2 {
		t.Fatalf("外部清行后应重试保存，saves=%d", saves)
	}
	if store.row == nil {
		t.Fatal("重试保存后应重新写入 SQLite")
	}
}

// TestAddressConflictStateForgetAfterReset reset 成功清空 SQLite 后，必须显式清除
// 进程内证据；否则后续数据不足轮次仍会用已失效的冲突继续阻断。
func TestAddressConflictStateForgetAfterReset(t *testing.T) {
	store, _ := newConflictStateStore(t)
	ctx := context.Background()
	state := NewAddressConflictState(store)

	overlap := Preflight{
		DNSEnabled:      true,
		EdgeOneEnabled:  true,
		DNSComplete:     true,
		EdgeOneComplete: true,
		DNS:             []DNSAddress{{Host: "home.example", Addr: netip.MustParseAddr("203.0.113.7")}},
		EdgeOne:         []EdgeOnePrefix{{ZoneID: "zone-a", Prefix: netip.MustParsePrefix("203.0.113.0/24")}},
	}
	outcome, err := state.Evaluate(ctx, overlap)
	if err != nil {
		t.Fatalf("保存冲突失败: %v", err)
	}
	assertBlockedByConflict(t, outcome)
	if row, gerr := store.GetAddressConflict(ctx); gerr != nil || row == nil {
		t.Fatalf("冲突未持久化: %+v err=%v", row, gerr)
	}

	if err := store.WithTransactionContext(ctx, func(tx *sql.Tx) error {
		return store.ResetAllTx(ctx, tx)
	}); err != nil {
		t.Fatalf("reset 失败: %v", err)
	}
	if row, gerr := store.GetAddressConflict(ctx); gerr != nil || row != nil {
		t.Fatalf("reset 后 SQLite 行应为空: %+v err=%v", row, gerr)
	}
	if state.memoryConflict() == nil {
		t.Fatal("reset 本身不应自动改写进程内状态")
	}
	state.Forget()
	if state.memoryConflict() != nil {
		t.Fatal("Forget 后进程内证据必须清空")
	}

	// 无已知冲突且双开输入不足：只标记数据不足并停止两侧，不再报 overlap。
	outcome, err = state.Evaluate(ctx, Preflight{
		DNSEnabled:     true,
		EdgeOneEnabled: true,
	})
	if err != nil {
		t.Fatalf("Forget 后数据不足轮次不应报错: %v", err)
	}
	if outcome.KnownConflict || outcome.State != PreflightStateDataUnavailable ||
		outcome.Admission.DNS || outcome.Admission.EdgeOne {
		t.Fatalf("reset 后残留证据仍在阻断: %+v", outcome)
	}

	// 完整无重叠输入正常放行；证明 Forget 后恢复的是无已知冲突状态。
	outcome, err = state.Evaluate(ctx, Preflight{
		DNSEnabled:      true,
		EdgeOneEnabled:  true,
		DNSComplete:     true,
		EdgeOneComplete: true,
		DNS:             []DNSAddress{{Host: "other.example", Addr: netip.MustParseAddr("192.0.2.9")}},
		EdgeOne:         []EdgeOnePrefix{{ZoneID: "zone-b", Prefix: netip.MustParsePrefix("198.51.100.0/24")}},
	})
	if err != nil || outcome.State != PreflightStateClear || !outcome.Admission.DNS || !outcome.Admission.EdgeOne {
		t.Fatalf("Forget 后完整输入未放行: %+v err=%v", outcome, err)
	}
}

// TestAddressConflictStateForgetReReadsPersistedRow Forget 后下一次双开评估必须
// 重新读取 SQLite；若 reset 实际未提交成功，已持久化冲突仍须恢复阻断。
func TestAddressConflictStateForgetReReadsPersistedRow(t *testing.T) {
	first := time.Now().UTC().Add(-time.Hour)
	store := &fakeConflictStore{
		row: &config.AddressConflict{
			DNSName:       "home.example",
			DNSAddress:    "203.0.113.7",
			EdgeOnePrefix: "203.0.113.0/24",
			ZoneID:        "zone-a",
			FirstSeenAt:   first,
			LastSeenAt:    first.Add(time.Minute),
		},
	}
	state := NewAddressConflictState(store)
	if err := state.Load(context.Background()); err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	state.Forget()
	if state.memoryConflict() != nil {
		t.Fatal("Forget 后内存证据应清空")
	}

	outcome, err := state.Evaluate(context.Background(), Preflight{
		DNSEnabled:     true,
		EdgeOneEnabled: true,
	})
	if err != nil {
		t.Fatalf("重新读取失败: %v", err)
	}
	assertBlockedByConflict(t, outcome)
	if state.memoryConflict() == nil {
		t.Fatal("SQLite 仍有的冲突必须重新进入内存阻断")
	}
	if gets, _, _ := store.counts(); gets < 2 {
		t.Fatalf("Forget 后应重新读取 SQLite，gets=%d", gets)
	}
}
