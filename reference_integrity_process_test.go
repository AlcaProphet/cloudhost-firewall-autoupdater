//go:build linux || darwin

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// i8r09StartProcess 共用当前 TestMain 的真实产品二进制；只有一个 goroutine 调用 Wait。
func i8r09StartProcess(t *testing.T, dataDir string, port int) (*exec.Cmd, *syncBuffer, <-chan error) {
	t.Helper()
	cmd := exec.Command(testBinary)
	cmd.Env = processEnv(dataDir, map[string]string{"WEBUI_PORT": strconv.Itoa(port)})
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
			t.Error(err)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("回收产品进程超时")
		}
	})
	return cmd, out, done
}

// TestI8R09ProcessRejectsThenRestartsWithRepairedDB 损坏引用在 HTTP 监听前阻断真实启动，离线精确修复后能使用同一路径重新启动。
func TestI8R09ProcessRejectsThenRestartsWithRepairedDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.db")
	s, err := config.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	var a, b int64
	withTxForTest(t, s, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		a, err = s.AddTargetTx(ctx, tx, config.TargetConfig{CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "resource-sentinel-a"})
		if err != nil {
			return err
		}
		b, err = s.AddTargetTx(ctx, tx, config.TargetConfig{CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "resource-sentinel-b"})
		if err != nil {
			return err
		}
		if err := s.AddRuleTx(ctx, tx, config.DomainRule{Host: "host-sentinel.example", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{int(a), int(b)}}); err != nil {
			return err
		}
		if err := s.SetSettingTx(ctx, tx, "sync_enabled", "false"); err != nil {
			return err
		}
		if err := s.SetSettingTx(ctx, tx, "tc_access_key", "credential-sentinel"); err != nil {
			return err
		}
		_, err = s.DeleteTargetTx(ctx, tx, int(a))
		return err
	})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	url := fmt.Sprintf("http://127.0.0.1:%d/api/health", port)
	_, out, done := i8r09StartProcess(t, dir, port)
	code, err := waitPermissionProcess(done, 5*time.Second)
	if err != nil || code != 1 {
		t.Fatalf("损坏库应非零退出: code=%d err=%v output=%s", code, err, out.String())
	}
	if !strings.Contains(out.String(), "加载配置失败: 规则 #1 引用了不存在的目标 #1，数据已损坏") {
		t.Fatalf("启动诊断=%s", out.String())
	}
	for _, forbidden := range []string{"host-sentinel.example", "resource-sentinel", "credential-sentinel", "开始同步", "WebUI"} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatalf("启动前应安全失败: %q output=%s", forbidden, out.String())
		}
	}
	assertPortClosed(t, url)
	s, err = config.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := s.GetRules()
	if err != nil || len(rules) != 1 {
		t.Fatal(rules, err)
	}
	repaired := rules[0]
	repaired.Targets = []int{int(b)}
	withTxForTest(t, s, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := s.UpdateRuleTx(ctx, tx, repaired.ID, repaired)
		if err != nil {
			return err
		}
		if rows != 1 {
			return fmt.Errorf("修复规则数量=%d", rows)
		}
		_, err = s.LoadBusinessSnapshotTx(ctx, tx)
		return err
	})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	cmd, out, done := i8r09StartProcess(t, dir, port)
	waitForHTTP(t, url)
	body := getBody(t, fmt.Sprintf("http://127.0.0.1:%d/api/rules", port))
	if !strings.Contains(body, fmt.Sprintf(`"targets":[%d]`, b)) {
		t.Fatalf("修复后引用=%s", body)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	// 沿用现有进程回归的15秒等待，覆盖产品10秒HTTP收尾预算。
	code, err = waitPermissionProcess(done, 15*time.Second)
	if err != nil || code != 0 {
		t.Fatalf("恢复后退出 code=%d err=%v output=%s", code, err, out.String())
	}
	assertPortClosed(t, url)
}
