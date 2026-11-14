package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	// Step 15：来源自身的最近运行时间/结论与间隔，供运行健康按来源判断。
	LastStartedAt   *time.Time `json:"last_started_at"`
	LastCompletedAt *time.Time `json:"last_completed_at"`
	LastOutcome     string     `json:"last_outcome"`
	IntervalSeconds int64      `json:"interval_seconds"`
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
	ProtectedCIDRs  []string               `json:"protected_cidrs"`
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
	// Step 11：当前过渡保护与清理证据。ProtectedCIDRs 为空数组表示已知无保护；
	// TransitionError 非空表示读取/写入过渡表失败，不能把未知当作无保护。
	ProtectedCIDRs           []string   `json:"protected_cidrs"`
	TransitionBaseVersion    string     `json:"transition_base_version"`
	TransitionPendingVersion string     `json:"transition_pending_version"`
	TransitionUpdatedAt      *time.Time `json:"transition_updated_at"`
	TransitionError          string     `json:"transition_error"`

	confirmationFingerprint [32]byte
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
	reused      *edgeOneCacheEntry
	queryFailed bool

	// Step 11 过渡保护：next 是本轮内存演进结果（可能待持久化），protected 是
	// 本轮预检/目标清理实际使用的保护集合；transitionErr 非空时 EdgeOne 输入
	// 不完整，但若已有内存保护仍可用于证明正向重叠。
	transition        *config.EdgeOneTransition
	protected         []netip.Prefix
	transitionChanged bool
	transitionDelete  bool
	transitionReset   bool
	transitionErr     string
	transitionReadErr string
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

// edgeOneTransitionsForRound 一次性读取本机过渡记录。
//
// store 未接线时返回空集合且不报错（零值测试夹具/历史入口）；生产启动始终
// 注入 Store。读取失败必须让 EdgeOne 输入不完整，不能把未知当作无保护。
func (s *Syncer) edgeOneTransitionsForRound() (map[string]config.EdgeOneTransition, string) {
	store := s.transitionStore()
	if store == nil {
		return map[string]config.EdgeOneTransition{}, ""
	}
	rows, err := store.ListEdgeOneTransitions(context.Background())
	if err != nil {
		return map[string]config.EdgeOneTransition{}, "读取过渡保护失败"
	}
	out := make(map[string]config.EdgeOneTransition, len(rows))
	for _, row := range rows {
		zone := strings.TrimSpace(row.ZoneID)
		if zone == "" {
			continue
		}
		row.ZoneID = zone
		out[zone] = row
	}
	return out, ""
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
	transitions, transitionErr := s.edgeOneTransitionsForRound()
	finish := func(site config.EdgeOneSite, data edgeOneSiteData) {
		zoneID := strings.TrimSpace(site.ZoneID)
		row, hasRow := transitions[zoneID]
		var rowPtr *config.EdgeOneTransition
		if hasRow {
			cp := row
			rowPtr = &cp
		}
		snapshotValid := data.unavailable == "" && !data.queryFailed &&
			strings.TrimSpace(data.snapshot.ZoneID) != "" &&
			strings.TrimSpace(data.snapshot.Current.Version) != "" &&
			provider.OriginACLSnapshotPrefixesValid(data.snapshot)
		var plan edgeOneTransitionPlanResult
		if snapshotValid {
			plan = planEdgeOneTransition(rowPtr, data.snapshot, s.now())
		} else {
			// 查询失败/输入不完整时只回填 Step 10 空保护，不执行释放。
			plan = edgeOneTransitionBackfill(rowPtr, s.now())
		}
		data.transition = plan.next
		data.protected = plan.protected
		data.transitionChanged = plan.changed
		data.transitionDelete = plan.delete
		data.transitionReset = plan.resetConfirmation
		data.transitionErr = transitionErr
		data.transitionReadErr = transitionErr
		out[zoneID] = data
		display := plan.next
		if display == nil {
			display = rowPtr
		}
		s.setEdgeSiteTransitionStatus(site, display, plan.protected, transitionErr)
	}
	for _, site := range state.Config.EdgeOneSites {
		zoneID := strings.TrimSpace(site.ZoneID)
		if zoneID == "" {
			continue
		}
		s.mu.RLock()
		cached, ok := s.edgeCache[zoneID]
		s.mu.RUnlock()
		fresh := ok && s.edgeCacheFresh(cached, site, state, s.now())
		if fresh && !force {
			data := edgeOneSiteData{site: site, snapshot: cached.snapshot, reused: &cached}
			s.applyEdgeOneQueryResult(state, epoch, site, cached.snapshot, "",
				cached.observedAt, cached.snapshot.PlannedActiveAt, false)
			finish(site, data)
			continue
		}
		if state.TEO == nil {
			data := edgeOneSiteData{site: site, unavailable: "TEO 客户端未接线"}
			s.applyEdgeOneQueryResult(state, epoch, site, provider.OriginACLSnapshot{},
				"TEO 客户端未接线", time.Time{}, nil, false)
			finish(site, data)
			continue
		}
		snapshot, err := state.TEO.DescribeOriginACL(context.Background(), zoneID)
		if err != nil {
			reason := safeTEOErrorText(err)
			if ok && s.edgeCacheFresh(cached, site, state, s.now()) {
				// 强制查询失败但旧缓存仍在有效期内：按合同使用最后成功快照。
				data := edgeOneSiteData{site: site, snapshot: cached.snapshot, reused: &cached, queryFailed: true}
				s.applyEdgeOneQueryResult(state, epoch, site, cached.snapshot, reason,
					cached.observedAt, cached.snapshot.PlannedActiveAt, false)
				finish(site, data)
				continue
			}
			data := edgeOneSiteData{site: site, unavailable: reason, queryFailed: true}
			s.applyEdgeOneQueryResult(state, epoch, site, provider.OriginACLSnapshot{},
				reason, time.Time{}, nil, false)
			finish(site, data)
			continue
		}
		observedAt := snapshot.ObservedAt
		if observedAt.IsZero() {
			observedAt = s.now()
		}
		data := edgeOneSiteData{site: site, snapshot: snapshot}
		s.applyEdgeOneQueryResult(state, epoch, site, snapshot, "",
			observedAt, snapshot.PlannedActiveAt, true)
		finish(site, data)
	}
	return out
}

// persistEdgeOneTransitions 在 EdgeOne 目标写入前尝试提交本轮保护演进。
//
// 成功时不改变内存结果；失败时按用户裁决 C 保留内存保护继续本轮，并记录
// 安全诊断供状态展示与后续轮次重试。读取失败已在 prepare 阶段标记，不在
// 这里重写或创建记录。
func (s *Syncer) persistEdgeOneTransitions(state *RuntimeState, epoch uint64, data map[string]edgeOneSiteData) {
	store := s.transitionStore()
	if store == nil {
		return
	}
	zones := make([]string, 0, len(data))
	for zone := range data {
		zones = append(zones, zone)
	}
	sort.Strings(zones)
	type failure struct {
		zone string
		err  error
	}
	failures := make([]failure, 0)
	applied := s.withStableConfig(func() bool {
		if state == nil || s.runtime.Snapshot() != state || s.Generation() != epoch {
			return false
		}
		for _, zone := range zones {
			item := data[zone]
			if item.transitionReadErr != "" {
				continue
			}
			var err error
			switch {
			case item.transitionDelete:
				err = store.DeleteEdgeOneTransition(context.Background(), zone)
			case item.transitionChanged && item.transition != nil:
				err = store.SaveEdgeOneProtectedObservation(context.Background(), *item.transition, item.transitionReset)
			default:
				continue
			}
			if err != nil {
				item.transitionErr = "写入过渡保护失败"
				data[zone] = item
				s.setEdgeSiteTransitionStatus(item.site, item.transition, item.protected, item.transitionErr)
				failures = append(failures, failure{zone: zone, err: err})
				continue
			}
			item.transitionChanged = false
			item.transitionDelete = false
			item.transitionErr = ""
			data[zone] = item
			s.setEdgeSiteTransitionStatus(item.site, item.transition, item.protected, item.transitionErr)
		}
		return true
	})
	if !applied {
		for _, zone := range zones {
			item := data[zone]
			if !item.transitionChanged && !item.transitionDelete {
				continue
			}
			item.transitionErr = "配置已变化，未写入过渡保护"
			data[zone] = item
			s.setEdgeSiteTransitionStatus(item.site, item.transition, item.protected, item.transitionErr)
		}
		slog.Warn("过渡保护未写入：配置已变化，使用内存保护继续本轮")
		return
	}
	for _, f := range failures {
		// 错误原文可能含 SQLite/路径细节，只记录固定阶段与站点。
		slog.Warn("过渡保护写入失败，使用内存保护继续本轮", "zone", f.zone, "error_type", fmt.Sprintf("%T", f.err))
	}
}

// validateReusedEdgeOneData 在预检使用前复核缓存，包含等待其他站点和 DNS 的时间。
func (s *Syncer) validateReusedEdgeOneData(state *RuntimeState, data map[string]edgeOneSiteData) bool {
	now := s.now()
	failed := false
	for zone, item := range data {
		failed = failed || item.queryFailed
		if item.reused != nil && !s.edgeCacheFresh(*item.reused, item.site, state, now) {
			item.unavailable = "EdgeOne 缓存已失效"
			data[zone] = item
		}
	}
	return failed
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
//
// 快照无效时仍加入过渡保护：查询失败不能把已知仍可能用于回源的网段
// 从正向重叠证据中删除。过渡表读取失败则完整性格外为 false。
func edgePrefixInputs(data map[string]edgeOneSiteData) ([]EdgeOnePrefix, bool) {
	zones := make([]string, 0, len(data))
	for zone := range data {
		zones = append(zones, zone)
	}
	sort.Strings(zones)
	complete := true
	out := make([]EdgeOnePrefix, 0)
	seen := make(map[string]bool)
	add := func(zone string, prefix netip.Prefix) {
		if !prefix.IsValid() {
			return
		}
		prefix = prefix.Masked()
		key := zone + "\x00" + prefix.String()
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, EdgeOnePrefix{ZoneID: zone, Prefix: prefix})
	}
	for _, zone := range zones {
		item := data[zone]
		if item.transitionReadErr != "" {
			complete = false
		}
		validSnapshot := item.unavailable == "" && strings.TrimSpace(item.snapshot.ZoneID) != "" &&
			strings.TrimSpace(item.snapshot.Current.Version) != "" &&
			provider.OriginACLSnapshotPrefixesValid(item.snapshot)
		if !validSnapshot {
			complete = false
		} else {
			for _, prefix := range item.snapshot.AllPrefixes() {
				add(zone, prefix)
			}
		}
		for _, prefix := range item.protected {
			add(zone, prefix)
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
			if st.confirmationFingerprint != pendingFingerprint(snapshot.Next) {
				st.ConfirmationState = ""
				st.ConfirmationID = ""
				st.LastConfirmationErr = ""
			}
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

// setEdgeSiteTransitionStatus 更新站点状态中的过渡保护摘要；不访问网络/数据库。
func (s *Syncer) setEdgeSiteTransitionStatus(site config.EdgeOneSite, row *config.EdgeOneTransition, protected []netip.Prefix, errText string) {
	zone := strings.TrimSpace(site.ZoneID)
	if zone == "" {
		return
	}
	values := protectedEdgeOneCIDRStrings(protected)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.edgeStatus == nil {
		s.edgeStatus = make(map[string]EdgeOneSiteStatus)
	}
	st := s.edgeStatus[zone]
	st.ZoneID = zone
	st.ProtectedCIDRs = values
	st.TransitionError = errText
	if row == nil {
		st.TransitionBaseVersion = ""
		st.TransitionPendingVersion = ""
		st.TransitionUpdatedAt = nil
	} else {
		st.TransitionBaseVersion = row.BaseVersion
		if row.BaseVersion != row.PendingVersion {
			st.TransitionPendingVersion = row.PendingVersion
		} else {
			// base == pending 表示已释放、仅保留旧替换保护残留；不是活动待更新版。
			st.TransitionPendingVersion = ""
		}
		updated := row.UpdatedAt
		st.TransitionUpdatedAt = &updated
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
		cp.ProtectedCIDRs = append([]string(nil), st.ProtectedCIDRs...)
		cp.LastQueryAt = copyTimePtr(st.LastQueryAt)
		cp.PlannedActiveAt = copyTimePtr(st.PlannedActiveAt)
		cp.ActualActiveAt = copyTimePtr(st.ActualActiveAt)
		cp.TransitionUpdatedAt = copyTimePtr(st.TransitionUpdatedAt)
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
	startedAt := s.now()
	if dnsDue {
		s.markSourceStarted(true, startedAt)
	}
	if edgeDue {
		s.markSourceStarted(false, startedAt)
	}
	epoch := s.Generation()

	edgeData := map[string]edgeOneSiteData{}
	if state.Config.EdgeOneEnabled {
		// EdgeOne 到期（含手动/首次启用/新站点/恢复）必须实际查询；
		// 仅 DNS 到期时允许复用仍有效的缓存。
		edgeData = s.prepareEdgeOneData(state, epoch, edgeDue)
		s.persistEdgeOneTransitions(state, epoch, edgeData)
	}

	dnsWrite, edgeWrite := dnsDue, edgeDue
	var dnsData *roundDNSData
	if state.Config.DNSFeatureEnabled() && state.Config.EdgeOneEnabled {
		round := newDNSRound(state)
		defer round.finish()
		dnsData = s.resolveRoundDNS(state, round)
		queryFailed := s.validateReusedEdgeOneData(state, edgeData)
		out.EdgeRetry = edgeDue && queryFailed
		edgePrefixes, edgeComplete := edgePrefixInputs(edgeData)
		outcome, err := s.evaluatePreflight(state, dnsData.addresses, dnsData.complete, edgePrefixes, edgeComplete)
		out.Preflight, out.PreflightErr = outcome, err
		s.storePreflight(state, epoch, outcome, edgePrefixes)
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
		queryFailed := s.validateReusedEdgeOneData(state, edgeData)
		out.EdgeRetry = edgeDue && queryFailed
		edgeComplete := edgeDataComplete(edgeData)
		out.Preflight = PreflightOutcome{
			State:           PreflightStateSingleSource,
			EdgeOneComplete: edgeComplete,
			Admission:       Admission{EdgeOne: edgeComplete},
		}
		edgePrefixes, _ := edgePrefixInputs(edgeData)
		s.storePreflight(state, epoch, out.Preflight, edgePrefixes)
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
		s.storePreflight(state, epoch, out.Preflight, nil)
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
	s.finishSourceResults(state, dnsDue, edgeDue, out)
	s.logPreflightOutcome(out.Preflight, out.PreflightErr)
	return out
}

// finishSourceResults 按本轮实际准入与执行情况记录来源级最近结论。
//
// 目标级 summary 仍是全局的；这里只把“该来源本轮是否获准并执行”与最终
// round outcome 组合，避免把预检阻断或数据不足误写成 idle。
func (s *Syncer) finishSourceResults(state *RuntimeState, dnsDue, edgeDue bool, out roundDispatch) {
	completed := s.now()
	if dnsDue {
		s.finishSourceResult(true, completed, s.sourceOutcome(state, true, out))
	}
	if edgeDue {
		s.finishSourceResult(false, completed, s.sourceOutcome(state, false, out))
	}
}

func (s *Syncer) sourceOutcome(state *RuntimeState, dnsSource bool, out roundDispatch) string {
	if out.PreflightErr != nil {
		return SourceOutcomeBlocked
	}
	if out.Preflight.State == PreflightStateOverlap {
		return SourceOutcomeBlocked
	}
	if dnsSource {
		if !state.Config.DNSFeatureEnabled() {
			return SourceOutcomeIdle
		}
		if !out.Preflight.Admission.DNS {
			if out.Preflight.KnownConflict {
				return SourceOutcomeBlocked
			}
			return SourceOutcomeIncomplete
		}
		if out.DNSRan {
			return s.lastRoundOutcome()
		}
		return SourceOutcomeIdle
	}
	if !state.Config.EdgeOneEnabled {
		return SourceOutcomeIdle
	}
	if !out.Preflight.Admission.EdgeOne {
		if out.Preflight.KnownConflict {
			return SourceOutcomeBlocked
		}
		return SourceOutcomeIncomplete
	}
	if out.EdgeRan {
		return s.lastRoundOutcome()
	}
	return SourceOutcomeIdle
}

func (s *Syncer) lastRoundOutcome() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lastRound == nil {
		return SourceOutcomeIdle
	}
	return string(s.lastRound.Outcome)
}

// logPreflightOutcome 只在预检状态边沿记录安全日志；持续冲突不重复刷屏。
func (s *Syncer) logPreflightOutcome(outcome PreflightOutcome, err error) {
	s.mu.Lock()
	previous, hadPrevious := s.preflightLogState, s.preflightLogSet
	s.preflightLogState = outcome.State
	s.preflightLogSet = true
	s.mu.Unlock()
	if err != nil {
		slog.Warn("全局预检状态读取失败，正式同步保持保守阻断", "error_type", fmt.Sprintf("%T", err))
		return
	}
	if hadPrevious && previous == outcome.State {
		return
	}
	switch outcome.State {
	case PreflightStateOverlap:
		if outcome.Overlap != nil {
			slog.Warn("全局预检地址重叠，正式同步阻断两侧",
				"dns_name", outcome.Overlap.DNS.Host,
				"dns_address", outcome.Overlap.DNS.Addr.Unmap().String(),
				"edgeone_prefix", outcome.Overlap.EdgeOne.Prefix.Masked().String(),
				"zone", outcome.Overlap.EdgeOne.ZoneID)
		} else {
			slog.Warn("全局预检保留已知地址冲突，正式同步阻断两侧")
		}
	case PreflightStateDataUnavailable:
		slog.Warn("全局预检数据不足", "dns_complete", outcome.DNSComplete, "edgeone_complete", outcome.EdgeOneComplete)
	case PreflightStateClear:
		if hadPrevious && previous != PreflightStateClear {
			slog.Info("全局预检已恢复：两侧完整且无地址重叠")
		}
	}
}

func edgeDataComplete(data map[string]edgeOneSiteData) bool {
	for _, item := range data {
		if item.transitionReadErr != "" || item.unavailable != "" ||
			strings.TrimSpace(item.snapshot.ZoneID) == "" ||
			strings.TrimSpace(item.snapshot.Current.Version) == "" ||
			!provider.OriginACLSnapshotPrefixesValid(item.snapshot) {
			return false
		}
	}
	return true
}

// storePreflight 仅在轮次捕获的状态与配置代次仍是当前值时更新只读摘要。
func (s *Syncer) storePreflight(state *RuntimeState, epoch uint64, outcome PreflightOutcome, protected []EdgeOnePrefix) {
	_ = s.withStableConfig(func() bool {
		if state == nil || s.runtime.Snapshot() != state || s.Generation() != epoch {
			return false
		}
		values := make([]string, 0, len(protected))
		seen := make(map[string]bool, len(protected))
		for _, item := range protected {
			if !item.Prefix.IsValid() {
				continue
			}
			v := item.Prefix.Masked().String()
			if seen[v] {
				continue
			}
			seen[v] = true
			values = append(values, v)
		}
		sort.Strings(values)
		s.mu.Lock()
		s.preflight = outcome
		s.preflightAt = s.now()
		s.preflightSet = true
		s.preflightProtected = values
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
			addr, ok := netip.AddrFromSlice(ip.IP)
			if !ok {
				out.complete = false
				continue
			}
			// 全局预检检查实际 DNS 应答，地址族限制仅用于目标规划。
			out.addresses = append(out.addresses, DNSAddress{Host: rule.Host, Addr: addr.Unmap()})
			if ip.IsIPv6 && !rule.EnableIPv6 {
				continue
			}
			out.resolved[rule.ID] = append(out.resolved[rule.ID], ip)
			used++
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
