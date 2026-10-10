package notifier

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

// TestI816SMTPCleanupWarnings 清理失败只记录固定阶段；已关闭连接不产生噪声。
func TestI816SMTPCleanupWarnings(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		warn bool
	}{
		{"nil", nil, false},
		{"closed", net.ErrClosed, false},
		{"wrapped_closed", fmt.Errorf("synthetic-secret: %w", net.ErrClosed), false},
		{"failure", errors.New("synthetic-secret SMTP password body"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t)
			logSMTPCleanupError("client_close", tc.err)
			got := logs.all()
			if (len(got) != 0) != tc.warn {
				t.Fatalf("告警=%v, want %v", got, tc.warn)
			}
			if tc.warn && (len(got) != 1 || !strings.Contains(got[0], "stage=client_close") || !strings.Contains(got[0], "category=cleanup_failed")) {
				t.Fatalf("缺少固定阶段/类别: %v", got)
			}
			for _, line := range got {
				if strings.Contains(line, "synthetic-secret") || strings.Contains(line, "password") || strings.Contains(line, "body") {
					t.Fatalf("清理日志泄露原始错误: %s", line)
				}
			}
		})
	}
}

// TestI816SMTPQuitCleanup 正常 QUIT 不报重复关闭；失败 QUIT 保留原有安全发送错误。
func TestI816SMTPQuitCleanup(t *testing.T) {
	for _, stage := range []string{"success", "quit", "greeting", "auth"} {
		t.Run(stage, func(t *testing.T) {
			logs := captureLogs(t)
			host, port := p319SMTP(t, stage)
			err := SendTestEmail(EmailConfig{Security: "auto_starttls", Host: host, Port: port, User: p319User, Pass: p319Password, From: "f@example.com", To: "t@example.com"}, "subject", p319Body)
			if stage == "success" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil {
					t.Fatal("预期发送失败")
				}
				p319AssertSafe(t, err.Error())
				if stage == "quit" && !strings.Contains(err.Error(), "SMTP QUIT 失败") {
					t.Fatalf("QUIT 主错误被覆盖: %v", err)
				}
			}
			for _, line := range logs.all() {
				if strings.Contains(line, "SMTP 清理失败") {
					t.Fatalf("标准库已关闭连接产生重复告警: %s", line)
				}
			}
		})
	}
}
