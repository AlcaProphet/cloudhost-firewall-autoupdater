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
						return 200, `{"TotalCount":101,"Instances":` + string(full) + `}`
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

// TestScanSWASPagination 验证计数矛盾、重复 ID、终止条件与实际请求次数。
func TestScanSWASPagination(t *testing.T) {
	page := func(start, n int, meta string) string {
		x := make([]map[string]string, n)
		for i := range x {
			x[i] = map[string]string{"InstanceId": fmt.Sprintf("id-%03d", start+i)}
		}
		b, e := json.Marshal(x)
		if e != nil {
			t.Fatal(e)
		}
		return `{"Instances":` + string(b) + meta + `}`
	}
	cases := []struct {
		name  string
		pages []string
		want  int
	}{
		{"short-before-total", []string{page(0, 1, `,"TotalCount":2`)}, -1},
		{"empty-before-total", []string{page(0, 0, `,"TotalCount":2`)}, -1},
		{"zero-with-resource", []string{page(0, 1, `,"TotalCount":0`)}, -1},
		{"negative-total", []string{page(0, 1, `,"TotalCount":-1`)}, -1},
		{"total-decreases", []string{page(0, 100, `,"TotalCount":101`), page(100, 1, `,"TotalCount":100`)}, -1},
		{"total-increases", []string{page(0, 100, `,"TotalCount":101`), page(100, 1, `,"TotalCount":102`)}, -1},
		{"duplicate-in-page", []string{`{"TotalCount":2,"Instances":[{"InstanceId":"dup"},{"InstanceId":"dup"}]}`}, -1},
		{"duplicate-across-pages", []string{page(0, 100, `,"TotalCount":101`), page(0, 1, `,"TotalCount":101`)}, -1},
		{"repeated-full-page", []string{page(0, 100, `,"TotalCount":200`), page(0, 100, `,"TotalCount":200`), page(0, 0, `,"TotalCount":200`)}, -1},
		{"wrong-echo-page", []string{page(0, 1, `,"TotalCount":1,"PageNumber":2,"PageSize":100`)}, -1},
		{"wrong-echo-size", []string{page(0, 1, `,"TotalCount":1,"PageNumber":1,"PageSize":10`)}, -1},
		{"oversized-page", []string{page(0, 101, `,"TotalCount":101`), page(0, 0, `,"TotalCount":101`)}, -1},
		{"missing-total", []string{page(0, 1, "")}, -1},
		{"null-total", []string{page(0, 1, `,"TotalCount":null`)}, -1},
		{"later-missing-total", []string{page(0, 100, `,"TotalCount":101`), page(100, 1, "")}, -1},
		{"late-total", []string{page(0, 100, ""), page(100, 1, `,"TotalCount":101`)}, -1},
		{"known-empty-tail", []string{page(0, 100, `,"TotalCount":101`), page(100, 0, `,"TotalCount":101`)}, -1},
		{"valid-zero", []string{page(0, 0, `,"TotalCount":0`)}, 0},
		{"valid-one", []string{page(0, 1, `,"TotalCount":1,"PageNumber":1,"PageSize":100`)}, 1},
		{"valid-exact-full", []string{page(0, 100, `,"TotalCount":100`), page(0, 0, `,"TotalCount":100`)}, 100},
		{"valid-two-pages", []string{page(0, 100, `,"TotalCount":101`), page(100, 1, `,"TotalCount":101`)}, 101},
		{"valid-two-full", []string{page(0, 100, `,"TotalCount":200`), page(100, 100, `,"TotalCount":200`), page(0, 0, `,"TotalCount":200`)}, 200},
		{"missing-total-full-tail", []string{page(0, 100, ""), page(100, 0, "")}, -1},
		// 总数不变且 ID 不重复仍不能识别扫描期间的等量资源替换。
		{"equal-count-replacement-not-detectable", []string{page(0, 100, `,"TotalCount":200`), page(101, 100, `,"TotalCount":200`), page(0, 0, `,"TotalCount":200`)}, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, host := newMockCloudAPI(t)
			m.reply = func(i int, r recordedRequest) (int, string) {
				if r.action() != "ListInstances" || r.query("PageNumber") != fmt.Sprint(i+1) || r.query("PageSize") != "100" {
					t.Errorf("request=%+v", r)
				}
				if i >= len(tc.pages) {
					return 400, `{"Code":"InvalidParameter","Message":"unexpected page"}`
				}
				return 200, tc.pages[i]
			}
			pool := NewClientPool(Credentials{})
			cli := mockSWAS(t, host).client
			if _, e := pool.GetOrCreate(pool.CacheKey(config.CloudAliSWAS, "cn-hangzhou"), func() (any, error) { return cli, nil }); e != nil {
				t.Fatal(e)
			}
			got, e := ScanResources(config.CloudAliSWAS, "cn-hangzhou", pool)
			want := tc.want
			if want < 0 {
				if !errors.Is(e, ErrSnapshotIncomplete) || got != nil {
					t.Fatalf("expected incomplete/nil, got n=%d err=%v", len(got), e)
				}
			} else if e != nil || got == nil || len(got) != want {
				t.Fatalf("expected n=%d got n=%d err=%v", want, len(got), e)
			}
			wantCalls := 1
			switch tc.name {
			case "total-decreases", "total-increases", "duplicate-across-pages", "repeated-full-page", "later-missing-total", "known-empty-tail", "valid-two-pages", "valid-two-full", "equal-count-replacement-not-detectable":
				wantCalls = 2
			}
			if len(m.recorded()) != wantCalls {
				t.Fatalf("calls=%d want=%d", len(m.recorded()), wantCalls)
			}
		})
	}
}

// TestScanSWASEdges 验证末页仍校验全部字段，并保留 SDK 解码及 ID 原值边界。
func TestScanSWASEdges(t *testing.T) {
	page := func(start, n int, meta string) string {
		x := make([]map[string]string, n)
		for i := range x {
			x[i] = map[string]string{"InstanceId": fmt.Sprintf("id-%03d", start+i)}
		}
		b, e := json.Marshal(x)
		if e != nil {
			t.Fatal(e)
		}
		return `{"Instances":` + string(b) + meta + `}`
	}
	full := page(0, 100, `,"TotalCount":101`)
	cases := []struct {
		name        string
		pages       []string
		want, calls int
	}{
		{"later-null-total", []string{full, page(100, 1, `,"TotalCount":null`)}, -1, 2},
		{"later-negative-total", []string{full, page(100, 1, `,"TotalCount":-1`)}, -1, 2},
		{"later-wrong-page", []string{full, page(100, 1, `,"TotalCount":101,"PageNumber":1`)}, -1, 2},
		{"later-wrong-size", []string{full, page(100, 1, `,"TotalCount":101,"PageSize":10`)}, -1, 2},
		{"later-early-short", []string{page(0, 100, `,"TotalCount":102`), page(100, 1, `,"TotalCount":102`)}, -1, 2},
		{"page-zero", []string{page(0, 1, `,"TotalCount":1,"PageNumber":0`)}, -1, 1},
		{"page-negative", []string{page(0, 1, `,"TotalCount":1,"PageNumber":-1`)}, -1, 1},
		{"size-zero", []string{page(0, 1, `,"TotalCount":1,"PageSize":0`)}, -1, 1},
		{"size-negative", []string{page(0, 1, `,"TotalCount":1,"PageSize":-1`)}, -1, 1},
		{"nullable-echo", []string{page(0, 1, `,"TotalCount":1,"PageNumber":null,"PageSize":null`)}, 1, 1},
		{"largest-total-empty", []string{page(0, 0, `,"TotalCount":2147483647`)}, -1, 1},
		{"duplicate-different-names", []string{`{"TotalCount":2,"Instances":[{"InstanceId":"same","InstanceName":"a"},{"InstanceId":"same","InstanceName":"b"}]}`}, -1, 1},
		{"raw-ids-distinct", []string{`{"TotalCount":3,"Instances":[{"InstanceId":"ID"},{"InstanceId":"id"},{"InstanceId":" id "}]}`}, 3, 1},
		{"decoded-string-count", []string{page(0, 1, `,"TotalCount":"1"`)}, 1, 1},
		{"decoded-fraction-count", []string{page(0, 1, `,"TotalCount":1.9`)}, 1, 1},
		{"decoded-bool-count", []string{page(0, 1, `,"TotalCount":true`)}, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, host := newMockCloudAPI(t)
			m.reply = func(i int, r recordedRequest) (int, string) {
				if r.query("PageNumber") != fmt.Sprint(i+1) || r.query("PageSize") != "100" {
					t.Errorf("request=%+v", r)
				}
				if i >= len(tc.pages) {
					return 400, `{"Code":"InvalidParameter","Message":"unexpected"}`
				}
				return 200, tc.pages[i]
			}
			pool := NewClientPool(Credentials{})
			cli := mockSWAS(t, host).client
			if _, e := pool.GetOrCreate(pool.CacheKey(config.CloudAliSWAS, "cn-hangzhou"), func() (any, error) { return cli, nil }); e != nil {
				t.Fatal(e)
			}
			got, e := ScanResources(config.CloudAliSWAS, "cn-hangzhou", pool)
			if tc.want < 0 {
				if !errors.Is(e, ErrSnapshotIncomplete) || got != nil {
					t.Fatalf("got=%v err=%v", got, e)
				}
			} else if e != nil || got == nil || len(got) != tc.want {
				t.Fatalf("n=%d err=%v", len(got), e)
			}
			if len(m.recorded()) != tc.calls {
				t.Fatalf("calls=%d want=%d", len(m.recorded()), tc.calls)
			}
		})
	}
}

// TestScanSWASIndependentCalls 同一缓存客户端的多次扫描不共享计数与去重状态。
func TestScanSWASIndependentCalls(t *testing.T) {
	m, host := newMockCloudAPI(t)
	m.reply = func(i int, r recordedRequest) (int, string) {
		switch i {
		case 0:
			return 200, `{"TotalCount":1,"Instances":[{"InstanceId":"same"}]}`
		case 1:
			return 200, `{"TotalCount":0,"Instances":[]}`
		default:
			return 200, `{"TotalCount":1,"Instances":[{"InstanceId":"same"}]}`
		}
	}
	pool := NewClientPool(Credentials{})
	cli := mockSWAS(t, host).client
	if _, e := pool.GetOrCreate(pool.CacheKey(config.CloudAliSWAS, "cn-hangzhou"), func() (any, error) { return cli, nil }); e != nil {
		t.Fatal(e)
	}
	for _, n := range []int{1, 0, 1} {
		got, e := ScanResources(config.CloudAliSWAS, "cn-hangzhou", pool)
		if e != nil || got == nil || len(got) != n {
			t.Fatalf("n=%d err=%v want=%d", len(got), e, n)
		}
	}
	for _, r := range m.recorded() {
		if r.query("PageNumber") != "1" {
			t.Fatalf("cross-scan page state")
		}
	}
}

// TestScanSWASBeyondRulePageCap 资源扫描不继承单实例规则快照的 100 页限制。
func TestScanSWASBeyondRulePageCap(t *testing.T) {
	m, host := newMockCloudAPI(t)
	m.reply = func(i int, r recordedRequest) (int, string) {
		if i >= 101 {
			return 400, `{"Code":"InvalidParameter","Message":"unexpected"}`
		}
		x := make([]map[string]string, 100)
		for j := range x {
			x[j] = map[string]string{"InstanceId": fmt.Sprint(i*100 + j)}
		}
		b, e := json.Marshal(x)
		if e != nil {
			t.Fatal(e)
		}
		return 200, `{"TotalCount":10100,"Instances":` + string(b) + `}`
	}
	pool := NewClientPool(Credentials{})
	cli := mockSWAS(t, host).client
	if _, e := pool.GetOrCreate(pool.CacheKey(config.CloudAliSWAS, "cn-hangzhou"), func() (any, error) { return cli, nil }); e != nil {
		t.Fatal(e)
	}
	got, e := ScanResources(config.CloudAliSWAS, "cn-hangzhou", pool)
	if e != nil || len(got) != 10100 || len(m.recorded()) != 101 {
		t.Fatalf("n=%d calls=%d err=%v", len(got), len(m.recorded()), e)
	}
}
