package provider

import (
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// TestScanResourcesUnknownType 未知云产品类型返回错误（不触发真实云 API 调用）
func TestScanResourcesUnknownType(t *testing.T) {
	pool := NewClientPool()
	_, err := ScanResources(config.CloudType("unknown_type"), "ap-guangzhou", pool)
	if err == nil {
		t.Fatal("未知云产品类型应返回错误")
	}
}
