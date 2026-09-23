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

// startProcess 以给定环境变量启动真实二进制，返回命令句柄与输出缓冲。
func startProcess(t *testing.T, dataDir string, extra map[string]string) (*exec.Cmd, *strings.Builder) {
	t.Helper()
	cmd := exec.Command(testBinary)
	out := &strings.Builder{}
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
