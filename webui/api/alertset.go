package api

import (
	"log/slog"
	"sync"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// alertSubscriptions 告警订阅的事件类型集合（邮件与 Webhook 共用，保持既有语义）。
var alertSubscriptions = []notifier.EventType{
	notifier.EventSyncError,
	notifier.EventDNSFailed,
}

// alertSet 一次配置快照对应的告警订阅集合（Build6 §12.4 第 5 条、§12.12）。
//
// 它**只包含本地构造好的 Subscriber**：构造阶段不发 SMTP、不发 Webhook、
// 不访问任何网络，因此可以在事务提交前安全地准备好；构造也不产生 error，
// 从而 commit 之后的应用分支不会失败。
type alertSet struct {
	email   notifier.Subscriber
	webhook notifier.Subscriber
	// emailToAddr / webhookChannel 只用于安全日志：绝不记录密码或 URL
	emailToAddr    string
	webhookChannel string
}

// BuildAlertSet 依据已校验的运行时配置构造候选告警订阅集合。
//
// 构造阶段零副作用：NewEmailNotifier / NewWebhookNotifier 只保存配置字段，
// 不建立连接、不发消息。
func BuildAlertSet(rc config.RuntimeConfig) alertSet {
	var set alertSet
	if rc.Email.Enabled {
		set.email = notifier.NewEmailNotifier(notifier.EmailConfig{
			Host: rc.Email.Host, Port: rc.Email.Port,
			User: rc.Email.Username, Pass: rc.Email.Password,
			From: rc.Email.FromAddr, To: rc.Email.ToAddr,
		})
		set.emailToAddr = rc.Email.ToAddr
	}
	if rc.Webhook.Enabled {
		set.webhook = notifier.NewWebhookNotifier(rc.Webhook.URL, rc.Webhook.Channel)
		set.webhookChannel = rc.Webhook.Channel
	}
	return set
}

// AlertManager 持有当前告警订阅，提供 commit 之后的无失败替换。
//
// Apply 先把旧订阅从事件总线移除、再安装新订阅；旧的已在途异步通知由
// EventBus 的异步回调自然完成，不在这里等待或取消。
type AlertManager struct {
	mu      sync.Mutex
	bus     *notifier.EventBus
	current alertSet

	// 按渠道持有的在途限流器（构造后不替换）：Issue6 A2 / §六.4 F5 要求
	// 「每渠道在途 ≤4」在配置热重载后仍然成立，因此限流器必须比 notifier 实例活得久。
	emailLimiter   *notifier.InFlightLimiter
	webhookLimiter *notifier.InFlightLimiter
}

// NewAlertManager 创建告警管理器；bus 为 nil 时只记录订阅集合（最小接线/测试场景）。
//
// 同时创建邮件与 Webhook 各自的在途限流器（容量 notifier.InFlightLimit = 4）。
func NewAlertManager(bus *notifier.EventBus) *AlertManager {
	return &AlertManager{
		bus:            bus,
		emailLimiter:   notifier.NewInFlightLimiter(notifier.InFlightLimit),
		webhookLimiter: notifier.NewInFlightLimiter(notifier.InFlightLimit),
	}
}

// Apply 取消旧订阅并安装新订阅（无失败的内存操作）。
func (m *AlertManager) Apply(set alertSet) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 注入本渠道的长期限流器：实例随配置更换，在途计数保持连续
	injectLimiter(set.email, m.emailLimiter)
	injectLimiter(set.webhook, m.webhookLimiter)

	if m.bus != nil {
		for _, et := range alertSubscriptions {
			if m.current.email != nil {
				m.bus.Unsubscribe(et, m.current.email)
			}
			if m.current.webhook != nil {
				m.bus.Unsubscribe(et, m.current.webhook)
			}
		}
		for _, et := range alertSubscriptions {
			if set.email != nil {
				m.bus.Subscribe(et, set.email)
			}
			if set.webhook != nil {
				m.bus.Subscribe(et, set.webhook)
			}
		}
	}
	m.current = set
}

// injectLimiter 把渠道限流器注入支持它的实例。
//
// 用非导出接口 + setter 注入，避免改动 NewEmailNotifier / NewWebhookNotifier 的既有
// 签名（既有测试与调用点零改动）。
func injectLimiter(sub notifier.Subscriber, limiter *notifier.InFlightLimiter) {
	if sub == nil || limiter == nil {
		return
	}
	if ln, ok := sub.(notifier.LimitedNotifier); ok {
		ln.SetInFlightLimiter(limiter)
	}
}

// Current 返回当前订阅集合（测试与只读观测用）。
func (m *AlertManager) Current() alertSet {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

// enabledChannels 返回当前处于启用状态的渠道名（用于安全日志）。
func (m *AlertManager) enabledChannels() (email, webhook bool, toAddr, channel string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	email = m.current.email != nil
	webhook = m.current.webhook != nil
	toAddr = m.current.emailToAddr
	channel = m.current.webhookChannel
	return
}

// LogStatus 输出告警渠道状态，只记录收件人与渠道名，绝不记录 SMTP 密码或 Webhook URL。
//
// verb 为 "已启用" 或 "已更新"；没有启用任何渠道时不输出，避免噪声。
func (m *AlertManager) LogStatus(verb string) {
	emailOn, webhookOn, toAddr, channel := m.enabledChannels()
	if emailOn {
		slog.Info("邮件告警"+verb, "to", toAddr)
	}
	if webhookOn {
		slog.Info("Webhook 告警"+verb, "channel", channel)
	}
}
