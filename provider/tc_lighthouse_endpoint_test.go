package provider

import "testing"

// TestStep16LighthouseEndpointDefaults 固定生产 Lighthouse 端点/协议默认值。
//
// Step 16 本地进程验收通过链接期 -X 覆盖这两个变量；本回归防止默认值或
// scheme 语义被测试接线意外改写。
func TestStep16LighthouseEndpointDefaults(t *testing.T) {
	if tcLighthouseEndpoint != "lighthouse.tencentcloudapi.com" {
		t.Fatalf("生产 Lighthouse endpoint = %q", tcLighthouseEndpoint)
	}
	if tcLighthouseScheme != "" {
		t.Fatalf("生产 Lighthouse scheme 必须留空以使用 SDK 默认 HTTPS，实际 %q", tcLighthouseScheme)
	}
}
