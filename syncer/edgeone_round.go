package syncer

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 本文件是 Build8 Step 8～10 的双源准备与调度层：EdgeOne 快照缓存、只读
// 预检输入、调度状态、站点状态与稳定区/代次写入。正式目标写入仍由
// runRound/target.go 负责；站点确认见 edgeone_confirm.go。

// SourceStatus 是单个功能来源的调度摘要（GET /api/sync/status）。
type SourceStatus struct {
	Enabled   bool       `json:"enabled"`
	Effective bool       `json:"effective"`
	NextDueAt *time.Time `json:"next_due_at"`
}

// AddressConflictStatus 是已查实重叠的安全摘要（不含请求载荷）。
type AddressConflictStatus struct {
	DNSName       string `json:"dns_name"`
	DNSAddress    string `json:"dns_address"`
	EdgeOnePrefix string `json:"edgeone_prefix"`
	ZoneID        string `json:"zone_id"`
}

// AddressPreflightStatus 是最近一次正式预检的摘要；尚无记录时 state 为空。
type AddressPreflightStatus struct {
	State           PreflightState         `json:"state"`
	Conflict        *AddressConflictStatus `json:"conflict,omitempty"`
	DNSComplete     bool                   `json:"dns_complete"`
	EdgeOneComplete bool                   `json:"edgeone_complete"`
	KnownConflict   bool                   `json:"known_conflict"`
	ObservedAt      *time.Time             `json:"observed_at,omitempty"`
}

// SiteTargetCoverage 是一个站点在一个物理目标上的回读证明摘要。
type SiteTargetCoverage struct {
	TargetID  int      `json:"target_id"`
	TargetKey string   `json:"target_key"`
	Complete  bool     `json:"complete"`
	Reasons   []string `json:"reasons"`
}

// EdgeOneSiteStatus 是一个站点的只读运行状态；不向腾讯云发请求。
type EdgeOneSiteStatus struct {
	ZoneID              string               `json:"zone_id"`
	TargetIDs           []int                `json:"target_ids"`
	CurrentVersion      string               `json:"current_version"`
	PendingVersion      string               `json:"pending_version"`
	PlannedActiveAt     *time.Time           `json:"planned_active_at"`
	LastQueryAt         *time.Time           `json:"last_query_at"`
	LastQueryRequestID  string               `json:"last_query_request_id"`
	LastQueryError      string               `json:"last_query_error"`
	Coverage            []SiteTargetCoverage `json:"coverage"`
	ConfirmationState   string               `json:"confirmation_state"`
	ConfirmationID      string               `json:"confirmation_request_id"`
	LastConfirmationErr string               `json:"last_confirmation_error"`
	ActualEffective     bool                 `json:"actual_effective"`
	ActualActiveAt      *time.Time           `json:"actual_active_at"`
	ActualConfirmed     *bool                `json:"actual_confirmed"`
}

// EdgeOneStatus 是 GET /api/edgeone/status 的响应主体。
type EdgeOneStatus struct {
	Enabled   bool                `json:"enabled"`
	Effective bool                `json:"effective"`
	NextDueAt *time.Time          `json:"next_due_at"`
	Sites     []EdgeOneSiteStatus `json:"sites"`
}

type edgeOneCacheEntry struct {
	snapshot      provider.OriginACLSnapshot
	siteKey       string
	credential    string
	observedAt    time.Time
	plannedActive *time.Time
}

type edgeOneSiteData struct {
	site        config.EdgeOneSite
	snapshot    provider.OriginACLSnapshot
	unavailable string
}

func (s *Syncer) now() time.Time { return time.Now() }

// edgeOneIntervalOf 返回可用的 EdgeOne 调度间隔；零值夹具按生产默认值处理。
func edgeOneIntervalOf(state *RuntimeState) time.Duration {
	if state == nil || state.Config.EdgeOneInterval <= 0 {
		return config.DefaultEdgeOneInterval
	}
	return state.Config.EdgeOneInterval
}

// edgeOneSitesSignature 是站点集合的稳定摘要，用于识别新增/修改/删除站点。
func edgeOneSitesSignature(sites []config.EdgeOneSite) string {
	keys := make([]string, 0, len(sites))
	for _, site := range sites {
		keys = append(keys, edgeOneSiteKey(site))
	}
	sort.Strings(keys)
	return strings.Join(keys, "\n")
}

// edgeOneSiteKey 是站点配置与绑定目标的稳定身份，用于缓存失效判断。
func edgeOneSiteKey(site config.EdgeOneSite) string {
	ids := append([]int(nil), site.TargetIDs...)
	sort.Ints(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%d", id))
	}
	return strings.TrimSpace(site.ZoneID) + "|" + site.Protocol + "|" + site.Ports + "|" +
		site.AddressFamily + "|" + strings.Join(parts, ",")
}

func (s *Syncer) credentialFingerprint(state *RuntimeState) string {
	if state == nil || state.Pool == nil {
		return ""
	}
	return state.Pool.Credentials().Fingerprint()
}

// edgeCacheFresh 判断缓存能否作为本轮完整输入复用。
func (s *Syncer) edgeCacheFresh(entry edgeOneCacheEntry, site config.EdgeOneSite, state *RuntimeState, now time.Time) bool {
	if state == nil {
		return false
	}
	if entry.siteKey != edgeOneSiteKey(site) || entry.credential != s.credentialFingerprint(state) {
		return false
	}
	if state.Config.EdgeOneInterval <= 0 || now.Sub(entry.observedAt) >= state.Config.EdgeOneInterval {
		return false
	}
	if entry.plannedActive != nil && !now.Before(*entry.plannedActive) && entry.observedAt.Before(*entry.plannedActive) {
		// 已观察到的计划切换时间到达后，旧快照不再自动视为完整输入。
		return false
	}
	return true
}

// prepareEdgeOneData 为全部站点准备一份本轮只读数据；同一站点最多查询一次。
//
// force 为 true 时必须实际查询（EdgeOne 到期、首次启用、新站点、手动触发、
// 恢复）；仅 DNS 到期时 force=false，可复用仍有效的缓存。强制查询失败时，
// 若旧缓存仍满足年龄/站点/凭据/计划切换条件，则继续使用旧快照并记录错误；
// 过期或失效缓存不得当作完整输入。
func (s *Syncer) prepareEdgeOneData(state *RuntimeState, epoch uint64, force bool) map[string]edgeOneSiteData {
	out := make(map[string]edgeOneSiteData)
	if state == nil || !state.Config.EdgeOneEnabled {
		return out
	}
	now := s.now()
	for _, site := range state.Config.EdgeOneSites {
		zoneID := strings.TrimSpace(site.ZoneID)
		if zoneID == "" {
			continue
		}
		s.mu.RLock()
		cached, ok := s.edgeCache[zoneID]
		s.mu.RUnlock()
		fresh := ok && s.edgeCacheFresh(cached, site, state, now)
		if fresh && !force {
			out[zoneID] = edgeOneSiteData{site: site, snapshot: cached.snapshot}
			s.applyEdgeOneQueryResult(state, epoch, site, cached.snapshot, "",
				cached.observedAt, cached.snapshot.PlannedActiveAt, false)
			continue
		}
		if state.TEO == nil {
			out[zoneID] = edgeOneSiteData{site: site, unavailable: "TEO 客户端未接线"}
			s.applyEdgeOneQueryResult(state, epoch, site, provider.OriginACLSnapshot{},
				"TEO 客户端未接线", time.Time{}, nil, false)
			continue
		}
		snapshot, err := state.TEO.DescribeOriginACL(context.Background(), zoneID)
		if err != nil {
			reason := safeTEOErrorText(err)
			if fresh {
				// 强制查询失败但旧缓存仍在有效期内：按合同使用最后成功快照。
				out[zoneID] = edgeOneSiteData{site: site, snapshot: cached.snapshot}
				s.applyEdgeOneQueryResult(state, epoch, site, cached.snapshot, reason,
					cached.observedAt, cached.snapshot.PlannedActiveAt, false)
				continue
			}
			out[zoneID] = edgeOneSiteData{site: site, unavailable: reason}
			s.applyEdgeOneQueryResult(state, epoch, site, provider.OriginACLSnapshot{},
				reason, time.Time{}, nil, false)
			continue
		}
		observedAt := snapshot.ObservedAt
		if observedAt.IsZero() {
			observedAt = s.now()
		}
		out[zoneID] = edgeOneSiteData{site: site, snapshot: snapshot}
		s.applyEdgeOneQueryResult(state, epoch, site, snapshot, "",
			observedAt, snapshot.PlannedActiveAt, true)
	}
	return out
}

// applyEdgeOneQueryResult 在协调器稳定区内校验轮次代次后写入缓存与状态。
// 网络查询不在稳定区内；state/epoch 不符时丢弃旧轮次结果。
func (s *Syncer) applyEdgeOneQueryResult(state *RuntimeState, epoch uint64, site config.EdgeOneSite, snapshot provider.OriginACLSnapshot, queryErr string, queryAt time.Time, planned *time.Time, updateCache bool) {
	_ = s.withStableConfig(func() bool {
		if state == nil || s.runtime.Snapshot() != state || s.Generation() != epoch {
			return false
		}
		if updateCache && queryErr == "" {
			s.storeEdgeOneCacheLocked(site, snapshot, queryAt, state)
		}
		s.setEdgeSiteQueryStatus(site, snapshot, queryErr, queryAt, planned)
		return true
	})
}

// storeEdgeOneCacheLocked 要求调用者已持有稳定区；内部只锁 s.mu。
func (s *Syncer) storeEdgeOneCacheLocked(site config.EdgeOneSite, snapshot provider.OriginACLSnapshot, observedAt time.Time, state *RuntimeState) {
	if observedAt.IsZero() {
		observedAt = s.now()
	}
	zoneID := strings.TrimSpace(site.ZoneID)
	if zoneID == "" {
		return
	}
	s.mu.Lock()
	if s.edgeCache == nil {
		s.edgeCache = make(map[string]edgeOneCacheEntry)
	}
	s.edgeCache[zoneID] = edgeOneCacheEntry{
		snapshot:      snapshot,
		siteKey:       edgeOneSiteKey(site),
		credential:    s.credentialFingerprint(state),
		observedAt:    observedAt,
		plannedActive: snapshot.PlannedActiveAt,
	}
	s.mu.Unlock()
}

// edgePrefixInputs 返回稳定排序的预检网段与完整性。
func edgePrefixInputs(data map[string]edgeOneSiteData) ([]EdgeOnePrefix, bool) {
	zones := make([]string, 0, len(data))
	for zone := range data {
		zones = append(zones, zone)
	}
	sort.Strings(zones)
	complete := true
	out := make([]EdgeOnePrefix, 0)
	for _, zone := range zones {
		item := data[zone]
		if item.unavailable != "" || strings.TrimSpace(item.snapshot.ZoneID) == "" ||
			strings.TrimSpace(item.snapshot.Current.Version) == "" ||
			!provider.OriginACLSnapshotPrefixesValid(item.snapshot) {
			complete = false
			continue
		}
		for _, prefix := range item.snapshot.AllPrefixes() {
			out = append(out, EdgeOnePrefix{ZoneID: zone, Prefix: prefix.Masked()})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ZoneID != out[j].ZoneID {
			return out[i].ZoneID < out[j].ZoneID
		}
		return out[i].Prefix.String() < out[j].Prefix.String()
	})
	return out, complete
}

// evaluatePreflight 调用正式冲突状态并返回本轮准入；任何存储错误都保守阻断。
func (s *Syncer) evaluatePreflight(state *RuntimeState, dns []DNSAddress, dnsComplete bool, edge []EdgeOnePrefix, edgeComplete bool) (outcome PreflightOutcome, err error) {
	in := Preflight{
		DNSEnabled:      state.Config.DNSFeatureEnabled(),
		EdgeOneEnabled:  state.Config.EdgeOneEnabled,
		DNSComplete:     dnsComplete,
		EdgeOneComplete: edgeComplete,
		DNS:             dns,
		EdgeOne:         edge,
	}
	if !in.DNSEnabled || !in.EdgeOneEnabled {
		in.KnownConflict = s.hasKnownConflict()
		outcome = PreflightOutcome{
			State:           PreflightStateSingleSource,
			DNSComplete:     dnsComplete,
			EdgeOneComplete: edgeComplete,
			KnownConflict:   in.KnownConflict,
			Admission:       DecideAdmission(in),
		}
		return outcome, nil
	}
	run := func() bool {
		if s.runtime.Snapshot() != state {
			return false
		}
		if s.conflict == nil {
			outcome = PreflightOutcome{
				State:           PreflightStateDataUnavailable,
				DNSComplete:     dnsComplete,
				EdgeOneComplete: edgeComplete,
			}
			outcome.Admission = DecideAdmission(in)
			outcome.State = preflightStateFor(in, false)
			err = errors.New("地址冲突状态未接线")
			return true
		}
		outcome, err = s.conflict.Evaluate(context.Background(), in)
		return true
	}
	if !s.withStableConfig(run) {
		return PreflightOutcome{State: PreflightStateDataUnavailable}, errors.New("配置已变化，放弃旧轮次预检状态写入")
	}
	return outcome, err
}

func preflightStateFor(in Preflight, overlap bool) PreflightState {
	if overlap {
		return PreflightStateOverlap
	}
	if in.DNSComplete && in.EdgeOneComplete {
		return PreflightStateClear
	}
	return PreflightStateDataUnavailable
}

func (s *Syncer) hasKnownConflict() bool {
	if s.conflict == nil {
		return false
	}
	return s.conflict.hasKnown()
}

// withStableConfig 在协调器稳定区内执行状态写入；未接线时直接执行。
func (s *Syncer) withStableConfig(fn func() bool) bool {
	s.mu.RLock()
	guard := s.stableConfig
	s.mu.RUnlock()
	if guard == nil {
		return fn()
	}
	return guard(fn)
}

// safeTEOErrorText 只保留安全分类与固定文案。
func safeTEOErrorText(err error) string {
	if err == nil {
		return ""
	}
	var teoErr *provider.TEOError
	if errors.As(err, &teoErr) {
		return teoErr.Error()
	}
	return "TEO 查询失败"
}

// safeConfirmErrorText 为确认状态/配置错误提供固定安全文案，避免把
// SQLite/路径细节误标成 TEO 查询失败，也避免回显底层错误原文。
func safeConfirmErrorText(err error) string {
	var teoErr *provider.TEOError
	if errors.As(err, &teoErr) {
		return teoErr.Error()
	}
	return "确认状态或配置检查失败"
}

// setEdgeSiteQueryStatus 更新进程内站点查询状态；不访问网络。
//
// queryAt 是实际成功查询时间（缓存命中时传缓存的观测时间），不是本轮时间；
// planned 是该快照对应的计划切换时间，缓存命中时也必须保留。
func (s *Syncer) setEdgeSiteQueryStatus(site config.EdgeOneSite, snapshot provider.OriginACLSnapshot, queryErr string, queryAt time.Time, planned *time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.edgeStatus == nil {
		s.edgeStatus = make(map[string]EdgeOneSiteStatus)
	}
	zone := strings.TrimSpace(site.ZoneID)
	st := s.edgeStatus[zone]
	st.ZoneID = zone
	st.TargetIDs = append([]int(nil), site.TargetIDs...)
	st.LastQueryError = queryErr
	if queryErr == "" {
		if queryAt.IsZero() {
			queryAt = s.now()
		}
		st.LastQueryAt = &queryAt
		st.LastQueryRequestID = snapshot.RequestID
		st.CurrentVersion = snapshot.Current.Version
		if snapshot.Next != nil {
			st.PendingVersion = snapshot.Next.Version
		} else {
			st.PendingVersion = ""
		}
		st.PlannedActiveAt = copyTimePtr(planned)
		st.ActualEffective = snapshot.Next == nil && strings.TrimSpace(snapshot.Current.Version) != ""
		st.ActualActiveAt = snapshot.Current.ActiveTime
		st.ActualConfirmed = snapshot.Current.IsPlaned
	}
	s.edgeStatus[zone] = st
}

// storeSiteProofs 保留测试/旧入口的无条件写入；生产轮次使用
// storeSiteProofsForState 校验状态指针与配置代次。
func (s *Syncer) storeSiteProofs(proofs map[string]SiteCoverage) {
	s.storeSiteProofsLocked(proofs)
}

// storeSiteProofsForState 只在轮次捕获的状态仍是当前状态、且证明属于同一
// 配置代次时写入；旧轮次证明不得覆盖新状态。
func (s *Syncer) storeSiteProofsForState(state *RuntimeState, epoch uint64, proofs map[string]SiteCoverage) {
	_ = s.withStableConfig(func() bool {
		if state == nil || s.runtime.Snapshot() != state || s.Generation() != epoch {
			return false
		}
		filtered := make(map[string]SiteCoverage, len(proofs))
		for zone, proof := range proofs {
			if proof.ConfigEpoch != epoch {
				continue
			}
			filtered[zone] = proof
		}
		s.storeSiteProofsLocked(filtered)
		return true
	})
}

func (s *Syncer) storeSiteProofsLocked(proofs map[string]SiteCoverage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.roundCoverage == nil {
		s.roundCoverage = make(map[string]SiteCoverage)
	}
	for zone, proof := range proofs {
		key := zone + "\x00" + proof.TargetKey
		s.roundCoverage[key] = proof
	}
}

// RoundCoverage 返回本轮覆盖证明副本（Step 10 确认前置）。
func (s *Syncer) RoundCoverage() map[string]SiteCoverage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]SiteCoverage, len(s.roundCoverage))
	for k, v := range s.roundCoverage {
		out[k] = v
	}
	return out
}

// clearRoundCoverage 在新 EdgeOne 轮前清空上一轮证明。
func (s *Syncer) clearRoundCoverage() {
	s.mu.Lock()
	s.roundCoverage = nil
	s.mu.Unlock()
}

// EdgeOneRuntimeStatus 返回进程内站点状态副本（不读 SQLite、不发 TEO 请求）。
func (s *Syncer) EdgeOneRuntimeStatus() []EdgeOneSiteStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]EdgeOneSiteStatus, 0, len(s.edgeStatus))
	for _, st := range s.edgeStatus {
		cp := st
		cp.TargetIDs = append([]int(nil), st.TargetIDs...)
		cp.Coverage = append([]SiteTargetCoverage(nil), st.Coverage...)
		cp.LastQueryAt = copyTimePtr(st.LastQueryAt)
		cp.PlannedActiveAt = copyTimePtr(st.PlannedActiveAt)
		cp.ActualActiveAt = copyTimePtr(st.ActualActiveAt)
		if st.ActualConfirmed != nil {
			v := *st.ActualConfirmed
			cp.ActualConfirmed = &v
		}
		out = append(out, cp)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ZoneID < out[j].ZoneID })
	return out
}

func copyTimePtr(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	cp := *v
	return &cp
}

// peekPendingFeatureDue 读取首次启用/新站点产生的立即到期标记。
func (s *Syncer) peekPendingFeatureDue() (dns, edge bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pendingDNSDue, s.pendingEdgeOneDue
}

// consumePendingFeatureDue 消费首次启用/新站点产生的立即到期标记。
func (s *Syncer) consumePendingFeatureDue() (dns, edge bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dns, edge = s.pendingDNSDue, s.pendingEdgeOneDue
	s.pendingDNSDue = false
	s.pendingEdgeOneDue = false
	return dns, edge
}

// sourceDue 判断某个 ticker tick 是否真的到达本来源的到期时间。
func (s *Syncer) sourceDue(now time.Time, dnsSource bool) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if dnsSource {
		return s.dnsScheduled && !now.Before(s.dnsNextDue)
	}
	return s.edgeScheduled && !now.Before(s.edgeNextDue)
}

// setNextDue 推进来源的下一次到期时间。
func (s *Syncer) setNextDue(now time.Time, dnsSource bool, interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if dnsSource {
		s.dnsScheduled = true
		s.dnsNextDue = now.Add(interval)
		return
	}
	s.edgeScheduled = true
	s.edgeNextDue = now.Add(interval)
}

// clearNextDue 在来源关闭或总暂停时清空调度展示。
func (s *Syncer) clearNextDue(dnsSource bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if dnsSource {
		s.dnsScheduled = false
		s.dnsNextDue = time.Time{}
		return
	}
	s.edgeScheduled = false
	s.edgeNextDue = time.Time{}
}

// roundDispatch 描述一轮实际调度了哪些来源，供 ticker 重置与状态更新使用。
type roundDispatch struct {
	DNSDue       bool
	EdgeDue      bool
	DNSRan       bool
	EdgeRan      bool
	EdgeRetry    bool
	Preflight    PreflightOutcome
	PreflightErr error
}

// runScheduledRound 是 Run 唯一正式轮入口：先准备来源数据，双开时完成全局预检，
// 再只让获准且到期的来源进入目标执行。
//
// 被预检/数据完整性阻断、因而没有进入 syncAllStateWithInput 的轮次也必须在
// 返回时清空 round_started_at，不能留下假在途轮次。
func (s *Syncer) runScheduledRound(state *RuntimeState, dnsDue, edgeDue bool) roundDispatch {
	out := roundDispatch{DNSDue: dnsDue, EdgeDue: edgeDue}
	if state == nil {
		return out
	}
	defer s.clearRoundStarted()
	dnsDue = dnsDue && state.Config.DNSFeatureEnabled()
	edgeDue = edgeDue && state.Config.EdgeOneEnabled
	out.DNSDue, out.EdgeDue = dnsDue, edgeDue
	epoch := s.Generation()

	edgeData := map[string]edgeOneSiteData{}
	if state.Config.EdgeOneEnabled {
		// EdgeOne 到期（含手动/首次启用/新站点/恢复）必须实际查询；
		// 仅 DNS 到期时允许复用仍有效的缓存。
		edgeData = s.prepareEdgeOneData(state, epoch, edgeDue)
	}

	dnsWrite, edgeWrite := dnsDue, edgeDue
	var dnsData *roundDNSData
	if state.Config.DNSFeatureEnabled() && state.Config.EdgeOneEnabled {
		round := newDNSRound(state)
		defer round.finish()
		dnsData = s.resolveRoundDNS(state, round)
		edgePrefixes, edgeComplete := edgePrefixInputs(edgeData)
		outcome, err := s.evaluatePreflight(state, dnsData.addresses, dnsData.complete, edgePrefixes, edgeComplete)
		out.Preflight, out.PreflightErr = outcome, err
		s.storePreflight(state, epoch, outcome)
		if err != nil {
			dnsWrite, edgeWrite = false, false
		} else {
			dnsWrite = dnsDue && outcome.Admission.DNS
			edgeWrite = edgeDue && outcome.Admission.EdgeOne
		}
		if edgeDue && !edgeWrite {
			out.EdgeRetry = true
		}
	} else if state.Config.EdgeOneEnabled {
		edgeComplete := edgeDataComplete(edgeData)
		out.Preflight = PreflightOutcome{
			State:           PreflightStateSingleSource,
			EdgeOneComplete: edgeComplete,
			Admission:       Admission{EdgeOne: edgeComplete},
		}
		s.storePreflight(state, epoch, out.Preflight)
		edgeWrite = edgeDue && edgeComplete
		if edgeDue && !edgeWrite {
			out.EdgeRetry = true
		}
	} else if state.Config.DNSFeatureEnabled() {
		out.Preflight = PreflightOutcome{
			State:         PreflightStateSingleSource,
			DNSComplete:   true,
			KnownConflict: s.hasKnownConflict(),
			Admission:     Admission{DNS: true},
		}
		s.storePreflight(state, epoch, out.Preflight)
	}

	if dnsWrite || edgeWrite {
		s.syncAllStateWithInput(state, dnsData, edgeData, dnsWrite, edgeWrite, epoch)
		out.DNSRan = dnsWrite
		out.EdgeRan = edgeWrite
	}
	if edgeWrite {
		if retry := s.ConfirmEdgeOneSites(state, edgeData, epoch); retry {
			out.EdgeRetry = true
		}
	}
	return out
}

func edgeDataComplete(data map[string]edgeOneSiteData) bool {
	for _, item := range data {
		if item.unavailable != "" || strings.TrimSpace(item.snapshot.ZoneID) == "" ||
			strings.TrimSpace(item.snapshot.Current.Version) == "" ||
			!provider.OriginACLSnapshotPrefixesValid(item.snapshot) {
			return false
		}
	}
	return true
}

// storePreflight 仅在轮次捕获的状态与配置代次仍是当前值时更新只读摘要。
func (s *Syncer) storePreflight(state *RuntimeState, epoch uint64, outcome PreflightOutcome) {
	_ = s.withStableConfig(func() bool {
		if state == nil || s.runtime.Snapshot() != state || s.Generation() != epoch {
			return false
		}
		s.mu.Lock()
		s.preflight = outcome
		s.preflightAt = s.now()
		s.preflightSet = true
		s.mu.Unlock()
		return true
	})
}

// nextEdgeRetry 返回下一次 EdgeOne 查询/只读复查的退避时长。
func (s *Syncer) nextEdgeRetry(edgeInterval time.Duration) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.edgeRetryDelay <= 0 {
		s.edgeRetryDelay = time.Minute
	} else {
		s.edgeRetryDelay *= 2
	}
	cap := time.Hour
	if edgeInterval > 0 && edgeInterval < cap {
		cap = edgeInterval
	}
	if s.edgeRetryDelay > cap {
		s.edgeRetryDelay = cap
	}
	return s.edgeRetryDelay
}

func (s *Syncer) resetEdgeRetry() {
	s.mu.Lock()
	s.edgeRetryDelay = 0
	s.mu.Unlock()
}

// settleEdgeAfterStartResume 在启动/恢复轮结束后应用 EdgeOne 失败退避。
//
// 启动/恢复的“成功”保持轮前计时，因此这里只在 EdgeRetry 时推进 next_due；
// 成功时只重置退避计数，不改变已设置的正常到期时间。
func (s *Syncer) settleEdgeAfterStartResume(latest *RuntimeState, dispatch roundDispatch) (time.Duration, bool) {
	if !dispatch.EdgeDue {
		return 0, false
	}
	if !dispatch.EdgeRetry {
		s.resetEdgeRetry()
		return 0, false
	}
	retry := s.nextEdgeRetry(edgeOneIntervalOf(latest))
	s.setNextDue(s.now(), false, retry)
	return retry, true
}

// forgetEdgeOneSiteLocked 清除一个站点的进程内缓存、状态、确认内存证据与
// 覆盖摘要；调用方必须持有 s.mu（applyStateLocked 路径）。
func (s *Syncer) forgetEdgeOneSiteLocked(zone string) {
	zone = strings.TrimSpace(zone)
	if zone == "" {
		return
	}
	delete(s.edgeCache, zone)
	delete(s.edgeStatus, zone)
	delete(s.confirmedMem, zone)
	delete(s.confirmErrors, zone)
	for key := range s.roundCoverage {
		if strings.HasPrefix(key, zone+"\x00") {
			delete(s.roundCoverage, key)
		}
	}
}

// ForgetEdgeOneSite 清除单个站点的进程内运行状态；站点删除时使用。
func (s *Syncer) ForgetEdgeOneSite(zone string) {
	s.mu.Lock()
	s.forgetEdgeOneSiteLocked(zone)
	s.mu.Unlock()
}

// ForgetEdgeOneRuntime 清除全部 EdgeOne 进程内运行状态；reset/完整导入使用。
func (s *Syncer) ForgetEdgeOneRuntime() {
	s.mu.Lock()
	s.edgeCache = nil
	s.edgeStatus = nil
	s.roundCoverage = nil
	s.confirmedMem = nil
	s.confirmErrors = nil
	s.preflightSet = false
	s.preflight = PreflightOutcome{}
	s.preflightAt = time.Time{}
	s.edgeRetryDelay = 0
	s.mu.Unlock()
}

// roundDNSData 是一轮双开 DNS 解析的冻结输入：目标 attempt 只复用该结果，
// 不再在重试中重新解析。仅 DNS 运行时不使用本结构，保留原逐 attempt 合同。
type roundDNSData struct {
	resolved  map[int][]dns.ResolvedIP
	errors    map[int]string
	rawErrors map[int]error
	addresses []DNSAddress
	complete  bool
}

// resolveRoundDNS 对全部适用域名每 host 只解析一次，并用 dnsRound 记录真实
// 解析结果（半开探测与失败计数）。同一 host 的结果供所有规则/目标复用。
func (s *Syncer) resolveRoundDNS(state *RuntimeState, round *dnsRound) *roundDNSData {
	out := &roundDNSData{
		resolved:  make(map[int][]dns.ResolvedIP),
		errors:    make(map[int]string),
		rawErrors: make(map[int]error),
		complete:  true,
	}
	if state == nil || !state.Config.DNSFeatureEnabled() {
		return out
	}
	rules := applicableRules(state)
	if len(rules) == 0 {
		return out
	}
	type hostResult struct {
		ips []dns.ResolvedIP
		err error
	}
	cache := make(map[string]hostResult)
	failedHosts := make(map[string]bool)
	for _, rule := range rules {
		key := dns.DomainKey(rule.Host)
		hr, ok := cache[key]
		if !ok {
			hr.ips, hr.err = round.resolve(rule.Host, func() ([]dns.ResolvedIP, error) {
				return s.resolveHost(state, rule.Host)
			})
			cache[key] = hr
			if hr.err != nil && !failedHosts[key] {
				failedHosts[key] = true
				s.bus.Publish(notifier.Event{
					Type:      notifier.EventDNSFailed,
					Timestamp: s.now(),
					Data:      map[string]any{"domain": rule.Host, "error": hr.err.Error()},
				})
			}
		}
		if hr.err != nil {
			out.errors[rule.ID] = hr.err.Error()
			out.rawErrors[rule.ID] = hr.err
			out.complete = false
			continue
		}
		used := 0
		for _, ip := range hr.ips {
			if ip.IsIPv6 && !rule.EnableIPv6 {
				continue
			}
			ips := out.resolved[rule.ID]
			ips = append(ips, ip)
			out.resolved[rule.ID] = ips
			used++
			if addr, ok := netip.AddrFromSlice(ip.IP); ok {
				out.addresses = append(out.addresses, DNSAddress{Host: rule.Host, Addr: addr.Unmap()})
			} else {
				out.complete = false
			}
		}
		if used == 0 {
			out.complete = false
		}
	}
	sort.SliceStable(out.addresses, func(i, j int) bool {
		if out.addresses[i].Host != out.addresses[j].Host {
			return out.addresses[i].Host < out.addresses[j].Host
		}
		return out.addresses[i].Addr.String() < out.addresses[j].Addr.String()
	})
	return out
}

// applicableRules 返回至少绑定到一个当前 Provider 的 DNS 规则。
func applicableRules(state *RuntimeState) []config.DomainRule {
	ids := make(map[int]bool, len(state.Providers))
	for _, p := range state.Providers {
		ids[p.TargetIndex()] = true
	}
	out := make([]config.DomainRule, 0, len(state.Config.DomainRules))
	for _, rule := range state.Config.DomainRules {
		if len(rule.Targets) == 0 {
			out = append(out, rule)
			continue
		}
		for _, id := range rule.Targets {
			if ids[id] {
				out = append(out, rule)
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
