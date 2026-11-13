package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// edgeOneStatusReader 是 Syncer 的只读运行态扩展；生产 *syncer.Syncer 实现。
type edgeOneStatusReader interface {
	EdgeOneRuntimeStatus() []syncer.EdgeOneSiteStatus
	RoundCoverage() map[string]syncer.SiteCoverage
}

// handleGetEdgeOneStatus 只读本机状态：配置站点 + 运行快照 + 确认记录。
// 不访问腾讯云，不触发查询/确认，不修改任何运行记录。
func (d *Deps) handleGetEdgeOneStatus(w http.ResponseWriter, r *http.Request) {
	if d.Syncer == nil || d.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "EdgeOne 状态未接线")
		return
	}
	reader, ok := d.Syncer.(edgeOneStatusReader)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "EdgeOne 运行状态不可用")
		return
	}
	sites, err := d.Store.LoadEdgeOneSites()
	if err != nil {
		writeInternalError(w, "读取 EdgeOne 站点失败", err)
		return
	}
	transitions, err := d.Store.ListEdgeOneTransitions(r.Context())
	if err != nil {
		writeInternalError(w, "读取 EdgeOne 确认状态失败", err)
		return
	}
	byZone := make(map[string]config.EdgeOneTransition, len(transitions))
	for _, t := range transitions {
		byZone[t.ZoneID] = t
	}
	runtimeByZone := make(map[string]syncer.EdgeOneSiteStatus)
	for _, st := range reader.EdgeOneRuntimeStatus() {
		runtimeByZone[st.ZoneID] = st
	}
	coverage := reader.RoundCoverage()
	syncStatus := d.Syncer.Status()

	out := syncer.EdgeOneStatus{
		Enabled:   syncStatus.EdgeOne.Enabled,
		Effective: syncStatus.EdgeOne.Effective,
		NextDueAt: syncStatus.EdgeOne.NextDueAt,
		Sites:     []syncer.EdgeOneSiteStatus{},
	}
	for _, site := range sites {
		zone := strings.TrimSpace(site.ZoneID)
		st := runtimeByZone[zone]
		st.ZoneID = zone
		st.TargetIDs = append([]int{}, site.TargetIDs...)
		if st.Coverage == nil {
			st.Coverage = []syncer.SiteTargetCoverage{}
		}
		if t, ok := byZone[zone]; ok {
			st.ConfirmationState = t.ConfirmationState
			st.ConfirmationID = t.ConfirmationRequestID
			if t.ConfirmationState == config.EdgeOneConfirmationUnknown {
				st.LastConfirmationErr = st.UnknownConfirmationDiagnostic(t)
			}
		}
		if st.ConfirmationState == "" {
			st.ConfirmationState = config.EdgeOneConfirmationNotAttempted
		}
		st.Coverage = buildSiteTargetCoverage(site, d.Store, coverage)
		out.Sites = append(out.Sites, st)
	}
	sort.SliceStable(out.Sites, func(i, j int) bool { return out.Sites[i].ZoneID < out.Sites[j].ZoneID })
	writeJSON(w, http.StatusOK, out)
}

func buildSiteTargetCoverage(site config.EdgeOneSite, store *config.Store, proofs map[string]syncer.SiteCoverage) []syncer.SiteTargetCoverage {
	targets, err := store.GetTargets()
	if err != nil {
		return []syncer.SiteTargetCoverage{}
	}
	byID := make(map[int]config.TargetConfig, len(targets))
	for _, t := range targets {
		byID[t.ID] = t
	}
	seen := make(map[string]bool, len(site.TargetIDs))
	out := make([]syncer.SiteTargetCoverage, 0, len(site.TargetIDs))
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
		item := syncer.SiteTargetCoverage{TargetID: id, TargetKey: key, Complete: false, Reasons: []string{"no_coverage"}}
		if proof, ok := proofs[site.ZoneID+"\x00"+key]; ok {
			item.Complete = proof.Complete && !proof.HasConflicts
			item.Reasons = append([]string{}, proof.Reasons...)
			if item.Reasons == nil {
				item.Reasons = []string{}
			}
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TargetID < out[j].TargetID })
	return out
}
