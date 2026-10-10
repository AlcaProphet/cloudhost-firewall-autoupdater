package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// TestI818SnapshotGETAbsentRows 四表都无行时，仍返回四个完整对象和既有表单默认值。
func TestI818SnapshotGETAbsentRows(t *testing.T) {
	e := newTestEnv(t)
	for _, table := range []string{"alert_policy", "alert_email", "alert_webhook", "uptime_kuma_push"} {
		e.execRaw(t, "DELETE FROM "+table)
	}
	w := e.do(t, http.MethodGet, "/api/alerts", "")
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	var objects map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &objects); err != nil {
		t.Fatal(err)
	}
	if len(objects) != 4 {
		t.Fatal("四对象字段集合变化")
	}
	for _, key := range []string{"policy", "email", "webhook", "uptime_kuma_push"} {
		raw, ok := objects[key]
		if !ok || string(raw) == "null" || len(raw) == 0 || raw[0] != '{' {
			t.Fatalf("对象缺失或非对象: %s", key)
		}
	}
	fields := map[string][]string{
		"policy":           {"dns_failed_enabled", "sync_error_enabled", "operational_error_enabled", "health_timeout"},
		"email":            {"enabled", "host", "port", "security", "username", "password", "from_addr", "to_addr", "subject", "body"},
		"webhook":          {"enabled", "url", "channel"},
		"uptime_kuma_push": {"enabled", "url", "interval"},
	}
	for object, keys := range fields {
		var values map[string]json.RawMessage
		if err := json.Unmarshal(objects[object], &values); err != nil {
			t.Fatal(err)
		}
		if len(values) != len(keys) {
			t.Fatalf("%s 字段集合变化", object)
		}
		for _, key := range keys {
			if raw, ok := values[key]; !ok || string(raw) == "null" {
				t.Fatalf("%s.%s 缺失或为 null", object, key)
			}
		}
	}
	got := getAlertsV3(t, e)
	if got.Policy.HealthTimeout != "10m" || got.Email.Security != config.DefaultSMTPSecurity || got.Email.Port != "587" || got.Email.Subject != config.DefaultEmailSubject || got.Email.Body != config.DefaultEmailBody || got.Webhook.Channel != "" || got.UptimeKumaPush.Interval != "60s" {
		t.Fatal("缺行默认值变化")
	}
	if got.Policy.DNSFailedEnabled || got.Policy.SyncErrorEnabled || got.Policy.OperationalErrorEnabled || got.Email.Enabled || got.Webhook.Enabled || got.UptimeKumaPush.Enabled || got.Email.Host != "" || got.Webhook.URL != "" || got.UptimeKumaPush.URL != "" {
		t.Fatal("缺行时应默认全部关闭且连接地址为空")
	}
	if e.applyCount() != 0 {
		t.Fatal("GET发布配置")
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatal("响应头变化")
	}
}

// TestI818SnapshotGETIndependentTables 无关业务表不可读时，告警页仍可独立读取。
func TestI818SnapshotGETIndependentTables(t *testing.T) {
	for _, table := range []string{"settings", "rules", "targets"} {
		t.Run(table, func(t *testing.T) {
			e := newTestEnv(t)
			e.execRaw(t, "DROP TABLE "+table)
			w := e.do(t, http.MethodGet, "/api/alerts", "")
			if w.Code != 200 {
				t.Fatalf("无关表损坏扩大失败面: %s %d", table, w.Code)
			}
		})
	}
}

// TestI818SnapshotGETKeepsRawEmailWebhook GET 不扩展校验或把空格字符串当成空值。
func TestI818SnapshotGETKeepsRawEmailWebhook(t *testing.T) {
	e := newTestEnv(t)
	e.execRaw(t, "INSERT OR REPLACE INTO alert_email(id,enabled,host,port,security,username,password,from_addr,to_addr,subject,body) VALUES(1,0,' repair host ','bad-port',' unknown-mode ','','','','','   ','   ')")
	e.execRaw(t, "INSERT OR REPLACE INTO alert_webhook(id,enabled,url,channel) VALUES(1,0,' repair url ','unknown-channel')")
	got := getAlertsV3(t, e)
	if got.Email.Security != " unknown-mode " || got.Email.Host != " repair host " || got.Email.Port != "bad-port" || got.Email.Subject != "   " || got.Email.Body != "   " || got.Webhook.URL != " repair url " || got.Webhook.Channel != "unknown-channel" {
		t.Fatal("GET额外归一化或校验了原始字段")
	}
}

// TestI818SnapshotGETFailures 四个读取阶段与事务开启失败共用安全 500 出口。
func TestI818SnapshotGETFailures(t *testing.T) {
	for _, stage := range []string{"begin", "alert_policy", "alert_email", "alert_webhook", "uptime_kuma_push", "policy-duration", "push-duration"} {
		t.Run(stage, func(t *testing.T) {
			e := newTestEnv(t)
			switch stage {
			case "begin":
				if err := e.store.Close(); err != nil {
					t.Fatal(err)
				}
			case "policy-duration":
				e.execRaw(t, "UPDATE alert_policy SET health_timeout='bad-value'")
			case "push-duration":
				e.execRaw(t, "UPDATE uptime_kuma_push SET interval='bad-value'")
			default:
				e.execRaw(t, "DROP TABLE "+stage)
			}
			w := e.do(t, http.MethodGet, "/api/alerts", "")
			assertJSONNoStore(t, w.Result(), http.StatusInternalServerError)
			var got map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got["error"] != "读取告警配置失败" {
				t.Fatalf("错误响应泄露或变化: %v", got)
			}
			if e.applyCount() != 0 {
				t.Fatal("GET 失败发布配置")
			}
		})
	}
}

// TestI818SnapshotGETCancelled 已取消的 HTTP 请求不返回成功配置。
func TestI818SnapshotGETCancelled(t *testing.T) {
	e := newTestEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mux := http.NewServeMux()
	e.deps.Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/alerts", nil).WithContext(ctx))
	assertJSONNoStore(t, w.Result(), http.StatusInternalServerError)
	if e.applyCount() != 0 {
		t.Fatal("取消的 GET 发布配置")
	}
	if next := e.do(t, http.MethodGet, "/api/alerts", ""); next.Code != http.StatusOK {
		t.Fatal("取消后读取未恢复")
	}
}

// i818Payload 用跨四对象的独立版本标记辨别撕裂，所有通知渠道保持关闭。
func i818Payload(version int64) string {
	return fmt.Sprintf(`{"policy":{"dns_failed_enabled":false,"sync_error_enabled":false,"operational_error_enabled":false,"health_timeout":"%dm"},"email":{"enabled":false,"host":"v%d","port":"587","security":"auto_starttls","username":"","password":"","from_addr":"","to_addr":"","subject":"v%d","body":"v%d"},"webhook":{"enabled":false,"url":"https://example.invalid/v%d","channel":"dingtalk"},"uptime_kuma_push":{"enabled":false,"url":"https://example.invalid/v%d","interval":"60s"}}`, version, version, version, version, version, version)
}

// TestI818SnapshotHTTPConcurrent 真实 HTTP GET 与协调器 PUT 并发时，每个响应四项同版本。
// 确定性窗口由 config 层屏障测试守护，本测试补充实际路由、事务与 JSON 整链。
func TestI818SnapshotHTTPConcurrent(t *testing.T) {
	e := newTestEnv(t)
	if w := e.do(t, http.MethodPut, "/api/alerts", i818Payload(1)); w.Code != http.StatusOK {
		t.Fatal("初始化 PUT 失败")
	}
	mux := http.NewServeMux()
	e.deps.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	client := server.Client()
	client.Timeout = 10 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var version atomic.Int64
	version.Store(1)
	failures := make(chan error, 5)
	start := make(chan struct{})
	var workers sync.WaitGroup
	request := func(method, body string) (alertsV3, error) {
		var got alertsV3
		req, err := http.NewRequestWithContext(ctx, method, server.URL+"/api/alerts", strings.NewReader(body))
		if err != nil {
			return got, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return got, err
		}
		if resp.StatusCode != http.StatusOK {
			closeErr := resp.Body.Close()
			return got, fmt.Errorf("HTTP 状态=%d;关闭=%v", resp.StatusCode, closeErr)
		}
		if resp.Header.Get("Cache-Control") != "no-store" {
			closeErr := resp.Body.Close()
			return got, fmt.Errorf("缓存控制缺失;关闭=%v", closeErr)
		}
		if method == http.MethodGet {
			err = json.NewDecoder(resp.Body).Decode(&got)
		} else {
			_, err = io.Copy(io.Discard, resp.Body)
		}
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		return got, err
	}
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for n := 0; n < 25; n++ {
				if _, err := request(http.MethodPut, i818Payload(version.Add(1))); err != nil {
					failures <- err
					return
				}
			}
		}()
	}
	for i := 0; i < 3; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for n := 0; n < 60; n++ {
				got, err := request(http.MethodGet, "")
				if err != nil {
					failures <- err
					return
				}
				var v int64
				if _, err = fmt.Sscanf(got.Policy.HealthTimeout, "%dm", &v); err != nil {
					failures <- err
					return
				}
				tag := fmt.Sprintf("v%d", v)
				url := "https://example.invalid/" + tag
				if got.Email.Host != tag || got.Email.Subject != tag || got.Email.Body != tag || got.Webhook.URL != url || got.UptimeKumaPush.URL != url {
					failures <- fmt.Errorf("GET 四项版本不一致: %s/%s/%s/%s", got.Policy.HealthTimeout, got.Email.Host, got.Webhook.URL, got.UptimeKumaPush.URL)
					return
				}
			}
		}()
	}
	close(start)
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if got := e.applyCount(); got != 51 {
		t.Errorf("发布次数=%d,期望 51 次 PUT；GET 不应发布", got)
	}
}
