//go:build linux || darwin

package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

func assertProcessFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s mode=%04o want=%04o", filepath.Base(path), info.Mode().Perm(), want)
	}
}

// startPermissionProcess 把 umask 限定在产品子进程；参数通过 argv 传入，不插值到 shell 文本。
func startPermissionProcess(t *testing.T, dataDir, cwd, mask string, extra map[string]string) (*exec.Cmd, *syncBuffer, <-chan error) {
	t.Helper()
	cmd := exec.Command("sh", "-c", "umask \"$1\"; exec \"$2\"", "permission-test", mask, testBinary)
	cmd.Env = processEnv(dataDir, extra)
	cmd.Dir = cwd
	out := &syncBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Errorf("清理测试进程: %v", err)
		}
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("清理测试进程超时")
		}
	})
	return cmd, out, done
}

// waitPermissionProcess 共用唯一 Wait 的结果，超时清理也不会并发调用 cmd.Wait。
func waitPermissionProcess(done <-chan error, timeout time.Duration) (int, error) {
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
		return -1, fmt.Errorf("权限测试进程在 %v 内未退出", timeout)
	}
}

// TestProcessDataPermissions 在真实启动路径覆盖新目录与两种旧目录/DB/锁模式。
func TestProcessDataPermissions(t *testing.T) {
	for _, mask := range []string{"0000", "0022", "0077", "0777"} {
		for _, wide := range []os.FileMode{0, 0755, 0777} {
			t.Run(fmt.Sprintf("umask_%s/existing_%04o", mask, wide), func(t *testing.T) {
				parent := t.TempDir()
				if err := os.Chmod(parent, 0755); err != nil {
					t.Fatal(err)
				}
				dataDir := filepath.Join(parent, "data")
				if wide != 0 {
					if err := os.Mkdir(dataDir, 0700); err != nil {
						t.Fatal(err)
					}
					s, err := config.OpenStore(filepath.Join(dataDir, "config.db"))
					if err != nil {
						t.Fatal(err)
					}
					if err := s.SetSetting("tc_access_key", "local-permission-sentinel"); err != nil {
						t.Fatal(err)
					}
					if err := s.Close(); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(filepath.Join(dataDir, "config.db"), 0666); err != nil {
						t.Fatal(err)
					}
					lock := config.GetPidFilePath(dataDir)
					if err := os.WriteFile(lock, []byte("invalid old pid"), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(lock, 0666); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dataDir, "user-file"), []byte("keep"), 0644); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(filepath.Join(dataDir, "user-file"), 0644); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(dataDir, wide); err != nil {
						t.Fatal(err)
					}
				}
				port := freePort(t)
				url := fmt.Sprintf("http://127.0.0.1:%d/api/health", port)
				cmd, out, done := startPermissionProcess(t, dataDir, parent, mask, map[string]string{"WEBUI_PORT": strconv.Itoa(port)})
				waitForHTTP(t, url)
				assertProcessFileMode(t, parent, 0755)
				assertProcessFileMode(t, dataDir, 0700)
				for _, name := range []string{"config.db", "config.db-wal", "config.db-shm", "fwalizer.pid"} {
					assertProcessFileMode(t, filepath.Join(dataDir, name), 0600)
				}
				if wide != 0 {
					assertProcessFileMode(t, filepath.Join(dataDir, "user-file"), 0644)
					resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(fmt.Sprintf("http://127.0.0.1:%d/api/settings", port))
					if err != nil {
						t.Fatal(err)
					}
					// 此处只核对持久化哨兵，响应内容不写入日志。
					var body strings.Builder
					if _, err := io.Copy(&body, resp.Body); err != nil {
						t.Fatal(err)
					}
					if err := resp.Body.Close(); err != nil {
						t.Fatal(err)
					}
					if resp.StatusCode != 200 || !strings.Contains(body.String(), "local-permission-sentinel") {
						t.Fatal("旧配置在权限迁移后丢失")
					}
				}
				if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
				code, err := waitPermissionProcess(done, 15*time.Second)
				if err != nil || code != 0 {
					t.Fatalf("退出code=%d err=%v\n%s", code, err, out.String())
				}
				assertPidFileReleased(t, dataDir)
			})
		}
	}
}

// TestProcessMissingHomeDoesNotUseCWD 防止默认路径失败时 chmod 当前目录或在其中建库。
func TestProcessMissingHomeDoesNotUseCWD(t *testing.T) {
	for _, raw := range []string{"", "  ", "explicit"} {
		t.Run(strconv.Quote(raw), func(t *testing.T) {
			cwd := t.TempDir()
			if err := os.Chmod(cwd, 0755); err != nil {
				t.Fatal(err)
			}
			dataDir := raw
			if raw == "explicit" {
				dataDir = filepath.Join(cwd, "dedicated")
			}
			port := freePort(t)
			url := fmt.Sprintf("http://127.0.0.1:%d/api/health", port)
			cmd, out, done := startPermissionProcess(t, dataDir, cwd, "0022", map[string]string{"HOME": "", "WEBUI_PORT": strconv.Itoa(port)})
			if raw == "explicit" {
				waitForHTTP(t, url)
				assertProcessFileMode(t, dataDir, 0700)
				if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
			}
			code, err := waitPermissionProcess(done, 15*time.Second)
			if err != nil {
				t.Fatalf("退出失败: %v\n%s", err, out.String())
			}
			if raw == "explicit" {
				if code != 0 {
					t.Fatalf("显式目录不能依赖HOME: code=%d\n%s", code, out.String())
				}
			} else {
				if code != 1 || !strings.Contains(out.String(), "确定默认数据目录失败") {
					t.Fatalf("错误默认路径仍启动: code=%d\n%s", code, out.String())
				}
				assertPortClosed(t, url)
			}
			assertProcessFileMode(t, cwd, 0755)
			for _, name := range []string{"config.db", "fwalizer.pid"} {
				if _, err := os.Stat(filepath.Join(cwd, name)); !os.IsNotExist(err) {
					t.Fatalf("默认路径失败污染CWD: %s %v", name, err)
				}
			}
		})
	}
}
