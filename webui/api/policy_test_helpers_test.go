package api

import (
	"fmt"
	"net"
	"testing"
	"time"
)

// splitHostPortForTest 把 "host:port" 拆开（测试夹具）
func splitHostPortForTest(addr string) (string, string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", fmt.Errorf("拆分地址失败: %w", err)
	}
	return host, port, nil
}

// waitForSMTPData 等待假 SMTP 收到报文（有界）
func waitForSMTPData(t *testing.T, rec *apiFakeSMTPRecord, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if rec.Data() != "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForNoSMTPData 断言在给定时限内假 SMTP 没有收到任何报文
func waitForNoSMTPData(t *testing.T, rec *apiFakeSMTPRecord, wait time.Duration) {
	t.Helper()
	time.Sleep(wait)
}
