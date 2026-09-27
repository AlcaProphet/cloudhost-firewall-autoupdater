package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// ─── Issue6 A1：阿里云客户端必须带应用层超时 ───
//
// 判别性：修复前四处构造点的 ConnectTimeout/ReadTimeout 均为 nil，darabonba-openapi
// 经 tea/dara 得到 http.Client.Timeout=0、Transport.ResponseHeaderTimeout=0、
// net.Dialer.Timeout=0，即完全无界，下列用例会一直阻塞到测试超时；修复后必须
// 在缩短的 deadline + 裕量内返回可被 A12 识别的超时错误。

// aliBlockingServer 启动一个「接受连接但永不回包」的 TCP 服务，模拟连接成功后
// 不返回 / TLS 或响应头挂起 / 网络黑洞。返回 host:port 与清理函数。
//
// 已接受的连接不加跟踪：客户端侧超时后会自行关闭，测试进程退出时全部回收。
func aliBlockingServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动阻塞服务失败: %v", err)
	}

	go func() {
		for {
			if _, err := ln.Accept(); err != nil {
				return
			}
			// 不写任何字节：客户端会一直等待响应头，直到自身 deadline 生效
		}
	}()

	return ln.Addr().String(), func() { _ = ln.Close() }
}

// withAliTestEndpoint 把阿里云端点解析接缝指向本地阻塞服务，并缩短超时取值。
// 返回恢复函数；必须 defer 调用。
func withAliTestEndpoint(t *testing.T, addr string, connectMS, readMS int) func() {
	t.Helper()
	origResolve := aliResolveEndpoint
	origConnect := aliConnectTimeoutMS
	origRead := aliReadTimeoutMS

	// 本地阻塞服务是明文 TCP：必须同时覆盖协议，否则 SDK 会走 TLS 握手
	aliResolveEndpoint = func(_, _ string) (string, string) { return addr, "http" }
	aliConnectTimeoutMS = connectMS
	aliReadTimeoutMS = readMS

	return func() {
		aliResolveEndpoint = origResolve
		aliConnectTimeoutMS = origConnect
		aliReadTimeoutMS = origRead
	}
}

// newAliTestPool 构造带最小凭据的 ClientPool。
//
// 每个用例必须使用独立 pool：ClientPool 按 cloud_type+region+账户 缓存 client，
// 共用 pool 会让第二个构造点直接命中第一个（已解析到旧端点的）缓存条目。
func newAliTestPool() *ClientPool {
	return NewClientPool(Credentials{AliyunAccessKeyID: "test-ak", AliyunAccessKeySecret: "test-sk"})
}

// assertAliTimeout 断言 err 是可被 syncer.isRetryable 识别的超时/网络错误。
func assertAliTimeout(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("期望超时错误，实际为 nil")
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") {
		return
	}
	t.Fatalf("期望超时错误，实际为: %v", err)
}

// aliTimeoutCase 一条阿里云构造路径的调用方式。
type aliTimeoutCase struct {
	name string
	call func(pool *ClientPool) error
}

func aliTimeoutCases() []aliTimeoutCase {
	return []aliTimeoutCase{
		{
			// 路径 1：正式 SWAS Provider（GetRules → ListFirewallRules）
			name: "正式 SWAS Provider",
			call: func(pool *ClientPool) error {
				p, err := NewProvider(config.TargetConfig{
					CloudType:  config.CloudAliSWAS,
					Region:     "cn-hangzhou",
					ResourceID: "test-swas-instance",
				}, 0, pool)
				if err != nil {
					return err
				}
				_, err = p.GetRules()
				return err
			},
		},
		{
			// 路径 2：正式 ECS Provider（GetRules → DescribeSecurityGroupAttribute）
			name: "正式 ECS Provider",
			call: func(pool *ClientPool) error {
				p, err := NewProvider(config.TargetConfig{
					CloudType:  config.CloudAliECS,
					Region:     "cn-hangzhou",
					ResourceID: "sg-test",
				}, 0, pool)
				if err != nil {
					return err
				}
				_, err = p.GetRules()
				return err
			},
		},
		{
			// 路径 3：资源扫描 scanAliSWAS（ListInstances）
			name: "扫描路径 scanAliSWAS",
			call: func(pool *ClientPool) error {
				_, err := scanAliSWAS("cn-hangzhou", pool)
				return err
			},
		},
		{
			// 路径 4：资源扫描 scanAliECS（DescribeSecurityGroups）
			name: "扫描路径 scanAliECS",
			call: func(pool *ClientPool) error {
				_, err := scanAliECS("cn-hangzhou", pool)
				return err
			},
		},
	}
}

// aliFastConnectMS / aliFastReadMS 是用例使用的缩短取值。
//
// 生产默认值（10_000 / 30_000）由 TestAliClientDefaultTimeoutValues 断言，机制则
// 由本用例在缩短取值下断言：真实等满 10s+30s 会让单次全仓门禁多花 2 分钟，而
// 「超时是否真的来自应用层 deadline」这一机制与取值大小无关。
const (
	aliFastConnectMS = 200
	aliFastReadMS    = 300
)

// TestAliClientRequestIsBounded 四条阿里云构造路径都必须在应用层 deadline 内返回。
//
// 红灯证据：修复前 ConnectTimeout/ReadTimeout 为 nil ⇒ 三个超时全 0 ⇒ 无界，
// 本用例会一直阻塞（由测试级 timeout 报出），永远等不到任何错误。
func TestAliClientRequestIsBounded(t *testing.T) {
	addr, closeServer := aliBlockingServer(t)
	defer closeServer()

	connectMS, readMS := aliFastConnectMS, aliFastReadMS
	// 单次请求整体上限 = Connect + Read；裕量覆盖连接建立、TLS/解析与调度开销
	budget := time.Duration(connectMS+readMS)*time.Millisecond + 5*time.Second

	for _, tc := range aliTimeoutCases() {
		t.Run(tc.name, func(t *testing.T) {
			restore := withAliTestEndpoint(t, addr, connectMS, readMS)
			defer restore()

			pool := newAliTestPool()

			done := make(chan error, 1)
			go func() { done <- tc.call(pool) }()

			start := time.Now()
			select {
			case err := <-done:
				elapsed := time.Since(start)
				// 上界：必须真的在 deadline 附近返回（无界时永远等不到结果）。
				if elapsed > budget {
					t.Fatalf("请求耗时 %v 超过预算 %v", elapsed, budget)
				}
				// 下界：必须确实等过一段 deadline，而不是立刻因别的原因失败。
				// 只取 Read 上限的一半作为门槛，避免 -race/负载下的毫秒级抖动
				// 把「刚好在 Read 触发」误判为「提前失败」；真正的判别力来自
				// 上界（无界时会一直阻塞）与 assertAliTimeout（错误必须是超时类）。
				if elapsed < time.Duration(readMS)*time.Millisecond/2 {
					t.Fatalf("请求在 %v 内就返回了，远早于 Read 上限 %dms：不是应用层超时",
						elapsed, readMS)
				}
				assertAliTimeout(t, err)
			case <-time.After(budget):
				t.Fatalf("请求未在 %v 内返回：应用层超时未生效（修复前为完全无界）", budget)
			}
		})
	}
}

// TestAliClientDefaultTimeoutValues 默认超时取值必须恒为 10_000 / 30_000（毫秒）。
//
// 覆盖 Issue6 A1「四处构造点默认超时值恒为 10_000/30_000」的不变量：常量与两个
// 接缝变量的默认值必须一致，避免实施时把接缝默认值改小而使生产超时漂移。
func TestAliClientDefaultTimeoutValues(t *testing.T) {
	if aliDefaultConnectTimeoutMS != 10_000 {
		t.Errorf("aliDefaultConnectTimeoutMS = %d, want 10000", aliDefaultConnectTimeoutMS)
	}
	if aliDefaultReadTimeoutMS != 30_000 {
		t.Errorf("aliDefaultReadTimeoutMS = %d, want 30000", aliDefaultReadTimeoutMS)
	}
	if aliConnectTimeoutMS != aliDefaultConnectTimeoutMS {
		t.Errorf("aliConnectTimeoutMS 默认值 = %d, want %d（接缝不得改变生产默认值）",
			aliConnectTimeoutMS, aliDefaultConnectTimeoutMS)
	}
	if aliReadTimeoutMS != aliDefaultReadTimeoutMS {
		t.Errorf("aliReadTimeoutMS 默认值 = %d, want %d（接缝不得改变生产默认值）",
			aliReadTimeoutMS, aliDefaultReadTimeoutMS)
	}
}

// TestAliClientConfigCarriesTimeouts 唯一样本必须把两个超时真正写进 openapi.Config，
// 且 four 处构造点共用同一个样本（取值不可能漂移）。
func TestAliClientConfigCarriesTimeouts(t *testing.T) {
	creds := Credentials{AliyunAccessKeyID: "ak", AliyunAccessKeySecret: "sk"}

	for _, svc := range []struct {
		service string
		region  string
	}{
		{"swas", "cn-hangzhou"},
		{"ecs", "cn-hangzhou"},
	} {
		cfg := newAliOpenAPIConfig(svc.service, svc.region, creds)

		if cfg.ConnectTimeout == nil || *cfg.ConnectTimeout != aliDefaultConnectTimeoutMS {
			t.Errorf("%s: ConnectTimeout = %v, want %d", svc.service, cfg.ConnectTimeout, aliDefaultConnectTimeoutMS)
		}
		if cfg.ReadTimeout == nil || *cfg.ReadTimeout != aliDefaultReadTimeoutMS {
			t.Errorf("%s: ReadTimeout = %v, want %d", svc.service, cfg.ReadTimeout, aliDefaultReadTimeoutMS)
		}
		wantEndpoint := fmt.Sprintf("%s.%s.aliyuncs.com", svc.service, svc.region)
		if cfg.Endpoint == nil || *cfg.Endpoint != wantEndpoint {
			t.Errorf("%s: Endpoint = %v, want %s", svc.service, cfg.Endpoint, wantEndpoint)
		}
		if cfg.Protocol == nil || *cfg.Protocol != "https" {
			t.Errorf("%s: Protocol = %v, want https", svc.service, cfg.Protocol)
		}
		if cfg.AccessKeyId == nil || *cfg.AccessKeyId != creds.AliyunAccessKeyID {
			t.Errorf("%s: AccessKeyId 未正确传递", svc.service)
		}
		if cfg.AccessKeySecret == nil || *cfg.AccessKeySecret != creds.AliyunAccessKeySecret {
			t.Errorf("%s: AccessKeySecret 未正确传递", svc.service)
		}
	}
}
