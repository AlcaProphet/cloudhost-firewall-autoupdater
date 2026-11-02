package syncer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	swas "github.com/alibabacloud-go/swas-open-20200601/v3/client"
	"github.com/alibabacloud-go/tea/tea"
)

// 同一个正式 Provider 的 syncAll 与 DryRun 重叠读取真实 SDK；
// 同时验证覆盖、同步结果和零额外写入。HTTP/DNS 均为本地夹具。
func TestI9R01SWASSyncPreviewOverlap(t *testing.T) {
	var reads, writes atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if action := r.Header.Get("X-Acs-Action"); action != "ListFirewallRules" {
			writes.Add(1)
			t.Errorf("已覆盖规则不应触发写入: %q", action)
			w.WriteHeader(http.StatusBadRequest)
			if err := json.NewEncoder(w).Encode(map[string]any{"Code": "InvalidParam"}); err != nil {
				t.Error(err)
			}
			return
		}
		if reads.Add(1) <= 2 {
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
		body := map[string]any{"TotalCount": 1, "FirewallRules": []any{map[string]any{
			"RuleId": "desired", "RuleProtocol": "TCP", "Port": "443/443",
			"SourceCidrIp": "1.1.1.1/32", "Policy": "accept", "Remark": "[auto-dns]",
		}}}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(unblock)
	client, err := swas.NewClient(&openapi.Config{
		AccessKeyId: tea.String("fake"), AccessKeySecret: tea.String("fake"),
		Endpoint: tea.String(strings.TrimPrefix(srv.URL, "http://")), Protocol: tea.String("http"),
		ConnectTimeout: tea.Int(1000), ReadTimeout: tea.Int(1000),
	})
	if err != nil {
		t.Fatal(err)
	}
	pool := provider.NewClientPool(provider.Credentials{})
	if _, err := pool.GetOrCreate(pool.CacheKey(config.CloudAliSWAS, "cn-hangzhou"), func() (any, error) {
		return client, nil
	}); err != nil {
		t.Fatal(err)
	}
	p, err := provider.NewProvider(config.TargetConfig{
		CloudType: config.CloudAliSWAS, Region: "cn-hangzhou", ResourceID: "swas-mock",
	}, 1, pool)
	if err != nil {
		t.Fatal(err)
	}
	s := newTargetSyncer(t, []provider.Provider{p}, []config.DomainRule{
		staticRule(1, "a.example.com", "TCP", "443"),
	}, map[string]string{"a.example.com": "1.1.1.1/32"})
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		s.syncAll()
	}()
	var preview DryRunResponse
	var previewErr error
	go func() {
		defer workers.Done()
		<-start
		preview, previewErr = s.DryRun()
	}()
	t.Cleanup(func() { unblock(); workers.Wait() })
	close(start)
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for range 2 {
		select {
		case <-entered:
		case <-timer.C:
			t.Fatal("正式同步与 Dry Run 未同时到达 SDK 的 HTTP 端点")
		}
	}
	unblock()
	workers.Wait()
	summary := s.Status().LastRound
	if summary == nil || summary.Outcome != RoundSuccess || summary.Total != 1 || summary.Failed != 0 || summary.Added != 0 || summary.Deleted != 0 {
		t.Fatalf("同步结果不符: %+v", summary)
	}
	if previewErr != nil || len(preview.Results) != 1 || preview.Results[0].TargetID != 1 || preview.Results[0].Error != "" || !preview.Results[0].CoverageReady {
		t.Fatalf("预览结果不符: %+v, %v", preview, previewErr)
	}
	if reads.Load() != 3 || writes.Load() != 0 {
		t.Fatalf("应仅有正式同步 S0/S1 与预览 S0: reads=%d writes=%d", reads.Load(), writes.Load())
	}
}
