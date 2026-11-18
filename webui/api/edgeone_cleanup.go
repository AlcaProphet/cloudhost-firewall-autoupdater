package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// 本文件承载 Build9 Step 4 的 EdgeOne TAG 清理只读预览入口。
//
// 预览只读：读取已发布运行时状态与四平台完整规则快照，可选只读解析适用 DNS
// 规则；不调用 Create/Delete、不发 TEO 确认、不写 SQLite、不发布配置、不修改
// 同步开关或正式覆盖状态。

// edgeOneCleanupPreviewRequest 是预览请求体。
//
// 客户端只能选择现有本地目标范围；TAG、候选与云端定位均由服务端根据当前
// 配置和完整快照确定。all 为 true 时不能同时提交非空 target_ids；空范围绝不
// 隐式扩大为全部。
type edgeOneCleanupPreviewRequest struct {
	TargetIDs []int `json:"target_ids"`
	All       *bool `json:"all"`
}

// edgeOneCleanupPreviewResponse 是只读预览响应。
type edgeOneCleanupPreviewResponse struct {
	PreviewID         string                 `json:"preview_id"`
	Tag               string                 `json:"tag"`
	Scope             string                 `json:"scope"`
	SelectedTargetIDs []int                  `json:"selected_target_ids"`
	CreatedAt         time.Time              `json:"created_at"`
	ExpiresAt         *time.Time             `json:"expires_at"`
	AllRead           bool                   `json:"all_read"`
	Summary           CleanupPreviewSummary  `json:"summary"`
	Targets           []CleanupPreviewTarget `json:"targets"`
	Warnings          []string               `json:"warnings"`
}

// handleEdgeOneCleanupPreview 生成只读清理预览和一次性预览标识。
func (d *Deps) handleEdgeOneCleanupPreview(w http.ResponseWriter, r *http.Request) {
	state := d.runtimeSnapshot()
	if state == nil || state.Pool == nil || strings.TrimSpace(state.Config.EdgeOneTag) == "" {
		writeError(w, http.StatusServiceUnavailable, "清理预览运行时尚未就绪")
		return
	}

	var req edgeOneCleanupPreviewRequest
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &req); err != nil {
		writeRequestError(w, err)
		return
	}
	selectedIDs, scopeMode, err := selectCleanupPreviewTargets(state, req)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	scope, err := cleanupScopeConfig(state, selectedIDs)
	if err != nil {
		if errors.Is(err, errCleanupScopeUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "清理预览运行时尚未就绪")
			return
		}
		writeRequestError(w, badRequest("所选目标已不在当前配置中"))
		return
	}

	registry := d.cleanupPreviewRegistry()
	targets := make([]CleanupPreviewTarget, 0, len(scope.Targets))
	for _, scoped := range scope.Targets {
		targets = append(targets, buildCleanupPreviewTarget(r.Context(), state, scoped, scope.Tag))
	}
	sort.SliceStable(targets, func(i, j int) bool {
		return targets[i].TargetIDs[0] < targets[j].TargetIDs[0]
	})

	summary := summarizeCleanupPreview(targets)
	warnings := cleanupPreviewWarnings(summary, targets)
	now := registry.now()
	resp := edgeOneCleanupPreviewResponse{
		Tag:               scope.Tag,
		Scope:             scopeMode,
		SelectedTargetIDs: append([]int(nil), selectedIDs...),
		CreatedAt:         now,
		AllRead:           summary.Targets > 0 && summary.TargetsUnread == 0,
		Summary:           summary,
		Targets:           targets,
		Warnings:          warnings,
	}

	// 只有至少一个目标实际读取成功才签发预览标识；全部未读时返回可见的
	// unread 结果，但不生成任何可被 Step 5 消费的授权。
	if summary.TargetsRead > 0 {
		digest, credentialFingerprint, derr := cleanupScopeDigest(state, selectedIDs)
		if derr != nil {
			writeError(w, http.StatusServiceUnavailable, "清理预览运行时尚未就绪")
			return
		}
		created, cerr := registry.Create(CleanupPreviewRecord{
			Tag:                   scope.Tag,
			Scope:                 scopeMode,
			SelectedTargetIDs:     append([]int(nil), selectedIDs...),
			Digest:                digest,
			CredentialFingerprint: credentialFingerprint,
			Targets:               targets,
			Warnings:              warnings,
		})
		if cerr != nil {
			writeInternalError(w, "生成清理预览标识失败", cerr)
			return
		}
		resp.PreviewID = created.ID
		resp.CreatedAt = created.CreatedAt
		expiresAt := created.ExpiresAt
		resp.ExpiresAt = &expiresAt
	}

	writeJSON(w, http.StatusOK, resp)
}

// selectCleanupPreviewTargets 解析请求范围，绝不把空选择扩大为全部。
func selectCleanupPreviewTargets(state *syncer.RuntimeState, req edgeOneCleanupPreviewRequest) ([]int, string, error) {
	if req.All != nil && *req.All {
		if len(req.TargetIDs) > 0 {
			return nil, "", badRequest("all 与 target_ids 不能同时提供")
		}
		ids, err := cleanupAllTargetIDs(state)
		if err != nil {
			return nil, "", badRequest("当前没有可用的已配置目标")
		}
		if len(ids) == 0 {
			return nil, "", badRequest("当前没有已配置目标")
		}
		return ids, cleanupScopeAll, nil
	}

	if len(req.TargetIDs) == 0 {
		return nil, "", badRequest("必须选择一个或多个目标，或显式选择全部")
	}
	known := make(map[int]bool, len(state.Config.Targets))
	for _, target := range state.Config.Targets {
		known[target.ID] = true
	}
	seen := make(map[int]bool, len(req.TargetIDs))
	ids := make([]int, 0, len(req.TargetIDs))
	missing := make([]int, 0)
	for _, id := range req.TargetIDs {
		if id <= 0 {
			return nil, "", badRequest("目标 ID 必须是大于 0 的十进制整数")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if !known[id] {
			missing = append(missing, id)
			continue
		}
		ids = append(ids, id)
	}
	if len(missing) > 0 {
		return nil, "", badRequest(fmt.Sprintf("目标不存在: %v", missing))
	}
	sort.Ints(ids)
	return ids, cleanupScopeSelected, nil
}

// buildCleanupPreviewTarget 读取一个物理目标的完整快照并归类。
func buildCleanupPreviewTarget(ctx context.Context, state *syncer.RuntimeState, scoped cleanupScopeTarget, tagValue string) CleanupPreviewTarget {
	target := CleanupPreviewTarget{
		TargetIDs:     append([]int(nil), scoped.TargetIDs...),
		CloudType:     scoped.CloudType,
		Region:        scoped.Region,
		ResourceID:    scoped.ResourceID,
		Sites:         scoped.Sites,
		Status:        cleanupTargetUnread,
		Rows:          []CleanupPreviewRow{},
		DNSDependency: cleanupDNSDependencyPotential(scoped.DNSRules, "目标快照未读取，无法验证 DNS 依赖"),
	}
	if target.Sites == nil {
		target.Sites = []config.EdgeOneSite{}
	}

	p, ok := cleanupProviderForTargets(state, scoped.TargetIDs)
	if !ok {
		target.ReasonCode = "provider_missing"
		target.Reason = "运行时 Provider 缺失"
		return target
	}
	if p.CloudType() != scoped.CloudType {
		target.ReasonCode = "provider_mismatch"
		target.Reason = "Provider 平台与目标不一致"
		return target
	}
	if ready, hint := providerCredentialsReady(scoped.CloudType, state.Config.Credentials); !ready {
		target.ReasonCode = "credentials_missing"
		target.Reason = hint
		return target
	}

	snapshot, err := p.GetSnapshot()
	if err != nil {
		code := "snapshot_failed"
		reason := "读取云端规则失败"
		if errors.Is(err, provider.ErrSnapshotIncomplete) {
			code = "snapshot_incomplete"
			reason = "云端规则快照不完整，无法验证"
		}
		slog.Warn("EdgeOne TAG 清理预览读取快照失败", "provider", p.Name(), "error", err)
		target.ReasonCode = code
		target.Reason = reason
		return target
	}
	if (scoped.CloudType == config.CloudTCLighthouse || scoped.CloudType == config.CloudTCCVM) &&
		strings.TrimSpace(snapshot.Revision) == "" {
		target.ReasonCode = "snapshot_incomplete"
		target.Reason = "云端规则快照缺少版本号，无法验证"
		return target
	}

	assessment := provider.AssessFirewallCleanup(scoped.CloudType, snapshot, tagValue)
	target.Status = cleanupTargetRead
	target.Revision = snapshot.Revision
	target.RulesRead = assessment.RulesRead
	target.MatchedRows = assessment.MatchedCount()
	target.RetainedRows = assessment.RetainedCount()
	target.DeletableRows = assessment.DeletableCount()
	target.SkippedRows = assessment.SkippedCount()
	target.Rows = buildCleanupPreviewRows(assessment)
	target.DNSDependency = assessCleanupDNSDependency(ctx, state, scoped, snapshot, assessment)
	return target
}

// cleanupProviderForTargets 从物理目标的别名中取任一个 Provider。
func cleanupProviderForTargets(state *syncer.RuntimeState, ids []int) (provider.Provider, bool) {
	byID := make(map[int]provider.Provider, len(state.Providers))
	for _, p := range state.Providers {
		byID[p.TargetIndex()] = p
	}
	for _, id := range ids {
		if p, ok := byID[id]; ok {
			return p, true
		}
	}
	return nil, false
}

// buildCleanupPreviewRows 把纯归类结果转换为稳定的 HTTP 明细。
func buildCleanupPreviewRows(assessment provider.FirewallCleanupAssessment) []CleanupPreviewRow {
	rows := make([]CleanupPreviewRow, 0, len(assessment.Rows))
	for _, row := range assessment.Rows {
		keys := make([]string, 0, len(row.Keys))
		family := ""
		for _, key := range row.Keys {
			keys = append(keys, key.String())
			if family == "" {
				family = string(key.Family)
			}
		}
		source := row.Rule.CidrBlock
		if source == "" {
			source = row.Rule.Ipv6CidrBlock
		}
		rows = append(rows, CleanupPreviewRow{
			Status:         row.Status,
			ReasonCode:     row.ReasonCode,
			Reason:         row.Reason,
			Protocol:       row.Rule.Protocol,
			Port:           row.Rule.Port,
			Source:         source,
			AddressFamily:  family,
			Action:         row.Rule.Action,
			Description:    row.Rule.Description,
			PolicyIndex:    row.Rule.PolicyIndex,
			RuleID:         row.Rule.RuleID,
			FunctionalKeys: keys,
			Rule:           row.Rule,
			Index:          row.Index,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Protocol != b.Protocol {
			return a.Protocol < b.Protocol
		}
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		if a.Action != b.Action {
			return a.Action < b.Action
		}
		if a.Description != b.Description {
			return a.Description < b.Description
		}
		if a.PolicyIndex != b.PolicyIndex {
			return a.PolicyIndex < b.PolicyIndex
		}
		return a.RuleID < b.RuleID
	})
	return rows
}

// summarizeCleanupPreview 汇总物理目标与物理行计数。
func summarizeCleanupPreview(targets []CleanupPreviewTarget) CleanupPreviewSummary {
	summary := CleanupPreviewSummary{Targets: len(targets)}
	for _, target := range targets {
		summary.RulesRead += target.RulesRead
		summary.MatchedRows += target.MatchedRows
		summary.RetainedRows += target.RetainedRows
		summary.DeletableRows += target.DeletableRows
		summary.SkippedRows += target.SkippedRows
		if target.Status == cleanupTargetRead {
			summary.TargetsRead++
		} else {
			summary.TargetsUnread++
		}
	}
	return summary
}

// cleanupPreviewWarnings 生成不含敏感信息的安全提示。
func cleanupPreviewWarnings(summary CleanupPreviewSummary, targets []CleanupPreviewTarget) []string {
	warnings := []string{}
	seen := make(map[string]bool)
	add := func(message string) {
		if message == "" || seen[message] {
			return
		}
		seen[message] = true
		warnings = append(warnings, message)
	}
	if summary.TargetsUnread > 0 {
		add("部分目标快照未读取，预览标识只覆盖已读取目标")
	}
	if summary.TargetsRead == 0 {
		add("没有可读取的目标，未生成预览标识")
	}
	if summary.SkippedRows > 0 {
		add("存在无法唯一定位的匹配行，已按行跳过")
	}
	for _, target := range targets {
		if target.DNSDependency.Potential && target.DNSDependency.Unverified {
			add("部分目标的 DNS 依赖未能验证，仅标记潜在依赖")
		}
		if target.DNSDependency.Confirmed {
			add("已确认 DNS 精确覆盖依赖，删除后 DNS 访问可能中断")
		}
	}
	return warnings
}

// cleanupDNSDependencyPotential 在未读取快照时给出保守的 DNS 依赖状态。
func cleanupDNSDependencyPotential(rules []config.DomainRule, reason string) CleanupDNSDependency {
	dep := CleanupDNSDependency{
		Potential:       len(rules) > 0,
		Status:          "none",
		ApplicableRules: len(rules),
	}
	if dep.Potential {
		dep.Status = "unverified"
		dep.Unverified = true
		dep.Reason = reason
	}
	return dep
}

// assessCleanupDNSDependency 计算删除整批可删行是否会移除 DNS 所需最后覆盖。
//
// 判定以“本次可删集合”为边界：只有某个 DNS desired key 的全部云端覆盖行都在
// 可删集合中，删除整批才会中断 DNS 访问。DNS TAG、手工、外部或跳过行覆盖时
// 不确认；解析失败只标记未验证，不阻塞预览。
func assessCleanupDNSDependency(
	ctx context.Context,
	state *syncer.RuntimeState,
	scoped cleanupScopeTarget,
	snapshot provider.RuleSnapshot,
	assessment provider.FirewallCleanupAssessment,
) CleanupDNSDependency {
	dep := CleanupDNSDependency{
		Potential:       len(scoped.DNSRules) > 0,
		Status:          "none",
		ApplicableRules: len(scoped.DNSRules),
	}
	if !dep.Potential {
		return dep
	}
	if assessment.DeletableCount() == 0 {
		dep.Status = "potential"
		dep.Reason = "存在适用 DNS 规则，但当前没有可删行"
		return dep
	}
	dnsTag := strings.TrimSpace(state.Config.Tag)
	if state.Resolver == nil || dnsTag == "" {
		dep.Status = "unverified"
		dep.Unverified = true
		dep.Reason = "DNS 解析器或 DNS TAG 不可用，无法确认依赖"
		return dep
	}

	resolved, dnsErrors, complete := resolveCleanupDNSRules(ctx, state.Resolver, scoped.DNSRules)
	plan := provider.PlanTarget(provider.TargetPlanInput{
		CloudType:  scoped.CloudType,
		Tag:        dnsTag,
		EdgeOneTag: strings.TrimSpace(state.Config.EdgeOneTag),
		Rules:      scoped.DNSRules,
		Resolved:   resolved,
		DNSErrors:  dnsErrors,
		Snapshot:   snapshot,
	})
	desired := make(map[provider.FunctionalKey]bool)
	for _, item := range plan.Desired {
		if item.Implementable && len(item.RuleIDs) > 0 {
			desired[item.Key] = true
		}
	}
	if len(desired) == 0 {
		if !complete {
			dep.Status = "unverified"
			dep.Unverified = true
			dep.Reason = "部分 DNS 规则未能解析，未发现可确认覆盖"
		} else {
			dep.Status = "potential"
			dep.Reason = "存在适用 DNS 规则，但当前没有可实施的 DNS 期望覆盖候选"
		}
		return dep
	}

	keyRows := make(map[provider.FunctionalKey][]int)
	for index, rule := range snapshot.Rules {
		keys, ok := provider.SnapshotRuleKeys(rule)
		if !ok {
			continue
		}
		seen := make(map[provider.FunctionalKey]bool, len(keys))
		for _, key := range keys {
			if seen[key] {
				continue
			}
			seen[key] = true
			keyRows[key] = append(keyRows[key], index)
		}
	}
	deletable := make(map[int]bool)
	for _, row := range assessment.DeletableRows() {
		deletable[row.Index] = true
	}

	confirmedRows := make(map[int]bool)
	for key := range desired {
		indices := keyRows[key]
		if len(indices) == 0 {
			continue
		}
		allDeletable := true
		for _, index := range indices {
			if !deletable[index] {
				allDeletable = false
				break
			}
		}
		if !allDeletable {
			continue
		}
		for _, index := range indices {
			confirmedRows[index] = true
		}
	}

	dep.ConfirmedRows = len(confirmedRows)
	dep.Confirmed = dep.ConfirmedRows > 0
	dep.Unverified = !complete
	switch {
	case dep.Confirmed:
		dep.Status = "confirmed"
		dep.Reason = "删除这些行会移除 DNS 规则所需的最后一份精确覆盖"
		if !complete {
			dep.Reason += "；部分 DNS 规则未能解析"
		}
	case !complete:
		dep.Status = "unverified"
		dep.Reason = "部分 DNS 规则未能解析，无法确认依赖"
	default:
		dep.Status = "potential"
		dep.Reason = "存在适用 DNS 规则，但当前可删行不是其最后覆盖"
	}
	return dep
}

// resolveCleanupDNSRules 只读解析适用 DNS 规则；不写熔断器、不发事件。
func resolveCleanupDNSRules(
	ctx context.Context,
	resolver *dns.Resolver,
	rules []config.DomainRule,
) (map[int][]dns.ResolvedIP, map[int]string, bool) {
	resolved := make(map[int][]dns.ResolvedIP)
	dnsErrors := make(map[int]string)
	complete := true

	type hostResult struct {
		ips []dns.ResolvedIP
		err error
	}
	cache := make(map[string]hostResult)
	for _, rule := range rules {
		key := dns.DomainKey(rule.Host)
		result, ok := cache[key]
		if !ok {
			result.ips, result.err = resolver.Resolve(ctx, rule.Host)
			cache[key] = result
		}
		if result.err != nil || len(result.ips) == 0 {
			dnsErrors[rule.ID] = "DNS 解析失败或为空"
			complete = false
			continue
		}
		used := make([]dns.ResolvedIP, 0, len(result.ips))
		for _, ip := range result.ips {
			if ip.IsIPv6 && !rule.EnableIPv6 {
				continue
			}
			used = append(used, ip)
		}
		if len(used) > 0 {
			resolved[rule.ID] = used
		}
	}
	return resolved, dnsErrors, complete
}
