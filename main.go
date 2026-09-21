package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/app"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/webui"
	webapi "github.com/alcaprophet/cloudhost-firewall-autoupdater/webui/api"
)

func main() {
	// CLI 子命令优先
	if app.RunCLI(os.Args) {
		return
	}

	// 检测模式
	mode := app.DetectMode(os.Getenv("FWALIZER_MODE"))

	var cfg *config.Config

	switch mode {
	case app.ModeEnv:
		// 从 .env 加载
		var err error
		cfg, err = config.LoadEnv(".env")
		if err != nil {
			fmt.Fprintf(os.Stderr, "加载 .env 失败: %v\n", err)
			os.Exit(1)
		}
	case app.ModeWebUI:
		// WebUI 模式：SQLite + HTTP Server + Syncer
		dataDir := config.GetDataDir()
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "创建数据目录失败: %v\n", err)
			os.Exit(1)
		}

		// pidfile 防多实例（仅 WebUI 模式）
		pidFile := config.GetPidFilePath(dataDir)
		cleanup, err := config.WritePidFile(pidFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		defer cleanup()

		dbPath := filepath.Join(dataDir, "config.db")
		store, err := config.OpenStore(dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "打开数据库失败: %v\n", err)
			os.Exit(1)
		}
		defer store.Close()

		cfg, err = store.LoadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
			os.Exit(1)
		}
		if err := config.ApplyWebUIEnv(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "加载 WebUI 监听配置失败: %v\n", err)
			os.Exit(1)
		}

		// 初始化日志（同时输出到 stdout 和 WebUI 日志流）
		logBroadcaster := webapi.NewLogBroadcaster(cfg.LogLevel)
		app.InitLoggerWithBroadcaster(cfg.LogLevel, logBroadcaster)

		// 启动 WebUI 服务器
		srv := webui.NewServer(store, cfg.WebUIHost, cfg.WebUIPort)
		srv.SetLogBroadcaster(logBroadcaster)

		// 创建同步引擎（初始 Provider 可为空，等待用户通过 WebUI 配置后热重载生效）
		provider.SetCredentials(cfg.TCAccessID, cfg.TCAccessKey, cfg.AliAccessID, cfg.AliAccessKey)
		pool := provider.NewClientPool()
		var providers []provider.Provider
		for _, t := range cfg.Targets {
			p, err := provider.NewProvider(t, t.ID, pool)
			if err != nil {
				fmt.Fprintf(os.Stderr, "创建 Provider 失败: %v\n", err)
				os.Exit(1)
			}
			providers = append(providers, p)
		}
		resolver := dns.NewResolver(cfg.DNS, cfg.DNSTimeout)
		s := syncer.New(cfg, providers, resolver)

		// 将 Syncer 和 EventBus 传入 WebUI（支持 status/trigger/dryrun/SSE）
		srv.SetSyncer(s, s.EventBus())

		// 同步日志写入：订阅 sync:complete 和 sync:error 事件
		logWriter := &webapi.StoreLogWriter{Store: store}
		s.EventBus().Subscribe(notifier.EventDomainSyncComplete, logWriter)
		s.EventBus().Subscribe(notifier.EventSyncError, logWriter)

		// 追踪当前活跃的告警 Notifier（用于热重载时取消旧订阅）
		var currentEmailNotifier notifier.Subscriber
		var currentWebhookNotifier notifier.Subscriber

		// 读取告警配置并注册 Notifier
		if emailCfg, err := store.GetAlertEmail(); err == nil && emailCfg != nil && emailCfg.Enabled {
			currentEmailNotifier = notifier.NewEmailNotifier(notifier.EmailConfig{
				Host: emailCfg.Host, Port: emailCfg.Port,
				User: emailCfg.Username, Pass: emailCfg.Password,
				From: emailCfg.FromAddr, To: emailCfg.ToAddr,
			})
			s.EventBus().Subscribe(notifier.EventSyncError, currentEmailNotifier)
			s.EventBus().Subscribe(notifier.EventDNSFailed, currentEmailNotifier)
			slog.Info("邮件告警已启用", "to", emailCfg.ToAddr)
		}

		if webhookCfg, err := store.GetAlertWebhook(); err == nil && webhookCfg != nil && webhookCfg.Enabled {
			currentWebhookNotifier = notifier.NewWebhookNotifier(webhookCfg.URL, webhookCfg.Channel)
			s.EventBus().Subscribe(notifier.EventSyncError, currentWebhookNotifier)
			s.EventBus().Subscribe(notifier.EventDNSFailed, currentWebhookNotifier)
			slog.Info("Webhook 告警已启用", "url", webhookCfg.URL)
		}

		// 接通热重载：WebUI 修改配置后重新加载并通知 Syncer
		srv.SetReloadFunc(func() {
			newCfg, err := store.LoadConfig()
			if err != nil {
				slog.Error("重载配置失败", "error", err)
				return
			}
			// 更新凭据
			provider.SetCredentials(newCfg.TCAccessID, newCfg.TCAccessKey, newCfg.AliAccessID, newCfg.AliAccessKey)
			// 重建 ClientPool 和 Provider 列表
			newPool := provider.NewClientPool()
			var newProviders []provider.Provider
			var failedTargets []string
			for _, t := range newCfg.Targets {
				p, err := provider.NewProvider(t, t.ID, newPool)
				if err != nil {
					slog.Error("重建 Provider 失败", "target", t.ResourceID, "error", err)
					failedTargets = append(failedTargets, t.ResourceID)
					continue
				}
				newProviders = append(newProviders, p)
			}
			// 失败汇总提示（避免部分目标静默丢失）
			if len(failedTargets) > 0 {
				slog.Error("部分目标重建失败", "failed", len(failedTargets), "targets", failedTargets)
			}
			s.ReloadProviders(newProviders)
			s.Reload(newCfg)
			// 若 DNS 配置变更，重建 Resolver 并热重载
			newResolver := dns.NewResolver(newCfg.DNS, newCfg.DNSTimeout)
			s.ReloadResolver(newResolver)

			// 重建告警订阅（先取消旧订阅，再按最新配置注册）
			if currentEmailNotifier != nil {
				s.EventBus().Unsubscribe(notifier.EventSyncError, currentEmailNotifier)
				s.EventBus().Unsubscribe(notifier.EventDNSFailed, currentEmailNotifier)
				currentEmailNotifier = nil
			}
			if currentWebhookNotifier != nil {
				s.EventBus().Unsubscribe(notifier.EventSyncError, currentWebhookNotifier)
				s.EventBus().Unsubscribe(notifier.EventDNSFailed, currentWebhookNotifier)
				currentWebhookNotifier = nil
			}

			if emailCfg, err := store.GetAlertEmail(); err == nil && emailCfg != nil && emailCfg.Enabled {
				currentEmailNotifier = notifier.NewEmailNotifier(notifier.EmailConfig{
					Host: emailCfg.Host, Port: emailCfg.Port,
					User: emailCfg.Username, Pass: emailCfg.Password,
					From: emailCfg.FromAddr, To: emailCfg.ToAddr,
				})
				s.EventBus().Subscribe(notifier.EventSyncError, currentEmailNotifier)
				s.EventBus().Subscribe(notifier.EventDNSFailed, currentEmailNotifier)
				slog.Info("邮件告警已更新", "to", emailCfg.ToAddr)
			}

			if webhookCfg, err := store.GetAlertWebhook(); err == nil && webhookCfg != nil && webhookCfg.Enabled {
				currentWebhookNotifier = notifier.NewWebhookNotifier(webhookCfg.URL, webhookCfg.Channel)
				s.EventBus().Subscribe(notifier.EventSyncError, currentWebhookNotifier)
				s.EventBus().Subscribe(notifier.EventDNSFailed, currentWebhookNotifier)
				slog.Info("Webhook 告警已更新", "url", webhookCfg.URL)
			}
		})

		if len(providers) == 0 {
			slog.Info("WebUI 已启动，请通过浏览器配置云资源凭据和目标", "port", cfg.WebUIPort)
		}
		go func() {
			actualPort, err := srv.Start()
			if err != nil {
				slog.Error("WebUI 服务器启动失败", "error", err)
				return
			}
			cfg.WebUIPort = actualPort
		}()

		go s.Run()

		// 等待停止信号（Ctrl+C）
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh
		slog.Info("收到停止信号，等待当前轮次完成...")
		s.Stop()
		s.Wait()
		return
	}

	if err := app.Run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "运行失败: %v\n", err)
		os.Exit(1)
	}
}
