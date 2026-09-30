// 本地 SDK → API → SQLite 回归，不代表真实云分页验收。
package api

import (
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

// TestScanECSCacheIntegrity 分别覆盖 P3-26 结构异常、P3-25 token 异常及正常缓存替换。
func TestScanECSCacheIntegrity(t *testing.T) {
	first := `{"SecurityGroups":{"SecurityGroup":[{"SecurityGroupId":"sg-new"}]},"NextToken":"T1"}`
	last := `{"SecurityGroups":{"SecurityGroup":[{"SecurityGroupId":"sg-last"}]}}`
	empty := `{"SecurityGroups":{"SecurityGroup":[]}}`
	cases := []struct {
		name    string
		pages   []string
		success bool
		count   int
	}{
		{"empty-first", []string{empty}, true, 0},
		{"normal-two", []string{first, last}, true, 2},
		{"empty-middle", []string{first, `{"SecurityGroups":{"SecurityGroup":[]},"NextToken":"T2"}`, last}, true, 2},
		{"empty-last", []string{first, empty}, true, 1},
		{"missing-first", []string{`{}`}, false, 0},
		{"missing-middle", []string{first, `{"NextToken":"T2"}`, last}, false, 0},
		{"null-wrapper", []string{first, `{"SecurityGroups":null,"NextToken":"T2"}`}, false, 0},
		{"missing-list", []string{first, `{"SecurityGroups":{},"NextToken":"T2"}`}, false, 0},
		{"null-list", []string{first, `{"SecurityGroups":{"SecurityGroup":null},"NextToken":"T2"}`}, false, 0},
		{"null-entry", []string{`{"SecurityGroups":{"SecurityGroup":[null]}}`}, false, 0},
		{"missing-id", []string{`{"SecurityGroups":{"SecurityGroup":[{}]}}`}, false, 0},
		{"empty-id", []string{`{"SecurityGroups":{"SecurityGroup":[{"SecurityGroupId":""}]}}`}, false, 0},
		{"repeat-token", []string{first, first}, false, 0},
		{"token-cycle", []string{first, `{"SecurityGroups":{"SecurityGroup":[]},"NextToken":"T2"}`, first}, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			old := []config.ScannedResource{{ResourceID: "sg-old", ResourceName: "old-name"}}
			if err := e.store.ReplaceScannedResources("ali_ecs", "cn-hangzhou", old); err != nil {
				t.Fatal(err)
			}
			if err := e.store.ReplaceScannedResources("ali_ecs", "cn-other", []config.ScannedResource{{ResourceID: "sg-other"}}); err != nil {
				t.Fatal(err)
			}
			before, err := e.store.GetScannedResources("ali_ecs")
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				i := int(calls.Add(1)) - 1
				w.Header().Set("Content-Type", "application/json")
				if i >= len(tc.pages) {
					w.WriteHeader(400)
					_, err := w.Write([]byte(`{"Code":"InvalidParameter","Message":"probe stop"}`))
					if err != nil {
						t.Error(err)
					}
					return
				}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				expected := ""
				if i == 1 {
					expected = "T1"
				}
				if i == 2 {
					expected = "T2"
				}
				if r.Form.Get("NextToken") != expected {
					t.Errorf("token=%q expected=%q", r.Form.Get("NextToken"), expected)
				}
				_, err := w.Write([]byte(tc.pages[i]))
				if err != nil {
					t.Error(err)
				}
			}))
			defer srv.Close()
			cli, err := ecs.NewClient(&openapi.Config{AccessKeyId: tea.String("fake"), AccessKeySecret: tea.String("fake"), Endpoint: tea.String(strings.TrimPrefix(srv.URL, "http://")), Protocol: tea.String("http")})
			if err != nil {
				t.Fatal(err)
			}
			creds := provider.Credentials{AliyunAccessKeyID: "fake", AliyunAccessKeySecret: "fake"}
			pool := provider.NewClientPool(creds)
			if _, err := pool.GetOrCreate(pool.CacheKey(config.CloudAliECS, "cn-hangzhou"), func() (any, error) { return cli, nil }); err != nil {
				t.Fatal(err)
			}
			e.runtime.Apply(&syncer.RuntimeState{Config: config.RuntimeConfig{Credentials: config.Credentials{AliyunAccessKeyID: "fake", AliyunAccessKeySecret: "fake"}}, Pool: pool})
			w := e.do(t, "POST", "/api/scan-resources", `{"cloud_type":"ali_ecs","region":"cn-hangzhou"}`)
			var got struct {
				Success bool
				Count   int
				Error   string
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Success != tc.success {
				t.Errorf("success=%v expected=%v body=%s", got.Success, tc.success, w.Body.String())
			}
			after, err := e.store.GetScannedResources("ali_ecs")
			if err != nil {
				t.Fatal(err)
			}
			if !tc.success {
				if got.Error == "" {
					t.Error("失败响应必须包含错误")
				}
				if !reflect.DeepEqual(before, after) {
					t.Errorf("cache changed before=%+v after=%+v", before, after)
				}
			} else {
				ids := make(map[string]bool)
				for _, resource := range after {
					ids[resource.ResourceID] = true
				}
				if ids["sg-old"] || !ids["sg-other"] || (tc.count >= 1 && !ids["sg-new"]) || (tc.count == 2 && !ids["sg-last"]) {
					t.Errorf("缓存资源内容不符: %+v", after)
				}
				if got.Count != tc.count || len(after) != tc.count+1 {
					t.Errorf("count=%d cache=%+v expected=%d", got.Count, after, tc.count)
				}
			}
			expectedCalls := len(tc.pages)
			if tc.name == "missing-middle" {
				expectedCalls = 2
			}
			if calls.Load() != int32(expectedCalls) {
				t.Errorf("calls=%d expected=%d", calls.Load(), expectedCalls)
			}
		})
	}
}
