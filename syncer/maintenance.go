package syncer

import (
	"errors"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// ErrMaintenanceUnavailable 表示引擎尚未运行或已停止，不再受理维护任务。
var ErrMaintenanceUnavailable = errors.New("同步引擎未运行或已停止")

// QueueMaintenance 将已受理任务交给唯一 Run 循环；HTTP 断开不取消任务。
// 回调不得派生后台云写入。Stop 后排队任务仍会被调用以登记拒绝结果，
// 但必须通过 MaintenanceAllowed 再取得每次删除的准入。
func (s *Syncer) QueueMaintenance(fn func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || !s.running {
		return ErrMaintenanceUnavailable
	}
	s.maintenanceQueue = append(s.maintenanceQueue, fn)
	select {
	case s.maintenanceCh <- struct{}{}:
	default:
	}
	return nil
}

func (s *Syncer) runMaintenance() {
	s.mu.Lock()
	if len(s.maintenanceQueue) == 0 {
		s.mu.Unlock()
		return
	}
	fn := s.maintenanceQueue[0]
	s.maintenanceQueue[0] = nil
	s.maintenanceQueue = s.maintenanceQueue[1:]
	if len(s.maintenanceQueue) > 0 {
		select {
		case s.maintenanceCh <- struct{}{}:
		default:
		}
	}
	s.mu.Unlock()
	fn()
}

// finishMaintenance 在 Run 退出前让已受理任务记录终态，不遗留工作协程。
func (s *Syncer) finishMaintenance() {
	for {
		s.mu.RLock()
		pending := len(s.maintenanceQueue) > 0
		s.mu.RUnlock()
		if !pending {
			return
		}
		s.runMaintenance()
	}
}

// MaintenanceAllowed 只检查生命周期；总暂停不阻止显式维护。
func (s *Syncer) MaintenanceAllowed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running && !s.stopped
}

// WaitMaintenanceCloud 复用正式目标冷却，在锁外等待；等待后调用方须复核授权。
func (s *Syncer) WaitMaintenanceCloud(ct config.CloudType) {
	s.mu.RLock()
	next := s.targetNextStart[ct]
	s.mu.RUnlock()
	s.sleep(time.Until(next))
}

// FinishMaintenanceCloud 将维护读写纳入同平台正式目标冷却。
func (s *Syncer) FinishMaintenanceCloud(ct config.CloudType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.targetNextStart == nil {
		s.targetNextStart = make(map[config.CloudType]time.Time)
	}
	s.targetNextStart[ct] = time.Now().Add(rateLimitInterval(ct))
}

// InvalidateTargetCoverage 撤销指定物理目标覆盖，保留确认与过渡指纹。
func (s *Syncer) InvalidateTargetCoverage(identity string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, proof := range s.roundCoverage {
		if proof.TargetKey == identity {
			delete(s.roundCoverage, key)
		}
	}
	for zone, st := range s.edgeStatus {
		for i := range st.Coverage {
			if st.Coverage[i].TargetKey == identity {
				st.Coverage[i].Complete = false
				st.Coverage[i].Reasons = []string{"manual_cleanup_revalidation_required"}
			}
		}
		s.edgeStatus[zone] = st
	}
}

// FirewallDeleteIsIdempotent 让手动清理复用正式同步的幂等码合同。
func FirewallDeleteIsIdempotent(err error) bool { return isIdempotentDelete(err) }
