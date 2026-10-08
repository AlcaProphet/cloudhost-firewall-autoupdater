package syncer

import (
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// CleanupObservation 分开记录 S1 候选观察与残留依据；历史和估计都不是当前云状态。
type CleanupObservation struct {
	Attempt         int       `json:"attempt"`
	Candidates      int       `json:"candidates"`
	CandidatesAt    time.Time `json:"candidates_at"`
	Deferred        int       `json:"deferred"`
	DeferredAt      time.Time `json:"deferred_at"`
	Basis           string    `json:"basis"` // s1、s2 或 delete_progress
	DesiredComplete bool      `json:"desired_complete"`
	Historical      bool      `json:"historical"`
}

// PlanObservation 的完整性只指规划输入，不表示覆盖成立或允许清理。
type PlanObservation struct {
	Attempt    int                  `json:"attempt"`
	Stage      string               `json:"stage"`
	ObservedAt time.Time            `json:"observed_at"`
	Complete   bool                 `json:"complete"`
	Issues     []provider.PlanIssue `json:"issues"`
	Historical bool                 `json:"historical"`
}

// UnsupportedObservation 分别保存最新规划与最近完整规划，禁止合并为当前结论。
type UnsupportedObservation struct {
	Latest       *PlanObservation `json:"latest"`
	LastComplete *PlanObservation `json:"last_complete"`
}

// CleanupObservationSummary 的四类目标互斥；只有 observed 参与已观察数量求和。
type CleanupObservationSummary struct {
	ObservedTargets    int  `json:"observed_targets"`
	EstimatedTargets   int  `json:"estimated_targets"`
	HistoricalTargets  int  `json:"historical_targets"`
	UnknownTargets     int  `json:"unknown_targets"`
	ObservedCandidates int  `json:"observed_candidates"`
	ObservedDeferred   int  `json:"observed_deferred"`
	Complete           bool `json:"complete"`
}

func desiredInputComplete(rules []config.DomainRule, resolved map[int][]dns.ResolvedIP, dnsErrors map[int]string) bool {
	for _, r := range rules {
		if _, failed := dnsErrors[r.ID]; failed || len(resolved[r.ID]) == 0 {
			return false
		}
	}
	return true
}

func (r *targetResult) observeUnsupported(issues []provider.PlanIssue, stage string, complete bool) {
	observation := &PlanObservation{Stage: stage, ObservedAt: time.Now(), Complete: complete, Issues: append([]provider.PlanIssue{}, issues...)}
	if r.unsupportedObservation == nil {
		r.unsupportedObservation = &UnsupportedObservation{}
	}
	r.unsupportedObservation.Latest = observation
	if complete {
		r.unsupportedObservation.LastComplete = observation
	}
	r.unsupported = observation.Issues
}

// mergeObservations 只用确实建立的新观察替换旧值，包括可信零；确认写入另行累计。
func (r *targetResult) mergeObservations(next targetResult, attempt int) {
	r.attempts = attempt
	if next.cleanupObservation != nil {
		next.cleanupObservation.Attempt = attempt
		r.cleanupObservation = next.cleanupObservation
	}
	if next.unsupportedObservation != nil {
		if r.unsupportedObservation == nil {
			r.unsupportedObservation = &UnsupportedObservation{}
		}
		next.unsupportedObservation.Latest.Attempt = attempt
		r.unsupportedObservation.Latest = next.unsupportedObservation.Latest
		if next.unsupportedObservation.LastComplete != nil {
			next.unsupportedObservation.LastComplete.Attempt = attempt
			r.unsupportedObservation.LastComplete = next.unsupportedObservation.LastComplete
		}
	}
	if o := r.cleanupObservation; o != nil {
		o.Historical = o.Attempt != attempt
		r.cleanupCandidates, r.cleanupDeferred = o.Candidates, o.Deferred
	}
	if o := r.unsupportedObservation; o != nil {
		o.Latest.Historical = o.Latest.Attempt != attempt
		r.unsupported = o.Latest.Issues
		if o.LastComplete != nil {
			o.LastComplete.Historical = o.LastComplete.Attempt != attempt
		}
	}
}

func (s *CleanupObservationSummary) add(r targetResult) {
	o := r.cleanupObservation
	switch {
	case o == nil:
		s.UnknownTargets++
	case o.Historical:
		s.HistoricalTargets++
	case o.Basis == "delete_progress" || !o.DesiredComplete:
		s.EstimatedTargets++
	default:
		s.ObservedTargets++
		s.ObservedCandidates += o.Candidates
		s.ObservedDeferred += o.Deferred
	}
	s.Complete = s.EstimatedTargets+s.HistoricalTargets+s.UnknownTargets == 0
}
