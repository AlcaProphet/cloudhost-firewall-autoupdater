//go:build dnsintegration

package dns

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// 显式真实上游验收：不因网络错误跳过，不固定公共域名的动态地址。
func TestResolve_Upstream223(t *testing.T) {
	if testing.Short() {
		t.Fatal("dnsintegration 真实验收不能与 -short 同时使用，请去掉 -short")
	}
	t.Run("公共域名", func(t *testing.T) {
		r := NewResolver("223.5.5.5:53", 5*time.Second)
		got, err := r.Resolve(context.Background(), "alidns.com.")
		if err != nil {
			t.Fatalf("223.5.5.5 解析失败: %v", err)
		}
		t.Logf("真实上游=223.5.5.5:53 域名=alidns.com. 结果=%v", got)
		if len(got) == 0 {
			t.Fatal("公共域名应返回地址")
		}
		for _, ip := range got {
			if ip.IP == nil || ip.IP.IsUnspecified() || ip.IP.IsLoopback() {
				t.Fatalf("无效公共地址: %v", ip)
			}
		}
	})
	t.Run("不存在域名", func(t *testing.T) {
		r := NewResolver("223.5.5.5:53", 5*time.Second)
		got, err := r.Resolve(context.Background(), "i804-upstream-check.invalid.")
		var de *net.DNSError
		t.Logf("真实上游=223.5.5.5:53 不存在域名错误=%v", err)
		if len(got) != 0 || !errors.As(err, &de) || !de.IsNotFound {
			t.Fatalf("必须确认域名不存在，超时不算通过: %v %v", got, err)
		}
	})
}
