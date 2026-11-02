// 本地真实ECS SDK、正式handler与临时SQLite回归，不代表真实云或浏览器断连验收。
package api

import (
	"context"
	"encoding/json"
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
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	"github.com/alibabacloud-go/tea/tea"
)

func TestI8R14ScanHandlerPersistenceContext(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		canceled, empty, invalid bool
	}{
		{name: "normal"}, {name: "normal_empty", empty: true},
		{name: "canceled_nonempty", canceled: true}, {name: "canceled_empty", canceled: true, empty: true},
		{name: "invalid_cloud_shape", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			for _, region := range []string{"cn-hangzhou", "cn-other"} {
				if err := e.store.ReplaceScannedResources("ali_ecs", region, []config.ScannedResource{{ResourceID: "old-" + region}}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := e.store.GetScannedResources("ali_ecs")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if tc.canceled {
					cancel()
				}
				w.Header().Set("Content-Type", "application/json")
				body := `{"SecurityGroups":{"SecurityGroup":[{"SecurityGroupId":"sg-new"}]}}`
				if tc.empty {
					body = `{"SecurityGroups":{"SecurityGroup":[]}}`
				}
				if tc.invalid {
					body = `{"SecurityGroups":{}}`
				}
				if _, err := w.Write([]byte(body)); err != nil {
					t.Error(err)
				}
			}))
			defer srv.Close()
			cli, err := ecs.NewClient(&openapi.Config{AccessKeyId: tea.String("fixture-id"), AccessKeySecret: tea.String("fixture-secret"), Endpoint: tea.String(strings.TrimPrefix(srv.URL, "http://")), Protocol: tea.String("http")})
			if err != nil {
				t.Fatal(err)
			}
			pool := provider.NewClientPool(provider.Credentials{AliyunAccessKeyID: "fixture-id", AliyunAccessKeySecret: "fixture-secret"})
			if _, err = pool.GetOrCreate(pool.CacheKey(config.CloudAliECS, "cn-hangzhou"), func() (any, error) { return cli, nil }); err != nil {
				t.Fatal(err)
			}
			e.runtime.Apply(&syncer.RuntimeState{Config: config.RuntimeConfig{Credentials: config.Credentials{AliyunAccessKeyID: "fixture-id", AliyunAccessKeySecret: "fixture-secret"}}, Pool: pool})
			req := httptest.NewRequest("POST", "/api/scan-resources", strings.NewReader(`{"cloud_type":"ali_ecs","region":"cn-hangzhou"}`)).WithContext(ctx)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux := http.NewServeMux()
			e.deps.Register(mux)
			mux.ServeHTTP(w, req)
			if calls.Load() != 1 {
				t.Errorf("SDK请求次数=%d", calls.Load())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Error("响应缺no-store")
			}
			body := map[string]json.RawMessage{}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			after, err := e.store.GetScannedResources("ali_ecs")
			if err != nil {
				t.Fatal(err)
			}
			if tc.canceled {
				if w.Code != 500 {
					t.Errorf("持久化取消应沿既有安全500: %d %s", w.Code, w.Body.String())
				}
				if len(body) != 1 || body["error"] == nil {
					t.Errorf("取消响应不应带成功或资源字段: %s", w.Body.String())
				}
				for _, bad := range []string{"fixture-secret", "context canceled", "sg-new"} {
					if strings.Contains(w.Body.String(), bad) {
						t.Errorf("安全响应泄露%q", bad)
					}
				}
				if !reflect.DeepEqual(before, after) {
					t.Errorf("请求取消仍替换缓存: before=%+v after=%+v", before, after)
				}
			} else if tc.invalid {
				if w.Code != 200 || string(body["success"]) != "false" || body["resources"] != nil || body["count"] != nil {
					t.Errorf("云结构失败合同改变: %d %s", w.Code, w.Body.String())
				}
				if !reflect.DeepEqual(before, after) {
					t.Error("云结构失败改变缓存")
				}
			} else {
				if w.Code != 200 || string(body["success"]) != "true" {
					t.Fatalf("正常扫描失败: %d %s", w.Code, w.Body.String())
				}
				var count int
				if err := json.Unmarshal(body["count"], &count); err != nil {
					t.Fatal(err)
				}
				want := 1
				if tc.empty {
					want = 0
				}
				if count != want {
					t.Errorf("count=%d want=%d", count, want)
				}
				resources := []config.ScannedResource{}
				if string(body["resources"]) == "null" {
					t.Error("成功resources不得为null")
				}
				if err := json.Unmarshal(body["resources"], &resources); err != nil {
					t.Fatal(err)
				}
				if len(resources) != want {
					t.Error("成功资源数量不符")
				}
				if len(after) != want+1 {
					t.Errorf("对应地域替换不符: %+v", after)
				}
				for _, old := range before {
					if old.Region == "cn-other" {
						found := false
						for _, now := range after {
							if old == now {
								found = true
							}
						}
						if !found {
							t.Error("覆盖了其他地域")
						}
					}
				}
			}
			if e.applyCount() != 0 {
				t.Error("扫描不应发布配置")
			}
		})
	}
}
