package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// withTxForTest 在事务内执行测试夹具写入并提交。
//
// 非事务 Store 写入入口（AddTarget/AddRule）已删除：生产写入统一经协调器
// 使用 *Tx 方法，测试夹具也必须走同一事务路径。
func withTxForTest(t *testing.T, s *config.Store, fn func(ctx context.Context, tx *sql.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("开始测试事务失败: %v", err)
	}
	if err := fn(ctx, tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			t.Errorf("回滚测试事务失败: %v", rbErr)
		}
		t.Fatalf("测试夹具写入失败: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("提交测试事务失败: %v", err)
	}
}

// addTargetForTest 事务内插入目标（测试夹具）
func addTargetForTest(t *testing.T, s *config.Store, tc config.TargetConfig) {
	t.Helper()
	withTxForTest(t, s, func(ctx context.Context, tx *sql.Tx) error {
		_, err := s.AddTargetTx(ctx, tx, tc)
		return err
	})
}

// addRuleForTest 事务内插入规则（测试夹具）
func addRuleForTest(t *testing.T, s *config.Store, r config.DomainRule) {
	t.Helper()
	withTxForTest(t, s, func(ctx context.Context, tx *sql.Tx) error {
		return s.AddRuleTx(ctx, tx, r)
	})
}

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

// assertPidFileReleased 断言进程退出后文件保留且内核锁已释放。
func assertPidFileReleased(t *testing.T, dataDir string) {
	t.Helper()
	path := config.GetPidFilePath(dataDir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("退出后锁文件应保留: %v", err)
	}
	cleanup, err := config.WritePidFile(path)
	if err != nil {
		t.Fatalf("退出后锁未释放: %v", err)
	}
	cleanup()
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
	assertPidFileReleased(t, dataDir)
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

	addTargetForTest(t, store, config.TargetConfig{
		CloudType:  config.CloudTCLighthouse,
		Region:     "ap-guangzhou",
		ResourceID: "lhins-step3-proc",
	})
	addRuleForTest(t, store, config.DomainRule{
		Host:     "step3.invalid",
		Protocol: "TCP",
		Ports:    "443",
		Action:   "ACCEPT",
	})
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
	addTargetForTest(t, store, config.TargetConfig{
		CloudType:  config.CloudTCLighthouse,
		Region:     "ap-guangzhou",
		ResourceID: "lhins-from-db",
	})
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
	assertPidFileReleased(t, dataDir)
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
	assertPidFileReleased(t, dataDir)
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
	assertPidFileReleased(t, dataDir)
}

// putJSON 发送 JSON PUT 请求并返回状态码（进程级用例专用）。
func putJSON(t *testing.T, url, body string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("请求 %s 失败: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("读取 %s 响应失败: %v", url, err)
	}
	return resp.StatusCode
}

// TestProcessSecretsNotLogged 进程日志不得包含云凭据、SMTP 密码或 Webhook URL（Build6 Step 4）。
//
// 「不泄露 Webhook URL」的改动位于 main 包的启动分支与 reload 闭包，webui/api 的
// sentinel 用例覆盖不到该层，因此这里用真实二进制 + 真实数据目录做进程级回归：
// 预置启用态告警（带唯一 sentinel）→ 启动（启动分支）→ PUT 触发一次热重载（reload 分支）
// → 断言日志只出现渠道名，六个敏感值一个都不出现。
func TestProcessSecretsNotLogged(t *testing.T) {
	const (
		sentinelWebhookURL  = "https://sentinel-process-check.invalid/hook-abc123"
		sentinelSMTPPass    = "sentinel-process-smtp-pass-xyz"
		sentinelTCAccessID  = "AKIDsentinelProcessCheck"
		sentinelTCAccessKey = "sentinelProcessKey"
	)

	dataDir := t.TempDir()
	store, err := config.OpenStore(filepath.Join(dataDir, "config.db"))
	if err != nil {
		t.Fatalf("预置数据库失败: %v", err)
	}
	if err := store.SetSetting("tc_access_id", sentinelTCAccessID); err != nil {
		_ = store.Close()
		t.Fatalf("预置凭据失败: %v", err)
	}
	if err := store.SetSetting("tc_access_key", sentinelTCAccessKey); err != nil {
		_ = store.Close()
		t.Fatalf("预置凭据失败: %v", err)
	}
	if err := store.SaveAlertEmail(&config.AlertEmailConfig{
		Enabled: true, Host: "smtp.example.com", Port: "587", Username: "u",
		Password: sentinelSMTPPass, FromAddr: "f@example.com", ToAddr: "t@example.com",
	}); err != nil {
		_ = store.Close()
		t.Fatalf("预置邮件告警失败: %v", err)
	}
	if err := store.SaveAlertWebhook(&config.AlertWebhookConfig{
		Enabled: true, URL: sentinelWebhookURL, Channel: "dingtalk",
	}); err != nil {
		_ = store.Close()
		t.Fatalf("预置 Webhook 失败: %v", err)
	}
	// Build7 §2.1：只有「渠道开启 + 触发开启」才安装订阅，因此这里显式开启 DNS 触发，
	// 使渠道启用/更新日志（本用例的同步点）真实产生。
	if err := store.WithTransaction(func(tx *sql.Tx) error {
		ctx := context.Background()
		return store.SaveAlertPolicyTx(ctx, tx, &config.AlertPolicyConfig{
			DNSFailedEnabled: true, HealthTimeout: 10 * time.Minute, HealthTimeoutText: "10m",
		})
	}); err != nil {
		_ = store.Close()
		t.Fatalf("预置告警策略失败: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("关闭预置数据库失败: %v", err)
	}

	port := freePort(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	cmd, out := startProcess(t, dataDir, map[string]string{"WEBUI_PORT": fmt.Sprintf("%d", port)})
	waitForHTTP(t, base+"/api/health")

	// 启动分支：应记录“已启用”，但只带渠道名
	waitForLog(t, out, "Webhook 告警已启用", 10*time.Second)

	// 热重载分支：PUT 一次合法设置触发 reload，会重新注册告警订阅
	if code := putJSON(t, base+"/api/settings", `{"theme":"dark"}`); code != http.StatusOK {
		t.Fatalf("PUT /api/settings 状态码 = %d, want 200", code)
	}
	waitForLog(t, out, "Webhook 告警已更新", 10*time.Second)

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("发送 SIGTERM 失败: %v", err)
	}
	code, err := waitForProcessExit(t, cmd, 20*time.Second)
	if err != nil {
		t.Fatalf("等待进程退出失败: %v\n输出:\n%s", err, out.String())
	}
	if code != 0 {
		t.Fatalf("退出码 = %d, want 0\n输出:\n%s", code, out.String())
	}

	output := out.String()
	if !strings.Contains(output, "channel=dingtalk") {
		t.Errorf("Webhook 告警日志应只带渠道名；输出:\n%s", output)
	}
	for _, secret := range []string{sentinelWebhookURL, sentinelSMTPPass, sentinelTCAccessID, sentinelTCAccessKey} {
		if strings.Contains(output, secret) {
			t.Errorf("进程日志泄露敏感值 %q；输出:\n%s", secret, output)
		}
	}
}

// ─── Build6 Step 5：真实二进制进程级 version 2 配置导入导出 ───

// TestProcessConfigExportImportRoundTrip 真实二进制进程级验证：
// POST 导出得到 v2 附件（含完整敏感快照）→ 在另一数据目录导入 → 业务关系重建。
//
// 这是「接口层 + 真实进程 + 真实 SQLite」的证据层，不使用 mock。
func TestProcessConfigExportImportRoundTrip(t *testing.T) {
	const (
		tcSecretID  = "AKIDprocessRoundTrip"
		tcSecretKey = "processRoundTripKey"
		smtpPass    = "processRoundTripSmtpPass"
		webhookURL  = "https://process-roundtrip.invalid/hook"
	)

	// 来源实例：预置带自增历史的目标、限定目标的规则与完整告警
	srcDir := t.TempDir()
	srcStore, err := config.OpenStore(filepath.Join(srcDir, "config.db"))
	if err != nil {
		t.Fatalf("预置数据库失败: %v", err)
	}
	addTargetForTest(t, srcStore, config.TargetConfig{CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "lhins-src-1"})
	addTargetForTest(t, srcStore, config.TargetConfig{CloudType: config.CloudTCCVM, Region: "ap-beijing", ResourceID: "sg-src-2"})
	srcTargets, err := srcStore.GetTargets()
	if err != nil {
		_ = srcStore.Close()
		t.Fatalf("GetTargets 失败: %v", err)
	}
	addRuleForTest(t, srcStore, config.DomainRule{
		Host: "roundtrip.example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
		Targets: []int{srcTargets[1].ID}, Comment: "跨实例",
	})
	if err := srcStore.SetSetting("tc_access_id", tcSecretID); err != nil {
		_ = srcStore.Close()
		t.Fatalf("预置凭据失败: %v", err)
	}
	if err := srcStore.SetSetting("tc_access_key", tcSecretKey); err != nil {
		_ = srcStore.Close()
		t.Fatalf("预置凭据失败: %v", err)
	}
	if err := srcStore.SetSetting("theme", "dark"); err != nil {
		_ = srcStore.Close()
		t.Fatalf("预置主题失败: %v", err)
	}
	if err := srcStore.SaveAlertEmail(&config.AlertEmailConfig{
		Enabled: true, Host: "smtp.example.com", Port: "587", Username: "u", Password: smtpPass,
		FromAddr: "f@example.com", ToAddr: "t@example.com",
	}); err != nil {
		_ = srcStore.Close()
		t.Fatalf("预置邮件告警失败: %v", err)
	}
	if err := srcStore.SaveAlertWebhook(&config.AlertWebhookConfig{
		Enabled: true, URL: webhookURL, Channel: "dingtalk",
	}); err != nil {
		_ = srcStore.Close()
		t.Fatalf("预置 Webhook 失败: %v", err)
	}
	// Build7：版本 3 必须端到端携带策略、邮件主题/正文与 Push（真实进程边界）
	if err := srcStore.WithTransaction(func(tx *sql.Tx) error {
		ctx := context.Background()
		if err := srcStore.SaveAlertPolicyTx(ctx, tx, &config.AlertPolicyConfig{
			DNSFailedEnabled: true, OperationalErrorEnabled: true,
			HealthTimeout: 25 * time.Minute, HealthTimeoutText: "25m",
		}); err != nil {
			return err
		}
		return srcStore.SaveUptimeKumaPushTx(ctx, tx, &config.UptimeKumaPushConfig{
			Enabled: true, URL: "https://kuma.example.invalid/api/push/tokE2E", Interval: 45 * time.Second, IntervalText: "45s",
		})
	}); err != nil {
		_ = srcStore.Close()
		t.Fatalf("预置策略/Push 失败: %v", err)
	}
	if err := srcStore.Close(); err != nil {
		t.Fatalf("关闭预置数据库失败: %v", err)
	}

	srcPort := freePort(t)
	srcBase := fmt.Sprintf("http://127.0.0.1:%d", srcPort)
	srcCmd, srcOut := startProcess(t, srcDir, map[string]string{"WEBUI_PORT": fmt.Sprintf("%d", srcPort)})
	waitForHTTP(t, srcBase+"/api/health")
	defer func() {
		if srcCmd.ProcessState == nil || !srcCmd.ProcessState.Exited() {
			_ = srcCmd.Process.Signal(syscall.SIGTERM)
			if _, werr := waitForProcessExit(t, srcCmd, 20*time.Second); werr != nil {
				t.Errorf("等待来源进程退出失败: %v", werr)
			}
		}
	}()

	// 1) POST 导出：旧 GET 端点必须已删除（静态文件兜底会给出 404/405，两者都表示路由不存在）
	if code := getStatus(t, http.MethodGet, srcBase+"/api/config/export", ""); code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
		t.Errorf("GET 导出状态码 = %d, want 404 或 405（旧端点应删除）", code)
	}
	exportBody, headers := postRaw(t, srcBase+"/api/config/export", "")
	if !strings.HasPrefix(headers.Get("Content-Type"), "application/json") {
		t.Errorf("Content-Type = %q", headers.Get("Content-Type"))
	}
	if headers.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", headers.Get("Cache-Control"))
	}
	if !strings.HasPrefix(headers.Get("Content-Disposition"), `attachment; filename="fwalizer-config-v3-`) {
		t.Errorf("Content-Disposition = %q", headers.Get("Content-Disposition"))
	}
	if !strings.HasSuffix(exportBody, "}\n") {
		t.Errorf("导出响应应以换行结束")
	}
	// 导出是唯一允许包含敏感值的响应
	for _, secret := range []string{tcSecretKey, smtpPass, webhookURL} {
		if !strings.Contains(exportBody, secret) {
			t.Errorf("v3 导出应包含敏感值 %q", secret)
		}
	}
	// Build7 §4.3：version 3 固定结构必须包含 policy 与 monitoring
	for _, fragment := range []string{`"version": 3`, `"policy"`, `"uptime_kuma_push"`, `"health_timeout": "25m"`, `"interval": "45s"`} {
		if !strings.Contains(exportBody, fragment) {
			t.Errorf("v3 导出缺少 %s；body=%s", fragment, exportBody)
		}
	}

	// 2) 目标实例：先写入不同自增历史，再导入
	dstDir := t.TempDir()
	dstStore, err := config.OpenStore(filepath.Join(dstDir, "config.db"))
	if err != nil {
		t.Fatalf("预置目标实例数据库失败: %v", err)
	}
	for i := 0; i < 5; i++ {
		addTargetForTest(t, dstStore, config.TargetConfig{CloudType: config.CloudAliECS, Region: "cn-hangzhou", ResourceID: "sg-dst"})
	}
	if err := dstStore.Close(); err != nil {
		t.Fatalf("关闭预置数据库失败: %v", err)
	}

	dstPort := freePort(t)
	dstBase := fmt.Sprintf("http://127.0.0.1:%d", dstPort)
	dstCmd, dstOut := startProcess(t, dstDir, map[string]string{"WEBUI_PORT": fmt.Sprintf("%d", dstPort)})
	waitForHTTP(t, dstBase+"/api/health")
	defer func() {
		if dstCmd.ProcessState == nil || !dstCmd.ProcessState.Exited() {
			_ = dstCmd.Process.Signal(syscall.SIGTERM)
			if _, werr := waitForProcessExit(t, dstCmd, 20*time.Second); werr != nil {
				t.Errorf("等待目标进程退出失败: %v", werr)
			}
		}
	}()

	if code := postJSON(t, dstBase+"/api/config/import", exportBody); code != http.StatusOK {
		t.Fatalf("导入状态码 = %d, want 200；来源日志:\n%s\n目标日志:\n%s", code, srcOut.String(), dstOut.String())
	}

	// 3) 通过 HTTP 校验业务关系与完整快照
	targetsBody := getBody(t, dstBase+"/api/targets")
	if !strings.Contains(targetsBody, "lhins-src-1") || !strings.Contains(targetsBody, "sg-src-2") {
		t.Errorf("导入后目标缺失: %s", targetsBody)
	}
	if strings.Contains(targetsBody, "sg-dst") {
		t.Errorf("导入应覆盖式替换旧目标: %s", targetsBody)
	}
	rulesBody := getBody(t, dstBase+"/api/rules")
	if !strings.Contains(rulesBody, "roundtrip.example.com") {
		t.Errorf("导入后规则缺失: %s", rulesBody)
	}
	settingsBody := getBody(t, dstBase+"/api/settings")
	if !strings.Contains(settingsBody, tcSecretID) || !strings.Contains(settingsBody, "dark") {
		t.Errorf("导入后凭据/主题缺失: %s", settingsBody)
	}
	alertCfgBody := getBody(t, dstBase+"/api/alerts")
	if !strings.Contains(alertCfgBody, smtpPass) || !strings.Contains(alertCfgBody, webhookURL) {
		t.Errorf("导入后告警缺失: %s", alertCfgBody)
	}
	for _, fragment := range []string{`"policy"`, `"uptime_kuma_push"`, `"health_timeout":"25m"`, `"interval":"45s"`} {
		if !strings.Contains(alertCfgBody, fragment) {
			t.Errorf("导入后四对象告警缺少 %s: %s", fragment, alertCfgBody)
		}
	}

	// 4) 版本策略：version 1 与 version 2 都必须 400（version 3 是唯一协议）
	if code := postJSON(t, dstBase+"/api/config/import", `{"version":1,"targets":[],"rules":[],"settings":{}}`); code != http.StatusBadRequest {
		t.Errorf("version 1 导入状态码 = %d, want 400", code)
	}
	if code := postJSON(t, dstBase+"/api/config/import", `{"version":2,"targets":[],"rules":[],"settings":{}}`); code != http.StatusBadRequest {
		t.Errorf("version 2 导入状态码 = %d, want 400", code)
	}

	// 5) 进程日志不得泄露任何敏感值（导出响应是唯一允许包含它们的 HTTP 响应）
	for name, output := range map[string]string{"来源": srcOut.String(), "目标": dstOut.String()} {
		for _, secret := range []string{tcSecretID, tcSecretKey, smtpPass, webhookURL} {
			if strings.Contains(output, secret) {
				t.Errorf("%s进程日志泄露敏感值 %q；输出:\n%s", name, secret, output)
			}
		}
	}
	if !strings.Contains(dstOut.String(), "导入成功") && !strings.Contains(dstOut.String(), "channel=dingtalk") {
		// 仅作可读性提示：日志内容不构成断言失败
		t.Logf("目标进程日志:\n%s", dstOut.String())
	}
}

// getStatus 发送指定方法的请求并返回状态码（进程级用例专用）。
func getStatus(t *testing.T, method, url, body string) int {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("请求 %s 失败: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	return resp.StatusCode
}

// postRaw 发送 POST 并返回响应体与响应头（进程级用例专用）。
func postRaw(t *testing.T, url, body string) (string, http.Header) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("请求 %s 失败: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s 状态码 = %d, want 200; body=%s", url, resp.StatusCode, string(raw))
	}
	return string(raw), resp.Header
}

// postJSON 发送 JSON POST 请求并返回状态码（进程级用例专用）。
func postJSON(t *testing.T, url, body string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("请求 %s 失败: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	return resp.StatusCode
}

// ─── Issue6 A19：pidfile 单实例的进程级判别回归 ───

// TestProcessSecondInstanceRejectedByPidFile 第二个实例必须拒绝启动并提示 PID。
//
// 判别性：全仓此前**没有任何**用例覆盖 pidfile 的「FWAlizer 已在运行」分支（只有
// 「退出后清理」断言）。本用例用真实二进制 + 真实 pidfile：第一个实例正常运行后，
// 第二个实例（同一 FWALIZER_DATA_DIR）必须以非零状态退出、打印已有 PID，且**不得**
// 启动 WebUI（端口不会被抢占）。
func TestProcessSecondInstanceRejectedByPidFile(t *testing.T) {
	dataDir := t.TempDir()
	port := freePort(t)

	first, firstOut := startProcess(t, dataDir, map[string]string{"WEBUI_PORT": fmt.Sprintf("%d", port)})
	waitForHTTP(t, fmt.Sprintf("http://127.0.0.1:%d/api/health", port))

	// pidfile 必须存在且记录的是第一个实例的 PID
	pidPath := config.GetPidFilePath(dataDir)
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("读取 pidfile 失败: %v（第一个实例必须先写入 pidfile）", err)
	}
	firstPID := strings.TrimSpace(string(raw))
	if firstPID == "" {
		t.Fatal("pidfile 内容为空")
	}
	if firstPID != strconv.Itoa(first.Process.Pid) {
		t.Errorf("pidfile PID = %q, want %d", firstPID, first.Process.Pid)
	}

	// 第二个实例：同一数据目录 → 必须被拒绝
	second, secondOut := startProcess(t, dataDir, map[string]string{"WEBUI_PORT": fmt.Sprintf("%d", port)})
	code, err := waitForProcessExit(t, second, 15*time.Second)
	if err != nil {
		t.Fatalf("第二个实例未在限期内退出: %v\n输出:\n%s", err, secondOut.String())
	}
	if code == 0 {
		t.Errorf("第二个实例退出码 = 0, want 非零\n输出:\n%s", secondOut.String())
	}
	if !strings.Contains(secondOut.String(), "FWAlizer 已在运行") {
		t.Errorf("第二个实例必须提示已有实例运行；输出:\n%s", secondOut.String())
	}
	if !strings.Contains(secondOut.String(), firstPID) {
		t.Errorf("第二个实例必须提示已有 PID %s；输出:\n%s", firstPID, secondOut.String())
	}

	// 第一个实例必须仍然健康（第二个实例不得抢占端口或数据目录）
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
	if err != nil {
		t.Fatalf("第二个实例被拒绝后第一个实例必须仍可访问: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("第一个实例 health 状态码 = %d, want 200", resp.StatusCode)
	}

	// 收尾：第一个实例正常退出并释放文件锁
	if err := first.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("发送 SIGTERM 失败: %v", err)
	}
	if code, err := waitForProcessExit(t, first, 20*time.Second); err != nil {
		t.Fatalf("第一个实例未正常退出: %v\n输出:\n%s", err, firstOut.String())
	} else if code != 0 {
		t.Errorf("第一个实例退出码 = %d, want 0\n输出:\n%s", code, firstOut.String())
	}
	assertPidFileReleased(t, dataDir)
}

// TestProcessPidFileRestart 验证陈旧 PID 和 SIGTERM/SIGKILL 后同目录重启。
func TestProcessPidFileRestart(t *testing.T) {
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		t.Run(sig.String(), func(t *testing.T) {
			dataDir := t.TempDir()
			path := config.GetPidFilePath(dataDir)
			if err := os.WriteFile(path, []byte("1\n"), 0600); err != nil {
				t.Fatal(err)
			}
			port := freePort(t)
			env := map[string]string{"WEBUI_PORT": strconv.Itoa(port)}
			first, out := startProcess(t, dataDir, env)
			url := fmt.Sprintf("http://127.0.0.1:%d/api/health", port)
			waitForHTTP(t, url)
			if err := first.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			code, err := waitForProcessExit(t, first, 20*time.Second)
			if err != nil {
				t.Fatalf("退出失败: %v\n%s", err, out.String())
			}
			if sig == syscall.SIGTERM && code != 0 {
				t.Fatalf("SIGTERM exit=%d", code)
			}
			if sig == syscall.SIGKILL && code == 0 {
				t.Fatal("SIGKILL 应为异常退出")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("锁文件未保留: %v", err)
			}
			second, secondOut := startProcess(t, dataDir, env)
			waitForHTTP(t, url)
			if err := second.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			if code, err := waitForProcessExit(t, second, 20*time.Second); err != nil || code != 0 {
				t.Fatalf("重启后退出: code=%d err=%v\n%s", code, err, secondOut.String())
			}
			assertPidFileReleased(t, dataDir)
		})
	}
}

// TestProcessPidFileKernelBarrier 两个真实进程均不得绕过测试持有的内核锁。
func TestProcessPidFileKernelBarrier(t *testing.T) {
	dataDir := t.TempDir()
	cleanup, err := config.WritePidFile(config.GetPidFilePath(dataDir))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for i := 0; i < 2; i++ {
		cmd, out := startProcess(t, dataDir, map[string]string{"WEBUI_PORT": strconv.Itoa(freePort(t))})
		code, err := waitForProcessExit(t, cmd, 15*time.Second)
		if err != nil || code == 0 || !strings.Contains(out.String(), "FWAlizer 已在运行") {
			t.Fatalf("内核锁屏障被绕过: code=%d err=%v\n%s", code, err, out.String())
		}
	}
}

// TestProcessPidFileConcurrentStart 同一屏障同时释放两个启动者，胜者在败者退出前保持运行。
func TestProcessPidFileConcurrentStart(t *testing.T) {
	dataDir := t.TempDir()
	ports := []int{freePort(t), freePort(t)}
	for ports[1] == ports[0] {
		ports[1] = freePort(t)
	}
	cmds := make([]*exec.Cmd, 2)
	outs := make([]*syncBuffer, 2)
	gate := make(chan struct{})
	started := make(chan error, 2)
	for i := range cmds {
		cmds[i] = exec.Command(testBinary)
		cmds[i].Env = processEnv(dataDir, map[string]string{"WEBUI_PORT": strconv.Itoa(ports[i])})
		outs[i] = &syncBuffer{}
		cmds[i].Stdout, cmds[i].Stderr = outs[i], outs[i]
		go func(cmd *exec.Cmd) { <-gate; started <- cmd.Start() }(cmds[i])
	}
	close(gate)
	var startErr error
	for range cmds {
		if err := <-started; err != nil {
			startErr = err
		}
	}
	type result struct {
		index int
		err   error
	}
	exited := make(chan result, 2)
	running, reaped := 0, 0
	for i, cmd := range cmds {
		if cmd.Process != nil {
			running++
			go func(i int, cmd *exec.Cmd) { exited <- result{i, cmd.Wait()} }(i, cmd)
		}
	}
	// 每个进程只有一个 Wait 所有者；失败时有界回收仍在运行的子进程。
	defer func() {
		if reaped == running {
			return
		}
		for _, cmd := range cmds {
			if cmd.Process != nil {
				if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
					t.Error(err)
				}
			}
		}
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for reaped < running {
			select {
			case <-exited:
				reaped++
			case <-timer.C:
				t.Error("并发测试子进程回收超时")
				return
			}
		}
	}()
	if startErr != nil {
		t.Fatal(startErr)
	}
	var loser result
	select {
	case loser = <-exited:
		reaped++
	case <-time.After(15 * time.Second):
		t.Fatalf("并发启动未拒绝一个实例\n%s\n%s", outs[0].String(), outs[1].String())
	}
	var exitErr *exec.ExitError
	if !errors.As(loser.err, &exitErr) || exitErr.ExitCode() == 0 || !strings.Contains(outs[loser.index].String(), "FWAlizer 已在运行") {
		t.Fatalf("败者未因锁竞争退出: %v\n%s", loser.err, outs[loser.index].String())
	}
	winner := 1 - loser.index
	waitForHTTP(t, fmt.Sprintf("http://127.0.0.1:%d/api/health", ports[winner]))
	if err := cmds[winner].Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-exited:
		reaped++
		if r.index != winner || r.err != nil {
			t.Fatalf("胜者退出失败: %v\n%s", r.err, outs[winner].String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("胜者未有界退出")
	}
	assertPidFileReleased(t, dataDir)
}

// ─── Build7 Step 4：真实二进制的 operational 端点与静态存活端点 ───

// TestProcessOperationalHealthEndpoint 真实进程：
//   - /api/health/operational 在同步引擎进入运行后必须返回 200 且 status=ok；
//   - /api/health 必须始终保持静态 {"status":"ok"}。
func TestProcessOperationalHealthEndpoint(t *testing.T) {
	dataDir := t.TempDir()
	port := freePort(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	cmd, out := startProcess(t, dataDir, map[string]string{"WEBUI_PORT": fmt.Sprintf("%d", port)})
	waitForHTTP(t, base+"/api/health")

	// 空库同步开启：Run 会先进入 running=true，再以 idle 轮次刷新 last_sync。
	// 轮询等待 operational 变为 200（不使用固定 sleep 猜测时序）。
	deadline := time.Now().Add(20 * time.Second)
	code := 0
	body := ""
	for time.Now().Before(deadline) {
		body = getBody(t, base+"/api/health/operational")
		code = getStatus(t, http.MethodGet, base+"/api/health/operational", "")
		if code == http.StatusOK {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if code != http.StatusOK {
		t.Fatalf("/api/health/operational 状态码 = %d, want 200; body=%s; 进程日志:\n%s", code, body, out.String())
	}
	if !strings.Contains(body, `"status":"ok"`) || !strings.Contains(body, `"reasons":[]`) {
		t.Errorf("operational 健康响应不符合契约: %s", body)
	}

	// 静态存活端点不受影响
	staticBody := getBody(t, base+"/api/health")
	if staticBody != `{"status":"ok"}` {
		t.Errorf("/api/health = %q, want exactly {\"status\":\"ok\"}", staticBody)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("发送 SIGTERM 失败: %v", err)
	}
	if exitCode, err := waitForProcessExit(t, cmd, 20*time.Second); err != nil || exitCode != 0 {
		t.Fatalf("进程退出失败: code=%d err=%v; 日志:\n%s", exitCode, err, out.String())
	}
}

// ─── Build7 Step 5：真实二进制 + 本地 HTTP mock 的 Push 心跳链路 ───

// TestProcessUptimeKumaPushHeartbeat 真实进程：保存 Push 配置后必须立即首发心跳，
// 且请求携带 status/msg/ping 与用户 token。
//
// 证据边界：这是**本地 HTTP mock**，不代表真实 Uptime Kuma 可达或 DOWN/恢复通知。
func TestProcessUptimeKumaPushHeartbeat(t *testing.T) {
	type pushHit struct {
		path  string
		query string
	}
	hits := make(chan pushHit, 8)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case hits <- pushHit{path: r.URL.Path, query: r.URL.RawQuery}:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer mock.Close()

	dataDir := t.TempDir()
	port := freePort(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	cmd, out := startProcess(t, dataDir, map[string]string{"WEBUI_PORT": fmt.Sprintf("%d", port)})
	waitForHTTP(t, base+"/api/health")

	const token = "e2e-push-token-abc123"
	body := `{"policy":{"dns_failed_enabled":false,"sync_error_enabled":false,` +
		`"operational_error_enabled":false,"health_timeout":"10m"},` +
		`"email":{"enabled":false,"host":"","port":"587","username":"","password":"",` +
		`"from_addr":"","to_addr":"","subject":"[FWAlizer] 告警通知",` +
		`"body":"FWAlizer 检测到运行异常，请检查同步日志。"},` +
		`"webhook":{"enabled":false,"url":"","channel":"dingtalk"},` +
		`"uptime_kuma_push":{"enabled":true,"url":"` + mock.URL + `/api/push/` + token + `?foo=bar","interval":"20s"}}`
	if code := putJSON(t, base+"/api/alerts", body); code != http.StatusOK {
		t.Fatalf("PUT /api/alerts 状态码 = %d, want 200", code)
	}

	// 启用后必须立即首发，不等待 20s 间隔
	var hit pushHit
	select {
	case hit = <-hits:
	case <-time.After(10 * time.Second):
		t.Fatalf("未在限期内收到 Push 心跳；进程日志:\n%s", out.String())
	}
	if !strings.Contains(hit.path, token) {
		t.Errorf("Push 路径必须保留 token: %q", hit.path)
	}
	values, err := url.ParseQuery(hit.query)
	if err != nil {
		t.Fatalf("解析 Push query 失败: %v", err)
	}
	if values.Get("status") != "up" || values.Get("msg") != "OK" {
		t.Errorf("Push query 不符合契约: %q", hit.query)
	}
	if _, err := strconv.ParseInt(values.Get("ping"), 10, 64); err != nil {
		t.Errorf("ping 必须是毫秒整数: %q", values.Get("ping"))
	}
	if values.Get("foo") != "bar" {
		t.Errorf("未知 query 必须保留: %q", hit.query)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("发送 SIGTERM 失败: %v", err)
	}
	if exitCode, err := waitForProcessExit(t, cmd, 20*time.Second); err != nil || exitCode != 0 {
		t.Fatalf("进程退出失败: code=%d err=%v; 日志:\n%s", exitCode, err, out.String())
	}
}

// ─── Build7 Step 7：测试邮件的 UI 载荷（P1 修复） ───

// postJSONWithBody 发送 JSON POST 请求并返回状态码与响应体（进程级用例专用）。
func postJSONWithBody(t *testing.T, url, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 35 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("请求 %s 失败: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf := &strings.Builder{}
	if _, err := io.Copy(buf, resp.Body); err != nil {
		t.Fatalf("读取 %s 响应失败: %v", url, err)
	}
	return resp.StatusCode, buf.String()
}

// fakeSMTPRecord 记录最小假 SMTP 服务器收到的一次会话内容。
type fakeSMTPRecord struct {
	mu    sync.Mutex
	rcpts []string
	data  string
}

func (r *fakeSMTPRecord) addRcpt(v string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rcpts = append(r.rcpts, v)
}

func (r *fakeSMTPRecord) setData(v string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = v
}

func (r *fakeSMTPRecord) Data() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data
}

func (r *fakeSMTPRecord) Rcpts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.rcpts...)
}

// startFakeSMTPForProcessTest 启动一个最小假 SMTP 服务器（不宣告 STARTTLS/AUTH），
// 供真实二进制的测试邮件用例使用；返回监听地址与采集器。
//
// 证据边界：这是本地假 SMTP，不代表真实 SMTP 服务器或收件箱投递。
func startFakeSMTPForProcessTest(t *testing.T) (string, string, *fakeSMTPRecord) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动假 SMTP 监听失败: %v", err)
	}
	rec := &fakeSMTPRecord{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveFakeSMTP(conn, rec)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", strconv.Itoa(addr.Port), rec
}

// serveFakeSMTP 处理一条最小 SMTP 会话：220 → EHLO → MAIL/RCPT → DATA → QUIT。
func serveFakeSMTP(conn net.Conn, rec *fakeSMTPRecord) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	write := func(s string) bool {
		if _, err := conn.Write([]byte(s)); err != nil {
			return false
		}
		return true
	}
	if !write("220 fake ESMTP ready\r\n") {
		return
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		switch {
		case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
			if !write("250-fake\r\n250 OK\r\n") {
				return
			}
		case strings.HasPrefix(line, "MAIL FROM"):
			if !write("250 OK\r\n") {
				return
			}
		case strings.HasPrefix(line, "RCPT TO"):
			rec.addRcpt(strings.TrimSpace(line))
			if !write("250 OK\r\n") {
				return
			}
		case strings.HasPrefix(line, "DATA"):
			if !write("354 End data with <CR><LF>.<CR><LF>\r\n") {
				return
			}
			var sb strings.Builder
			for {
				dataLine, derr := reader.ReadString('\n')
				if derr != nil {
					return
				}
				if dataLine == ".\r\n" || dataLine == ".\n" {
					break
				}
				sb.WriteString(dataLine)
			}
			rec.setData(sb.String())
			if !write("250 OK\r\n") {
				return
			}
		case strings.HasPrefix(line, "QUIT"):
			_ = write("221 Bye\r\n")
			return
		default:
			if !write("250 OK\r\n") {
				return
			}
		}
	}
}

// TestProcessTestEmailWithUIPayload 真实进程：与告警页现在构造的**完全一致的 8 字段载荷**
// 必须走完 SMTP 会话并返回成功；同时锁定严格解码契约（多带 enabled 必须 400）。
//
// 判别性：修复前告警页序列化整个 email 表单对象（含 enabled），后端必然 400，
// 页面「SMTP 已接受」成功态不可达；本用例锁住修复后的 UI 载荷形状。
//
// 证据边界：使用本地假 SMTP，不代表真实 SMTP 服务器或收件箱投递。
func TestProcessTestEmailWithUIPayload(t *testing.T) {
	host, port, rec := startFakeSMTPForProcessTest(t)

	dataDir := t.TempDir()
	webPort := freePort(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", webPort)
	cmd, out := startProcess(t, dataDir, map[string]string{"WEBUI_PORT": fmt.Sprintf("%d", webPort)})
	waitForHTTP(t, base+"/api/health")

	// 邮件渠道保持关闭：测试发送不要求 email.enabled=true（Build7 §5.1），
	// 但 SMTP 字段必须完整（按「实际要发送」校验）。
	alertsBody := `{"policy":{"dns_failed_enabled":false,"sync_error_enabled":false,` +
		`"operational_error_enabled":false,"health_timeout":"10m"},` +
		`"email":{"enabled":false,"host":"` + host + `","port":"` + port + `",` +
		`"username":"","password":"","from_addr":"from@example.com",` +
		`"to_addr":"a@example.com, b@example.com","subject":"[FWAlizer] 告警通知",` +
		`"body":"FWAlizer 检测到运行异常，请检查同步日志。"},` +
		`"webhook":{"enabled":false,"url":"","channel":"dingtalk"},` +
		`"uptime_kuma_push":{"enabled":false,"url":"","interval":"60s"}}`
	if code := putJSON(t, base+"/api/alerts", alertsBody); code != http.StatusOK {
		t.Fatalf("PUT /api/alerts 状态码 = %d, want 200；日志:\n%s", code, out.String())
	}

	// 与 webui/frontend/src/views/Alerts.vue 的 testSend() 显式构造的载荷逐字段一致：
	// 只有 8 个发送字段，不含 enabled（Build7 §5.1）。
	uiPayload := fmt.Sprintf(`{"host":%q,"port":%q,"username":"","password":"",`+
		`"from_addr":"from@example.com","to_addr":"a@example.com, b@example.com",`+
		`"subject":"[FWAlizer] 告警通知","body":"FWAlizer 检测到运行异常，请检查同步日志。"}`,
		host, port)

	code, body := postJSONWithBody(t, base+"/api/alerts/test-email", uiPayload)
	if code != http.StatusOK {
		t.Fatalf("POST /api/alerts/test-email 状态码 = %d, want 200; body=%s；日志:\n%s", code, body, out.String())
	}
	if !strings.Contains(body, `"success":true`) || !strings.Contains(body, "SMTP 服务器已接受测试邮件") {
		t.Fatalf("测试邮件成功口径错误: %s", body)
	}

	// 服务端必须真的走完 SMTP 会话：主题追加固定后缀、正文追加固定说明与时间
	mailData := rec.Data()
	if !strings.Contains(mailData, "Subject: [FWAlizer] 告警通知 - 测试邮件") {
		t.Errorf("测试邮件主题缺少固定后缀: %q", mailData)
	}
	if !strings.Contains(mailData, "这是一次手动测试邮件") {
		t.Errorf("测试邮件正文缺少固定说明: %q", mailData)
	}
	if !strings.Contains(mailData, "Content-Type: text/plain; charset=UTF-8") {
		t.Errorf("测试邮件必须固定为纯文本 UTF-8: %q", mailData)
	}
	// 多收件人必须逐项 Trim 后分别投递
	rcpts := rec.Rcpts()
	if len(rcpts) != 2 {
		t.Fatalf("RCPT 数量 = %d, want 2: %v", len(rcpts), rcpts)
	}
	for i, want := range []string{"a@example.com", "b@example.com"} {
		if !strings.Contains(rcpts[i], "<"+want+">") {
			t.Errorf("第 %d 个收件人未逐项 Trim: %q", i+1, rcpts[i])
		}
	}

	// 反向断言：修复前的页面载荷（整个 email 对象，多一个 enabled）必须被严格拒绝。
	withEnabled := `{"enabled":false,` + strings.TrimPrefix(uiPayload, "{")
	code2, body2 := postJSONWithBody(t, base+"/api/alerts/test-email", withEnabled)
	if code2 != http.StatusBadRequest || !strings.Contains(body2, "enabled") {
		t.Errorf("多带 enabled 必须 400 且指明未知字段: code=%d body=%s", code2, body2)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("发送 SIGTERM 失败: %v", err)
	}
	if exitCode, err := waitForProcessExit(t, cmd, 20*time.Second); err != nil || exitCode != 0 {
		t.Fatalf("进程退出失败: code=%d err=%v; 日志:\n%s", exitCode, err, out.String())
	}
}

// ─── Build7 Step 7：重启后 Push 首发不得误报 DOWN（P2 判别性用例） ───

// pushHitRecord 记录一次 Push 心跳的路径与 query。
type pushHitRecord struct {
	path  string
	query string
}

// drainPushHits 非阻塞排空 channel 中已到达的心跳。
func drainPushHits(ch chan pushHitRecord) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// TestProcessRestartPushFirstHeartbeatIsUp Build7 Step 7：
// Push 配置持久化后重启进程，**第一条**心跳必须是 status=up&msg=OK，且启动日志
// 不得出现「运行健康异常」。
//
// 判别性：修复前 run.go 先启动监督器/Push、后启动 Syncer，而 running 只在
// Syncer.Run 内才置 true —— 子进程设 GOMAXPROCS=1 时首检必然读到 running=false，
// 于是首条心跳为 status=down&msg=同步引擎未运行 并写 WARN（实测 12/12 复现）。
//
// 证据边界：本地 HTTP mock，不代表真实 Uptime Kuma。
func TestProcessRestartPushFirstHeartbeatIsUp(t *testing.T) {
	hits := make(chan pushHitRecord, 16)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case hits <- pushHitRecord{path: r.URL.Path, query: r.URL.RawQuery}:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer mock.Close()

	dataDir := t.TempDir()
	const token = "p2-restart-token"

	// 第一次启动：保存 Push 配置（触发条件开启，便于同时观察是否误发边沿事件）
	port1 := freePort(t)
	base1 := fmt.Sprintf("http://127.0.0.1:%d", port1)
	cmd1, out1 := startProcess(t, dataDir, map[string]string{"WEBUI_PORT": fmt.Sprintf("%d", port1)})
	waitForHTTP(t, base1+"/api/health")

	alertsBody := `{"policy":{"dns_failed_enabled":false,"sync_error_enabled":false,` +
		`"operational_error_enabled":true,"health_timeout":"10m"},` +
		`"email":{"enabled":false,"host":"","port":"587","username":"","password":"",` +
		`"from_addr":"","to_addr":"","subject":"[FWAlizer] 告警通知",` +
		`"body":"FWAlizer 检测到运行异常，请检查同步日志。"},` +
		`"webhook":{"enabled":false,"url":"","channel":"dingtalk"},` +
		`"uptime_kuma_push":{"enabled":true,"url":"` + mock.URL + `/api/push/` + token + `?foo=bar","interval":"20s"}}`
	if code := putJSON(t, base1+"/api/alerts", alertsBody); code != http.StatusOK {
		t.Fatalf("PUT /api/alerts 状态码 = %d, want 200；日志:\n%s", code, out1.String())
	}
	// 启用后立即首发（保存时的正常心跳），等待并丢弃
	select {
	case <-hits:
	case <-time.After(10 * time.Second):
		t.Fatalf("保存 Push 配置后未收到首发心跳；日志:\n%s", out1.String())
	}
	if err := cmd1.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("第一实例 SIGTERM 失败: %v", err)
	}
	if exitCode, err := waitForProcessExit(t, cmd1, 20*time.Second); err != nil || exitCode != 0 {
		t.Fatalf("第一实例退出失败: code=%d err=%v", exitCode, err)
	}
	drainPushHits(hits)

	// 第二次启动：同一数据目录（Push 配置已持久化），GOMAXPROCS=1 放大启动竞态
	port2 := freePort(t)
	base2 := fmt.Sprintf("http://127.0.0.1:%d", port2)
	cmd2, out2 := startProcess(t, dataDir, map[string]string{
		"WEBUI_PORT": fmt.Sprintf("%d", port2),
		"GOMAXPROCS": "1",
	})
	waitForHTTP(t, base2+"/api/health")

	var first pushHitRecord
	select {
	case first = <-hits:
	case <-time.After(15 * time.Second):
		t.Fatalf("重启后未收到 Push 心跳；日志:\n%s", out2.String())
	}
	values, err := url.ParseQuery(first.query)
	if err != nil {
		t.Fatalf("解析首条心跳 query 失败: %v", err)
	}
	if values.Get("status") != "up" || values.Get("msg") != "OK" {
		t.Errorf("重启后首条心跳必须为 up/OK（不得把尚未启动误判为引擎未运行）: %q", first.query)
	}
	if !strings.Contains(first.path, token) {
		t.Errorf("Push 路径必须保留 token: %q", first.path)
	}
	if values.Get("foo") != "bar" {
		t.Errorf("未知 query 必须保留: %q", first.query)
	}

	// 启动日志不得出现伪运行健康异常（修复前该 WARN 必然出现）
	if strings.Contains(out2.String(), "运行健康异常") {
		t.Errorf("启动日志出现伪运行健康异常:\n%s", out2.String())
	}

	if err := cmd2.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("第二实例 SIGTERM 失败: %v", err)
	}
	if exitCode, err := waitForProcessExit(t, cmd2, 20*time.Second); err != nil || exitCode != 0 {
		t.Fatalf("第二实例退出失败: code=%d err=%v; 日志:\n%s", exitCode, err, out2.String())
	}
}
