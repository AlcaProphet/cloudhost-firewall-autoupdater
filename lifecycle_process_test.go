package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// 使用真实 10 秒 HTTP 窗口与本地假 SMTP：关闭连接后仍须等待测试邮件 handler。
// SMTP 服务器接受不代表真实邮件投递，客户端因强制关连接仍应报告失败。
func TestI8R19ProcessRequestDrain(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered, release, smtpDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	record := &fakeSMTPRecord{}
	go func() {
		defer close(smtpDone)
		conn, err := listener.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				t.Error(err)
			}
			return
		}
		close(entered)
		<-release
		serveFakeSMTP(conn, record)
	}()
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
		select {
		case <-smtpDone:
		case <-time.After(3 * time.Second):
			t.Error("假 SMTP 未退出")
		}
	})
	dataDir, port := t.TempDir(), freePort(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	cmd, out := startProcess(t, dataDir, map[string]string{"WEBUI_PORT": strconv.Itoa(port)})
	waitForHTTP(t, base+"/api/health")
	smtpPort := listener.Addr().(*net.TCPAddr).Port
	payload := fmt.Sprintf(`{"host":"127.0.0.1","port":%q,"security":"auto_starttls","username":"","password":"","from_addr":"from@example.com","to_addr":"to@example.com","subject":"I8R19","body":"shutdown fixture"}`, strconv.Itoa(smtpPort))
	requestDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 25 * time.Second}
		defer client.CloseIdleConnections()
		resp, err := client.Post(base+"/api/alerts/test-email", "application/json", strings.NewReader(payload))
		if resp != nil {
			_, readErr := io.Copy(io.Discard, resp.Body)
			err = errors.Join(err, readErr, resp.Body.Close())
		}
		requestDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("测试邮件未进入 SMTP")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitForLog(t, out, "HTTP 收尾超时，已强制关闭剩余连接", 15*time.Second)
	// 完成信号对应真实连接关闭，不能用固定 sleep 推断 handler 已退出。
	select {
	case err := <-requestDone:
		if err == nil {
			t.Error("HTTP 被强制关闭，客户端不应得到成功响应")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP 强制关闭未完成")
	}
	cleanup, lockErr := config.WritePidFile(config.GetPidFilePath(dataDir))
	if lockErr == nil {
		cleanup()
		t.Error("I8R19: handler 未完成时进程已释放数据目录锁")
	}
	once.Do(func() { close(release) })
	if code, err := waitForProcessExit(t, cmd, 8*time.Second); err != nil || code != 0 {
		t.Fatalf("进程收尾失败: code=%d err=%v\n%s", code, err, out.String())
	}
	if record.Data() == "" || !strings.Contains(out.String(), "测试邮件已被 SMTP 服务器接受") {
		t.Error("I8R19: HTTP 强制关闭后没有等待 SMTP handler 完成")
	}
	assertPidFileReleased(t, dataDir)
}
