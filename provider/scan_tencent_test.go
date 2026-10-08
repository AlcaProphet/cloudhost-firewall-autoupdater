package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	tcerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	lighthouse "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/lighthouse/v20200324"
	vpc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
)

// TestDecodeTencentScanPage 验证原生 nil 与 SDK 响应结构，不把异常解释为零资源。
func TestDecodeTencentScanPage(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudTCLighthouse, config.CloudTCCVM} {
		list, id, name := "InstanceSet", "InstanceId", "InstanceName"
		if ct == config.CloudTCCVM {
			list, id, name = "SecurityGroupSet", "SecurityGroupId", "SecurityGroupName"
		}
		wrap := func(x string) string { return `{"Response":{` + x + `}}` }
		decode := func(payload string) ([]ScannedCloudResource, error) {
			if ct == config.CloudTCLighthouse {
				var resp *lighthouse.DescribeInstancesResponse
				if payload != "native-nil" {
					resp = &lighthouse.DescribeInstancesResponse{}
					if err := json.Unmarshal([]byte(payload), resp); err != nil {
						t.Fatal(err)
					}
				}
				return decodeLighthouseScanPage(resp, "ap-guangzhou")
			}
			var resp *vpc.DescribeSecurityGroupsResponse
			if payload != "native-nil" {
				resp = &vpc.DescribeSecurityGroupsResponse{}
				if err := json.Unmarshal([]byte(payload), resp); err != nil {
					t.Fatal(err)
				}
			}
			return decodeCVMScanPage(resp, "ap-guangzhou")
		}
		cases := []struct {
			name, payload string
			want          []ScannedCloudResource
		}{
			{"native-nil", "native-nil", nil}, {"missing-response", `{}`, nil}, {"null-response", `{"Response":null}`, nil},
			{"missing-list", wrap(``), nil}, {"null-list", wrap(`"` + list + `":null`), nil},
			{"zero-missing-list", wrap(`"TotalCount":0`), nil}, {"zero-null-list", wrap(`"TotalCount":0,"` + list + `":null`), nil},
			{"null-entry", wrap(`"` + list + `":[null]`), nil},
			{"missing-id", wrap(`"` + list + `":[{}]`), nil}, {"null-id", wrap(`"` + list + `":[{"` + id + `":null}]`), nil}, {"empty-id", wrap(`"` + list + `":[{"` + id + `":""}]`), nil},
			{"valid-then-null", wrap(`"` + list + `":[{"` + id + `":"first"},null]`), nil},
			{"empty", wrap(`"` + list + `":[]`), []ScannedCloudResource{}},
			{"missing-name", wrap(`"` + list + `":[{"` + id + `":"new"}]`), []ScannedCloudResource{{ResourceID: "new", Region: "ap-guangzhou"}}},
			{"null-name", wrap(`"` + list + `":[{"` + id + `":"new","` + name + `":null}]`), []ScannedCloudResource{{ResourceID: "new", Region: "ap-guangzhou"}}},
			{"normal", wrap(`"` + list + `":[{"` + id + `":"new","` + name + `":"名称"}]`), []ScannedCloudResource{{ResourceID: "new", Name: "名称", Region: "ap-guangzhou"}}},
		}
		for _, tc := range cases {
			t.Run(string(ct)+"/"+tc.name, func(t *testing.T) {
				got, err := decode(tc.payload)
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
}

// TestScanTencentErrorClassification 首页和后续页失败均丢弃累计资源，保留 SDK 错误分类。
func TestScanTencentErrorClassification(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudTCLighthouse, config.CloudTCCVM} {
		list, id := "InstanceSet", "InstanceId"
		if ct == config.CloudTCCVM {
			list, id = "SecurityGroupSet", "SecurityGroupId"
		}
		full := make([]map[string]string, 100)
		for i := range full {
			full[i] = map[string]string{id: fmt.Sprintf("id-%d", i)}
		}
		b, err := json.Marshal(full)
		if err != nil {
			t.Fatal(err)
		}
		for _, later := range []bool{false, true} {
			for _, kind := range []string{"sdk-error", "invalid-json", "missing", "bad-element", "bad-id"} {
				t.Run(fmt.Sprintf("%s/%s/later=%v", ct, kind, later), func(t *testing.T) {
					m, host := newMockCloudAPI(t)
					m.reply = func(i int, _ recordedRequest) (int, string) {
						if later && i == 0 {
							return 200, `{"Response":{"` + list + `":` + string(b) + `}}`
						}
						switch kind {
						case "sdk-error":
							return 200, `{"Response":{"Error":{"Code":"UnauthorizedOperation","Message":"fixture"},"RequestId":"probe"}}`
						case "invalid-json":
							return 200, `{`
						case "missing":
							return 200, `{"Response":{}}`
						case "bad-element":
							return 200, `{"Response":{"` + list + `":[null]}}`
						default:
							return 200, `{"Response":{"` + list + `":[{}]}}`
						}
					}
					var cli any
					if ct == config.CloudTCLighthouse {
						cli = mockLighthouse(t, host).client
					} else {
						cli = mockCVM(t, host).client
					}
					pool := NewClientPool(Credentials{})
					if _, err := pool.GetOrCreate(pool.CacheKey(ct, "ap-guangzhou"), func() (any, error) { return cli, nil }); err != nil {
						t.Fatal(err)
					}
					got, err := ScanResources(ct, "ap-guangzhou", pool)
					if err == nil || got != nil {
						t.Fatalf("got=%+v err=%v", got, err)
					}
					if kind == "sdk-error" {
						var sdkErr *tcerrors.TencentCloudSDKError
						if !errors.As(err, &sdkErr) || sdkErr.GetCode() != "UnauthorizedOperation" || errors.Is(err, ErrSnapshotIncomplete) {
							t.Fatalf("error classification=%v", err)
						}
					} else if kind != "invalid-json" && !errors.Is(err, ErrSnapshotIncomplete) {
						t.Fatalf("not incomplete: %v", err)
					}
					if kind == "invalid-json" && errors.Is(err, ErrSnapshotIncomplete) {
						t.Fatalf("SDK decode error reclassified: %v", err)
					}
					want := 1
					if later {
						want = 2
					}
					if len(m.recorded()) != want {
						t.Fatalf("requests=%d want=%d", len(m.recorded()), want)
					}
					for i, req := range m.recorded() {
						body := bodyJSON(t, req)
						wantOffset := any(float64(i * 100))
						wantLimit := any(float64(100))
						action := "DescribeInstances"
						if ct == config.CloudTCCVM {
							wantOffset = fmt.Sprint(i * 100)
							wantLimit = "100"
							action = "DescribeSecurityGroups"
						}
						if req.Method != http.MethodPost || req.action() != action || body["Offset"] != wantOffset || body["Limit"] != wantLimit {
							t.Fatalf("request=%+v", req)
						}
					}
				})
			}
		}
	}
}
