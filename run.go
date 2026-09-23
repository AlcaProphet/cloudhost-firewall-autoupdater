package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/app"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/webui"
	webapi "github.com/alcaprophet/cloudhost-firewall-autoupdater/webui/api"
)

// run 是 main 的可测试边界：返回进程退出码。
//
// 固定口径（Build6 Step 2）：不接受任何命令行参数；出现任意参数时输出错误、
// 以非零状态退出，并且不读取部署参数、不创建数据目录、不监听端口。
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		fmt.Fprintf(stderr, "不支持命令行参数: %v\n", args[1:])
		fmt.Fprintln(stderr, "用法: fwalizer   # 无参数启动 WebUI，请通过浏览器完成全部配置")
		return 2
	}

	deploy, err := config.LoadDeploymentConfig()
	if err != nil {
		fmt.Fprintf(stderr, "部署参数无效: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(deploy.DataDir, 0755); err != nil {
		fmt.Fprintf(stderr, "创建数据目录失败: %v\n", err)
		return 1
	}

	return runWebUI(deploy, stderr)
}

// runWebUI 启动唯一运行形态：WebUI + SQLite + Syncer。
func runWebUI(deploy config.DeploymentConfig, stderr io.Writer) int {
	// pidfile 防多实例
	pidFile := config.GetPidFilePath(deploy.DataDir)
	cleanup, err := config.WritePidFile(pidFile)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	defer cleanup()

	dbPath := filepath.Join(deploy.DataDir, "config.db")
	store, err := config.OpenStore(dbPath)
	if err != nil {
		fmt.Fprintf(stderr, "打开数据库失败: %v\n", err)
		return 1
	}
	defer func() {
		if cerr := store.Close(); cerr != nil {
			slog.Error("关闭数据库失败", "error", cerr)
		}
	}()

	cfg, err := store.LoadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "加载配置失败: %v\n", err)
		return 1
	}

	// 初始化日志（同时输出到 stdout 和 WebUI 日志流）
	logBroadcaster := webapi.NewLogBroadcaster(cfg.LogLevel)
	app.InitLoggerWithBroadcaster(cfg.LogLevel, logBroadcaster)

	// 监听地址和端口只来自部署参数，不参与 SQLite 业务配置
	srv := webui.NewServer(store, deploy.Host, deploy.Port)
	srv.SetLogBroadcaster(logBroadcaster)

	// 创建同步引擎（初始 Provider 可为空，等待用户通过 WebUI 配置后热重载生效）
	provider.SetCredentials(cfg.TCAccessID, cfg.TCAccessKey, cfg.AliAccessID, cfg.AliAccessKey)
	pool := provider.NewClientPool()
	var providers []provider.Provider
	for _, t := range cfg.Targets {
		p, err := provider.NewProvider(t, t.ID, pool)
		if err != nil {
			fmt.Fprintf(stderr, "创建 Provider 失败: %v\n", err)
			return 1
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

	// 信号监听必须在 HTTP 绑定前建立：绑定失败时的极早信号也不会丢失。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// 同步绑定 HTTP：绑定成功后才启动 Syncer，确保“WebUI 可用”与“同步在跑”一致
	actualPort, err := srv.Start()
	if err != nil {
		signal.Stop(sigCh)
		fmt.Fprintf(stderr, "WebUI 监听失败: %v\n", err)
		return 1
	}
	if actualPort != deploy.Port {
		slog.Warn("WebUI 未使用请求端口", "请求端口", deploy.Port, "实际端口", actualPort)
	}

	if len(providers) == 0 {
		slog.Info("WebUI 已启动，请通过浏览器配置云资源凭据和目标", "访问地址", srv.Addr())
	}

	go s.Run()

	// Serve 结果通道：非正常退出必须能被 main 感知（关闭流程内的收尾会被归一化为 nil）
	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- srv.Wait() }()

	// 同时等待 OS 信号与 Serve 结果；两者进入同一收尾路径，退出码按原因区分
	var serveErr error
	select {
	case sig := <-sigCh:
		slog.Info("收到停止信号，等待当前轮次完成...", "signal", sig.String())
	case serveErr = <-serveErrCh:
		if serveErr != nil {
			slog.Error("HTTP 服务异常退出，进入收尾", "error", serveErr)
		}
	}
	signal.Stop(sigCh)

	// 立即在 goroutine 中启动有界 HTTP shutdown（先关闭监听入口，再等待普通请求与 SSE 退出）；
	// 随后立即停止 Syncer，阻止当前轮次结束后再开始新一轮。
	httpDone := make(chan error, 1)
	shutdownStarted := make(chan struct{})
	go func() {
		// 先记录并置位“HTTP 关闭已启动”，再让出调度：收尾顺序在日志与语义上可确定
		slog.Info("开始 HTTP 关闭")
		close(shutdownStarted)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownErr := srv.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			if errors.Is(shutdownErr, context.DeadlineExceeded) {
				slog.Warn("HTTP 收尾超时，已强制关闭剩余连接；同步轮次不受影响")
			} else {
				slog.Warn("HTTP 收尾出错", "error", shutdownErr)
			}
		} else {
			slog.Info("HTTP 关闭完成")
		}
		httpDone <- shutdownErr
	}()
	<-shutdownStarted
	s.Stop()

	// HTTP 收尾最多等待 10s；超时后强制关闭已在 Shutdown 内完成，这里不再无限等待
	select {
	case <-httpDone:
	case <-time.After(10 * time.Second):
		slog.Warn("等待 HTTP 收尾超过 10 秒")
	}

	// 无论 HTTP 是否超时，都无超时等待当前同步轮次完成（强要求）
	s.Wait()

	if serveErr != nil {
		return 1
	}
	return 0
}
