//go:build linux || darwin

package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// TestPidFileStaleContents 验证 PID 不再判活，持锁期间 GC 不会提前释放文件。
func TestPidFileStaleContents(t *testing.T) {
	for _, content := range []string{"1\n", strconv.Itoa(os.Getpid()) + "\n", "", "invalid\n"} {
		t.Run(strconv.Quote(content), func(t *testing.T) {
			path := GetPidFilePath(t.TempDir())
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			cleanup, err := WritePidFile(path)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			runtime.GC()
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := strconv.Itoa(os.Getpid()) + "\n"
			if string(raw) != want {
				t.Fatalf("PID = %q, want %q", raw, want)
			}
			secondCleanup, err := WritePidFile(path)
			if err == nil {
				secondCleanup()
				t.Fatal("持锁时第二次取得锁成功")
			}
			if !strings.Contains(err.Error(), "FWAlizer 已在运行") {
				t.Fatal(err)
			}
		})
	}
}

// TestPidFileRejectsKernelLock 用真实内核锁构造判别控制，旧 PID-only 实现会忽略该锁。
func TestPidFileRejectsKernelLock(t *testing.T) {
	for _, content := range []string{"1\n", strconv.Itoa(os.Getpid()) + "\n", "", "invalid\n"} {
		t.Run(strconv.Quote(content), func(t *testing.T) {
			path := GetPidFilePath(t.TempDir())
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			f, err := os.OpenFile(path, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := f.Close(); err != nil {
					t.Error(err)
				}
			}()
			if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			cleanup, err := WritePidFile(path)
			if err == nil {
				cleanup()
				t.Fatal("竞争者绕过真实内核锁")
			}
			if cleanup != nil {
				t.Fatal("失败不应返回 cleanup")
			}
			if !strings.Contains(err.Error(), "FWAlizer 已在运行") {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != content {
				t.Fatalf("竞争者覆盖诊断内容: %q", raw)
			}
		})
	}
}

// TestPidFileReleaseAndPermissions 验证释放后保留同一文件、可复用且新建权限为 0600。
func TestPidFileReleaseAndPermissions(t *testing.T) {
	path := GetPidFilePath(t.TempDir())
	cleanup, err := WritePidFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	if before.Mode().Perm() != 0600 {
		cleanup()
		t.Fatalf("mode = %o", before.Mode().Perm())
	}
	cleanup()
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("cleanup 替换了锁文件")
	}
	cleanup, err = WritePidFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
}

// TestPidFileIndependentDirectories 不同数据目录可独立取得锁。
func TestPidFileIndependentDirectories(t *testing.T) {
	first, err := WritePidFile(GetPidFilePath(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	second, err := WritePidFile(GetPidFilePath(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer second()
}

// TestPidFileOpenErrors 路径错误必须原样包装，不能误报已有实例。
func TestPidFileOpenErrors(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{dir, filepath.Join(dir, "missing", "fwalizer.pid")} {
		cleanup, err := WritePidFile(path)
		if err == nil {
			cleanup()
			t.Fatal("非法路径取得锁")
		}
		if strings.Contains(err.Error(), "FWAlizer 已在运行") {
			t.Fatal(err)
		}
	}
}

// TestPidFileLockError 系统锁错误保留原因，不误报已有实例。
func TestPidFileLockError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	err = lockPidFile(f)
	if !errors.Is(err, syscall.EBADF) {
		t.Fatalf("锁错误 = %v, want EBADF", err)
	}
	if strings.Contains(err.Error(), "FWAlizer 已在运行") {
		t.Fatal(err)
	}
}
