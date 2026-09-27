package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// TestScannedResourcesQueryValidation GET/DELETE /api/scanned-resources 的 cloud_type
// 必须复用与目标 CRUD、扫描请求相同的枚举校验（Build6 §4.2）：
// 缺失、空白与非法枚举一律 400，且不得改动扫描缓存。
func TestScannedResourcesQueryValidation(t *testing.T) {
	e := newTestEnv(t)
	if err := e.store.ReplaceScannedResources("tc_lighthouse", "ap-guangzhou", []config.ScannedResource{
		{CloudType: "tc_lighthouse", Region: "ap-guangzhou", ResourceID: "lhins-scan", ResourceName: "扫描"},
	}); err != nil {
		t.Fatalf("预置扫描结果失败: %v", err)
	}

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"GET 缺少参数", http.MethodGet, "/api/scanned-resources"},
		{"GET 非法枚举", http.MethodGet, "/api/scanned-resources?cloud_type=aws_ec2"},
		{"GET 空白参数", http.MethodGet, "/api/scanned-resources?cloud_type=%20%20"},
		{"DELETE 缺少参数", http.MethodDelete, "/api/scanned-resources"},
		{"DELETE 非法枚举", http.MethodDelete, "/api/scanned-resources?cloud_type=aws_ec2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := e.do(t, tc.method, tc.path, "")
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			scanned, err := e.store.GetScannedResources("tc_lighthouse")
			if err != nil || len(scanned) != 1 {
				t.Errorf("被拒绝的请求不得改动扫描缓存: %d 条, err=%v", len(scanned), err)
			}
		})
	}
}

// TestScannedResourcesQueryValid 合法枚举仍可读取与清理，且只影响所查询的云类型。
func TestScannedResourcesQueryValid(t *testing.T) {
	e := newTestEnv(t)
	for _, ct := range []string{"tc_lighthouse", "tc_cvm"} {
		if err := e.store.ReplaceScannedResources(ct, "ap-guangzhou", []config.ScannedResource{
			{CloudType: ct, Region: "ap-guangzhou", ResourceID: ct + "-1"},
		}); err != nil {
			t.Fatalf("预置 %s 扫描结果失败: %v", ct, err)
		}
	}

	w := e.do(t, http.MethodGet, "/api/scanned-resources?cloud_type=tc_cvm", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET 状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var got []config.ScannedResource
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析 GET 响应失败: %v", err)
	}
	if len(got) != 1 || got[0].ResourceID != "tc_cvm-1" {
		t.Errorf("GET 只应返回所查询云类型的结果: %+v", got)
	}

	w = e.do(t, http.MethodDelete, "/api/scanned-resources?cloud_type=tc_cvm", "")
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE 状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if scanned, _ := e.store.GetScannedResources("tc_cvm"); len(scanned) != 0 {
		t.Errorf("DELETE 后 tc_cvm 扫描结果应清空: %+v", scanned)
	}
	if scanned, _ := e.store.GetScannedResources("tc_lighthouse"); len(scanned) != 1 {
		t.Errorf("DELETE 不得影响其他云类型: %+v", scanned)
	}
}
