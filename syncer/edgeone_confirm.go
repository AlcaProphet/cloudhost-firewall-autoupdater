package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	SaveEdgeOneObservation(ctx context.Context, transition config.EdgeOneTransition) error
	SetEdgeOneConfirmation(ctx context.Context, zoneID, pendingVersion string, pendingCIDRs []netip.Prefix, state, requestID string, updated time.Time) (bool, error)
}

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
		// 已 accepted/unknown 的同一指纹先于覆盖检查去重：accepted 不因本轮
		// 目标回读暂时不完整而重发；unknown 只做只读复查。
		if transition, terr := store.GetEdgeOneTransition(context.Background(), zone); terr == nil && transition != nil {
			expectedJSON, _ := canonicalCIDRListJSON(data.snapshot.Next.Prefixes)
			if transition.PendingVersion == data.snapshot.Next.Version &&
				canonicalCIDRListJSONString(transition.PendingCIDRs) == expectedJSON {
				switch transition.ConfirmationState {
				case config.EdgeOneConfirmationAccepted:
					s.setEdgeConfirmStatusForState(state, epoch, zone, config.EdgeOneConfirmationAccepted, transition.ConfirmationRequestID, "")
					continue
				case config.EdgeOneConfirmationUnknown:
					s.setEdgeConfirmStatusForState(state, epoch, zone, config.EdgeOneConfirmationUnknown, transition.ConfirmationRequestID, "")
					// unknown 必须先只读复查，不能仅凭缓存等待下一轮。
					if state.TEO != nil {
						if after, derr := state.TEO.DescribeOriginACL(context.Background(), zone); derr == nil {
							s.applyEdgeOneQueryResult(state, epoch, site, after, "", after.ObservedAt, after.PlannedActiveAt, true)
							if after.Next == nil {
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
		needRetry, err := s.confirmOneSite(state, site, data.snapshot, store, epoch)
		if needRetry {
			retry = true
		}
		if err != nil {
			s.setEdgeConfirmErrorForState(state, epoch, zone, safeConfirmErrorText(err))
		}
	}
	return retry
}

func (s *Syncer) confirmOneSite(state *RuntimeState, site config.EdgeOneSite, snapshot provider.OriginACLSnapshot, store EdgeOneTransitionStore, epoch uint64) (bool, error) {
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
			s.setEdgeConfirmStatusForState(state, epoch, zone, config.EdgeOneConfirmationAccepted, transition.ConfirmationRequestID, "")
			return false, nil
		case config.EdgeOneConfirmationUnknown:
			// 同一指纹仍待确认：本轮只读复查，不自动重发。
			s.setEdgeConfirmStatusForState(state, epoch, zone, config.EdgeOneConfirmationUnknown, transition.ConfirmationRequestID, "")
			return true, nil
		}
	}
	if s.confirmedMemory(zone) == fingerprint {
		s.setEdgeConfirmStatusForState(state, epoch, zone, config.EdgeOneConfirmationAccepted, "", "")
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
	s.applyEdgeOneQueryResult(state, epoch, site, recheck, "", recheck.ObservedAt, recheck.PlannedActiveAt, true)
	if recheck.Current.Version != snapshot.Current.Version ||
		currentFingerprint(recheck.Current) != currentFingerprint(snapshot.Current) ||
		recheck.Next == nil || recheck.Next.Version != pending.Version ||
		pendingFingerprint(recheck.Next) != fingerprint {
		s.setEdgeConfirmErrorForState(state, epoch, zone, "发送前重查发现版本或网段变化，本轮未发送")
		return true, nil
	}

	// 发送前状态准入与 unknown 前置提交在同一稳定区；网络调用不在锁内。
	prepared := false
	allowed := s.withStableConfig(func() bool {
		if s.runtime.Snapshot() != state || s.Generation() != epoch || s.isStopped() {
			return false
		}
		if !state.Config.SyncEnabled || !state.Config.EdgeOneEnabled {
			return false
		}
		currentSite, ok := edgeSiteForProof(state.Config.EdgeOneSites, zone)
		if !ok || edgeOneSiteKey(currentSite) != edgeOneSiteKey(site) {
			return false
		}
		if err := store.SaveEdgeOneObservation(context.Background(), config.EdgeOneTransition{
			ZoneID:            zone,
			BaseVersion:       snapshot.Current.Version,
			PendingVersion:    pending.Version,
			PendingCIDRs:      pending.Prefixes,
			ProtectedCIDRs:    nil,
			ConfirmationState: config.EdgeOneConfirmationNotAttempted,
			UpdatedAt:         time.Now().UTC(),
		}); err != nil {
			return false
		}
		ok, err = store.SetEdgeOneConfirmation(context.Background(), zone, pending.Version, pending.Prefixes,
			config.EdgeOneConfirmationUnknown, "", time.Now().UTC())
		if err != nil || !ok {
			return false
		}
		prepared = true
		return true
	})
	if !allowed || !prepared {
		return true, errors.New("确认前置状态提交失败或配置已变化")
	}

	requestID, confirmErr := state.TEO.ConfirmOriginACLUpdate(context.Background(), zone)
	var teoErr *provider.TEOError
	if errors.As(confirmErr, &teoErr) && teoErr.Kind == provider.TEOErrorAlreadyLatest {
		// 网络复查在稳定区外；返回后再次按代次校验。
		after, derr := state.TEO.DescribeOriginACL(context.Background(), zone)
		if derr == nil {
			s.applyEdgeOneQueryResult(state, epoch, site, after, "", after.ObservedAt, after.PlannedActiveAt, true)
		}
		current := s.withCurrentEdgeConfirmation(state, epoch, func() {
			s.setEdgeConfirmStatus(zone, config.EdgeOneConfirmationUnknown, "", "云端已是最新版本，未记录 accepted")
		})
		return current && (derr != nil || (after.Next != nil && currentFingerprint(after.Current) != currentFingerprint(pending))), nil
	}

	retry := false
	s.withCurrentEdgeConfirmation(state, epoch, func() {
		switch {
		case confirmErr == nil:
			saved, saveErr := store.SetEdgeOneConfirmation(context.Background(), zone, pending.Version, pending.Prefixes,
				config.EdgeOneConfirmationAccepted, requestID, time.Now().UTC())
			if saveErr != nil || !saved {
				s.rememberConfirmed(zone, fingerprint)
				s.setEdgeConfirmError(zone, "确认成功但状态保存失败或指纹已变化")
				retry = true
				return
			}
			s.forgetConfirmed(zone)
			s.setEdgeConfirmStatus(zone, config.EdgeOneConfirmationAccepted, requestID, "")
		case teoErr != nil && (teoErr.NotSent || teoErr.Kind == provider.TEOErrorPermission || teoErr.Kind == provider.TEOErrorDenied):
			saved, saveErr := store.SetEdgeOneConfirmation(context.Background(), zone, pending.Version, pending.Prefixes,
				config.EdgeOneConfirmationNotAttempted, "", time.Now().UTC())
			if saveErr != nil || !saved {
				s.setEdgeConfirmStatus(zone, config.EdgeOneConfirmationUnknown, "", "确认未执行，但状态更新失败或指纹已变化")
			} else {
				s.setEdgeConfirmStatus(zone, config.EdgeOneConfirmationNotAttempted, "", "确认未执行或服务端明确拒绝，允许下一轮重试")
			}
			retry = true
		default:
			s.setEdgeConfirmStatus(zone, config.EdgeOneConfirmationUnknown, "", "确认结果未知（超时或网络错误）")
			retry = true
		}
	})
	return retry, nil
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

func (s *Syncer) setEdgeConfirmStatusForState(state *RuntimeState, epoch uint64, zone, status, requestID, errText string) {
	s.withCurrentEdgeConfirmation(state, epoch, func() {
		s.setEdgeConfirmStatus(zone, status, requestID, errText)
	})
}

func (s *Syncer) setEdgeConfirmErrorForState(state *RuntimeState, epoch uint64, zone, errText string) {
	s.withCurrentEdgeConfirmation(state, epoch, func() {
		s.setEdgeConfirmError(zone, errText)
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

func (s *Syncer) setEdgeConfirmStatus(zone, state, requestID, errText string) {
	zone = strings.TrimSpace(zone)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.edgeStatus == nil {
		s.edgeStatus = make(map[string]EdgeOneSiteStatus)
	}
	st := s.edgeStatus[zone]
	st.ZoneID = zone
	st.ConfirmationState = state
	if requestID != "" {
		st.ConfirmationID = requestID
	}
	st.LastConfirmationErr = errText
	s.edgeStatus[zone] = st
}

func (s *Syncer) setEdgeConfirmError(zone, errText string) {
	zone = strings.TrimSpace(zone)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.confirmErrors == nil {
		s.confirmErrors = make(map[string]string)
	}
	s.confirmErrors[zone] = errText
	if s.edgeStatus == nil {
		s.edgeStatus = make(map[string]EdgeOneSiteStatus)
	}
	st := s.edgeStatus[zone]
	st.ZoneID = zone
	st.LastConfirmationErr = errText
	s.edgeStatus[zone] = st
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
