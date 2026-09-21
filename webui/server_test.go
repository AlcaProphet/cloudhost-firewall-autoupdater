package webui

import (
	"fmt"
	"net"
	"testing"
)

func TestFindAvailablePortUsesConfiguredHost(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("占用测试端口失败: %v", err)
	}
	defer l.Close()

	occupied := l.Addr().(*net.TCPAddr).Port
	actual := findAvailablePort("127.0.0.1", occupied)
	if actual == occupied {
		t.Fatalf("实际端口 = %d, 不应继续使用已占用端口", actual)
	}

	probe, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", actual)))
	if err != nil {
		t.Fatalf("验证随机端口失败: %v", err)
	}
	probe.Close()
}

func TestFindAvailablePortAcceptsAllInterfaces(t *testing.T) {
	l, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("分配测试端口失败: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	if actual := findAvailablePort("0.0.0.0", port); actual != port {
		t.Errorf("实际端口 = %d, want %d", actual, port)
	}
}
