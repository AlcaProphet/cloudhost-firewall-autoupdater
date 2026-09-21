package api

import (
	"log/slog"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// StoreLogWriter 将同步事件写入 SQLite 同步日志
type StoreLogWriter struct {
	Store *config.Store
}

// toInt 兼容事件 Data 中的数字类型（进程内为 int；事件数据若经 JSON 往返则为 float64）
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

// OnEvent 实现 notifier.Subscriber 接口
func (w *StoreLogWriter) OnEvent(event notifier.Event) error {
	log := config.SyncLog{Timestamp: event.Timestamp}
	if v, ok := event.Data["provider"].(string); ok {
		// 提取资源 ID：从 "tc_lighthouse(lhins-xxx)" 格式中取括号内部分
		if start := strings.Index(v, "("); start >= 0 {
			if end := strings.Index(v, ")"); end > start {
				log.Target = v[start+1 : end]
			} else {
				log.Target = v
			}
		} else {
			log.Target = v
		}
	}
	if v, ok := event.Data["domain"].(string); ok {
		log.Domain = v
	}
	switch event.Type {
	case notifier.EventSyncError:
		log.Result = "failed"
		if v, ok := event.Data["error"].(string); ok {
			log.Error = v
		}
	case notifier.EventDomainSyncComplete:
		log.Result = "success"
		// 读取实际写入计数（Build4 Step 1：计数链路打通，修复历史记录新增/删除恒为 0）
		if v, ok := event.Data["added"]; ok {
			log.Added = toInt(v)
		}
		if v, ok := event.Data["deleted"]; ok {
			log.Deleted = toInt(v)
		}
	default:
		return nil
	}
	if err := w.Store.AddSyncLog(log); err != nil {
		slog.Warn("写入同步日志失败", "error", err)
	}
	return nil
}
