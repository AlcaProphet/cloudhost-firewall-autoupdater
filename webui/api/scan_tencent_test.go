package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	lighthouse "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/lighthouse/v20200324"
	vpc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
)

// TestScanTencentCacheIntegrity 通过真实腾讯 SDK、正式 handler 与临时 SQLite 守护扫描缓存。
func TestScanTencentCacheIntegrity(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudTCLighthouse, config.CloudTCCVM} {
		list, id := "InstanceSet", "InstanceId"
		if ct == config.CloudTCCVM {
			list, id = "SecurityGroupSet", "SecurityGroupId"
		}
		wrap := func(content string) string { return `{"Response":{` + content + `}}` }
		empty := wrap(`"` + list + `":[]`)
		one := wrap(`"` + list + `":[{"` + id + `":"new"}]`)
		entries := make([]map[string]string, 100)
		for i := range entries {
			entries[i] = map[string]string{id: fmt.Sprintf("new-%03d", i)}
		}
		fullJSON, err := json.Marshal(entries)
		if err != nil {
			t.Fatal(err)
		}
		full := wrap(`"` + list + `":` + string(fullJSON) + `,"TotalCount":100`)
		cases := []struct {
			name  string
			pages []string
		}{
			{"missing-response", []string{`{}`}}, {"null-response", []string{`{"Response":null}`}},
			{"missing-list", []string{wrap(``)}}, {"null-list", []string{wrap(`"` + list + `":null`)}},
			{"zero-missing-list", []string{wrap(`"TotalCount":0`)}}, {"zero-null-list", []string{wrap(`"TotalCount":0,"` + list + `":null`)}},
			{"positive-missing-list", []string{wrap(`"TotalCount":1`)}},
			{"null-entry", []string{wrap(`"` + list + `":[null]`)}},
			{"missing-id", []string{wrap(`"` + list + `":[{}]`)}},
			{"empty-id", []string{wrap(`"` + list + `":[{"` + id + `":""}]`)}},
			{"empty-list", []string{empty}}, {"normal", []string{one}},
			{"later-missing-response", []string{full, `{}`}}, {"later-missing-list", []string{full, wrap(``)}},
			{"later-zero-null", []string{full, wrap(`"TotalCount":0,"` + list + `":null`)}},
			{"full-empty-last", []string{full, empty}},
			{"valid-then-null", []string{wrap(`"` + list + `":[{"` + id + `":"new"},null]`)}},
			{"later-null-entry", []string{full, wrap(`"` + list + `":[null]`)}},
			{"later-missing-id", []string{full, wrap(`"` + list + `":[{}]`)}},
			{"later-null-list", []string{full, wrap(`"` + list + `":null`)}},
		}
		for _, tc := range cases {
			t.Run(string(ct)+"/"+tc.name, func(t *testing.T) {
				e := newTestEnv(t)
				if err := e.store.ReplaceScannedResources(string(ct), "ap-guangzhou", []config.ScannedResource{{ResourceID: "old", ResourceName: "old-name"}}); err != nil {
					t.Fatal(err)
				}
				if err := e.store.ReplaceScannedResources(string(ct), "ap-other", []config.ScannedResource{{ResourceID: "other"}}); err != nil {
					t.Fatal(err)
				}
				before, err := e.store.GetScannedResources(string(ct))
				if err != nil {
					t.Fatal(err)
				}
				var calls atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var q struct{ Offset json.RawMessage }
					if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
						t.Error(err)
					}
					i := int(calls.Add(1)) - 1
					want := fmt.Sprint(i * 100)
					got := strings.Trim(string(q.Offset), `"`)
					if got != want {
						t.Errorf("offset=%s want=%s", got, want)
					}
					w.Header().Set("Content-Type", "application/json")
					if i >= len(tc.pages) {
						t.Error("unexpected page")
						w.WriteHeader(400)
						return
					}
					if _, err := w.Write([]byte(tc.pages[i])); err != nil {
						t.Error(err)
					}
				}))
				defer srv.Close()
				cpf := profile.NewClientProfile()
				cpf.HttpProfile.Endpoint = strings.TrimPrefix(srv.URL, "http://")
				cpf.HttpProfile.Scheme = "http"
				var cli any
				if ct == config.CloudTCLighthouse {
					cli, err = lighthouse.NewClient(common.NewCredential("fake", "fake"), "ap-guangzhou", cpf)
				} else {
					cli, err = vpc.NewClient(common.NewCredential("fake", "fake"), "ap-guangzhou", cpf)
				}
				if err != nil {
					t.Fatal(err)
				}
				pool := provider.NewClientPool(provider.Credentials{TencentSecretID: "fake", TencentSecretKey: "fake"})
				if _, err := pool.GetOrCreate(pool.CacheKey(ct, "ap-guangzhou"), func() (any, error) { return cli, nil }); err != nil {
					t.Fatal(err)
				}
				e.runtime.Apply(&syncer.RuntimeState{Config: config.RuntimeConfig{Credentials: config.Credentials{TencentSecretID: "fake", TencentSecretKey: "fake"}}, Pool: pool})
				w := e.do(t, "POST", "/api/scan-resources", fmt.Sprintf(`{"cloud_type":%q,"region":"ap-guangzhou"}`, ct))
				after, err := e.store.GetScannedResources(string(ct))
				if err != nil {
					t.Fatal(err)
				}
				var result struct {
					Success bool
					Count   int
					Error   string
				}
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				success := tc.name == "empty-list" || tc.name == "normal" || tc.name == "full-empty-last"
				if result.Success != success {
					t.Errorf("success=%v want=%v body=%s", result.Success, success, w.Body.String())
				}
				if !success && !reflect.DeepEqual(before, after) {
					t.Errorf("异常扫描改变旧缓存: before=%+v after=%+v", before, after)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil {
					t.Fatal(err)
				}
				if success {
					var items []config.ScannedResource
					raw, ok := fields["resources"]
					if !ok || string(raw) == "null" {
						t.Fatalf("resources 缺失或为 null: %s", w.Body.String())
					}
					if err := json.Unmarshal(raw, &items); err != nil {
						t.Fatal(err)
					}
					if items == nil || len(items) != result.Count {
						t.Errorf("resources 数组与 count 不符: %s", w.Body.String())
					}
				} else {
					if result.Error == "" {
						t.Error("失败响应缺少错误")
					}
					if _, ok := fields["resources"]; ok {
						t.Error("失败响应包含部分资源")
					}
					if _, ok := fields["count"]; ok {
						t.Error("失败响应包含成功计数")
					}
				}
				if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
					t.Errorf("HTTP contract: code=%d headers=%v", w.Code, w.Header())
				}
				if success {
					wantCount := 0
					if tc.name == "normal" {
						wantCount = 1
					}
					if tc.name == "full-empty-last" {
						wantCount = 100
					}
					if result.Count != wantCount || len(after) != wantCount+1 {
						t.Errorf("success result count=%d cache_size=%d", result.Count, len(after))
					}
					foundOther := false
					expectedIDs := make(map[string]bool)
					if wantCount == 1 {
						expectedIDs["new"] = true
					}
					if wantCount == 100 {
						for i := 0; i < 100; i++ {
							expectedIDs[fmt.Sprintf("new-%03d", i)] = true
						}
					}
					for _, r := range after {
						if r.ResourceID == "old" {
							t.Error("old region resource retained")
						}
						if r.ResourceID != "other" {
							if !expectedIDs[r.ResourceID] || r.Region != "ap-guangzhou" || r.CloudType != string(ct) || r.ResourceName != "" {
								t.Errorf("缓存资源内容异常: %+v", r)
							}
							delete(expectedIDs, r.ResourceID)
						}
						if r.ResourceID == "other" {
							foundOther = true
							if !reflect.DeepEqual(r, before[0]) && !reflect.DeepEqual(r, before[1]) {
								t.Errorf("其他地域缓存改变: %+v", r)
							}
						}
					}
					if len(expectedIDs) != 0 {
						t.Errorf("缓存缺少期望资源: %+v", expectedIDs)
					}
					if !foundOther {
						t.Error("other region changed")
					}
				}
				if int(calls.Load()) != len(tc.pages) {
					t.Errorf("calls=%d want=%d", calls.Load(), len(tc.pages))
				}
			})
		}
	}
}
