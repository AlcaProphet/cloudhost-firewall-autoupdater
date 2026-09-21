package app

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// Run 应用主入口
func Run(cfg *config.Config) error {
	// 1. 初始化日志
	InitLogger(cfg.LogLevel)

	// 2. 校验配置
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("配置校验失败: %w", err)
	}

	// 3. 设置凭据
	provider.SetCredentials(cfg.TCAccessID, cfg.TCAccessKey, cfg.AliAccessID, cfg.AliAccessKey)

	// 4. 创建 ClientPool
	pool := provider.NewClientPool()

	// 5. 创建 Providers
	var providers []provider.Provider
	for i, t := range cfg.Targets {
		p, err := provider.NewProvider(t, i, pool)
		if err != nil {
			return fmt.Errorf("创建 Provider 失败 [%s]: %w", t.ResourceID, err)
		}
		providers = append(providers, p)
	}

	// 6. 创建 DNS Resolver
	resolver := dns.NewResolver(cfg.DNS, cfg.DNSTimeout)

	// 7. 创建 Syncer 并启动
	s := syncer.New(cfg, providers, resolver)
	go s.Run()

	// 8. 等待停止信号
	syncer.WaitForSignal(s)
	return nil
}

// InitLogger 初始化日志系统
func InitLogger(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))
}

// InitLoggerWithBroadcaster 初始化日志系统（同时输出到 stdout 和额外的 Handler，如 WebUI 日志流）
func InitLoggerWithBroadcaster(level string, extra slog.Handler) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	stdout := slog.NewTextHandler(os.Stdout, opts)
	slog.SetDefault(slog.New(NewMultiHandler(stdout, extra)))
}
