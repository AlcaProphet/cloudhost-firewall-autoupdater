package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	swas "github.com/alibabacloud-go/swas-open-20200601/v3/client"
	"github.com/alibabacloud-go/tea/tea"
)

// 两个正式 Provider 必须命中同一池条目，覆盖连接测试另建 Provider 的情况。
func swasConcurrentProviders(t *testing.T, srv *httptest.Server) (*ClientPool, *AliSWAS, *AliSWAS) {
	t.Helper()
	client, err := swas.NewClient(&openapi.Config{
		AccessKeyId: tea.String("fake"), AccessKeySecret: tea.String("fake"),
		Endpoint: tea.String(strings.TrimPrefix(srv.URL, "http://")), Protocol: tea.String("http"),
		ConnectTimeout: tea.Int(1000), ReadTimeout: tea.Int(1000),
	})
	if err != nil {
		t.Fatal(err)
	}
	pool := NewClientPool(Credentials{AliyunAccessKeyID: "fake", AliyunAccessKeySecret: "fake"})
	if _, err := pool.GetOrCreate(pool.CacheKey(config.CloudAliSWAS, "cn-hangzhou"), func() (any, error) {
		return client, nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.TargetConfig{CloudType: config.CloudAliSWAS, Region: "cn-hangzhou", ResourceID: "swas-mock"}
	a, err := newAliSWAS(cfg, 1, pool)
	if err != nil {
		t.Fatal(err)
	}
	b, err := newAliSWAS(cfg, 2, pool)
	if err != nil {
		t.Fatal(err)
	}
	pa, pb := a.(*AliSWAS), b.(*AliSWAS)
	if pa.client != pb.client {
		t.Fatal("夹具必须复用池中同一个基础 client")
	}
	return pool, pa, pb
}

// 真实 SDK → 本地 HTTP。每个快照跨两页，并要求两条操作同时在途；
// 既检查竞争，也防止把全局串行化误当成读取隔离。不访问真实云。
func TestI9R01SWASConcurrentSnapshot(t *testing.T) {
	for _, kind := range []string{"same_provider_reads", "pooled_provider_reads", "read_create", "read_delete", "read_scan"} {
		t.Run(kind, func(t *testing.T) {
			entered := make(chan struct{}, 2)
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					return
				}
				action := r.Header.Get("X-Acs-Action")
				if action != "ListFirewallRules" || r.Form.Get("PageNumber") == "1" {
					select {
					case entered <- struct{}{}:
					case <-r.Context().Done():
						return
					}
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				body := map[string]any{}
				switch action {
				case "ListFirewallRules":
					page := r.Form.Get("PageNumber")
					if page != "1" && page != "2" || r.Form.Get("PageSize") != "100" {
						t.Errorf("分页参数不符: %v", r.Form)
					}
					body["TotalCount"] = 2
					body["FirewallRules"] = []any{map[string]any{
						"RuleId": "r" + page, "RuleProtocol": "TCP", "Port": "443/443",
						"SourceCidrIp": "1.2.3.4/32", "Policy": "accept", "Remark": "[auto-dns]",
					}}
				case "CreateFirewallRules":
					body["FirewallRuleIds"] = []string{"created"}
				case "DeleteFirewallRules":
					body["RequestId"] = "deleted"
				case "ListInstances":
					body["TotalCount"], body["PageNumber"], body["PageSize"] = 1, 1, 100
					body["Instances"] = []any{map[string]any{"InstanceId": "swas-mock"}}
				default:
					t.Errorf("意外的 API: %q", action)
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(body); err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(srv.Close)
			t.Cleanup(unblock)
			pool, a, b := swasConcurrentProviders(t, srv)
			read := func(p *AliSWAS) error {
				snapshot, err := p.GetSnapshot()
				if err == nil && (len(snapshot.Rules) != 2 || snapshot.Rules[0].RuleID != "r1" || snapshot.Rules[1].RuleID != "r2") {
					return fmt.Errorf("未返回完整两页规则: %+v", snapshot)
				}
				return err
			}
			other := func() error { return read(b) }
			switch kind {
			case "same_provider_reads":
				other = func() error { return read(a) }
			case "read_create":
				other = func() error {
					result, err := b.CreateRules(RuleSnapshot{}, []config.RuleAction{{
						Protocol: "TCP", Port: "443/443", CidrBlock: "1.2.3.4/32", Action: "ACCEPT", Description: "[auto-dns]",
					}})
					if err == nil && result.Written != 1 {
						return fmt.Errorf("新增结果不符: %+v", result)
					}
					return err
				}
			case "read_delete":
				other = func() error {
					result, err := b.DeleteRules(RuleSnapshot{}, []config.RuleInfo{{RuleID: "stale"}})
					if err == nil && (result.Deleted != 1 || result.Resolved != 1) {
						return fmt.Errorf("删除结果不符: %+v", result)
					}
					return err
				}
			case "read_scan":
				other = func() error {
					resources, err := ScanResources(config.CloudAliSWAS, "cn-hangzhou", pool)
					if err == nil && (len(resources) != 1 || resources[0].ResourceID != "swas-mock") {
						return fmt.Errorf("扫描结果不符: %+v", resources)
					}
					return err
				}
			}
			start := make(chan struct{})
			done := make(chan error, 2)
			var workers sync.WaitGroup
			workers.Add(2)
			for _, call := range []func() error{func() error { return read(a) }, other} {
				go func() {
					defer workers.Done()
					<-start
					done <- call()
				}()
			}
			t.Cleanup(func() { unblock(); workers.Wait() })
			close(start)
			timer := time.NewTimer(3 * time.Second)
			defer timer.Stop()
			for range 2 {
				select {
				case <-entered:
				case <-timer.C:
					t.Fatal("两条操作未同时到达 HTTP 端点")
				}
			}
			unblock()
			for range 2 {
				if err := <-done; err != nil {
					t.Error(err)
				}
			}
			want := int32(3)
			if strings.HasSuffix(kind, "reads") {
				want = 4
			}
			if got := requests.Load(); got != want {
				t.Errorf("请求数=%d, want %d", got, want)
			}
		})
	}
}

// 同时保护正常结果、第二页失败的空快照/错误链，以及各出口的连接收尾。
func TestI9R01SWASSnapshotResultAndCleanup(t *testing.T) {
	for _, kind := range []string{"success", "sdk_error", "count_change"} {
		t.Run(kind, func(t *testing.T) {
			closed := make(chan struct{}, 4)
			var mu sync.Mutex
			addresses := []string{}
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				addresses = append(addresses, r.RemoteAddr)
				mu.Unlock()
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					return
				}
				page := r.Form.Get("PageNumber")
				w.Header().Set("Content-Type", "application/json")
				body := swasPage([]string{swasRuleJSON("r"+page, "1.2.3.4/32")}, 2, 1)
				if page == "2" {
					body = swasPage([]string{swasRuleJSON("r2", "1.2.3.4/32")}, 2, 2)
					switch kind {
					case "sdk_error":
						w.WriteHeader(http.StatusForbidden)
						body = `{"Code":"Forbidden.RAM","Message":"local denied","RequestId":"denied"}`
					case "count_change":
						body = swasPage(nil, 3, 2)
					}
				}
				if _, err := w.Write([]byte(body)); err != nil {
					t.Error(err)
				}
			}))
			srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateClosed {
					closed <- struct{}{}
				}
			}
			srv.Start()
			t.Cleanup(srv.Close)
			_, p, _ := swasConcurrentProviders(t, srv)
			snapshot, err := p.GetSnapshot()
			if kind == "success" {
				if err != nil || len(snapshot.Rules) != 2 || snapshot.Rules[0].RuleID != "r1" || snapshot.Rules[1].RuleID != "r2" {
					t.Fatalf("完整读取失败: %+v, %v", snapshot, err)
				}
			} else {
				if err == nil || len(snapshot.Rules) != 0 || snapshot.Revision != "" {
					t.Fatalf("失败时返回半截快照或丢失错误: %+v, %v", snapshot, err)
				}
				if kind == "sdk_error" {
					var sdk *tea.SDKError
					if !errors.As(err, &sdk) || tea.StringValue(sdk.Code) != "Forbidden.RAM" {
						t.Fatalf("SDK 错误链丢失: %T, %v", err, err)
					}
				} else if !errors.Is(err, ErrSnapshotIncomplete) {
					t.Fatalf("快照不完整标记丢失: %v", err)
				}
			}
			mu.Lock()
			got := append([]string(nil), addresses...)
			mu.Unlock()
			if len(got) != 2 || got[0] != got[1] {
				t.Fatalf("同一快照应在同一连接读取两页: %v", got)
			}
			timer := time.NewTimer(3 * time.Second)
			defer timer.Stop()
			select {
			case <-closed:
			case <-timer.C:
				t.Fatal("快照结束后未关闭操作的空闲连接")
			}
		})
	}
}
