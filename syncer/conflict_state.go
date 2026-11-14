package syncer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// AddressConflictStore 是运行态冲突记录的最小读写接口。
//
// *config.Store 实现该接口；测试可用真实临时 SQLite 或受控替身。
type AddressConflictStore interface {
	GetAddressConflict(ctx context.Context) (*config.AddressConflict, error)
	SaveAddressConflict(ctx context.Context, conflict config.AddressConflict) error
	ClearAddressConflict(ctx context.Context) error
}

// AddressConflictState 保存一次双开全局预检的进程内与 SQLite 证据。
//
// 固定顺序：
//   - 发现重叠时先记录进程内证据，再尝试持久化；首次写库失败后后续轮次仍阻断并重试；
//   - 启动加载已提交记录；双开读取失败直接阻断；
//   - 只有两侧完整、无重叠且解除提交成功后才清除进程内状态并恢复。
//
// 本结构不持有 RuntimeState、不访问云、不修改 DNS 熔断器；正式调度由 Step 8 接线。
type AddressConflictState struct {
	store AddressConflictStore

	// evalMu 串行化加载/评估，避免同进程两轮同时改写运行态记录。
	evalMu sync.Mutex

	mu        sync.Mutex
	known     *config.AddressConflict
	loaded    bool
	persisted bool
}

// NewAddressConflictState 创建运行态冲突状态；store 必须非 nil 才能执行评估。
func NewAddressConflictState(store AddressConflictStore) *AddressConflictState {
	return &AddressConflictState{store: store}
}

// Load 读取已提交的冲突记录（启动恢复入口）。
//
// 读取失败时保留已有进程内证据并返回错误，不把未知状态降级为“无冲突”。
func (s *AddressConflictState) Load(ctx context.Context) error {
	if s == nil || s.store == nil {
		return errors.New("地址冲突状态缺少 Store")
	}
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	return s.loadLocked(ctx)
}

// loadLocked 调用方必须持有 evalMu。
func (s *AddressConflictState) loadLocked(ctx context.Context) error {
	conflict, err := s.store.GetAddressConflict(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.loaded = false
		return err
	}
	s.loaded = true
	if conflict == nil {
		// 已提交记录不存在：即使进程内还有证据，也不能认为它已持久化。
		// 后续数据不足轮次会重新保存，避免外部 reset/清理后证据只留在内存。
		s.persisted = false
		return nil
	}
	if s.known == nil {
		cp := *conflict
		s.known = &cp
		s.persisted = true
	} else {
		// 进程内可能有比 SQLite 更新的证据（例如首次保存失败后再次观察）。
		// 以库内的首次时间为准，保留内存中待写入的证据和最后观察时间。
		s.known.FirstSeenAt = conflict.FirstSeenAt
		s.persisted = sameConflictEvidence(s.known, conflict) && s.known.LastSeenAt.Equal(conflict.LastSeenAt)
	}
	return nil
}

// sameConflictEvidence 比较冲突证据的业务字段，不比较 last_seen_at。
func sameConflictEvidence(a, b *config.AddressConflict) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.DNSName == b.DNSName &&
		a.DNSAddress == b.DNSAddress &&
		a.EdgeOnePrefix == b.EdgeOnePrefix &&
		a.ZoneID == b.ZoneID
}

// Evaluate 执行一次全局预检并返回数据层准入。
//
// 只有 DNS 与 EdgeOne 同时启用时才读取冲突状态并执行全局判定；单项启用继续
// 允许正常侧，同时保留已知冲突证据（不阻塞单项写入）。返回 error 时调用方必须
// 把两侧都视为阻断：错误只会来自冲突状态读取/保存/解除失败。
func (s *AddressConflictState) Evaluate(ctx context.Context, in Preflight) (PreflightOutcome, error) {
	if s == nil || s.store == nil {
		return PreflightOutcome{
			State:           PreflightStateDataUnavailable,
			DNSComplete:     in.DNSComplete,
			EdgeOneComplete: in.EdgeOneComplete,
		}, errors.New("地址冲突状态缺少 Store")
	}

	s.evalMu.Lock()
	defer s.evalMu.Unlock()

	// 非法样本不能因为调用方传了 Complete=true 就被当作完整输入。
	if !allDNSAddressesValid(in.DNS) {
		in.DNSComplete = false
	}
	if !allEdgeOnePrefixesValid(in.EdgeOne) {
		in.EdgeOneComplete = false
	}

	outcome := PreflightOutcome{
		DNSComplete:     in.DNSComplete,
		EdgeOneComplete: in.EdgeOneComplete,
	}

	// 单开或双关：不做全局判定，不读取/改写运行态记录。
	if !in.DNSEnabled || !in.EdgeOneEnabled {
		known := s.hasKnown()
		in.KnownConflict = known
		outcome.KnownConflict = known
		outcome.State = PreflightStateSingleSource
		outcome.Admission = DecideAdmission(in)
		return outcome, nil
	}

	// 正向命中优先于查询失败：部分样本也可能证明重叠，读库失败时仍须保留证据。
	host, prefix, overlap := FirstOverlap(in.DNS, in.EdgeOne)
	known, persisted, err := s.currentKnown(ctx)
	var observed config.AddressConflict
	if overlap {
		evidence := AddressOverlap{DNS: host, EdgeOne: prefix}
		outcome.Overlap = &evidence
		observed = s.observe(evidence)
		known = true
	}
	outcome.KnownConflict = known
	if known {
		outcome.State = PreflightStateOverlap
		outcome.Admission = Admission{KeepConflict: true}
	}
	if err != nil {
		// 读取恢复前不改写 SQLite；已查实的重叠留在进程内继续阻断。
		if !known {
			outcome.State = PreflightStateDataUnavailable
		}
		return outcome, fmt.Errorf("读取地址冲突状态失败: %w", err)
	}
	if overlap {
		return outcome, s.persist(ctx, observed)
	}
	in.KnownConflict = known

	if known {
		if in.DNSComplete && in.EdgeOneComplete {
			// 完整有效输入证明无重叠：先提交解除，成功后才清除进程内状态。
			if err := s.store.ClearAddressConflict(ctx); err != nil {
				return outcome, err
			}
			s.clearMemory()
			outcome.State = PreflightStateClear
			outcome.KnownConflict = false
			in.KnownConflict = false
			outcome.Admission = DecideAdmission(in)
			return outcome, nil
		}

		// 数据不足：保持阻断；首次保存失败的证据在后续轮次继续重试持久化。
		if !persisted {
			current := s.memoryConflict()
			if current == nil {
				return outcome, errors.New("地址冲突状态不一致：缺少进程内证据")
			}
			if err := s.persist(ctx, *current); err != nil {
				return outcome, err
			}
		}
		return outcome, nil
	}

	// 无已知冲突、无正向命中：按完整性单侧继续或两侧继续。
	outcome.Admission = DecideAdmission(in)
	if in.DNSComplete && in.EdgeOneComplete {
		outcome.State = PreflightStateClear
	} else {
		outcome.State = PreflightStateDataUnavailable
	}
	return outcome, nil
}

// hasKnown 读取进程内已知冲突；不触发 SQLite 读取。
func (s *AddressConflictState) hasKnown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.known != nil
}

// currentKnown 返回是否存在已知冲突及是否已持久化。
//
// 尚未成功加载时，即使已有进程内证据也要重试读取 SQLite。
func (s *AddressConflictState) currentKnown(ctx context.Context) (known, persisted bool, err error) {
	s.mu.Lock()
	known = s.known != nil
	persisted = s.persisted
	loaded := s.loaded
	s.mu.Unlock()
	if loaded {
		return known, persisted, nil
	}
	if err := s.loadLocked(ctx); err != nil {
		return known, persisted, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.known != nil, s.persisted, nil
}

// observe 先在进程内记录重叠证据，再交给 persist 持久化。
//
// 已有冲突时保留 first_seen_at；证据或 last_seen_at 变化都要求重新保存。
func (s *AddressConflictState) observe(overlap AddressOverlap) config.AddressConflict {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	firstSeen := now
	if s.known != nil && !s.known.FirstSeenAt.IsZero() {
		firstSeen = s.known.FirstSeenAt
	}
	conflict := config.AddressConflict{
		DNSName:       overlap.DNS.Host,
		DNSAddress:    overlap.DNS.Addr.Unmap().String(),
		EdgeOnePrefix: overlap.EdgeOne.Prefix.Masked().String(),
		ZoneID:        overlap.EdgeOne.ZoneID,
		FirstSeenAt:   firstSeen.UTC(),
		LastSeenAt:    now,
	}
	s.known = &conflict
	// 本轮观察（含 last_seen 刷新）尚未确认写入；persist 成功后再置 true。
	s.persisted = false
	return conflict
}

// persist 保存冲突证据；save 失败时保留 observe 已设置的进程内状态。
func (s *AddressConflictState) persist(ctx context.Context, conflict config.AddressConflict) error {
	if err := s.store.SaveAddressConflict(ctx, conflict); err != nil {
		return err
	}
	s.mu.Lock()
	s.persisted = true
	s.mu.Unlock()
	return nil
}

// Forget 清除进程内冲突证据，供配置 reset 已成功提交后调用。
//
// reset 事务负责删除 SQLite 行；本方法只清内存并把状态标为未加载，下一次双开
// 评估会重新读取 SQLite。这样即使 reset 未真正提交，记录仍会重新进入阻断。
func (s *AddressConflictState) Forget() {
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	s.mu.Lock()
	s.known = nil
	s.persisted = false
	// 下一次双开评估重新读取 SQLite；若 reset 实际未提交，记录会重新进入阻断。
	s.loaded = false
	s.mu.Unlock()
}

// clearMemory 只在 SQLite 解除提交成功之后调用。
func (s *AddressConflictState) clearMemory() {
	s.mu.Lock()
	s.known = nil
	s.persisted = false
	s.loaded = true
	s.mu.Unlock()
}

// memoryConflict 返回进程内证据副本；没有证据时返回 nil。
func (s *AddressConflictState) memoryConflict() *config.AddressConflict {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.known == nil {
		return nil
	}
	cp := *s.known
	return &cp
}

// Preview 计算与正式预检相同的准入结论，但不写入任何运行状态。
//
// 只读模拟必须与正式 Evaluate 共用 FirstOverlap/DecideAdmission；区别仅在于
// 不 observe/persist/clear，也不把读到的 SQLite 记录写入进程内缓存。
func (s *AddressConflictState) Preview(ctx context.Context, in Preflight) (PreflightOutcome, error) {
	if s == nil || s.store == nil {
		return PreflightOutcome{
			State:           PreflightStateDataUnavailable,
			DNSComplete:     in.DNSComplete,
			EdgeOneComplete: in.EdgeOneComplete,
		}, errors.New("地址冲突状态缺少 Store")
	}

	s.evalMu.Lock()
	defer s.evalMu.Unlock()

	if !allDNSAddressesValid(in.DNS) {
		in.DNSComplete = false
	}
	if !allEdgeOnePrefixesValid(in.EdgeOne) {
		in.EdgeOneComplete = false
	}
	s.mu.Lock()
	knownMemory := s.known != nil
	s.mu.Unlock()

	outcome := PreflightOutcome{
		DNSComplete:     in.DNSComplete,
		EdgeOneComplete: in.EdgeOneComplete,
	}

	// 单项/双关与正式 Evaluate 一样不读取冲突表。
	if !in.DNSEnabled || !in.EdgeOneEnabled {
		in.KnownConflict = knownMemory
		outcome.KnownConflict = knownMemory
		outcome.State = PreflightStateSingleSource
		outcome.Admission = DecideAdmission(in)
		return outcome, nil
	}

	host, prefix, overlap := FirstOverlap(in.DNS, in.EdgeOne)
	persisted, err := s.store.GetAddressConflict(ctx)
	known := knownMemory || persisted != nil
	if overlap {
		evidence := AddressOverlap{DNS: host, EdgeOne: prefix}
		outcome.Overlap = &evidence
		outcome.KnownConflict = true
		outcome.State = PreflightStateOverlap
		outcome.Admission = Admission{KeepConflict: true}
		if err != nil {
			return outcome, err
		}
		return outcome, nil
	}
	if err != nil {
		outcome.KnownConflict = known
		if known {
			outcome.State = PreflightStateOverlap
			outcome.Admission = Admission{KeepConflict: true}
		} else {
			outcome.State = PreflightStateDataUnavailable
		}
		return outcome, err
	}

	in.KnownConflict = known
	outcome.KnownConflict = known
	if known {
		if in.DNSComplete && in.EdgeOneComplete {
			// 正式轮会先提交解除再放行；预览只展示“可解除后放行”，不改写记录。
			in.KnownConflict = false
			outcome.KnownConflict = false
			outcome.State = PreflightStateClear
			outcome.Admission = DecideAdmission(in)
			return outcome, nil
		}
		outcome.State = PreflightStateOverlap
		outcome.Admission = Admission{KeepConflict: true}
		return outcome, nil
	}

	outcome.Admission = DecideAdmission(in)
	if in.DNSComplete && in.EdgeOneComplete {
		outcome.State = PreflightStateClear
	} else {
		outcome.State = PreflightStateDataUnavailable
	}
	return outcome, nil
}
