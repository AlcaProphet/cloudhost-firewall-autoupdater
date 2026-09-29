package api

import (
	"fmt"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// StoreLogWriter 将目标级同步事件写入 SQLite 同步日志（Issue7 §7.3）。
//
// 固定语义：
//   - 一目标完成/失败只写**一条**日志；
//   - target 写资源 ID，domain 写稳定排序后用 ", " 连接的来源域名；
//   - result 使用 success/partial/failed；cleanup_deferred 只追加可读详情，
//     绝不把 success 改成 partial；
//   - added/deleted 只写云端确认数；
//   - AddSyncLog 失败必须**直接返回**给 EventBus 统一 WARN，不能内部吞掉。
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
	// 目标级来源域名：发布方已按稳定顺序连接；无来源时留空
	if v, ok := event.Data["domain"].(string); ok {
		log.Domain = v
	}
	if v, ok := event.Data["added"]; ok {
		log.Added = toInt(v)
	}
	if v, ok := event.Data["deleted"]; ok {
		log.Deleted = toInt(v)
	}

	switch event.Type {
	case notifier.EventSyncError:
		log.Result = "failed"
		if v, ok := event.Data["error"].(string); ok {
			log.Error = v
		}
		log.Error = joinDetails(log.Error, targetDetailLines(event))
	case notifier.EventTargetSyncComplete:
		// 目标级结论：partial 只来自「平台能力限制」，其余成功路径一律 success
		switch outcome, _ := event.Data["outcome"].(string); outcome {
		case "partial":
			log.Result = "partial"
		default:
			log.Result = "success"
		}
		log.Error = joinDetails("", targetDetailLines(event))
	default:
		return nil
	}

	if err := w.Store.AddSyncLog(log); err != nil {
		// 由 EventBus 的统一「事件处理失败」WARN 处理，这里不吞错
		return err
	}
	return nil
}

// targetDetailLines 生成目标级可读详情：无法实施项与清理延后都只追加详情，
// 不改变 result 口径。
func targetDetailLines(event notifier.Event) []string {
	var lines []string

	if unsupported, ok := event.Data["unsupported"].([]provider.PlanIssue); ok && len(unsupported) > 0 {
		lines = append(lines, formatUnsupportedDetails(unsupported)...)
	}

	candidates := toInt(event.Data["cleanup_candidates"])
	resolved := toInt(event.Data["cleanup_deleted"])
	deferred := toInt(event.Data["cleanup_deferred"])
	if candidates > 0 {
		lines = append(lines, fmt.Sprintf("清理候选 %d 条：已确认清理 %d 条，延后 %d 条", candidates, resolved, deferred))
	}

	return lines
}

// joinDetails 把多行详情合并为持久化列可容纳的文本（保留已有错误作为首行）。
func joinDetails(head string, lines []string) string {
	body := strings.Join(lines, "\n")
	switch {
	case head == "":
		return body
	case body == "":
		return head
	default:
		return head + "\n" + body
	}
}

func formatUnsupportedDetails(details []provider.PlanIssue) []string {
	lines := []string{fmt.Sprintf("无法实施 %d 条期望规则", len(details))}
	for _, detail := range details {
		protocol, port, action, cidr := "-", "-", "-", "-"
		if detail.Key != nil {
			protocol = detail.Key.Protocol
			port = detail.Key.Port
			action = detail.Key.Action
			cidr = detail.Key.CIDR
		}
		reason := detail.Message
		if reason == "" {
			reason = detail.Code
		}
		if reason == "" {
			reason = "-"
		}
		lines = append(lines, fmt.Sprintf("- %s %s %s %s：%s", protocol, port, action, cidr, reason))
	}
	return lines
}
