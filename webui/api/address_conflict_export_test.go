package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// Build8 Step 7：v4 导出只包含业务配置，运行态冲突记录不得进入备份。
func TestExportV4ExcludesAddressConflictRuntimeState(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	if err := e.store.SaveAddressConflict(ctx, config.AddressConflict{
		DNSName:       "runtime-only.example",
		DNSAddress:    "203.0.113.7",
		EdgeOnePrefix: "203.0.113.0/24",
		ZoneID:        "zone-runtime",
		FirstSeenAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastSeenAt:    time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("预置运行态冲突失败: %v", err)
	}

	w := e.do(t, http.MethodPost, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("导出状态码 = %d; body=%s", w.Code, w.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("解析导出失败: %v", err)
	}
	if _, ok := raw["sync_address_conflict"]; ok {
		t.Fatal("运行态冲突表不得进入 v4 配置包")
	}
	if _, ok := raw["edgeone_origin_transitions"]; ok {
		t.Fatal("过渡保护表不得进入 v4 配置包")
	}
	if strings.Contains(w.Body.String(), "runtime-only.example") ||
		strings.Contains(w.Body.String(), "zone-runtime") {
		t.Fatal("冲突证据不得出现在导出内容中")
	}

	// 导出是只读的：运行态记录保持原样，仍由预检解除或 reset 清理。
	row, err := e.store.GetAddressConflict(ctx)
	if err != nil || row == nil {
		t.Fatalf("导出不得清除运行态记录: %+v err=%v", row, err)
	}
	if row.ZoneID != "zone-runtime" {
		t.Fatalf("运行态记录被改写: %+v", row)
	}
}
