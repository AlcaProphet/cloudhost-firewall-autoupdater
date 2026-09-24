package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// errStatus 返回已分类错误的状态码（未分类或 nil 返回 0）
func errStatus(err error) int {
	if err == nil {
		return 0
	}
	var he *httpError
	if errors.As(err, &he) {
		return he.status
	}
	return 0
}

// decodeBody 直接调用严格解码 helper
func decodeBody(t *testing.T, body string, dst any) error {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()
	return decodeJSONStrict(w, req, maxJSONBodyBytes, dst)
}

type decodePayload struct {
	Name string `json:"name"`
}

// TestDecodeJSONStrict 覆盖 1 MiB 上限、未知字段、类型错误、尾随 JSON、多顶层值与空 body
func TestDecodeJSONStrict(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantStatus  int
		wantErrPart string
	}{
		{"合法", `{"name":"a"}`, 0, ""},
		{"未知字段", `{"name":"a","id":1}`, http.StatusBadRequest, "未知字段"},
		{"类型错误", `{"name":123}`, http.StatusBadRequest, "类型错误"},
		{"顶层数组", `[]`, http.StatusBadRequest, "类型错误"},
		{"尾随 JSON", `{"name":"a"}{"name":"b"}`, http.StatusBadRequest, "只能包含一个 JSON 值"},
		{"多个顶层值", `{"name":"a"} 1`, http.StatusBadRequest, "只能包含一个 JSON 值"},
		{"尾随垃圾", `{"name":"a"} xxx`, http.StatusBadRequest, "JSON 格式错误"},
		{"语法错误", `{"name":}`, http.StatusBadRequest, "JSON 格式错误"},
		{"空 body", ``, http.StatusBadRequest, "不能为空"},
		{"超限 1MiB", `{"name":"` + strings.Repeat("a", maxJSONBodyBytes) + `"}`, http.StatusRequestEntityTooLarge, "超过大小上限"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var dst decodePayload
			err := decodeBody(t, tc.body, &dst)
			if tc.wantStatus == 0 {
				if err != nil {
					t.Fatalf("应通过，实际: %v", err)
				}
				if dst.Name != "a" {
					t.Errorf("解码结果 = %q, want a", dst.Name)
				}
				return
			}
			if got := errStatus(err); got != tc.wantStatus {
				t.Fatalf("状态码 = %d, want %d (err=%v)", got, tc.wantStatus, err)
			}
			if !strings.Contains(err.Error(), tc.wantErrPart) {
				t.Errorf("错误文本 %q 应包含 %q", err.Error(), tc.wantErrPart)
			}
		})
	}
}

// TestDecodeJSONStrict_UnknownFieldName 未知字段错误应指出字段名（不敏感）
func TestDecodeJSONStrict_UnknownFieldName(t *testing.T) {
	var dst decodePayload
	err := decodeBody(t, `{"webui_port":"1"}`, &dst)
	if err == nil || !strings.Contains(err.Error(), "webui_port") {
		t.Fatalf("错误应指出未知字段名: %v", err)
	}
}

// TestParsePathIDStrict 路径 ID 必须严格解析且大于 0（不复用 Sscanf 的宽松扫描）
func TestParsePathIDStrict(t *testing.T) {
	// strconv.Atoi 接受可选正号（"+3" = 3），这是 Build6 §4.1 指定的解析方式
	valid := map[string]int{"12": 12, "1": 1, "+3": 3}
	for raw, want := range valid {
		req := httptest.NewRequest(http.MethodPut, "/", nil)
		req.SetPathValue("id", raw)
		got, err := parsePathID(req)
		if err != nil {
			t.Errorf("parsePathID(%q) 应通过: %v", raw, err)
		}
		if got != want {
			t.Errorf("parsePathID(%q) = %d, want %d", raw, got, want)
		}
	}

	for _, raw := range []string{"12abc", "-5", "0", "", " 7", "1e3", "99999999999999999999", "abc", "3.5"} {
		req := httptest.NewRequest(http.MethodPut, "/", nil)
		req.SetPathValue("id", raw)
		if got, err := parsePathID(req); err == nil {
			t.Errorf("parsePathID(%q) 应失败，实际 %d", raw, got)
		}
	}
}
