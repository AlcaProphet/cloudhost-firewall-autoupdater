package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/app"
)

// i8111Guard 预置并发布旧配置，逐请求证明拒绝没有改变数据库或已发布对象。
func i8111Guard(t *testing.T) (*testEnv, func(*testing.T)) {
	t.Helper()
	e := newTestEnv(t)
	seedImportBaseline(t, e)
	if w := e.do(t, http.MethodPut, "/api/settings", `{"tag":"keep-tag"}`); w.Code != http.StatusOK {
		t.Fatalf("发布旧配置失败: %d", w.Code)
	}
	before, err := e.store.LoadBusinessSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	scanned, err := e.store.GetScannedResources("tc_lighthouse")
	if err != nil {
		t.Fatal(err)
	}
	logs, err := e.store.GetSyncLogs(10)
	if err != nil {
		t.Fatal(err)
	}
	state, applies, level := e.snapshot(), e.applyCount(), app.LogLevelVar.Level()
	e.alerts.mu.Lock()
	alerts := e.alerts.current
	e.alerts.mu.Unlock()
	return e, func(t *testing.T) {
		t.Helper()
		after, err := e.store.LoadBusinessSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Error("拒绝导入改变了完整业务快照")
		}
		afterScanned, err := e.store.GetScannedResources("tc_lighthouse")
		if err != nil {
			t.Fatal(err)
		}
		afterLogs, err := e.store.GetSyncLogs(10)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(scanned, afterScanned) || !reflect.DeepEqual(logs, afterLogs) {
			t.Error("拒绝导入改变了扫描缓存或同步日志")
		}
		if e.snapshot() != state || e.applyCount() != applies || app.LogLevelVar.Level() != level {
			t.Error("拒绝导入改变了运行时、发布次数或日志级别")
		}
		e.alerts.mu.Lock()
		unchanged := reflect.DeepEqual(alerts, e.alerts.current)
		e.alerts.mu.Unlock()
		if !unchanged {
			t.Error("拒绝导入替换了告警订阅集合")
		}
	}
}

// TestI8111RejectJSON 覆盖覆盖/合并、大小写、转义重复与非法编码的正式端点。
func TestI8111RejectJSON(t *testing.T) {
	e, unchanged := i8111Guard(t)
	base := validBundle()
	replace := func(old, next string) string { return strings.Replace(base, old, next, 1) }
	cases := []struct{ name, body, message string }{
		{"旧版本后覆盖", replace(`"version":3`, `"version":2,"version":3`), "重复字段"},
		{"旧版本1后覆盖", replace(`"version":3`, `"version":1,"version":3`), "重复字段"},
		{"null后覆盖", replace(`"version":3`, `"version":null,"version":3`), "重复字段"},
		{"相同值重复", replace(`"version":3`, `"version":3,"version":3`), "重复字段"},
		{"转义重复", replace(`"version":3`, `"version":3,"\u0076ersion":3`), "重复字段"},
		{"大小写同字段", replace(`"version":3`, `"version":3,"VERSION":3`), "未知字段"},
		{"大写单字段", replace(`"version":3`, `"VERSION":3`), "未知字段"},
		{"首字母大写", replace(`"version":3`, `"Version":3`), "未知字段"},
		{"转义大写", replace(`"version":3`, `"\u0056ERSION":3`), "未知字段"},
		{"Unicode大小写", replace(`"settings":`, `"\u017fettings":`), "未知字段"},
		{"null后空数组", replace(`"targets":[]`, `"targets":null,"targets":[]`), "重复字段"},
		{"空数组后null", replace(`"targets":[]`, `"targets":[],"targets":null`), "重复字段"},
		{"null后对象", replace(`"monitoring":`, `"monitoring":null,"monitoring":`), "重复字段"},
		{"对象合并", replace(`"uptime_kuma_push":{"enabled":false,"url":"","interval":"60s"}`, `"uptime_kuma_push":{"enabled":false},"uptime_kuma_push":{"url":"","interval":"60s"}`), "重复字段"},
		{"对象后空对象", strings.TrimSuffix(base, "}") + `,"monitoring":{}}`, "重复字段"},
		{"嵌套转义重复", replace(`"tag":"auto-dns"`, `"tag":"first","\u0074ag":"auto-dns"`), "重复字段"},
		{"数组元素转义重复", bundleWith(`[{"export_id":1,"\u0065xport_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"x"}]`, `[]`), "重复字段"},
		{"下划线不可省略", replace(`"dns_timeout":"10s"`, `"dnsTimeout":"10s"`), "未知字段"},
		{"非法UTF8", replace(`"body":"FWAlizer 检测到运行异常，请检查同步日志。"`, `"body":"`+string([]byte{0xff})+`"`), "JSON 格式错误"},
		{"孤立高代理项", replace(`"body":"FWAlizer 检测到运行异常，请检查同步日志。"`, `"body":"\ud800"`), "JSON 格式错误"},
		{"孤立低代理项", replace(`"body":"FWAlizer 检测到运行异常，请检查同步日志。"`, `"body":"\udc00"`), "JSON 格式错误"},
		{"尾随对象", base + `{}`, "JSON 格式错误"},
		{"尾随标量", base + ` 1`, "JSON 格式错误"},
		{"尾随垃圾", base + ` garbage`, "JSON 格式错误"},
		{"空请求", " \n\t", "不能为空"},
		{"顶层null", `null`, "JSON 对象"},
		{"顶层数组", `[]`, "JSON 对象"},
		{"顶层标量", `3`, "JSON 对象"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := e.do(t, http.MethodPost, "/api/config/import", tc.body)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), tc.message) {
				t.Errorf("状态/诊断 = %d %s，期望 400 %s", w.Code, w.Body.String(), tc.message)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Error("错误响应缺少 no-store")
			}
			unchanged(t)
		})
	}
}

// i8111Fixture 含目标与规则，覆盖固定合同的全部 58 条字段路径。
func i8111Fixture() string {
	return bundleWith(
		`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"x"}]`,
		`[{"host":"probe.test","protocol":"TCP","ports":"443","action":"ACCEPT","target_export_ids":[1],"comment":"","enable_ipv6":false}]`,
	)
}

// i8111Fields 按对象/数组结构收集字段路径；不依赖生产 DTO 或解析器元数据。
func i8111Fields(t *testing.T, raw json.RawMessage, path []string) [][]string {
	t.Helper()
	var paths [][]string
	switch raw[0] {
	case '{':
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatal(err)
		}
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			next := append(slices.Clone(path), key)
			paths = append(paths, next)
			paths = append(paths, i8111Fields(t, object[key], next)...)
		}
	case '[':
		var array []json.RawMessage
		if err := json.Unmarshal(raw, &array); err != nil {
			t.Fatal(err)
		}
		for i, value := range array {
			paths = append(paths, i8111Fields(t, value, append(slices.Clone(path), strconv.Itoa(i)))...)
		}
	}
	return paths
}

// i8111Edit 只编辑指定路径；重复字段直接拼入原对象，避免 map 编码抹去重复证据。
func i8111Edit(t *testing.T, raw json.RawMessage, path []string, mode string) json.RawMessage {
	t.Helper()
	var value any
	if raw[0] == '[' {
		var array []json.RawMessage
		if err := json.Unmarshal(raw, &array); err != nil {
			t.Fatal(err)
		}
		i, err := strconv.Atoi(path[0])
		if err != nil {
			t.Fatal(err)
		}
		array[i] = i8111Edit(t, array[i], path[1:], mode)
		value = array
	} else {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatal(err)
		}
		key := path[0]
		if len(path) > 1 {
			object[key] = i8111Edit(t, object[key], path[1:], mode)
		} else {
			switch mode {
			case "大写":
				object[strings.ToUpper(key)] = object[key]
				delete(object, key)
			case "缺失":
				delete(object, key)
			case "null":
				object[key] = json.RawMessage("null")
			case "重复":
				encoded, err := json.Marshal(object)
				if err != nil {
					t.Fatal(err)
				}
				return append(encoded[:len(encoded)-1], []byte(","+strconv.Quote(key)+":"+string(object[key])+"}")...)
			default:
				t.Fatalf("未知夹具编辑模式: %s", mode)
			}
		}
		value = object
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestI8111AllFieldPaths(t *testing.T) {
	e, unchanged := i8111Guard(t)
	base := json.RawMessage(i8111Fixture())
	paths := i8111Fields(t, base, nil)
	if len(paths) != 58 {
		t.Fatalf("合同字段路径 = %d，期望 58", len(paths))
	}
	for _, mode := range []string{"大写", "重复", "缺失", "null"} {
		for _, path := range paths {
			t.Run(mode+"/"+strings.Join(path, "/"), func(t *testing.T) {
				w := e.do(t, http.MethodPost, "/api/config/import", string(i8111Edit(t, base, path, mode)))
				if w.Code != http.StatusBadRequest {
					t.Errorf("状态 = %d，期望 400", w.Code)
				}
				unchanged(t)
			})
		}
	}
}

func TestI8111ValidJSON(t *testing.T) {
	base := i8111Fixture()
	cases := []struct{ name, body string }{
		{"标准完整包", base},
		{"合法空数组", validBundle()},
		{"顶层字段转义", strings.Replace(base, `"version":3`, `"\u0076ersion":3`, 1)},
		{"数组字段转义", strings.Replace(base, `"export_id":1`, `"\u0065xport_id":1`, 1)},
		{"值转义", strings.Replace(base, `"tag":"auto-dns"`, `"tag":"auto-\u0064ns"`, 1)},
		{"合法代理项对", strings.Replace(base, `"comment":""`, `"comment":"\ud83d\ude00"`, 1)},
		{"前后空白", " \n" + base + "\t\r\n"},
		{"不同对象同名字段", bundleWith(`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"a"},{"export_id":2,"cloud_type":"tc_cvm","region":"gz","resource_id":"b"}]`, `[]`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			w := e.do(t, http.MethodPost, "/api/config/import", tc.body)
			if w.Code != http.StatusOK || e.applyCount() != 1 {
				t.Fatalf("合法配置未成功且仅发布一次: %d %s", w.Code, w.Body.String())
			}
			if e.snapshot() == nil || e.snapshot().Config.Tag != "auto-dns" {
				t.Error("运行时未采用解码后的设置")
			}
			if tc.name == "合法代理项对" {
				rules, err := e.store.GetRules()
				if err != nil || len(rules) != 1 || rules[0].Comment != "😀" {
					t.Error("合法代理项对未按原值写入")
				}
			}
		})
	}
}

func TestI8111BodyLimit(t *testing.T) {
	base := validBundle()
	t.Run("恰好10MiB", func(t *testing.T) {
		e := newTestEnv(t)
		body := base + strings.Repeat(" ", maxImportBodyBytes-len(base))
		w := e.do(t, http.MethodPost, "/api/config/import", body)
		if w.Code != http.StatusOK || e.applyCount() != 1 {
			t.Fatalf("恰好上限应允许: %d", w.Code)
		}
	})
	e, unchanged := i8111Guard(t)
	for _, prefix := range []string{base, `{"unknown":true}`, `not-json`} {
		t.Run(prefix[:8], func(t *testing.T) {
			body := prefix + strings.Repeat(" ", maxImportBodyBytes+1-len(prefix))
			w := e.do(t, http.MethodPost, "/api/config/import", body)
			if w.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("超限必须先返回 413: %d", w.Code)
			}
			unchanged(t)
		})
	}
}

type i8111FailReader struct{}

func (i8111FailReader) Read([]byte) (int, error) { return 0, errors.New("I8111_READ_SECRET") }
func (i8111FailReader) Close() error             { return nil }

func TestI8111SafeErrors(t *testing.T) {
	e, unchanged := i8111Guard(t)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	secret := "I8111_VALUE_SECRET"
	for _, tc := range []struct{ body, message string }{
		{strings.Replace(validBundle(), `"version":3`, `"version":"`+secret+`"`, 1), "/version"},
		{strings.Replace(validBundle(), `"password":""`, `"password":{"secret":"`+secret+`"}`, 1), "/alerts/email/password"},
		{strings.Replace(validBundle(), `"password":""`, `"password":"`+secret+`","password":""`, 1), "重复字段"},
		{strings.TrimSuffix(validBundle(), "}") + `,"extra":"` + secret + `"}`, "/extra"},
	} {
		w := e.do(t, http.MethodPost, "/api/config/import", tc.body)
		if w.Code != 400 || !strings.Contains(w.Body.String(), tc.message) || strings.Contains(w.Body.String(), secret) {
			t.Errorf("错误应只有字段路径与安全原因: %d %s", w.Code, w.Body.String())
		}
		unchanged(t)
	}
	// 标准库的整数溢出诊断与 JSONValue 都包含原始数字，必须明确屏蔽。
	const numericSecret = "922337203685477580812345"
	overflow := e.do(t, http.MethodPost, "/api/config/import", strings.Replace(validBundle(), `"version":3`, `"version":`+numericSecret, 1))
	if overflow.Code != 400 || !strings.Contains(overflow.Body.String(), "/version") || strings.Contains(overflow.Body.String(), numericSecret) {
		t.Errorf("整数溢出错误泄露原值或丢失字段定位: %d %s", overflow.Code, overflow.Body.String())
	}
	unchanged(t)
	req := httptest.NewRequest(http.MethodPost, "/api/config/import", nil)
	req.Body = i8111FailReader{}
	w := httptest.NewRecorder()
	e.deps.handleConfigImport(w, req)
	if w.Code != 400 || strings.Contains(w.Body.String(), "I8111_READ_SECRET") || logs.Len() != 0 {
		t.Errorf("读取/解码失败不得泄露原文或记录负载: %d %s", w.Code, w.Body.String())
	}
	unchanged(t)
}

// TestI8111RejectBeforeCoordinator 锁住协调器，证明非法 JSON 在事务入口前返回。
func TestI8111RejectBeforeCoordinator(t *testing.T) {
	e := newTestEnv(t)
	e.deps.Coord.mu.Lock()
	done := make(chan int, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/api/config/import", strings.NewReader(strings.Replace(validBundle(), `"version":3`, `"version":3,"version":3`, 1)))
		w := httptest.NewRecorder()
		e.deps.handleConfigImport(w, req)
		done <- w.Code
	}()
	select {
	case code := <-done:
		e.deps.Coord.mu.Unlock()
		if code != 400 {
			t.Fatalf("非法 JSON 状态 = %d，期望 400", code)
		}
	case <-time.After(time.Second):
		e.deps.Coord.mu.Unlock()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("释放协调器后请求仍未退出")
		}
		t.Fatal("非法 JSON 进入了配置协调器")
	}
}

// TestI8111OrdinaryAPIBoundary 本项只收紧导入，不改变普通 API 的解码语义。
func TestI8111OrdinaryAPIBoundary(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodPut, "/api/settings", `{"TAG":"first","tag":"last"}`)
	if w.Code != http.StatusOK || e.snapshot().Config.Tag != "last" {
		t.Fatalf("普通 API 语义被扩大修改: %d", w.Code)
	}
}

// 保证故障读取夹具实现完整请求体接口。
var _ io.ReadCloser = i8111FailReader{}
