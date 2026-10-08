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
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	swas "github.com/alibabacloud-go/swas-open-20200601/v3/client"
	"github.com/alibabacloud-go/tea/tea"
)

// TestScanSWASCacheIntegrity 通过真实 SWAS SDK、正式 handler 与临时 SQLite 守护扫描缓存。
func TestScanSWASCacheIntegrity(t *testing.T) {
	ct := config.CloudAliSWAS
	list, id := "Instances", "InstanceId"
	wrap := func(content string) string { return `{` + content + `}` }
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
		{"missing-response", []string{`{}`}},
		{"null-list", []string{wrap(`"` + list + `":null`)}},
		{"zero-missing-list", []string{wrap(`"TotalCount":0`)}}, {"zero-null-list", []string{wrap(`"TotalCount":0,"` + list + `":null`)}},
		{"positive-missing-list", []string{wrap(`"TotalCount":1`)}},
		{"null-entry", []string{wrap(`"` + list + `":[null]`)}},
		{"null-id", []string{wrap(`"Instances":[{"InstanceId":null}]`)}},
		{"null-name", []string{wrap(`"Instances":[{"InstanceId":"new","InstanceName":null}]`)}},
		{"raw-values", []string{wrap(`"Instances":[{"InstanceId":" raw-ID ","InstanceName":" 名称 "}]`)}},
		{"zero-empty", []string{wrap(`"TotalCount":0,"Instances":[]`)}},
		{"sdk-error", []string{`sdk-error`}},
		{"later-sdk-error", []string{full, `sdk-error`}},
		{"invalid-json", []string{`{`}},
		{"later-invalid-json", []string{full, `{`}},
		{"missing-id", []string{wrap(`"` + list + `":[{}]`)}},
		{"empty-id", []string{wrap(`"` + list + `":[{"` + id + `":""}]`)}},
		{"empty-list", []string{empty}}, {"normal", []string{one}},
		{"later-missing-response", []string{full, `{}`}},
		{"later-zero-null", []string{full, wrap(`"TotalCount":0,"` + list + `":null`)}},
		{"full-empty-last", []string{full, empty}},
		{"full-short-last", []string{full, one}},
		{"valid-then-null", []string{wrap(`"` + list + `":[{"` + id + `":"new"},null]`)}},
		{"later-null-entry", []string{full, wrap(`"` + list + `":[null]`)}},
		{"later-missing-id", []string{full, wrap(`"` + list + `":[{}]`)}},
		{"later-null-list", []string{full, wrap(`"` + list + `":null`)}},
	}
	for _, tc := range cases {
		t.Run(string(ct)+"/"+tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			if err := e.store.ReplaceScannedResources(string(ct), "cn-hangzhou", []config.ScannedResource{{ResourceID: "old", ResourceName: "old-name"}}); err != nil {
				t.Fatal(err)
			}
			if err := e.store.ReplaceScannedResources(string(ct), "cn-other", []config.ScannedResource{{ResourceID: "other"}}); err != nil {
				t.Fatal(err)
			}
			if err := e.store.ReplaceScannedResources("ali_ecs", "cn-hangzhou", []config.ScannedResource{{ResourceID: "sg-other"}}); err != nil {
				t.Fatal(err)
			}
			otherBefore, err := e.store.GetScannedResources("ali_ecs")
			if err != nil {
				t.Fatal(err)
			}
			before, err := e.store.GetScannedResources(string(ct))
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				i := int(calls.Add(1)) - 1
				if r.Header.Get("X-Acs-Action") != "ListInstances" || r.Form.Get("RegionId") != "cn-hangzhou" || r.Form.Get("PageNumber") != fmt.Sprint(i+1) || r.Form.Get("PageSize") != "100" {
					t.Errorf("request params=%v", r.Form)
				}
				w.Header().Set("Content-Type", "application/json")
				if i >= len(tc.pages) {
					t.Error("unexpected page")
					w.WriteHeader(400)
					return
				}
				payload := tc.pages[i]
				if payload == "sdk-error" {
					w.WriteHeader(400)
					payload = `{"Code":"InvalidParameter","Message":"fixture","RequestId":"probe"}`
				}
				if _, err := w.Write([]byte(payload)); err != nil {
					t.Error(err)
				}
			}))
			defer srv.Close()
			cli, err := swas.NewClient(&openapi.Config{AccessKeyId: tea.String("fake"), AccessKeySecret: tea.String("fake"), Endpoint: tea.String(strings.TrimPrefix(srv.URL, "http://")), Protocol: tea.String("http")})
			if err != nil {
				t.Fatal(err)
			}
			pool := provider.NewClientPool(provider.Credentials{AliyunAccessKeyID: "fake", AliyunAccessKeySecret: "fake"})
			if _, err := pool.GetOrCreate(pool.CacheKey(ct, "cn-hangzhou"), func() (any, error) { return cli, nil }); err != nil {
				t.Fatal(err)
			}
			e.runtime.Apply(&syncer.RuntimeState{Config: config.RuntimeConfig{Credentials: config.Credentials{AliyunAccessKeyID: "fake", AliyunAccessKeySecret: "fake"}}, Pool: pool})
			w := e.do(t, "POST", "/api/scan-resources", fmt.Sprintf(`{"cloud_type":%q,"region":"cn-hangzhou"}`, ct))
			after, err := e.store.GetScannedResources(string(ct))
			if err != nil {
				t.Fatal(err)
			}
			otherAfter, err := e.store.GetScannedResources("ali_ecs")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(otherBefore, otherAfter) {
				t.Error("other product cache changed")
			}
			var result struct {
				Success bool
				Count   int
				Error   string
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			success := tc.name == "empty-list" || tc.name == "zero-empty" || tc.name == "normal" || tc.name == "null-name" || tc.name == "raw-values" || tc.name == "full-empty-last" || tc.name == "full-short-last"
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
				if tc.name == "normal" || tc.name == "null-name" || tc.name == "raw-values" {
					wantCount = 1
				}
				if tc.name == "full-empty-last" || tc.name == "full-short-last" {
					wantCount = 100
					if tc.name == "full-short-last" {
						wantCount = 101
					}
				}
				if result.Count != wantCount || len(after) != wantCount+1 {
					t.Errorf("success result count=%d cache_size=%d", result.Count, len(after))
				}
				foundOther := false
				expectedIDs := make(map[string]bool)
				if wantCount == 1 {
					expectedIDs["new"] = true
					if tc.name == "raw-values" {
						delete(expectedIDs, "new")
						expectedIDs[" raw-ID "] = true
					}
				}
				if wantCount >= 100 {
					if wantCount == 101 {
						expectedIDs["new"] = true
					}
					for i := 0; i < 100; i++ {
						expectedIDs[fmt.Sprintf("new-%03d", i)] = true
					}
				}
				for _, r := range after {
					if r.ResourceID == "old" {
						t.Error("old region resource retained")
					}
					if r.ResourceID != "other" {
						expectedName := ""
						if tc.name == "raw-values" {
							expectedName = " 名称 "
						}
						if !expectedIDs[r.ResourceID] || r.Region != "cn-hangzhou" || r.CloudType != string(ct) || r.ResourceName != expectedName {
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
