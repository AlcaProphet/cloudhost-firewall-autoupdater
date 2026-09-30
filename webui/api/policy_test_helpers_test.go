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

// waitForNoSMTPData 在给定时限内等待（「未收到报文」的判定由调用点在等待后完成）。
// rec 形参仅为与 waitForSMTPData 的调用点保持对称而保留。
func waitForNoSMTPData(t *testing.T, _ *apiFakeSMTPRecord, wait time.Duration) {
	t.Helper()
	time.Sleep(wait)
}
