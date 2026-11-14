package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// EdgeOneTransitionStore 是确认状态的最小读写接口；*config.Store 实现。
type EdgeOneTransitionStore interface {
	GetEdgeOneTransition(ctx context.Context, zoneID string) (*config.EdgeOneTransition, error)
	ListEdgeOneTransitions(ctx context.Context) ([]config.EdgeOneTransition, error)
	SaveEdgeOneObservation(ctx context.Context, transition config.EdgeOneTransition) error
	// SaveEdgeOneProtectedObservation 显式写入 protected_cidrs；resetConfirmation
	// 用于已释放指纹被新指纹替换时强制重置确认状态。
	SaveEdgeOneProtectedObservation(ctx context.Context, transition config.EdgeOneTransition, resetConfirmation bool) error
	DeleteEdgeOneTransition(ctx context.Context, zoneID string) error
	SetEdgeOneConfirmation(ctx context.Context, zoneID, pendingVersion string, pendingCIDRs []netip.Prefix, state, requestID string, updated time.Time) (bool, error)
	RestoreUnsentEdgeOneConfirmation(ctx context.Context, zoneID, pendingVersion string, pendingCIDRs []netip.Prefix, preparedAt time.Time) (bool, error)
}

// EdgeOneUnknownConfirmationDiagnostic 用于本机只有 unknown 记录时的只读状态展示。
const EdgeOneUnknownConfirmationDiagnostic = "确认结果未知，需只读复查"

// SetEdgeOneTransitionStore 注入确认状态表；必须在 Run 前调用。
func (s *Syncer) SetEdgeOneTransitionStore(store EdgeOneTransitionStore) {
	s.mu.Lock()
	s.transitions = store
	s.mu.Unlock()
}

func (s *Syncer) transitionStore() EdgeOneTransitionStore {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.transitions
}

// requiredTargetKeys 返回站点全部实际绑定目标的物理身份（去重、稳定排序）。
func requiredTargetKeys(site config.EdgeOneSite, targets []config.TargetConfig) []string {
	byID := make(map[int]config.TargetConfig, len(targets))
	for _, t := range targets {
		byID[t.ID] = t
	}
	seen := make(map[string]bool, len(site.TargetIDs))
	out := make([]string, 0, len(site.TargetIDs))
	for _, id := range site.TargetIDs {
		t, ok := byID[id]
		if !ok {
			continue
		}
		key := config.TargetIdentity(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// siteProofsReady 校验站点全部实际目标的本轮证明与目标快照一致。
func siteProofsReady(site config.EdgeOneSite, targets []config.TargetConfig, proofs map[string]SiteCoverage, snapshot provider.OriginACLSnapshot, epoch uint64) bool {
	required := requiredTargetKeys(site, targets)
	if len(required) == 0 || snapshot.Next == nil {
		return false
	}
	wantCurrent := snapshot.Current.Version
	wantPending := snapshot.Next.Version
	wantDigest := snapshotDigest(snapshot)
	for _, targetKey := range required {
		proof, ok := proofs[site.ZoneID+"\x00"+targetKey]
		if !ok || !proof.Complete || proof.HasConflicts ||
			proof.Current != wantCurrent || proof.Pending != wantPending ||
			proof.SnapshotDigest != wantDigest || proof.ConfigEpoch != epoch {
			return false
		}
	}
	return true
}

// ConfirmEdgeOneSites 在目标执行完成后尝试站点自动确认。
//
// 返回 true 表示本轮存在需要退避复查的 unknown/错误；调度层据此不要重置为
// 正常 EdgeOne 间隔。所有网络调用都在稳定区外，发送前状态检查在区内。
func (s *Syncer) ConfirmEdgeOneSites(state *RuntimeState, edgeData map[string]edgeOneSiteData, epoch uint64) bool {
	if state == nil || !state.Config.EdgeOneEnabled || !state.Config.SyncEnabled || s.isStopped() {
		return false
	}
	store := s.transitionStore()
	if store == nil {
		return false
	}
	retry := false
	sites := append([]config.EdgeOneSite(nil), state.Config.EdgeOneSites...)
	sort.SliceStable(sites, func(i, j int) bool { return sites[i].ZoneID < sites[j].ZoneID })
	for _, site := range sites {
		zone := strings.TrimSpace(site.ZoneID)
		data := edgeData[zone]
		if zone == "" || data.unavailable != "" || data.snapshot.Next == nil {
			continue
		}
		fingerprint := pendingFingerprint(data.snapshot.Next)
		// 已 accepted/unknown 的同一指纹先于覆盖检查去重：accepted 不因本轮
		// 目标回读暂时不完整而重发；unknown 只做只读复查。
		if transition, terr := store.GetEdgeOneTransition(context.Background(), zone); terr == nil && transition != nil {
			expectedJSON, _ := canonicalCIDRListJSON(data.snapshot.Next.Prefixes)
			if transition.PendingVersion == data.snapshot.Next.Version &&
				canonicalCIDRListJSONString(transition.PendingCIDRs) == expectedJSON {
				switch transition.ConfirmationState {
				case config.EdgeOneConfirmationAccepted:
					s.setEdgeConfirmStatusForState(state, epoch, zone, fingerprint, config.EdgeOneConfirmationAccepted, transition.ConfirmationRequestID)
					continue
				case config.EdgeOneConfirmationUnknown:
					s.setEdgeConfirmStatusForState(state, epoch, zone, fingerprint, config.EdgeOneConfirmationUnknown, transition.ConfirmationRequestID)
					// unknown 必须先只读复查，不能仅凭缓存等待下一轮。
					if state.TEO != nil {
						if after, derr := state.TEO.DescribeOriginACL(context.Background(), zone); derr == nil {
							s.applyEdgeOneQueryResult(state, epoch, site, after, "", after.ObservedAt, after.PlannedActiveAt, true, transition)
							if transitionIsCurrent(transition, after) {
								continue
							}
						}
					}
					retry = true
					continue
				}
			}
		}
		if !siteProofsReady(site, state.Config.Targets, s.RoundCoverage(), data.snapshot, epoch) {
			continue
		}
		needRetry, err := s.confirmOneSite(state, site, data, store, epoch)
		if needRetry {
			retry = true
		}
		if err != nil {
			safe := safeConfirmErrorText(err)
			s.setEdgeConfirmErrorForState(state, epoch, zone, fingerprint, safe)
			slog.Warn("EdgeOne 站点确认未完成", "zone", zone, "reason", safe)
		}
	}
	return retry
}

func (s *Syncer) confirmOneSite(state *RuntimeState, site config.EdgeOneSite, data edgeOneSiteData, store EdgeOneTransitionStore, epoch uint64) (bool, error) {
	snapshot := data.snapshot
	zone := strings.TrimSpace(site.ZoneID)
	pending := *snapshot.Next
	fingerprint := pendingFingerprint(&pending)
	pendingJSON, err := canonicalCIDRListJSON(pending.Prefixes)
	if err != nil {
		return false, err
	}
	transition, err := store.GetEdgeOneTransition(context.Background(), zone)
	if err != nil {
		return true, fmt.Errorf("读取确认状态失败: %w", err)
	}
	if transition != nil && transition.PendingVersion == pending.Version &&
		canonicalCIDRListJSONString(transition.PendingCIDRs) == pendingJSON {
		switch transition.ConfirmationState {
		case config.EdgeOneConfirmationAccepted:
			s.setEdgeConfirmStatusForState(state, epoch, zone, fingerprint, config.EdgeOneConfirmationAccepted, transition.ConfirmationRequestID)
			return false, nil
		case config.EdgeOneConfirmationUnknown:
			// 同一指纹仍待确认：本轮只读复查，不自动重发。
			s.setEdgeConfirmStatusForState(state, epoch, zone, fingerprint, config.EdgeOneConfirmationUnknown, transition.ConfirmationRequestID)
			return true, nil
		}
	}
	if s.confirmedMemory(zone) == fingerprint {
		s.setEdgeConfirmStatusForState(state, epoch, zone, fingerprint, config.EdgeOneConfirmationAccepted, "")
		return false, nil
	}

	// 发送前重查：版本或规范网段变化时放弃本轮确认，交给下一轮重新规划。
	if state.TEO == nil {
		return true, errors.New("TEO 客户端未接线")
	}
	recheck, err := state.TEO.DescribeOriginACL(context.Background(), zone)
	if err != nil {
		return true, err
	}
	// 确认前重查也是成功查询：刷新缓存年龄/快照，供后续轮次复用。
	s.applyEdgeOneQueryResult(state, epoch, site, recheck, "", recheck.ObservedAt, recheck.PlannedActiveAt, true, transition)
	if recheck.Current.Version != snapshot.Current.Version ||
		currentFingerprint(recheck.Current) != currentFingerprint(snapshot.Current) ||
		recheck.Next == nil || recheck.Next.Version != pending.Version ||
		pendingFingerprint(recheck.Next) != fingerprint {
		s.setEdgeConfirmErrorForState(state, epoch, zone, fingerprint, "发送前重查发现版本或网段变化，本轮未发送")
		return true, nil
	}

	observation := config.EdgeOneTransition{
		ZoneID: zone, BaseVersion: snapshot.Current.Version, PendingVersion: pending.Version,
		PendingCIDRs: pending.Prefixes, ConfirmationState: config.EdgeOneConfirmationNotAttempted,
	}
	// The legacy upsert preserves explicit protection but replaces pending_cidrs.
	// Preserve any networks still represented only by that old fingerprint.
	needsProtectionWrite := false
	if transition != nil && !sameTransitionPending(*transition, pending.Version, pending.Prefixes) {
		keep := edgeOneTransitionProtected(transition)
		if len(subtractEdgeOnePrefixes(keep, transition.ProtectedCIDRs)) > 0 {
			if data.transitionErr != "" {
				return true, errors.New("旧网段保护尚未保存，延后确认")
			}
			observation.ProtectedCIDRs = unionEdgeOnePrefixes(keep, pending.Prefixes)
			needsProtectionWrite = true
		}
	}

	// 先提交 unknown，TEO 客户端等完限速后再做最终准入。
	var preparedAt time.Time
	prepared := s.withStableConfig(func() bool {
		if !s.canConfirmEdgeOne(state, epoch, site) {
			return false
		}
		observation.UpdatedAt = time.Now().UTC()
		var err error
		if needsProtectionWrite {
			err = store.SaveEdgeOneProtectedObservation(context.Background(), observation, true)
		} else {
			err = store.SaveEdgeOneObservation(context.Background(), observation)
		}
		if err != nil {
			return false
		}
		preparedAt = time.Now().UTC()
		ok, err := store.SetEdgeOneConfirmation(context.Background(), zone, pending.Version, pending.Prefixes,
			config.EdgeOneConfirmationUnknown, "", preparedAt)
		return err == nil && ok
	})
	if !prepared {
		return true, errors.New("确认前置状态提交失败或配置已变化")
	}

	admitted := false
	requestID, confirmErr := state.TEO.ConfirmOriginACLUpdate(context.Background(), zone, func() bool {
		admitted = s.withStableConfig(func() bool {
			return s.canConfirmEdgeOne(state, epoch, site)
		})
		return admitted
	})
	var teoErr *provider.TEOError
	errors.As(confirmErr, &teoErr)
	if teoErr != nil && teoErr.NotSent {
		retry := s.withStableConfig(func() bool {
			current := s.runtime.Snapshot() == state && s.Generation() == epoch
			if admitted && !current {
				return false
			}
			// 尚未取得最终准入且明确未发送时，可跨代次条件撤销本次前置记录。
			saved, saveErr := store.RestoreUnsentEdgeOneConfirmation(context.Background(), zone, pending.Version, pending.Prefixes, preparedAt)
			if saveErr != nil {
				slog.Warn("确认未发送，撤销前置状态失败", "zone", zone)
			}
			if !current {
				return false
			}
			if saveErr != nil || !saved {
				s.setEdgeConfirmStatus(zone, fingerprint, config.EdgeOneConfirmationUnknown, "", "确认未发送，但状态更新失败或记录已变化")
			} else {
				s.setEdgeConfirmStatus(zone, fingerprint, config.EdgeOneConfirmationNotAttempted, "", safeConfirmErrorText(confirmErr))
			}
			return true
		})
		return retry, nil
	}
	if teoErr != nil && teoErr.Kind == provider.TEOErrorAlreadyLatest {
		slog.Warn("EdgeOne 云端已是最新版本，未记录 accepted", "zone", zone)
		// 网络复查在稳定区外；返回后再次按代次校验。
		after, derr := state.TEO.DescribeOriginACL(context.Background(), zone)
		if derr == nil {
			s.applyEdgeOneQueryResult(state, epoch, site, after, "", after.ObservedAt, after.PlannedActiveAt, true, &observation)
		}
		current := s.withCurrentEdgeConfirmation(state, epoch, func() {
			s.setEdgeConfirmStatus(zone, fingerprint, config.EdgeOneConfirmationUnknown, "", "云端已是最新版本，未记录 accepted")
		})
		return current && (derr != nil || !transitionIsCurrent(&observation, after)), nil
	}

	retry := false
	s.withCurrentEdgeConfirmation(state, epoch, func() {
		switch {
		case confirmErr == nil:
			saved, saveErr := store.SetEdgeOneConfirmation(context.Background(), zone, pending.Version, pending.Prefixes,
				config.EdgeOneConfirmationAccepted, requestID, time.Now().UTC())
			if saveErr != nil || !saved {
				s.rememberConfirmed(zone, fingerprint)
				s.setEdgeConfirmStatus(zone, fingerprint, config.EdgeOneConfirmationUnknown, "", "确认成功但状态保存失败或指纹已变化")
				slog.Warn("EdgeOne 站点确认成功但状态保存失败", "zone", zone)
				retry = true
				return
			}
			s.forgetConfirmed(zone)
			s.setEdgeConfirmStatus(zone, fingerprint, config.EdgeOneConfirmationAccepted, requestID, "")
			slog.Info("EdgeOne 站点确认成功", "zone", zone, "pending_version", pending.Version, "request_id", requestID)
		case teoErr != nil && (teoErr.Kind == provider.TEOErrorPermission || teoErr.Kind == provider.TEOErrorDenied):
			saved, saveErr := store.SetEdgeOneConfirmation(context.Background(), zone, pending.Version, pending.Prefixes,
				config.EdgeOneConfirmationNotAttempted, "", time.Now().UTC())
			if saveErr != nil || !saved {
				s.setEdgeConfirmStatus(zone, fingerprint, config.EdgeOneConfirmationUnknown, "", "确认未执行，但状态更新失败或指纹已变化")
			} else {
				s.setEdgeConfirmStatus(zone, fingerprint, config.EdgeOneConfirmationNotAttempted, "", "确认未执行或服务端明确拒绝，允许下一轮重试")
			}
			retry = true
		default:
			s.setEdgeConfirmStatus(zone, fingerprint, config.EdgeOneConfirmationUnknown, "", "确认结果未知（超时或网络错误）")
			slog.Warn("EdgeOne 站点确认结果未知", "zone", zone)
			retry = true
		}
	})
	return retry, nil
}

// canConfirmEdgeOne 同锁读取 Stop 和配置状态。调用方须在协调器稳定区内。
func (s *Syncer) canConfirmEdgeOne(state *RuntimeState, epoch uint64, site config.EdgeOneSite) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.stopped || s.runtime.Snapshot() != state || s.generation != epoch ||
		!state.Config.SyncEnabled || !state.Config.EdgeOneEnabled {
		return false
	}
	currentSite, ok := edgeSiteForProof(state.Config.EdgeOneSites, site.ZoneID)
	return ok && edgeOneSiteKey(currentSite) == edgeOneSiteKey(site)
}

// withCurrentEdgeConfirmation 序列化配置发布和确认结果写入。网络操作始终在锁外。
func (s *Syncer) withCurrentEdgeConfirmation(state *RuntimeState, epoch uint64, apply func()) bool {
	return s.withStableConfig(func() bool {
		if s.runtime.Snapshot() != state || s.Generation() != epoch {
			return false
		}
		apply()
		return true
	})
}

func (s *Syncer) setEdgeConfirmStatusForState(state *RuntimeState, epoch uint64, zone string, fingerprint [32]byte, status, requestID string) {
	s.withCurrentEdgeConfirmation(state, epoch, func() {
		s.setEdgeConfirmStatus(zone, fingerprint, status, requestID, "")
	})
}

func (s *Syncer) setEdgeConfirmErrorForState(state *RuntimeState, epoch uint64, zone string, fingerprint [32]byte, errText string) {
	s.withCurrentEdgeConfirmation(state, epoch, func() {
		s.setEdgeConfirmStatus(zone, fingerprint, "", "", errText)
	})
}

func canonicalCIDRListJSON(prefixes []netip.Prefix) (string, error) {
	return configCanonicalCIDRJSON(prefixes)
}

func canonicalCIDRListJSONString(prefixes []netip.Prefix) string {
	s, _ := configCanonicalCIDRJSON(prefixes)
	return s
}

// 通过 config 包的导出辅助保持 JSON 口径一致；若未导出则使用本地实现。
func configCanonicalCIDRJSON(prefixes []netip.Prefix) (string, error) {
	values := make([]string, 0, len(prefixes))
	seen := make(map[string]bool, len(prefixes))
	for _, p := range prefixes {
		if !p.IsValid() {
			return "", errors.New("非法 CIDR")
		}
		s := p.Masked().String()
		if seen[s] {
			continue
		}
		seen[s] = true
		values = append(values, s)
	}
	sort.Strings(values)
	return canonicalJSONArray(values), nil
}

func canonicalJSONArray(values []string) string {
	raw, _ := json.Marshal(values)
	return string(raw)
}

func (s *Syncer) setEdgeConfirmStatus(zone string, fingerprint [32]byte, state, requestID, errText string) {
	zone = strings.TrimSpace(zone)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.edgeStatus == nil {
		s.edgeStatus = make(map[string]EdgeOneSiteStatus)
	}
	st := s.edgeStatus[zone]
	if state == "" && st.confirmationFingerprint == fingerprint {
		state, requestID = st.ConfirmationState, st.ConfirmationID
	}
	if state == config.EdgeOneConfirmationUnknown && errText == "" {
		errText = EdgeOneUnknownConfirmationDiagnostic
		if st.confirmationFingerprint == fingerprint && st.ConfirmationState == state && st.LastConfirmationErr != "" {
			errText = st.LastConfirmationErr
		}
	}
	st.ZoneID = zone
	st.confirmationFingerprint = fingerprint
	st.ConfirmationState = state
	st.ConfirmationID = requestID
	st.LastConfirmationErr = errText
	s.edgeStatus[zone] = st
}

// UnknownConfirmationDiagnostic 返回此 SQLite 指纹的安全诊断。
func (st EdgeOneSiteStatus) UnknownConfirmationDiagnostic(transition config.EdgeOneTransition) string {
	pending := provider.OriginACLVersion{Version: transition.PendingVersion, Prefixes: transition.PendingCIDRs}
	if st.confirmationFingerprint == pendingFingerprint(&pending) && st.LastConfirmationErr != "" {
		return st.LastConfirmationErr
	}
	return EdgeOneUnknownConfirmationDiagnostic
}

func (s *Syncer) confirmedMemory(zone string) [32]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.confirmedMem[strings.TrimSpace(zone)]
}

func (s *Syncer) rememberConfirmed(zone string, fingerprint [32]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.confirmedMem == nil {
		s.confirmedMem = make(map[string][32]byte)
	}
	s.confirmedMem[strings.TrimSpace(zone)] = fingerprint
}

func (s *Syncer) forgetConfirmed(zone string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.confirmedMem, strings.TrimSpace(zone))
}
