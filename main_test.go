package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// testBinary 由 TestMain 构建一次，供所有进程级用例复用。
var testBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fwalizer-proc-test-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建临时目录失败: %v\n", err)
		os.Exit(1)
	}
	testBinary = filepath.Join(dir, "fwalizer-test")
	build := exec.Command("go", "build", "-o", testBinary, ".")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "构建测试二进制失败: %v\n", err)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// freePort 申请一个当前空闲的 TCP 端口（关闭 listener 后返回端口号）。
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("申请空闲端口失败: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("释放探测端口失败: %v", err)
	}
	return port
}

// processEnv 构造受控子进程环境：只保留必要的系统变量，再加显式部署/业务变量。
func processEnv(dataDir string, extra map[string]string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"FWALIZER_DATA_DIR=" + dataDir,
		"WEBUI_HOST=127.0.0.1",
	}
	// 显式清空业务变量，避免继承测试宿主环境（os.Getenv 无法区分空值与未设置）
	for _, k := range []string{"TARGETS", "TC_ACCESS_ID", "TC_ACCESS_KEY", "ALI_ACCESS_ID", "ALI_ACCESS_KEY", "INTERVAL", "FWALIZER_MODE", "WEBUI_PORT"} {
		env = append(env, k+"=")
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

// syncBuffer 是并发安全的子进程输出缓冲：
// os/exec 在独立 goroutine 中写入，测试同时在轮询读取，必须串行化。
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startProcess 以给定环境变量启动真实二进制，返回命令句柄与并发安全的输出缓冲。
func startProcess(t *testing.T, dataDir string, extra map[string]string) (*exec.Cmd, *syncBuffer) {
	t.Helper()
	cmd := exec.Command(testBinary)
	out := &syncBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = processEnv(dataDir, extra)
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动测试进程失败: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	})
	return cmd, out
}

// waitForHTTP 轮询等待 HTTP 端点可用。
func waitForHTTP(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("状态码 %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("等待 %s 就绪超时: %v", url, lastErr)
}

// getBody 请求 URL 并返回响应体
func getBody(t *testing.T, url string) string {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("请求 %s 失败: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf := &strings.Builder{}
	if _, err := io.Copy(buf, resp.Body); err != nil {
		t.Fatalf("读取 %s 响应失败: %v", url, err)
	}
	return buf.String()
}

// ─── Step 3 进程级测试辅助 ───

// waitForProcessExit 等待子进程退出并返回退出码；超时返回错误（不 kill，由 t.Cleanup 兜底）。
func waitForProcessExit(t *testing.T, cmd *exec.Cmd, timeout time.Duration) (int, error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return 0, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	case <-time.After(timeout):
		return -1, fmt.Errorf("进程 %d 在 %v 内未退出", cmd.Process.Pid, timeout)
	}
}

// assertPortClosed 断言给定端口已不再接受 HTTP 请求。
func assertPortClosed(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(url)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("进程退出后 %s 仍可访问", url)
	}
}

// assertPidFileCleanup 断言进程退出后 pidfile 已被清理。
func assertPidFileCleanup(t *testing.T, dataDir string, timeout time.Duration) {
	t.Helper()
	pidPath := config.GetPidFilePath(dataDir)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pidPath); os.IsNotExist(err) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("进程退出后 pidfile 未清理: %s", pidPath)
}

// waitForLog 轮询等待子进程输出中出现指定子串。
func waitForLog(t *testing.T, out *syncBuffer, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待日志 %q 超时；当前输出:\n%s", want, out.String())
}

// runUntilSignal 启动子进程 → 等待 health 就绪 → 发送信号 → 等待退出并返回退出码与完整输出。
func runUntilSignal(t *testing.T, dataDir string, extra map[string]string, sig os.Signal, timeout time.Duration) (int, string) {
	t.Helper()
	port := freePort(t)
	env := map[string]string{"WEBUI_PORT": fmt.Sprintf("%d", port)}
	for k, v := range extra {
		env[k] = v
	}
	cmd, out := startProcess(t, dataDir, env)
	waitForHTTP(t, fmt.Sprintf("http://127.0.0.1:%d/api/health", port))

	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatalf("发送信号 %v 失败: %v", sig, err)
	}
	code, err := waitForProcessExit(t, cmd, timeout)
	if err != nil {
		t.Fatalf("等待进程退出失败: %v\n输出:\n%s", err, out.String())
	}
	assertPortClosed(t, fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
	assertPidFileCleanup(t, dataDir, 3*time.Second)
	return code, out.String()
}

// seedTargetAndRule 写入一个目标与一条域名规则，用于构造真实同步轮次。
// dns 传入不可达地址可让 DNS 解析快速失败，从而得到可读的同步轮次日志。
func seedTargetAndRule(t *testing.T, dataDir, dnsAddr, dnsTimeout string) {
	t.Helper()
	store, err := config.OpenStore(filepath.Join(dataDir, "config.db"))
	if err != nil {
		t.Fatalf("准备测试数据库失败: %v", err)
	}
	defer func() {
		if cerr := store.Close(); cerr != nil {
			t.Errorf("关闭测试数据库失败: %v", cerr)
		}
	}()

	if err := store.AddTarget(config.TargetConfig{
		CloudType:  config.CloudTCLighthouse,
		Region:     "ap-guangzhou",
		ResourceID: "lhins-step3-proc",
	}); err != nil {
		t.Fatalf("写入测试目标失败: %v", err)
	}
	if err := store.AddRule(config.DomainRule{
		Host:     "step3.invalid",
		Protocol: "TCP",
		Ports:    "443",
		Action:   "ACCEPT",
	}); err != nil {
		t.Fatalf("写入测试规则失败: %v", err)
	}
	for k, v := range map[string]string{"dns": dnsAddr, "dns_timeout": dnsTimeout, "interval": "5m"} {
		if v == "" {
			continue
		}
		if err := store.SetSetting(k, v); err != nil {
			t.Fatalf("写入设置 %s 失败: %v", k, err)
		}
	}
}

// TestNoArgsStartsWebUIWithEmptyDatabase 空数据库、无参数时进入唯一 WebUI 启动路径
func TestNoArgsStartsWebUIWithEmptyDatabase(t *testing.T) {
	dataDir := t.TempDir()
	port := freePort(t)

	cmd, out := startProcess(t, dataDir, map[string]string{"WEBUI_PORT": fmt.Sprintf("%d", port)})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForHTTP(t, base+"/api/health")

	// 无参数启动必须创建并打开数据目录中的 SQLite
	if _, err := os.Stat(filepath.Join(dataDir, "config.db")); err != nil {
		t.Errorf("启动后应存在 SQLite 数据库: %v", err)
	}
	// 空数据库下业务接口可用
	if body := getBody(t, base+"/api/targets"); !strings.Contains(body, "[]") {
		t.Errorf("空数据库下 /api/targets 应为空数组；响应: %s", body)
	}

	// 进程应保持存活（WebUI 常驻）
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		t.Fatalf("WebUI 进程不应自行退出；输出:\n%s", out.String())
	}
}

// TestCommandLineArgumentsExitNonZero 任意命令行参数都必须非零退出且不初始化数据目录
func TestCommandLineArgumentsExitNonZero(t *testing.T) {
	for _, args := range [][]string{
		{"version"},
		{"validate"},
		{"validate", ".env"},
		{"backup"},
		{"restore", "config.db.bak.20260101_000000"},
		{"--help"},
		{"-h"},
		{"--version"},
		{"serve"},
		{"whatever"},
		{"--"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			dataDir := filepath.Join(t.TempDir(), "data")
			cmd := exec.Command(testBinary, args...)
			cmd.Env = processEnv(dataDir, map[string]string{"WEBUI_PORT": fmt.Sprintf("%d", freePort(t))})
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("参数 %v 应非零退出；输出:\n%s", args, out)
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("期望非零退出码，实际错误: %v", err)
			}
			if exitErr.ExitCode() == 0 {
				t.Errorf("参数 %v 退出码 = 0, want 非零", args)
			}
			if !strings.Contains(string(out), "不支持命令行参数") {
				t.Errorf("参数 %v 应打印「不支持命令行参数」；输出:\n%s", args, out)
			}
			// 参数退出不得创建数据目录（即未初始化、未监听端口）
			if _, statErr := os.Stat(dataDir); !os.IsNotExist(statErr) {
				t.Errorf("参数 %v 退出后不应创建数据目录 %s (stat err = %v)", args, dataDir, statErr)
			}
		})
	}
}

// TestInvalidWebUIPortExitsNonZero 非法 WEBUI_PORT 必须启动失败
func TestInvalidWebUIPortExitsNonZero(t *testing.T) {
	for _, raw := range []string{"0", "65536", "-1", "abc", "1e3"} {
		t.Run(raw, func(t *testing.T) {
			dataDir := t.TempDir()
			cmd := exec.Command(testBinary)
			cmd.Env = processEnv(dataDir, map[string]string{"WEBUI_PORT": raw})
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("WEBUI_PORT=%s 应非零退出；输出:\n%s", raw, out)
			}
			if !strings.Contains(string(out), "WEBUI_PORT") {
				t.Errorf("WEBUI_PORT=%s 的错误输出应包含键名；输出:\n%s", raw, out)
			}
		})
	}
}

// TestBusinessEnvDoesNotOverrideSQLite 业务环境变量不改变 SQLite 配置、也不切换运行模式
func TestBusinessEnvDoesNotOverrideSQLite(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "config.db")

	store, err := config.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("准备测试数据库失败: %v", err)
	}
	if err := store.AddTarget(config.TargetConfig{
		CloudType:  config.CloudTCLighthouse,
		Region:     "ap-guangzhou",
		ResourceID: "lhins-from-db",
	}); err != nil {
		_ = store.Close()
		t.Fatalf("写入测试目标失败: %v", err)
	}
	if err := store.SetSetting("interval", "7m"); err != nil {
		_ = store.Close()
		t.Fatalf("写入测试设置失败: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("关闭测试数据库失败: %v", err)
	}

	port := freePort(t)
	startProcess(t, dataDir, map[string]string{
		"WEBUI_PORT":    fmt.Sprintf("%d", port),
		"TARGETS":       "tc_lighthouse|lhins-from-env|ap-beijing",
		"TC_ACCESS_ID":  "AKIDfromEnv",
		"INTERVAL":      "99m",
		"FWALIZER_MODE": "env",
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForHTTP(t, base+"/api/health")

	targetsBody := getBody(t, base+"/api/targets")
	if !strings.Contains(targetsBody, "lhins-from-db") {
		t.Errorf("目标应来自 SQLite；响应: %s", targetsBody)
	}
	if strings.Contains(targetsBody, "lhins-from-env") {
		t.Errorf("TARGETS 环境变量不应注入目标；响应: %s", targetsBody)
	}

	settingsBody := getBody(t, base+"/api/settings")
	if !strings.Contains(settingsBody, "7m") {
		t.Errorf("interval 应来自 SQLite（7m）；响应: %s", settingsBody)
	}
	if strings.Contains(settingsBody, "99m") {
		t.Errorf("INTERVAL 环境变量不应覆盖 SQLite 设置；响应: %s", settingsBody)
	}
	if strings.Contains(settingsBody, "AKIDfromEnv") {
		t.Errorf("TC_ACCESS_ID 环境变量不应进入业务配置；响应: %s", settingsBody)
	}
}

// runUntilSignalWithLog 启动子进程 → 等待 health → 等待指定日志出现 → 发送信号 →
// 等待退出。用于确定性构造“信号到达时某操作仍在进行”的场景，不依赖固定 sleep 猜时序。
func runUntilSignalWithLog(t *testing.T, dataDir string, extra map[string]string, wantLog string, sig os.Signal, timeout time.Duration) (int, string) {
	t.Helper()
	port := freePort(t)
	env := map[string]string{"WEBUI_PORT": fmt.Sprintf("%d", port)}
	for k, v := range extra {
		env[k] = v
	}
	cmd, out := startProcess(t, dataDir, env)
	waitForHTTP(t, fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
	waitForLog(t, out, wantLog, 10*time.Second)

	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatalf("发送信号 %v 失败: %v", sig, err)
	}
	code, err := waitForProcessExit(t, cmd, timeout)
	if err != nil {
		t.Fatalf("等待进程退出失败: %v\n输出:\n%s", err, out.String())
	}
	assertPortClosed(t, fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
	assertPidFileCleanup(t, dataDir, 3*time.Second)
	return code, out.String()
}

// ─── Step 3（O5-04 / O5-05）：进程级信号、收尾顺序与绑定失败 ───

// TestProcessSIGTERMGracefulShutdown SIGTERM 走完整收尾路径后以 0 退出
func TestProcessSIGTERMGracefulShutdown(t *testing.T) {
	dataDir := t.TempDir()
	code, out := runUntilSignal(t, dataDir, nil, syscall.SIGTERM, 20*time.Second)

	if code != 0 {
		t.Errorf("SIGTERM 退出码 = %d, want 0；输出:\n%s", code, out)
	}
	for _, want := range []string{
		"收到停止信号，等待当前轮次完成...",
		"开始 HTTP 关闭",
		"同步引擎停止",
		"HTTP 关闭完成",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("SIGTERM 收尾日志缺少 %q；输出:\n%s", want, out)
		}
	}
	// 收尾顺序契约：必须先启动 HTTP shutdown，再停止 Syncer（main 等待 shutdownStarted 后才 Stop）
	if strings.Index(out, "开始 HTTP 关闭") > strings.Index(out, "同步引擎停止") {
		t.Errorf("收尾顺序错误：应先启动 HTTP 关闭再停止 Syncer；输出:\n%s", out)
	}
	// 无长连接时 HTTP 应正常收尾，不得走强制关闭
	if strings.Contains(out, "强制关闭剩余连接") {
		t.Errorf("无长连接时不应强制关闭 HTTP；输出:\n%s", out)
	}
}

// TestProcessSIGINTGracefulShutdown SIGINT 与 SIGTERM 走同一收尾路径并以 0 退出
func TestProcessSIGINTGracefulShutdown(t *testing.T) {
	dataDir := t.TempDir()
	code, out := runUntilSignal(t, dataDir, nil, syscall.SIGINT, 20*time.Second)

	if code != 0 {
		t.Errorf("SIGINT 退出码 = %d, want 0；输出:\n%s", code, out)
	}
	if !strings.Contains(out, "收到停止信号，等待当前轮次完成...") {
		t.Errorf("SIGINT 未进入统一收尾路径；输出:\n%s", out)
	}
	if !strings.Contains(out, "开始 HTTP 关闭") {
		t.Errorf("SIGINT 未启动 HTTP 关闭；输出:\n%s", out)
	}
}

// TestProcessCompletesInFlightRoundAfterSignal 收到信号时已经开始的同步轮次必须完成后才退出。
// 通过等待“开始同步”日志后立即发信号，确定性构造 in-flight 轮次；不使用固定 sleep 猜时序。
// 证据边界：信号与“下一轮是否已启动”之间存在固有竞争，本用例不做无竞态硬断言，
// 仅断言“开始同步 → 停止信号 → 本轮完成 → 同步引擎停止”的顺序与退出码。
func TestProcessCompletesInFlightRoundAfterSignal(t *testing.T) {
	dataDir := t.TempDir()
	// 预置目标与规则，并把 DNS 指向不可达地址（短超时）：解析快速失败，本轮仍会正常结束
	seedTargetAndRule(t, dataDir, "192.0.2.1", "3s")

	code, out := runUntilSignalWithLog(t, dataDir, nil, "开始同步", syscall.SIGTERM, 20*time.Second)
	if code != 0 {
		t.Fatalf("退出码 = %d, want 0；输出:\n%s", code, out)
	}

	syncStartIdx := strings.Index(out, "开始同步")
	stopIdx := strings.Index(out, "收到停止信号，等待当前轮次完成...")
	if syncStartIdx < 0 || stopIdx < 0 {
		t.Fatalf("缺少同步/停止信号日志；输出:\n%s", out)
	}
	// 信号之后必须仍能看到当前轮次的结束，且必须出现同步引擎停止
	after := out[stopIdx:]
	if !strings.Contains(after, "同步完成") && !strings.Contains(after, "DNS 解析失败") {
		t.Errorf("信号后未观察到当前同步轮次继续并结束；输出:\n%s", out)
	}
	if !strings.Contains(out, "同步引擎停止") {
		t.Errorf("缺少同步引擎停止日志；输出:\n%s", out)
	}
}

// TestProcessBindFailureDoesNotStartSyncer HTTP 绑定失败时不启动 Syncer，且以非零退出
func TestProcessBindFailureDoesNotStartSyncer(t *testing.T) {
	dataDir := t.TempDir()
	// 预置目标与规则：若 Syncer 被错误启动，日志中必然出现“开始同步”
	seedTargetAndRule(t, dataDir, "", "")

	// 不可解析的监听地址：非 EADDRINUSE，不得降级到随机端口，因此绑定必然失败
	cmd, out := startProcess(t, dataDir, map[string]string{
		"WEBUI_HOST": "fwalizer-step3-bind-failure.invalid",
		"WEBUI_PORT": "60200",
	})
	code, err := waitForProcessExit(t, cmd, 30*time.Second)
	if err != nil {
		t.Fatalf("等待进程退出失败: %v\n输出:\n%s", err, out.String())
	}
	if code == 0 {
		t.Errorf("HTTP 绑定失败应非零退出，实际 exit=0；输出:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "WebUI 监听失败") {
		t.Errorf("缺少监听失败错误输出；输出:\n%s", out.String())
	}
	if strings.Contains(out.String(), "开始同步") {
		t.Errorf("HTTP 绑定失败时不得启动 Syncer；输出:\n%s", out.String())
	}
	assertPidFileCleanup(t, dataDir, 3*time.Second)
}

// TestProcessSSEExitsOnServerShutdown 真实 SSE 连接在服务器 shutdown 时主动退出并完成收尾。
// 该用例同时证明：已建立的长连接不会让 http.Server.Shutdown 一直等到 10 秒上限。
func TestProcessSSEExitsOnServerShutdown(t *testing.T) {
	dataDir := t.TempDir()
	port := freePort(t)
	cmd, out := startProcess(t, dataDir, map[string]string{"WEBUI_PORT": fmt.Sprintf("%d", port)})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForHTTP(t, base+"/api/health")

	// 建立真实 SSE 长连接（不设客户端超时，连接由服务器 shutdown 终止）
	resp, err := http.Get(base + "/api/sync/events")
	if err != nil {
		t.Fatalf("建立 SSE 连接失败: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE 状态码 = %d, want 200", resp.StatusCode)
	}
	// 给 handler 建立订阅留出极短窗口（后续以“HTTP 关闭完成”日志为确定证据）
	time.Sleep(200 * time.Millisecond)

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("发送 SIGTERM 失败: %v", err)
	}
	code, err := waitForProcessExit(t, cmd, 20*time.Second)
	if err != nil {
		t.Fatalf("SSE 连接存在时进程未在 20s 内退出（可能未主动关闭 SSE）: %v\n输出:\n%s", err, out.String())
	}
	if code != 0 {
		t.Errorf("退出码 = %d, want 0；输出:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "服务器关闭，同步事件 SSE 退出") {
		t.Errorf("缺少 SSE 主动退出日志；输出:\n%s", out.String())
	}
	if strings.Contains(out.String(), "强制关闭剩余连接") {
		t.Errorf("SSE 已主动退出，不应触发强制关闭；输出:\n%s", out.String())
	}
	assertPortClosed(t, base+"/api/health")
	assertPidFileCleanup(t, dataDir, 3*time.Second)
}
