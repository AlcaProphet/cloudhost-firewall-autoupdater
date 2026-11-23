//go:build step16

package process

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
	"golang.org/x/net/dns/dnsmessage"
)

// 本文件是 Build8 Step 16 的进程级验收夹具（仅 `-tags step16` 编译）。
//
// 边界：
//   - 子进程由当前工作树构建，入口与生产完全一致；仅通过 -ldflags 把 TEO /
//     Lighthouse 端点与 scheme 指向回环 HTTP 替身，生产构建不带该参数。
//   - 云侧是状态化替身，不访问真实腾讯云；DNS 是本地随机回环 UDP 服务。
//   - 临时 FWALIZER_DATA_DIR 由 t.TempDir() 提供；重启场景沿用同一目录。
//
// 该构建标签不进入 `go test ./...`、`make all`、CI 或发布产物。

const step16ModulePath = "github.com/alcaprophet/cloudhost-firewall-autoupdater"

// ───────────────────────── 假腾讯云 API ─────────────────────────

type step16Zone struct {
	Status        string
	CurrentVer    string
	CurrentV4     []string
	CurrentV6     []string
	NextVer       string
	NextV4        []string
	NextV6        []string
	PlannedAt     string
	DescribeMode  string // "" / "incomplete" / "permission"
	ConfirmMode   string // "" / "error" / "block"
	DescribeCount int
	ConfirmCount  int
	LastConfirmID string

	// MutateOnDescribe 在指定 1-based Describe 次数上应用一次状态变化，
	// 用于“确认前重查发现版本/网段变化”的反向用例。
	MutateOnDescribe int
	MutateCurrentVer string
	MutateCurrentV4  []string
	MutateCurrentV6  []string
	MutateNextVer    string
	MutateNextV4     []string
	MutateNextV6     []string
	MutateNextClear  bool

	confirmStarted  bool
	confirmReleased bool
	confirmRelease  chan struct{}
}

type step16LHRule struct {
	Protocol    string
	Port        string
	Cidr        string
	Ipv6        string
	Action      string
	Description string
}

type step16Instance struct {
	Version       uint64
	Rules         []step16LHRule
	DescribeCalls int
	CreateCalls   int
	DeleteCalls   int
	// IncompleteDescribeCalls 是 1-based Describe 调用序号；命中时返回空规则集，
	// 用于让 S1 缺项而不污染 S0。
	IncompleteDescribeCalls map[int]bool
	// Step 7：仅改变指定回读的端口，模拟云端未完整写入组合端口。
	OmitPortOnDescribe map[int]string
	DeleteError        bool
}

type step16Cloud struct {
	teo *httptest.Server
	lh  *httptest.Server

	mu        sync.Mutex
	zones     map[string]*step16Zone
	instances map[string]*step16Instance
	actions   []string
}

func newStep16Cloud(t *testing.T) *step16Cloud {
	t.Helper()
	c := &step16Cloud{zones: map[string]*step16Zone{}, instances: map[string]*step16Instance{}}
	c.teo = httptest.NewServer(http.HandlerFunc(c.handleTEO))
	c.lh = httptest.NewServer(http.HandlerFunc(c.handleLighthouse))
	t.Cleanup(func() {
		c.teo.Close()
		c.lh.Close()
	})
	return c
}

func (c *step16Cloud) teoAddr() string { return strings.TrimPrefix(c.teo.URL, "http://") }
func (c *step16Cloud) lhAddr() string  { return strings.TrimPrefix(c.lh.URL, "http://") }

func (c *step16Cloud) setZone(zoneID string, zone step16Zone) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := zone
	cp.CurrentV4 = append([]string(nil), zone.CurrentV4...)
	cp.CurrentV6 = append([]string(nil), zone.CurrentV6...)
	cp.NextV4 = append([]string(nil), zone.NextV4...)
	cp.NextV6 = append([]string(nil), zone.NextV6...)
	if cp.Status == "" {
		cp.Status = "online"
	}
	if cp.PlannedAt == "" {
		cp.PlannedAt = "2030-01-02T03:04:05+08:00"
	}
	// 替换运行状态不应重置请求计数；计数属于替身观测，不属于当前快照。
	if old := c.zones[zoneID]; old != nil {
		cp.DescribeCount = old.DescribeCount
		cp.ConfirmCount = old.ConfirmCount
		cp.LastConfirmID = old.LastConfirmID
	}
	c.zones[zoneID] = &cp
}

func (c *step16Cloud) patchZone(zoneID string, fn func(*step16Zone)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	zone := c.zones[zoneID]
	if zone == nil {
		zone = &step16Zone{Status: "online", PlannedAt: "2030-01-02T03:04:05+08:00"}
		c.zones[zoneID] = zone
	}
	fn(zone)
}

func (c *step16Cloud) setInstance(instanceID string, inst step16Instance) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if inst.Version == 0 {
		inst.Version = 1
	}
	cp := inst
	cp.Rules = append([]step16LHRule(nil), inst.Rules...)
	cp.IncompleteDescribeCalls = map[int]bool{}
	for k, v := range inst.IncompleteDescribeCalls {
		cp.IncompleteDescribeCalls[k] = v
	}
	cp.OmitPortOnDescribe = map[int]string{}
	for k, v := range inst.OmitPortOnDescribe {
		cp.OmitPortOnDescribe[k] = v
	}
	c.instances[instanceID] = &cp
}

func (c *step16Cloud) instanceSnapshot(instanceID string) (step16Instance, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	inst := c.instances[instanceID]
	if inst == nil {
		return step16Instance{}, false
	}
	cp := *inst
	cp.Rules = append([]step16LHRule(nil), inst.Rules...)
	return cp, true
}

func (c *step16Cloud) teoCounts(zoneID string) (describe, confirm int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if zone := c.zones[zoneID]; zone != nil {
		return zone.DescribeCount, zone.ConfirmCount
	}
	return 0, 0
}

func (c *step16Cloud) lhCounts(instanceID string) (describe, create, remove int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if inst := c.instances[instanceID]; inst != nil {
		return inst.DescribeCalls, inst.CreateCalls, inst.DeleteCalls
	}
	return 0, 0, 0
}

func (c *step16Cloud) totalLHCounts() (describe, create, remove int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, inst := range c.instances {
		describe += inst.DescribeCalls
		create += inst.CreateCalls
		remove += inst.DeleteCalls
	}
	return describe, create, remove
}

func (c *step16Cloud) actionLog() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.actions...)
}

func (c *step16Cloud) record(action string) {
	c.actions = append(c.actions, time.Now().UTC().Format(time.RFC3339Nano)+" "+action)
}

func (c *step16Cloud) waitConfirmStarted(zoneID string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		started := c.zones[zoneID] != nil && c.zones[zoneID].confirmStarted
		c.mu.Unlock()
		if started {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func (c *step16Cloud) releaseConfirm(zoneID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	zone := c.zones[zoneID]
	if zone == nil || zone.confirmRelease == nil || zone.confirmReleased {
		return
	}
	close(zone.confirmRelease)
	zone.confirmReleased = true
}

func (c *step16Cloud) handleTEO(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	action := r.Header.Get("X-TC-Action")
	var req struct {
		ZoneId string `json:"ZoneId"`
	}
	_ = json.Unmarshal(body, &req)
	zoneID := strings.TrimSpace(req.ZoneId)

	c.mu.Lock()
	zone := c.zones[zoneID]
	if zone == nil {
		zone = &step16Zone{Status: "online", PlannedAt: "2030-01-02T03:04:05+08:00"}
		c.zones[zoneID] = zone
	}
	c.record("teo " + action + " zone=" + zoneID)
	switch action {
	case "DescribeOriginACL":
		zone.DescribeCount++
		c.applyTEOMutationLocked(zone)
		mode := zone.DescribeMode
		count := zone.DescribeCount
		requestID := fmt.Sprintf("req-teo-describe-%s-%d", zoneID, count)
		if mode == "incomplete" {
			c.mu.Unlock()
			writeTCJSON(w, map[string]any{"Response": map[string]any{"RequestId": requestID}})
			return
		}
		if mode == "permission" {
			c.mu.Unlock()
			writeTCJSON(w, map[string]any{"Response": map[string]any{
				"Error":     map[string]any{"Code": "UnauthorizedOperation", "Message": "simulated permission denied"},
				"RequestId": requestID,
			}})
			return
		}
		payload := c.teoInfoLocked(zone, requestID)
		c.mu.Unlock()
		writeTCJSON(w, map[string]any{"Response": payload})
	case "ConfirmOriginACLUpdate":
		zone.ConfirmCount++
		mode := zone.ConfirmMode
		requestID := fmt.Sprintf("req-teo-confirm-%s-%d", zoneID, zone.ConfirmCount)
		if mode == "block" {
			zone.confirmStarted = true
			if zone.confirmRelease == nil {
				zone.confirmRelease = make(chan struct{})
			}
			release := zone.confirmRelease
			c.mu.Unlock()
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			c.mu.Lock()
			zone.LastConfirmID = requestID
			c.mu.Unlock()
			writeTCJSON(w, map[string]any{"Response": map[string]any{"RequestId": requestID}})
			return
		}
		if mode == "error" {
			c.mu.Unlock()
			writeTCJSON(w, map[string]any{"Response": map[string]any{
				"Error":     map[string]any{"Code": "InternalError", "Message": "simulated unknown"},
				"RequestId": requestID,
			}})
			return
		}
		zone.LastConfirmID = requestID
		c.mu.Unlock()
		writeTCJSON(w, map[string]any{"Response": map[string]any{"RequestId": requestID}})
	default:
		c.mu.Unlock()
		writeTCJSON(w, map[string]any{"Response": map[string]any{
			"Error":     map[string]any{"Code": "InvalidAction", "Message": "unsupported"},
			"RequestId": "req-teo-unknown",
		}})
	}
}

func (c *step16Cloud) applyTEOMutationLocked(zone *step16Zone) {
	if zone.MutateOnDescribe == 0 || zone.DescribeCount != zone.MutateOnDescribe {
		return
	}
	if zone.MutateCurrentVer != "" {
		zone.CurrentVer = zone.MutateCurrentVer
	}
	if zone.MutateCurrentV4 != nil {
		zone.CurrentV4 = append([]string(nil), zone.MutateCurrentV4...)
	}
	if zone.MutateCurrentV6 != nil {
		zone.CurrentV6 = append([]string(nil), zone.MutateCurrentV6...)
	}
	if zone.MutateNextClear {
		zone.NextVer = ""
		zone.NextV4 = nil
		zone.NextV6 = nil
	} else {
		if zone.MutateNextVer != "" {
			zone.NextVer = zone.MutateNextVer
		}
		if zone.MutateNextV4 != nil {
			zone.NextV4 = append([]string(nil), zone.MutateNextV4...)
		}
		if zone.MutateNextV6 != nil {
			zone.NextV6 = append([]string(nil), zone.MutateNextV6...)
		}
	}
}

func (c *step16Cloud) teoInfoLocked(zone *step16Zone, requestID string) map[string]any {
	current := map[string]any{
		"Version": zone.CurrentVer,
		"EntireAddresses": map[string]any{
			"IPv4": append([]string{}, zone.CurrentV4...),
			"IPv6": append([]string{}, zone.CurrentV6...),
		},
	}
	info := map[string]any{
		"Status":           zone.Status,
		"CurrentOriginACL": current,
	}
	if zone.NextVer != "" {
		info["NextOriginACL"] = map[string]any{
			"Version":           zone.NextVer,
			"PlannedActiveTime": zone.PlannedAt,
			"EntireAddresses": map[string]any{
				"IPv4": append([]string{}, zone.NextV4...),
				"IPv6": append([]string{}, zone.NextV6...),
			},
		}
	}
	return map[string]any{"OriginACLInfo": info, "RequestId": requestID}
}

func (c *step16Cloud) handleLighthouse(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	action := r.Header.Get("X-TC-Action")
	var req struct {
		InstanceId      string `json:"InstanceId"`
		FirewallVersion uint64 `json:"FirewallVersion"`
		Offset          uint64 `json:"Offset"`
		Limit           uint64 `json:"Limit"`
		FirewallRules   []struct {
			Protocol                string `json:"Protocol"`
			Port                    string `json:"Port"`
			CidrBlock               string `json:"CidrBlock"`
			Ipv6CidrBlock           string `json:"Ipv6CidrBlock"`
			Action                  string `json:"Action"`
			FirewallRuleDescription string `json:"FirewallRuleDescription"`
		} `json:"FirewallRules"`
	}
	_ = json.Unmarshal(body, &req)
	instanceID := strings.TrimSpace(req.InstanceId)

	c.mu.Lock()
	inst := c.instances[instanceID]
	if inst == nil {
		inst = &step16Instance{Version: 1, IncompleteDescribeCalls: map[int]bool{}}
		c.instances[instanceID] = inst
	}
	c.record("lighthouse " + action + " instance=" + instanceID)
	switch action {
	case "DescribeFirewallRules":
		inst.DescribeCalls++
		call := inst.DescribeCalls
		rules := append([]step16LHRule(nil), inst.Rules...)
		if inst.IncompleteDescribeCalls[call] {
			rules = []step16LHRule{}
		}
		for i := range rules {
			if port := inst.OmitPortOnDescribe[call]; port != "" && strings.HasPrefix(rules[i].Description, "[edgeone]") {
				rules[i].Port = port
			}
		}
		// SDK 使用 Offset/Limit 分页；满页必须由后续空页终止。
		start := min(int(req.Offset), len(rules))
		end := len(rules)
		if req.Limit > 0 {
			end = min(start+int(req.Limit), end)
		}
		rules = rules[start:end]
		version := inst.Version
		c.mu.Unlock()
		out := make([]map[string]any, 0, len(rules))
		for _, rule := range rules {
			item := map[string]any{
				"Protocol":                rule.Protocol,
				"Port":                    rule.Port,
				"Action":                  rule.Action,
				"FirewallRuleDescription": rule.Description,
			}
			if rule.Ipv6 != "" {
				item["Ipv6CidrBlock"] = rule.Ipv6
			} else {
				item["CidrBlock"] = rule.Cidr
			}
			out = append(out, item)
		}
		writeTCJSON(w, map[string]any{"Response": map[string]any{
			"FirewallRuleSet": out,
			"FirewallVersion": version,
			"RequestId":       fmt.Sprintf("req-lh-describe-%s-%d", instanceID, call),
		}})
	case "CreateFirewallRules":
		if req.FirewallVersion != inst.Version {
			c.mu.Unlock()
			writeTCJSON(w, map[string]any{"Response": map[string]any{
				"Error":     map[string]any{"Code": "UnsupportedOperation.FirewallVersionMismatch", "Message": "version mismatch"},
				"RequestId": "req-lh-create-mismatch",
			}})
			return
		}
		for _, r := range req.FirewallRules {
			rule := step16LHRule{Protocol: r.Protocol, Port: r.Port, Cidr: r.CidrBlock, Ipv6: r.Ipv6CidrBlock, Action: r.Action, Description: r.FirewallRuleDescription}
			if !step16ContainsRule(inst.Rules, rule) {
				inst.Rules = append(inst.Rules, rule)
			}
		}
		inst.Version++
		inst.CreateCalls++
		version := inst.Version
		c.mu.Unlock()
		writeTCJSON(w, map[string]any{"Response": map[string]any{
			"FirewallVersion": version,
			"RequestId":       fmt.Sprintf("req-lh-create-%s-%d", instanceID, version),
		}})
	case "DeleteFirewallRules":
		if inst.DeleteError {
			inst.DeleteCalls++
			c.mu.Unlock()
			writeTCJSON(w, map[string]any{"Response": map[string]any{"Error": map[string]any{"Code": "AuthFailure.UnauthorizedOperation", "Message": "fixture delete denied"}, "RequestId": "req-lh-delete-denied"}})
			return
		}
		if req.FirewallVersion != inst.Version {
			c.mu.Unlock()
			writeTCJSON(w, map[string]any{"Response": map[string]any{
				"Error":     map[string]any{"Code": "UnsupportedOperation.FirewallVersionMismatch", "Message": "version mismatch"},
				"RequestId": "req-lh-delete-mismatch",
			}})
			return
		}
		for _, r := range req.FirewallRules {
			rule := step16LHRule{Protocol: r.Protocol, Port: r.Port, Cidr: r.CidrBlock, Ipv6: r.Ipv6CidrBlock, Action: r.Action}
			for i, existing := range inst.Rules {
				if step16SameRule(existing, rule) {
					inst.Rules = append(inst.Rules[:i], inst.Rules[i+1:]...)
					break
				}
			}
		}
		inst.Version++
		inst.DeleteCalls++
		version := inst.Version
		c.mu.Unlock()
		writeTCJSON(w, map[string]any{"Response": map[string]any{
			"FirewallVersion": version,
			"RequestId":       fmt.Sprintf("req-lh-delete-%s-%d", instanceID, version),
		}})
	default:
		c.mu.Unlock()
		writeTCJSON(w, map[string]any{"Response": map[string]any{
			"Error":     map[string]any{"Code": "InvalidAction", "Message": "unsupported"},
			"RequestId": "req-lh-unknown",
		}})
	}
}

func writeTCJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func step16ContainsRule(rules []step16LHRule, want step16LHRule) bool {
	for _, rule := range rules {
		if step16SameRule(rule, want) {
			return true
		}
	}
	return false
}

func step16SameRule(a, b step16LHRule) bool {
	return a.Protocol == b.Protocol && a.Port == b.Port && a.Cidr == b.Cidr && a.Ipv6 == b.Ipv6 && a.Action == b.Action
}

// ───────────────────────── 假 DNS ─────────────────────────

type step16DNS struct {
	conn net.PacketConn
	done chan struct{}

	mu      sync.Mutex
	ip      net.IP
	mode    string
	queries int
	fault   error
	seen    chan struct{}
	once    sync.Once
}

func newStep16DNS(t *testing.T) *step16DNS {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动本地 DNS 失败: %v", err)
	}
	d := &step16DNS{conn: conn, done: make(chan struct{}), ip: net.ParseIP("192.0.2.10"), seen: make(chan struct{})}
	go d.serve()
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("关闭本地 DNS 失败: %v", err)
		}
		select {
		case <-d.done:
		case <-time.After(3 * time.Second):
			t.Error("本地 DNS 未退出")
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.fault != nil {
			t.Errorf("本地 DNS 错误: %v", d.fault)
		}
	})
	return d
}

func (d *step16DNS) addr() string { return d.conn.LocalAddr().String() }

func (d *step16DNS) setIP(ip string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ip = net.ParseIP(ip)
}

func (d *step16DNS) setMode(mode string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mode = mode
}

func (d *step16DNS) queryCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.queries
}

func (d *step16DNS) serve() {
	defer close(d.done)
	buf := make([]byte, 4096)
	for {
		n, peer, err := d.conn.ReadFrom(buf)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				d.recordFault(err)
			}
			return
		}
		var req dnsmessage.Message
		if err := req.Unpack(buf[:n]); err != nil {
			d.recordFault(err)
			return
		}
		if len(req.Questions) != 1 {
			d.recordFault(fmt.Errorf("DNS 问题数=%d", len(req.Questions)))
			return
		}
		q := req.Questions[0]
		d.mu.Lock()
		d.queries++
		mode := d.mode
		ip := append(net.IP(nil), d.ip...)
		d.mu.Unlock()
		d.once.Do(func() { close(d.seen) })
		if mode == "silent" {
			continue
		}
		reply := dnsmessage.Message{
			Header: dnsmessage.Header{
				ID: req.ID, Response: true, Authoritative: true,
				RecursionDesired: req.RecursionDesired, RecursionAvailable: true,
			},
			Questions: req.Questions,
		}
		switch mode {
		case "nxdomain":
			reply.RCode = dnsmessage.RCodeNameError
		case "servfail":
			reply.RCode = dnsmessage.RCodeServerFailure
		}
		if mode == "" || mode == "ok" {
			if q.Type == dnsmessage.TypeA && ip.To4() != nil {
				var a [4]byte
				copy(a[:], ip.To4())
				reply.Answers = append(reply.Answers, dnsmessage.Resource{
					Header: dnsmessage.ResourceHeader{Name: q.Name, Type: q.Type, Class: dnsmessage.ClassINET, TTL: 60},
					Body:   &dnsmessage.AResource{A: a},
				})
			}
		}
		packed, err := reply.Pack()
		if err != nil {
			d.recordFault(err)
			return
		}
		if _, err := d.conn.WriteTo(packed, peer); err != nil {
			d.recordFault(err)
			return
		}
	}
}

func (d *step16DNS) recordFault(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fault == nil {
		d.fault = err
	}
}

// ───────────────────────── 真实二进制进程夹具 ─────────────────────────

type step16Fixture struct {
	t       *testing.T
	cloud   *step16Cloud
	dns     *step16DNS
	binary  string
	dataDir string

	cmd  *exec.Cmd
	out  *syncBuffer
	port int
	base string

	targetIDs []int
}

func newStep16Fixture(t *testing.T) *step16Fixture {
	t.Helper()
	cloud := newStep16Cloud(t)
	dns := newStep16DNS(t)
	binary := buildStep16Binary(t, cloud.teoAddr(), cloud.lhAddr())
	f := &step16Fixture{t: t, cloud: cloud, dns: dns, binary: binary, dataDir: t.TempDir()}
	t.Cleanup(func() {
		if f.cmd != nil && f.cmd.Process != nil {
			_ = f.cmd.Process.Kill()
			_ = f.cmd.Wait()
			f.cmd = nil
		}
	})
	return f
}

func buildStep16Binary(t *testing.T, teoAddr, lhAddr string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "fwalizer-step16")
	ldflags := strings.Join([]string{
		"-X", step16ModulePath + "/provider.teoDefaultEndpoint=" + teoAddr,
		"-X", step16ModulePath + "/provider.teoDefaultScheme=HTTP",
		"-X", step16ModulePath + "/provider.tcLighthouseEndpoint=" + lhAddr,
		"-X", step16ModulePath + "/provider.tcLighthouseScheme=http",
	}, " ")
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", out, ".")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("构建 Step 16 测试二进制失败: %v", err)
	}
	return out
}

// setup 创建目标与业务设置。fn 在默认种子之后执行，可追加规则/站点或覆盖设置。
func (f *step16Fixture) setup(targets int, fn func(t *testing.T, store *config.Store, ids []int)) {
	f.t.Helper()
	store, err := config.OpenStore(filepath.Join(f.dataDir, "config.db"))
	if err != nil {
		f.t.Fatalf("打开临时 SQLite 失败: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			f.t.Errorf("关闭临时 SQLite 失败: %v", err)
		}
	}()

	ctx := context.Background()
	tx, err := store.BeginTx(ctx)
	if err != nil {
		f.t.Fatalf("开始目标播种事务失败: %v", err)
	}
	for i := 0; i < targets; i++ {
		resource := fmt.Sprintf("lhins-step16-%c", 'a'+i)
		id, err := store.AddTargetTx(ctx, tx, config.TargetConfig{
			CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: resource,
		})
		if err != nil {
			_ = tx.Rollback()
			f.t.Fatalf("写入目标 %s 失败: %v", resource, err)
		}
		f.targetIDs = append(f.targetIDs, int(id))
	}
	if err := tx.Commit(); err != nil {
		f.t.Fatalf("提交目标播种事务失败: %v", err)
	}

	if err := store.SetSetting("tc_access_id", "AKID-step16-fake"); err != nil {
		f.t.Fatalf("写入 tc_access_id 失败: %v", err)
	}
	if err := store.SetSetting("tc_access_key", "step16-fake-secret"); err != nil {
		f.t.Fatalf("写入 tc_access_key 失败: %v", err)
	}
	for key, value := range map[string]string{
		"tag": "auto-ddns", "interval": "5m",
		"dns": f.dns.addr(), "dns_timeout": "2s", "dns_fail_threshold": "5",
		"log_level": "debug", "theme": "light", "sync_enabled": "false",
		"dns_enabled": "true", "edgeone_enabled": "true",
		"edgeone_tag": "edgeone", "edgeone_interval": "72h",
	} {
		if err := store.SetSetting(key, value); err != nil {
			f.t.Fatalf("写入设置 %s 失败: %v", key, err)
		}
	}
	if fn != nil {
		fn(f.t, store, f.targetIDs)
	}
}

func (f *step16Fixture) start() {
	f.t.Helper()
	if f.cmd != nil {
		f.t.Fatal("进程已启动")
	}
	f.port = freePort(f.t)
	f.base = fmt.Sprintf("http://127.0.0.1:%d", f.port)
	cmd := exec.Command(f.binary)
	out := &syncBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = processEnv(f.dataDir, map[string]string{"WEBUI_PORT": strconv.Itoa(f.port)})
	if err := cmd.Start(); err != nil {
		f.t.Fatalf("启动 Step 16 进程失败: %v", err)
	}
	f.cmd = cmd
	f.out = out
	waitForHTTP(f.t, f.base+"/api/health")
	// 等待同步主循环进入运行态，确保后续触发/暂停不会撞启动窗口。
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		status, err := f.status()
		if err == nil && status.Running && status.StartedAt != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("等待同步主循环进入运行态超时；输出:\n%s", out.String())
}

func (f *step16Fixture) stop() {
	f.t.Helper()
	if f.cmd == nil {
		return
	}
	cmd := f.cmd
	started := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		f.t.Fatalf("向 Step 16 进程发送 SIGTERM 失败: %v", err)
	}
	code, err := waitForProcessExit(f.t, cmd, 30*time.Second)
	if err != nil {
		f.t.Fatalf("等待 Step 16 进程退出失败: %v\n输出:\n%s", err, f.out.String())
	}
	if code != 0 {
		f.t.Fatalf("Step 16 进程退出码 = %d, want 0\n输出:\n%s", code, f.out.String())
	}
	assertPortClosed(f.t, f.base+"/api/health")
	assertPidFileReleased(f.t, f.dataDir)
	f.t.Logf("STEP16 SIGTERM exit code=%d elapsed=%s", code, time.Since(started))
	f.cmd = nil
}

func (f *step16Fixture) restart() {
	f.t.Helper()
	f.stop()
	f.start()
}

func (f *step16Fixture) url(path string) string { return f.base + path }

func (f *step16Fixture) status() (syncer.SyncStatus, error) {
	var out syncer.SyncStatus
	err := step16GetJSON(f.base+"/api/sync/status", &out)
	return out, err
}

func (f *step16Fixture) edgeStatus() (syncer.EdgeOneStatus, error) {
	var out syncer.EdgeOneStatus
	err := step16GetJSON(f.base+"/api/edgeone/status", &out)
	return out, err
}

func (f *step16Fixture) settings() (map[string]string, error) {
	var out map[string]string
	err := step16GetJSON(f.base+"/api/settings", &out)
	return out, err
}

func (f *step16Fixture) health() (int, map[string]any, error) {
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(f.base + "/api/health/operational")
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

func (f *step16Fixture) trigger() int { return step16Post(f.t, f.base+"/api/sync/trigger") }
func (f *step16Fixture) pause() int   { return step16Post(f.t, f.base+"/api/sync/pause") }
func (f *step16Fixture) resume() int  { return step16Post(f.t, f.base+"/api/sync/resume") }

func (f *step16Fixture) putSettings(values map[string]string) int {
	f.t.Helper()
	body, _ := json.Marshal(values)
	return step16Request(f.t, http.MethodPut, f.base+"/api/settings", string(body))
}

func (f *step16Fixture) dryRun() (syncer.DryRunResponse, int) {
	f.t.Helper()
	body := step16RawRequest(f.t, http.MethodPost, f.base+"/api/sync/dryrun", "")
	var out syncer.DryRunResponse
	if err := json.Unmarshal(body, &out); err != nil {
		f.t.Fatalf("解析 Dry Run 响应失败: %v; body=%s", err, body)
	}
	return out, http.StatusOK
}

func (f *step16Fixture) waitIdle(timeout time.Duration) syncer.SyncStatus {
	f.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := f.status()
		if err == nil && status.RoundStartedAt == nil {
			return status
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("等待同步轮次空闲超时")
	return syncer.SyncStatus{}
}

func (f *step16Fixture) waitRoundAfter(prev *time.Time, timeout time.Duration) syncer.SyncStatus {
	f.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := f.status()
		if err == nil && status.RoundStartedAt == nil && status.LastSync != nil &&
			(prev == nil || status.LastSync.After(*prev)) {
			return status
		}
		time.Sleep(20 * time.Millisecond)
	}
	status, _ := f.status()
	f.t.Fatalf("等待同步轮次完成超时；status=%+v\n输出:\n%s", status, f.out.String())
	return syncer.SyncStatus{}
}

// waitSourceCompletedAfter 等待一个不产生 last_sync 的阻断轮次完成。
//
// 预检 overlap/known-conflict 在目标执行前返回，运行记录只体现在来源
// last_completed_at，因此不能只等 LastSync。
func (f *step16Fixture) waitSourceCompletedAfter(prev *time.Time, timeout time.Duration) syncer.SyncStatus {
	f.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := f.status()
		if err == nil && status.RoundStartedAt == nil && status.EdgeOne.LastCompletedAt != nil &&
			(prev == nil || status.EdgeOne.LastCompletedAt.After(*prev)) {
			return status
		}
		time.Sleep(20 * time.Millisecond)
	}
	status, _ := f.status()
	f.t.Fatalf("等待阻断轮次完成超时；status=%+v\n输出:\n%s", status, f.out.String())
	return syncer.SyncStatus{}
}

// waitDNSCompletedAfter 等待 DNS 来源完成一次阻断/正式轮次。
// waitTEOConfirmCount 等待指定站点的确认请求实际到达替身。
func (f *step16Fixture) waitTEOConfirmCount(zone string, want int, timeout time.Duration) {
	f.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, confirm := f.cloud.teoCounts(zone)
		if confirm >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, confirm := f.cloud.teoCounts(zone)
	f.t.Fatalf("等待 TEO 确认请求超时: zone=%s confirm=%d want=%d\n输出:\n%s", zone, confirm, want, f.out.String())
}

func (f *step16Fixture) waitDNSCompletedAfter(prev *time.Time, timeout time.Duration) syncer.SyncStatus {
	f.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := f.status()
		if err == nil && status.RoundStartedAt == nil && status.DNS.LastCompletedAt != nil &&
			(prev == nil || status.DNS.LastCompletedAt.After(*prev)) {
			return status
		}
		time.Sleep(20 * time.Millisecond)
	}
	status, _ := f.status()
	f.t.Fatalf("等待 DNS 来源完成超时；status=%+v\n输出:\n%s", status, f.out.String())
	return syncer.SyncStatus{}
}

func (f *step16Fixture) waitForEnabled(enabled bool, timeout time.Duration) syncer.SyncStatus {
	f.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := f.status()
		if err == nil && status.Enabled == enabled {
			return status
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("等待 enabled=%v 超时", enabled)
	return syncer.SyncStatus{}
}

func (f *step16Fixture) conflict() (*config.AddressConflict, error) {
	store, err := config.OpenStore(filepath.Join(f.dataDir, "config.db"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	return store.GetAddressConflict(context.Background())
}

func (f *step16Fixture) transition(zone string) (*config.EdgeOneTransition, error) {
	store, err := config.OpenStore(filepath.Join(f.dataDir, "config.db"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	return store.GetEdgeOneTransition(context.Background(), zone)
}

func (f *step16Fixture) syncLogs(limit int) ([]config.SyncLog, error) {
	store, err := config.OpenStore(filepath.Join(f.dataDir, "config.db"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	return store.GetSyncLogs(limit)
}

// waitSyncLogCount 等待异步日志 writer 落库至少 n 条目标记录。
func (f *step16Fixture) waitSyncLogCount(n int, timeout time.Duration) []config.SyncLog {
	f.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		logs, err := f.syncLogs(100)
		if err == nil && len(logs) >= n {
			return logs
		}
		time.Sleep(20 * time.Millisecond)
	}
	logs, _ := f.syncLogs(100)
	f.t.Fatalf("等待 sync_logs 落库超时: got=%d want>=%d", len(logs), n)
	return nil
}

func (f *step16Fixture) countRows(table string) (int, error) {
	db, err := sql.Open("sqlite", filepath.Join(f.dataDir, "config.db"))
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func step16GetJSON(url string, out any) error {
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s = %d: %s", url, resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func step16Post(t *testing.T, url string) int {
	t.Helper()
	return step16Request(t, http.MethodPost, url, "")
}

func step16Request(t *testing.T, method, url, body string) int {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("请求 %s 失败: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func step16RawRequest(t *testing.T, method, url, body string) []byte {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("请求 %s 失败: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取 %s 响应失败: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("请求 %s 状态码 = %d: %s", url, resp.StatusCode, raw)
	}
	return raw
}

func step16ContainsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// step16AddRule 在单个事务中写入域名规则，供 setup 回调使用。
func step16AddRule(t *testing.T, store *config.Store, rule config.DomainRule) {
	t.Helper()
	if err := store.WithTransaction(func(tx *sql.Tx) error {
		return store.AddRuleTx(context.Background(), tx, rule)
	}); err != nil {
		t.Fatalf("写入域名规则失败: %v", err)
	}
}

// step16AddSite 在单个事务中写入站点绑定，供 setup 回调使用。
func step16AddSite(t *testing.T, store *config.Store, site config.EdgeOneSite) {
	t.Helper()
	if err := store.WithTransaction(func(tx *sql.Tx) error {
		return store.AddEdgeOneSiteTx(context.Background(), tx, site)
	}); err != nil {
		t.Fatalf("写入 EdgeOne 站点失败: %v", err)
	}
}

// step16CreateLegacyDB 构造 Build8 之前的“已初始化旧库”最小形态：
// 只有 settings 表，OpenStore 必须补齐其余表并把 DNS TAG 迁移为 auto-ddns。
func step16CreateLegacyDB(t *testing.T, dir string, values map[string]string) {
	t.Helper()
	path := filepath.Join(dir, "config.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开旧库失败: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("关闭旧库失败: %v", err)
		}
	}()
	if _, err := db.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("创建旧库 settings 表失败: %v", err)
	}
	for key, value := range values {
		if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", key, value); err != nil {
			t.Fatalf("写入旧库设置 %s 失败: %v", key, err)
		}
	}
}
