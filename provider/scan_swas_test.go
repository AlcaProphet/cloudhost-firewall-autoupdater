package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	swas "github.com/alibabacloud-go/swas-open-20200601/v3/client"
	"github.com/alibabacloud-go/tea/tea"
)

// TestDecodeSWASScanPage 验证原生 nil 及页结构，不将异常解释为零资源。
func TestDecodeSWASScanPage(t *testing.T) {
	cases := []struct {
		name, payload string
		want          []ScannedCloudResource
	}{
		{"native-nil", "", nil}, {"missing-body", `{}`, nil}, {"null-body", `{"body":null}`, nil},
		{"missing-list", `{"body":{}}`, nil}, {"null-list", `{"body":{"Instances":null}}`, nil},
		{"zero-missing-list", `{"body":{"TotalCount":0}}`, nil}, {"zero-null-list", `{"body":{"TotalCount":0,"Instances":null}}`, nil},
		{"null-entry", `{"body":{"Instances":[null]}}`, nil},
		{"missing-id", `{"body":{"Instances":[{}]}}`, nil}, {"null-id", `{"body":{"Instances":[{"InstanceId":null}]}}`, nil},
		{"empty-id", `{"body":{"Instances":[{"InstanceId":""}]}}`, nil},
		{"valid-then-null", `{"body":{"Instances":[{"InstanceId":"first"},null]}}`, nil},
		{"valid-then-missing-id", `{"body":{"Instances":[{"InstanceId":"first"},{}]}}`, nil},
		{"empty", `{"body":{"Instances":[]}}`, []ScannedCloudResource{}},
		{"zero-empty", `{"body":{"TotalCount":0,"Instances":[]}}`, []ScannedCloudResource{}},
		{"missing-name", `{"body":{"Instances":[{"InstanceId":"new"}]}}`, []ScannedCloudResource{{ResourceID: "new", Region: "cn-hangzhou"}}},
		{"null-name", `{"body":{"Instances":[{"InstanceId":"new","InstanceName":null}]}}`, []ScannedCloudResource{{ResourceID: "new", Region: "cn-hangzhou"}}},
		{"raw-values", `{"body":{"Instances":[{"InstanceId":" raw-ID ","InstanceName":" 名称 "}]}}`, []ScannedCloudResource{{ResourceID: " raw-ID ", Name: " 名称 ", Region: "cn-hangzhou"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var resp *swas.ListInstancesResponse
			if tc.payload != "" {
				resp = &swas.ListInstancesResponse{}
				if err := json.Unmarshal([]byte(tc.payload), resp); err != nil {
					t.Fatal(err)
				}
			}
			got, err := decodeSWASScanPage(resp, "cn-hangzhou")
			if tc.want == nil {
				if !errors.Is(err, ErrSnapshotIncomplete) || got != nil {
					t.Fatalf("got=%+v err=%v", got, err)
				}
			} else if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got=%+v err=%v want=%+v", got, err, tc.want)
			}
		})
	}
}

// TestScanSWASErrorClassification 首页与后续页失败均丢弃累计资源，保留 SDK 错误分类。
func TestScanSWASErrorClassification(t *testing.T) {
	entries := make([]map[string]string, 100)
	for i := range entries {
		entries[i] = map[string]string{"InstanceId": fmt.Sprintf("new-%d", i)}
	}
	full, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, later := range []bool{false, true} {
		for _, kind := range []string{"sdk-error", "invalid-json", "missing", "null-list", "null-entry", "missing-id"} {
			t.Run(fmt.Sprintf("%s/later=%v", kind, later), func(t *testing.T) {
				m, host := newMockCloudAPI(t)
				m.reply = func(i int, req recordedRequest) (int, string) {
					if req.action() != "ListInstances" || req.query("RegionId") != "cn-hangzhou" || req.query("PageNumber") != fmt.Sprint(i+1) || req.query("PageSize") != "100" {
						t.Errorf("unexpected request: %+v", req)
					}
					if later && i == 0 {
						return 200, `{"Instances":` + string(full) + `}`
					}
					switch kind {
					case "sdk-error":
						return 400, `{"Code":"InvalidParameter","Message":"fixture","RequestId":"probe"}`
					case "invalid-json":
						return 200, `{`
					case "missing":
						return 200, `{}`
					case "null-list":
						return 200, `{"Instances":null}`
					case "null-entry":
						return 200, `{"Instances":[null]}`
					default:
						return 200, `{"Instances":[{}]}`
					}
				}
				pool := NewClientPool(Credentials{})
				cli := mockSWAS(t, host).client
				if _, err := pool.GetOrCreate(pool.CacheKey(config.CloudAliSWAS, "cn-hangzhou"), func() (any, error) { return cli, nil }); err != nil {
					t.Fatal(err)
				}
				got, err := ScanResources(config.CloudAliSWAS, "cn-hangzhou", pool)
				if err == nil || got != nil {
					t.Fatalf("got=%+v err=%v", got, err)
				}
				if kind == "sdk-error" {
					var sdkErr *tea.SDKError
					if !errors.As(err, &sdkErr) || tea.StringValue(sdkErr.Code) != "InvalidParameter" || errors.Is(err, ErrSnapshotIncomplete) {
						t.Fatalf("SDK error classification=%v", err)
					}
				} else if kind == "invalid-json" {
					if errors.Is(err, ErrSnapshotIncomplete) {
						t.Fatalf("SDK decode error reclassified: %v", err)
					}
				} else if !errors.Is(err, ErrSnapshotIncomplete) {
					t.Fatalf("not incomplete: %v", err)
				}
				wantCalls := 1
				if later {
					wantCalls = 2
				}
				if len(m.recorded()) != wantCalls {
					t.Fatalf("calls=%d want=%d", len(m.recorded()), wantCalls)
				}
			})
		}
	}
}
