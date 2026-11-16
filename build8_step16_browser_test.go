//go:build step16

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// TestStep16BrowserUI 用真实 Chrome + CDP 验证深链、侧栏、历史、设置回显、
// 多目标绑定、导入刷新、暂停和 Dry Run。
func TestStep16BrowserUI(t *testing.T) {
	f := newStep16Fixture(t)
	f.cloud.setZone("zone-browser", step16Zone{
		CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24"},
		NextVer: "v2", NextV4: []string{"198.51.100.0/24"},
	})
	f.dns.setIP("198.18.0.10")
	f.setup(2, func(t *testing.T, store *config.Store, ids []int) {
		step16AddSite(t, store, config.EdgeOneSite{
			ZoneID: "zone-browser", TargetIDs: ids, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
		})
		step16AddRule(t, store, config.DomainRule{
			Host: "browser.example.test", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{ids[0]},
		})
	})
	f.start()
	control := newStep16Control(t, f)
	runStep16Browser(t, "ui", f.base, control.URL)
	f.stop()
}

// TestStep16BrowserEdgeOneStates 用真实 Chrome 验证冲突、单侧继续、恢复以及
// unknown/accepted/effective 的站点状态展示。
func TestStep16BrowserEdgeOneStates(t *testing.T) {
	f := newStep16Fixture(t)
	f.cloud.setZone("zone-states", step16Zone{
		CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/24"},
		NextVer: "v2", NextV4: []string{"198.51.100.0/24"},
	})
	f.dns.setIP("198.18.0.10")
	f.setup(2, func(t *testing.T, store *config.Store, ids []int) {
		step16AddSite(t, store, config.EdgeOneSite{
			ZoneID: "zone-states", TargetIDs: ids, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
		})
		step16AddRule(t, store, config.DomainRule{
			Host: "states.example.test", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{ids[0]},
		})
	})
	f.start()
	defer func() {
		if t.Failed() {
			t.Logf("Step 16 进程输出:\n%s", f.out.String())
		}
	}()
	control := newStep16Control(t, f)
	runStep16Browser(t, "edge", f.base, control.URL)
	f.stop()
}

type step16Control struct {
	*httptest.Server
	f *step16Fixture
}

func newStep16Control(t *testing.T, f *step16Fixture) *step16Control {
	t.Helper()
	c := &step16Control{f: f}
	mux := http.NewServeMux()
	mux.HandleFunc("/state/teo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Zone           string   `json:"zone"`
			CurrentVersion string   `json:"current_version"`
			CurrentV4      []string `json:"current_v4"`
			NextVersion    string   `json:"next_version"`
			NextV4         []string `json:"next_v4"`
			DescribeMode   string   `json:"describe_mode"`
			ConfirmMode    string   `json:"confirm_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.cloud.setZone(req.Zone, step16Zone{
			CurrentVer: req.CurrentVersion, CurrentV4: req.CurrentV4,
			NextVer: req.NextVersion, NextV4: req.NextV4,
			DescribeMode: req.DescribeMode, ConfirmMode: req.ConfirmMode,
		})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/state/dns", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			IP   string `json:"ip"`
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.IP != "" {
			f.dns.setIP(req.IP)
		}
		f.dns.setMode(req.Mode)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		status, err := f.status()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("/edge-status", func(w http.ResponseWriter, r *http.Request) {
		status, err := f.edgeStatus()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		code, body, err := f.health()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, code, body)
	})
	mux.HandleFunc("/counters", func(w http.ResponseWriter, r *http.Request) {
		td, cf := f.cloud.teoCounts("zone-browser")
		td2, cf2 := f.cloud.teoCounts("zone-states")
		d, c, x := f.cloud.totalLHCounts()
		writeJSON(w, http.StatusOK, map[string]any{
			"teo_browser": [2]int{td, cf}, "teo_states": [2]int{td2, cf2},
			"lh": [3]int{d, c, x}, "actions": f.cloud.actionLog(),
		})
	})
	mux.HandleFunc("/confirm-started", func(w http.ResponseWriter, r *http.Request) {
		zone := r.URL.Query().Get("zone")
		writeJSON(w, http.StatusOK, map[string]bool{"started": f.cloud.waitConfirmStarted(zone, 30*time.Second)})
	})
	mux.HandleFunc("/release-confirm", func(w http.ResponseWriter, r *http.Request) {
		f.cloud.releaseConfirm(r.URL.Query().Get("zone"))
		writeJSON(w, http.StatusOK, map[string]bool{"released": true})
	})
	mux.HandleFunc("/sync/", func(w http.ResponseWriter, r *http.Request) {
		action := strings.TrimPrefix(r.URL.Path, "/sync/")
		var code int
		switch action {
		case "trigger":
			code = f.trigger()
		case "pause":
			code = f.pause()
		case "resume":
			code = f.resume()
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"http_status": code})
	})
	mux.HandleFunc("/import-v4", func(w http.ResponseWriter, r *http.Request) {
		code, body, err := step16ImportV4WithPorts(f.base, "8443")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, code, map[string]any{"body": body})
	})
	c.Server = httptest.NewServer(mux)
	t.Cleanup(c.Server.Close)
	return c
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

func step16ImportV4WithPorts(base, ports string) (int, string, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Post(base+"/api/config/export", "application/json", nil)
	if err != nil {
		return 0, "", err
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return 0, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, string(raw), fmt.Errorf("export status %d", resp.StatusCode)
	}
	var bundle map[string]any
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return 0, "", err
	}
	sites, _ := bundle["edgeone_sites"].([]any)
	if len(sites) == 0 {
		return 0, "", fmt.Errorf("export has no edgeone_sites")
	}
	site, _ := sites[0].(map[string]any)
	site["ports"] = ports
	modified, err := json.Marshal(bundle)
	if err != nil {
		return 0, "", err
	}
	resp, err = client.Post(base+"/api/config/import", "application/json", bytes.NewReader(modified))
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), nil
}

func runStep16Browser(t *testing.T, scenario, appURL, controlURL string) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位浏览器夹具")
	}
	script := filepath.Join(filepath.Dir(file), "testdata", "step16", "browser.mjs")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("浏览器脚本缺失: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script, scenario, appURL, controlURL)
	cmd.Env = append(os.Environ(), "CHROME_BIN=/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("浏览器验收超时；输出:\n%s", out.String())
	}
	if err != nil {
		t.Fatalf("浏览器验收失败: %v；输出:\n%s", err, out.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.Contains(line, "STEP16_BROWSER") {
			t.Log(line)
		}
	}
}
