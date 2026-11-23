package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// 本文件承载 Build9 Step 4 的一次性预览标识与范围摘要。
//
// 预览记录只存在于进程内存，不进入 SQLite、配置包、日志表或正式覆盖状态。
// Step 5 在执行任何云端删除前必须调用 Take；Take 原子消费记录，只负责本地
// 存在性、过期、重复提交、配置范围与凭据代次校验。云端版本、稳定定位和候选
// 整行身份仍由 Step 5 在执行前重新读取快照核对。

const (
	// cleanupPreviewTTL 是一次预览标识的最长有效时间。
	cleanupPreviewTTL = 10 * time.Minute
	// cleanupPreviewRetention 在过期后继续保留终态记录的时间，用于重复提交/
	// 过期诊断；超过后在下一次创建时机会式清理。
	cleanupPreviewRetention = time.Hour

	cleanupPreviewActive      = "active"
	cleanupPreviewConsumed    = "consumed"
	cleanupPreviewExpired     = "expired"
	cleanupPreviewInvalidated = "invalidated"

	cleanupScopeSelected = "selected"
	cleanupScopeAll      = "all"

	cleanupTargetRead   = "read"
	cleanupTargetUnread = "unread"

	// CleanupPreviewNotFound 预览标识不存在（进程重启后也会得到该原因）。
	CleanupPreviewNotFound = "preview_not_found"
	// CleanupPreviewExpired 预览标识已过期。
	CleanupPreviewExpired = "preview_expired"
	// CleanupPreviewConsumed 预览标识已被一次确认消费。
	CleanupPreviewConsumed = "preview_consumed"
	// CleanupPreviewConfigChanged 当前配置范围/凭据/TAG/站点/DNS 规则已变化。
	CleanupPreviewConfigChanged = "preview_config_changed"
	// CleanupPreviewInvalidated 预览标识已因先前的配置变化失效。
	CleanupPreviewInvalidated = "preview_invalidated"
	// CleanupPreviewUnavailable 当前运行时状态不可用于复核。
	CleanupPreviewUnavailable = "preview_unavailable"
)

var (
	// errCleanupScopeUnavailable 表示运行时状态、凭据池或 TAG 不可用。
	errCleanupScopeUnavailable = errors.New("清理预览运行时不可用")
	// errCleanupScopeChanged 表示所选目标已不在当前配置中。
	errCleanupScopeChanged = errors.New("清理预览范围已变化")
)

// CleanupPreviewRow 是一条当前 TAG 匹配物理行的只读明细。
//
// Rule 与 Index 只供服务端 Step 5 重新核对候选身份，不写入 HTTP 响应。
type CleanupPreviewRow struct {
	Status         string   `json:"status"`
	ReasonCode     string   `json:"reason_code,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	Protocol       string   `json:"protocol"`
	Port           string   `json:"port"`
	Source         string   `json:"source"`
	AddressFamily  string   `json:"address_family"`
	Action         string   `json:"action"`
	Description    string   `json:"description"`
	PolicyIndex    string   `json:"policy_index,omitempty"`
	RuleID         string   `json:"rule_id,omitempty"`
	FunctionalKeys []string `json:"functional_keys"`

	Rule  config.RuleInfo `json:"-"`
	Index int             `json:"-"`
}

// CleanupDNSDependency 是一个物理目标的 DNS 覆盖依赖摘要。
type CleanupDNSDependency struct {
	Potential       bool   `json:"potential"`
	Confirmed       bool   `json:"confirmed"`
	Unverified      bool   `json:"unverified"`
	Status          string `json:"status"`
	ApplicableRules int    `json:"applicable_rules"`
	ConfirmedRows   int    `json:"confirmed_rows"`
	Reason          string `json:"reason,omitempty"`
}

// CleanupPreviewTarget 是一个物理目标的预览结果；未读目标也出现在同一数组。
type CleanupPreviewTarget struct {
	TargetIDs     []int                `json:"target_ids"`
	CloudType     config.CloudType     `json:"cloud_type"`
	Region        string               `json:"region"`
	ResourceID    string               `json:"resource_id"`
	Sites         []config.EdgeOneSite `json:"sites"`
	Status        string               `json:"status"`
	ReasonCode    string               `json:"reason_code,omitempty"`
	Reason        string               `json:"reason,omitempty"`
	Revision      string               `json:"revision"`
	RulesRead     int                  `json:"rules_read"`
	MatchedRows   int                  `json:"matched_rows"`
	RetainedRows  int                  `json:"retained_rows"`
	DeletableRows int                  `json:"deletable_rows"`
	SkippedRows   int                  `json:"skipped_rows"`
	Rows          []CleanupPreviewRow  `json:"rows"`
	DNSDependency CleanupDNSDependency `json:"dns_dependency"`
}

// CleanupPreviewSummary 是一次预览的物理行汇总。
type CleanupPreviewSummary struct {
	Targets       int `json:"targets"`
	TargetsRead   int `json:"targets_read"`
	TargetsUnread int `json:"targets_unread"`
	RulesRead     int `json:"rules_read"`
	MatchedRows   int `json:"matched_rows"`
	RetainedRows  int `json:"retained_rows"`
	DeletableRows int `json:"deletable_rows"`
	SkippedRows   int `json:"skipped_rows"`
}

// CleanupPreviewRecord 是服务端一次性预览记录。
type CleanupPreviewRecord struct {
	ID                    string
	Tag                   string
	Scope                 string
	SelectedTargetIDs     []int
	CreatedAt             time.Time
	ExpiresAt             time.Time
	Digest                string
	CredentialFingerprint string
	Targets               []CleanupPreviewTarget
	Warnings              []string
	Status                string
	ConsumedAt            *time.Time
}

// CleanupPreviewRegistry 是进程内一次性预览注册表。
type CleanupPreviewRegistry struct {
	mu       sync.Mutex
	previews map[string]*CleanupPreviewRecord
	now      func() time.Time
}

// NewCleanupPreviewRegistry 创建空注册表。
func NewCleanupPreviewRegistry() *CleanupPreviewRegistry {
	return &CleanupPreviewRegistry{
		previews: make(map[string]*CleanupPreviewRecord),
		now:      time.Now,
	}
}

// Create 保存一份新预览记录并分配随机标识。
func (r *CleanupPreviewRegistry) Create(record CleanupPreviewRecord) (CleanupPreviewRecord, error) {
	id, err := newCleanupPreviewID()
	if err != nil {
		return CleanupPreviewRecord{}, err
	}
	now := r.now()
	record.ID = id
	record.CreatedAt = now
	record.ExpiresAt = now.Add(cleanupPreviewTTL)
	record.Status = cleanupPreviewActive
	record.ConsumedAt = nil
	record.SelectedTargetIDs = append([]int(nil), record.SelectedTargetIDs...)
	record.Warnings = append([]string(nil), record.Warnings...)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.purgeLocked(now)
	stored := cleanupPreviewRecordCopy(record)
	r.previews[id] = &stored
	return cleanupPreviewRecordCopy(stored), nil
}

// Lookup 只读返回预览状态，不消费标识。
//
// 过期时会把终态标为 expired；已消费/已失效记录保留原因，供重复提交识别。
func (r *CleanupPreviewRegistry) Lookup(id string) (CleanupPreviewRecord, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.previews[id]
	if !ok {
		return CleanupPreviewRecord{}, CleanupPreviewNotFound
	}
	r.markExpiredLocked(record)
	switch record.Status {
	case cleanupPreviewConsumed:
		return cleanupPreviewRecordCopy(*record), CleanupPreviewConsumed
	case cleanupPreviewExpired:
		return cleanupPreviewRecordCopy(*record), CleanupPreviewExpired
	case cleanupPreviewInvalidated:
		return cleanupPreviewRecordCopy(*record), CleanupPreviewInvalidated
	default:
		return cleanupPreviewRecordCopy(*record), ""
	}
}

// Take 原子复核并消费一次预览标识。
//
// current 必须是 Step 5 执行前从 RuntimeManager 取得的同一份不可变运行时状态。
// 本方法只做本地校验；成功返回后 Step 5 只能使用 Status==read 的目标和
// Status==deletable 的物理行重新读取云端快照并核对定位，unread/skipped 行不得
// 进入任何删除请求。
func (r *CleanupPreviewRegistry) Take(id string, current *syncer.RuntimeState) (CleanupPreviewRecord, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.previews[id]
	if !ok {
		return CleanupPreviewRecord{}, CleanupPreviewNotFound
	}
	now := r.now()
	r.markExpiredLocked(record)
	switch record.Status {
	case cleanupPreviewConsumed:
		return cleanupPreviewRecordCopy(*record), CleanupPreviewConsumed
	case cleanupPreviewExpired:
		return cleanupPreviewRecordCopy(*record), CleanupPreviewExpired
	case cleanupPreviewInvalidated:
		return cleanupPreviewRecordCopy(*record), CleanupPreviewInvalidated
	}

	if current == nil || current.Pool == nil {
		return cleanupPreviewRecordCopy(*record), CleanupPreviewUnavailable
	}
	if record.Scope == cleanupScopeAll {
		ids, err := cleanupAllTargetIDs(current)
		if err != nil {
			return cleanupPreviewRecordCopy(*record), CleanupPreviewUnavailable
		}
		if !slices.Equal(ids, record.SelectedTargetIDs) {
			record.Status = cleanupPreviewInvalidated
			return cleanupPreviewRecordCopy(*record), CleanupPreviewConfigChanged
		}
	}
	digest, _, err := cleanupScopeDigest(current, record.SelectedTargetIDs)
	if err != nil {
		if errors.Is(err, errCleanupScopeUnavailable) {
			return cleanupPreviewRecordCopy(*record), CleanupPreviewUnavailable
		}
		record.Status = cleanupPreviewInvalidated
		return cleanupPreviewRecordCopy(*record), CleanupPreviewConfigChanged
	}
	if digest != record.Digest {
		record.Status = cleanupPreviewInvalidated
		return cleanupPreviewRecordCopy(*record), CleanupPreviewConfigChanged
	}

	record.Status = cleanupPreviewConsumed
	consumedAt := now
	record.ConsumedAt = &consumedAt
	return cleanupPreviewRecordCopy(*record), ""
}

// markExpiredLocked 在读取/消费时把已到期的 active 记录标为 expired。
func (r *CleanupPreviewRegistry) markExpiredLocked(record *CleanupPreviewRecord) {
	if record.Status != cleanupPreviewActive {
		return
	}
	if !r.now().Before(record.ExpiresAt) {
		record.Status = cleanupPreviewExpired
	}
}

// purgeLocked 机会式清理过期超过保留期的终态记录，避免长期占用内存。
func (r *CleanupPreviewRegistry) purgeLocked(now time.Time) {
	cutoff := now.Add(-cleanupPreviewRetention)
	for id, record := range r.previews {
		if record.Status == cleanupPreviewActive {
			if now.Before(record.ExpiresAt) {
				continue
			}
			record.Status = cleanupPreviewExpired
		}
		if record.ExpiresAt.Before(cutoff) {
			delete(r.previews, id)
		}
	}
}

// cleanupScopeTarget 是范围摘要中的一个物理目标。
type cleanupScopeTarget struct {
	CloudType  config.CloudType
	Region     string
	ResourceID string
	TargetIDs  []int
	Sites      []config.EdgeOneSite
	DNSRules   []config.DomainRule
}

// cleanupScopeSnapshot 是可序列化为稳定摘要的配置视图。
type cleanupScopeSnapshot struct {
	Tag                   string
	DNSTag                string
	CredentialFingerprint string
	Targets               []cleanupScopeTarget
}

// cleanupScopeConfig 从一份不可变运行时状态构建所选物理目标的范围视图。
func cleanupScopeConfig(state *syncer.RuntimeState, selectedIDs []int) (cleanupScopeSnapshot, error) {
	if state == nil || state.Pool == nil || strings.TrimSpace(state.Config.EdgeOneTag) == "" {
		return cleanupScopeSnapshot{}, errCleanupScopeUnavailable
	}
	targetByID := make(map[int]config.TargetConfig, len(state.Config.Targets))
	for _, target := range state.Config.Targets {
		targetByID[target.ID] = target
	}

	ids := append([]int(nil), selectedIDs...)
	sort.Ints(ids)
	ids = slices.Compact(ids)
	if len(ids) == 0 {
		return cleanupScopeSnapshot{}, errCleanupScopeChanged
	}

	type group struct {
		identity string
		targets  []config.TargetConfig
	}
	selectedIdentities := make(map[string]bool)
	for _, id := range ids {
		target, ok := targetByID[id]
		if !ok {
			return cleanupScopeSnapshot{}, errCleanupScopeChanged
		}
		selectedIdentities[config.TargetIdentity(target)] = true
	}

	// 选择任一别名即覆盖该物理目标；必须把同物理目标的全部已配置别名一并纳入，
	// 否则响应、摘要与 Step 5 复核会漏掉其他别名上的站点/配置变化。
	groups := make(map[string]*group)
	order := make([]string, 0, len(selectedIdentities))
	for identity := range selectedIdentities {
		order = append(order, identity)
	}
	sort.Strings(order)
	for _, identity := range order {
		groups[identity] = &group{identity: identity}
	}
	for _, target := range state.Config.Targets {
		identity := config.TargetIdentity(target)
		if !selectedIdentities[identity] {
			continue
		}
		groups[identity].targets = append(groups[identity].targets, target)
	}

	out := cleanupScopeSnapshot{
		Tag:                   strings.TrimSpace(state.Config.EdgeOneTag),
		DNSTag:                strings.TrimSpace(state.Config.Tag),
		CredentialFingerprint: state.Pool.Credentials().Fingerprint(),
		Targets:               make([]cleanupScopeTarget, 0, len(order)),
	}
	for _, identity := range order {
		g := groups[identity]
		sort.SliceStable(g.targets, func(i, j int) bool { return g.targets[i].ID < g.targets[j].ID })
		targetIDs := make([]int, 0, len(g.targets))
		idSet := make(map[int]bool, len(g.targets))
		for _, target := range g.targets {
			targetIDs = append(targetIDs, target.ID)
			idSet[target.ID] = true
		}
		sites := provider.SelectEdgeOneSitesForIdentity(state.Config.EdgeOneSites, state.Config.Targets, identity)
		if sites == nil {
			sites = []config.EdgeOneSite{}
		}
		dnsRules := applicableCleanupDNSRules(state.Config.DomainRules, idSet)
		first := g.targets[0]
		out.Targets = append(out.Targets, cleanupScopeTarget{
			CloudType:  first.CloudType,
			Region:     first.Region,
			ResourceID: first.ResourceID,
			TargetIDs:  targetIDs,
			Sites:      sites,
			DNSRules:   dnsRules,
		})
	}
	return out, nil
}

// cleanupScopeDigest 计算与 Step 5 复核相同的范围摘要。
func cleanupScopeDigest(state *syncer.RuntimeState, selectedIDs []int) (string, string, error) {
	scope, err := cleanupScopeConfig(state, selectedIDs)
	if err != nil {
		return "", "", err
	}
	data, err := json.Marshal(scope)
	if err != nil {
		return "", "", fmt.Errorf("序列化清理预览范围失败: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), scope.CredentialFingerprint, nil
}

// cleanupAllTargetIDs 返回当前配置的全部本地目标 ID（升序）。
func cleanupAllTargetIDs(state *syncer.RuntimeState) ([]int, error) {
	if state == nil {
		return nil, errCleanupScopeUnavailable
	}
	ids := make([]int, 0, len(state.Config.Targets))
	for _, target := range state.Config.Targets {
		ids = append(ids, target.ID)
	}
	sort.Ints(ids)
	return ids, nil
}

// applicableCleanupDNSRules 返回适用于任一给定本地目标 ID 的 DNS 规则。
//
// targets 为空表示全部目标；规则按本地 ID 升序返回，Targets 深拷贝。
func applicableCleanupDNSRules(rules []config.DomainRule, targetIDs map[int]bool) []config.DomainRule {
	out := make([]config.DomainRule, 0)
	for _, rule := range rules {
		applies := len(rule.Targets) == 0
		if !applies {
			for _, id := range rule.Targets {
				if targetIDs[id] {
					applies = true
					break
				}
			}
		}
		if !applies {
			continue
		}
		cp := rule
		cp.Targets = append([]int(nil), rule.Targets...)
		sort.Ints(cp.Targets)
		out = append(out, cp)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// newCleanupPreviewID 生成 16 字节不可预测标识。
func newCleanupPreviewID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("生成清理预览标识失败: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// cleanupPreviewRecordCopy 深拷贝预览记录，避免调用方修改注册表内部状态。
func cleanupPreviewRecordCopy(record CleanupPreviewRecord) CleanupPreviewRecord {
	out := record
	out.SelectedTargetIDs = append([]int(nil), record.SelectedTargetIDs...)
	out.Warnings = append([]string(nil), record.Warnings...)
	out.Targets = make([]CleanupPreviewTarget, len(record.Targets))
	for i, target := range record.Targets {
		cp := target
		cp.TargetIDs = append([]int(nil), target.TargetIDs...)
		cp.Sites = make([]config.EdgeOneSite, len(target.Sites))
		for j, site := range target.Sites {
			siteCopy := site
			siteCopy.TargetIDs = append([]int(nil), site.TargetIDs...)
			cp.Sites[j] = siteCopy
		}
		cp.Rows = make([]CleanupPreviewRow, len(target.Rows))
		for j, row := range target.Rows {
			rowCopy := row
			rowCopy.FunctionalKeys = append([]string(nil), row.FunctionalKeys...)
			cp.Rows[j] = rowCopy
		}
		out.Targets[i] = cp
	}
	if record.ConsumedAt != nil {
		t := *record.ConsumedAt
		out.ConsumedAt = &t
	}
	return out
}

// Invalidate 锁存执行期间观察到的配置变化；已消费记录也不能因配置恢复复活。
func (r *CleanupPreviewRegistry) Invalidate(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if record, ok := r.previews[id]; ok {
		record.Status = cleanupPreviewInvalidated
	}
}
