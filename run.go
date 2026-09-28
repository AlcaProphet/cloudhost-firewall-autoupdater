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
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/health"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
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

// syncerStartWait 是「等待同步主循环进入运行态」的有界上限。
//
// 正常路径下 Run 会在微秒级关闭 Started 信号；上限只用于兜底
// 「Stop 先于 Run 导致 Run 被吸收态拒绝」这类不可能出现在本启动序列的情况。
const syncerStartWait = 2 * time.Second

// waitSyncerRunning 有界等待同步主循环进入运行态（running=true 已可见）。
//
// 超时不阻塞启动：记录 WARN 后继续，后续由运行健康判定兜底
// （启动宽限内不视为异常，超过宽限仍按「同步引擎未运行」处理）。
func waitSyncerRunning(s *syncer.Syncer) {
	select {
	case <-s.Started():
	case <-time.After(syncerStartWait):
		slog.Warn("等待同步主循环进入运行态超时，继续启动运行健康监督器与 Push 心跳")
	}
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

	// 启动期只读一次完整业务快照：SQLite 是唯一业务配置源
	snapshot, err := store.LoadBusinessSnapshot()
	if err != nil {
		fmt.Fprintf(stderr, "加载配置失败: %v\n", err)
		return 1
	}
	runtimeCfg := snapshot.ToRuntimeConfig()

	// 初始化日志（同时输出到 stdout 和 WebUI 日志流）
	logBroadcaster := webapi.NewLogBroadcaster(runtimeCfg.LogLevel)
	app.InitLoggerWithBroadcaster(runtimeCfg.LogLevel, logBroadcaster)

	// 初始完整运行时状态：一次构造（凭据、Provider、Resolver、熔断器）
	initialState, err := syncer.BuildRuntimeState(nil, runtimeCfg, syncer.BreakerReset)
	if err != nil {
		fmt.Fprintf(stderr, "创建运行时状态失败: %v\n", err)
		return 1
	}
	runtimeManager := syncer.NewRuntimeManager(initialState)

	// 监听地址和端口只来自部署参数，不参与 SQLite 业务配置
	srv := webui.NewServer(store, deploy.Host, deploy.Port)
	srv.SetLogBroadcaster(logBroadcaster)

	// 创建同步引擎：状态由 RuntimeManager 提供，调度由 Syncer 内部单一控制通道驱动
	s := syncer.New(runtimeManager)

	// 告警订阅管理器：候选集合在事务提交前构造，commit 后无失败替换。
	// 每次状态发布完成后重新读取生效中的告警集合并输出安全日志（只含收件人/渠道名）。
	alertManager := webapi.NewAlertManager(s.EventBus())
	alertManager.Apply(webapi.BuildAlertSet(runtimeCfg))
	alertManager.LogStatus("已启用")
	s.SetStateAppliedHook(func(*syncer.RuntimeState) { alertManager.LogStatus("已更新") })

	// 运行健康（Build7 Step 4）：唯一计算源 + 30 秒内部监督器。
	// 判定只读取已发布的运行时快照与同步状态，不做任何网络访问；
	// SQLite 探活由 Store.PingContext 在 2 秒上限内完成。
	healthChecker := health.New(health.Deps{
		Pinger: store,
		Status: s.Status,
		Policy: func() config.AlertPolicyConfig {
			if st := runtimeManager.Snapshot(); st != nil {
				return st.Config.Policy
			}
			return config.DefaultAlertPolicy()
		},
		Interval: func() time.Duration {
			if st := runtimeManager.Snapshot(); st != nil {
				return st.Config.Interval
			}
			return 0
		},
	})
	supervisor := health.NewSupervisor(health.SupervisorDeps{Checker: healthChecker, Bus: s.EventBus()})

	// Uptime Kuma Push 心跳循环（Build7 Step 5）：默认关闭（策略默认 enabled=false 且 URL 为空），
	// 配置保存在 commit 后唤醒；失败只写安全 WARN，不影响应用健康。
	pusher := health.NewPusher(health.PusherDeps{
		Checker: healthChecker,
		Config: func() health.PushConfig {
			st := runtimeManager.Snapshot()
			if st == nil {
				return health.PushConfig{}
			}
			cfg := st.Config.UptimeKumaPush
			return health.PushConfig{Enabled: cfg.Enabled, URL: cfg.URL, Interval: cfg.Interval}
		},
	})

	// 将 Syncer、EventBus 与运行时接线传入 WebUI
	// （status/trigger/dryrun/SSE + 连接测试/资源扫描的只读快照来源）
	srv.SetSyncer(s, s.EventBus())
	srv.SetRuntimeWiring(runtimeManager, alertManager)
	srv.SetHealth(supervisor)
	srv.SetPush(pusher)

	// 同步日志写入：订阅 sync:complete 和 sync:error 事件
	logWriter := &webapi.StoreLogWriter{Store: store}
	s.EventBus().Subscribe(notifier.EventDomainSyncComplete, logWriter)
	s.EventBus().Subscribe(notifier.EventSyncError, logWriter)

	// 信号监听必须在 HTTP 绑定前建立：绑定失败时的极早信号也不会丢失。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// 同步绑定 HTTP：绑定成功后才启动 Syncer，确保“WebUI 可用”与“同步在跑”一致
	// 端口降级（仅 EADDRINUSE）由 Server.Start 记录权威 WARN，这里不重复输出
	if _, err := srv.Start(); err != nil {
		signal.Stop(sigCh)
		fmt.Fprintf(stderr, "WebUI 监听失败: %v\n", err)
		return 1
	}

	if len(initialState.Providers) == 0 {
		slog.Info("WebUI 已启动，请通过浏览器配置云资源凭据和目标", "访问地址", srv.Addr())
	}

	// 先启动同步主循环并有界等待其进入运行态，再启动运行健康监督器与 Push 心跳：
	// 两者的首检/首发都会现场计算健康，若先于 Syncer.Run 置 running=true，
	// 会把「引擎尚未启动」误判为「引擎未运行」——导致启动即误报 WARN、
	// Push 首条心跳误报 DOWN，甚至（第三开关 + 渠道开启时）发出一次误报告警（Build7 Step 7）。
	go s.Run()
	waitSyncerRunning(s)
	go supervisor.Run()
	go pusher.Run()

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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), webui.ShutdownTimeout)
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
	// 先停 Push（取消在途 HTTP）与运行健康监督器（有界：最多等待一次 SQLite 探活），
	// 再停止 Syncer：顺序反过来会让监督器在 shutdown 期间看到「主循环已停止」并制造伪异常告警。
	pusher.Stop()
	supervisor.Stop()
	s.Stop()

	// HTTP 收尾最多等待同一时限；超时后强制关闭已在 Shutdown 内完成，这里不再无限等待
	select {
	case <-httpDone:
	case <-time.After(webui.ShutdownTimeout):
		slog.Warn("等待 HTTP 收尾超过上限", "timeout", webui.ShutdownTimeout)
	}

	// 无论 HTTP 是否超时，都无超时等待当前同步轮次完成（强要求）
	s.Wait()

	if serveErr != nil {
		return 1
	}
	return 0
}
