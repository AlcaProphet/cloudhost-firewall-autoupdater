package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// 本文件是 Build8 Step 3 的 API 回归：站点 CRUD、完整运行快照发布、
// 目标引用保护、站内实际资源别名、端口/地址族校验和事务回滚。

func edgeOneSiteBody(zone string, targets []int, protocol, ports, family string) string {
	rawTargets, _ := json.Marshal(targets)
	return fmt.Sprintf(`{"zone_id":%q,"target_ids":%s,"protocol":%q,"ports":%q,"address_family":%q}`,
		zone, rawTargets, protocol, ports, family)
}

func decodeEdgeOneSites(t *testing.T, body []byte) []config.EdgeOneSite {
	t.Helper()
	var envelope struct {
		Sites []config.EdgeOneSite `json:"sites"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("解析站点列表失败: %v; body=%s", err, body)
	}
	return envelope.Sites
}

func TestEdgeOneSitesEmptyListShape(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodGet, "/api/edgeone/sites", "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if string(raw["sites"]) != "[]" {
		t.Errorf("空站点列表必须是 []，实际 %s", raw["sites"])
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("只读 GET 不得发布，实际 %d", got)
	}
}

func TestEdgeOneSiteCRUDAndPublication(t *testing.T) {
	e := newTestEnv(t)
	a := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-a")
	b := e.seedTarget(t, config.CloudAliECS, "cn-hangzhou", "sg-b")
	c := e.seedTarget(t, config.CloudTCLighthouse, "ap-shanghai", "lhins-c")

	w := e.do(t, http.MethodPost, "/api/edgeone/sites",
		edgeOneSiteBody("zone-b", []int{a, b}, "TCP", "80,443", "ipv4"))
	if w.Code != http.StatusCreated {
		t.Fatalf("创建 zone-b 状态码 = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Fatalf("创建站点应发布一次，实际 %d", got)
	}
	st := e.snapshot()
	if st == nil || len(st.Config.EdgeOneSites) != 1 ||
		st.Config.EdgeOneSites[0].ZoneID != "zone-b" ||
		len(st.Config.EdgeOneSites[0].TargetIDs) != 2 {
		t.Fatalf("运行时未发布站点: %+v", st)
	}

	w = e.do(t, http.MethodPost, "/api/edgeone/sites",
		edgeOneSiteBody("zone-a", []int{c}, "UDP", "53", "dual"))
	if w.Code != http.StatusCreated {
		t.Fatalf("创建 zone-a 状态码 = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 2 {
		t.Errorf("第二个站点应再发布一次，实际 %d", got)
	}

	w = e.do(t, http.MethodGet, "/api/edgeone/sites", "")
	sites := decodeEdgeOneSites(t, w.Body.Bytes())
	if len(sites) != 2 || sites[0].ZoneID != "zone-a" || sites[1].ZoneID != "zone-b" {
		t.Fatalf("站点列表必须按 zone_id 稳定排序: %+v", sites)
	}
	if len(sites[1].TargetIDs) != 2 || sites[1].TargetIDs[0] != a || sites[1].TargetIDs[1] != b {
		t.Errorf("zone-b 绑定目标不正确: %+v", sites[1].TargetIDs)
	}

	w = e.do(t, http.MethodPost, "/api/edgeone/sites",
		edgeOneSiteBody("zone-b", []int{a}, "TCP", "80", "ipv4"))
	if w.Code != http.StatusConflict {
		t.Errorf("重复站点状态码 = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 2 {
		t.Errorf("重复站点不得发布，实际 %d", got)
	}

	w = e.do(t, http.MethodPut, "/api/edgeone/sites/zone-b",
		edgeOneSiteBody("zone-b", []int{b, c}, "TCP", "8443", "ipv6"))
	if w.Code != http.StatusOK {
		t.Fatalf("更新 zone-b 状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 3 {
		t.Errorf("更新站点应发布一次，实际 %d", got)
	}
	if st := e.snapshot(); st == nil || len(st.Config.EdgeOneSites) != 2 {
		t.Errorf("更新后运行时站点数量不正确: %+v", st)
	}
	w = e.do(t, http.MethodGet, "/api/edgeone/sites", "")
	sites = decodeEdgeOneSites(t, w.Body.Bytes())
	var zoneB *config.EdgeOneSite
	for i := range sites {
		if sites[i].ZoneID == "zone-b" {
			zoneB = &sites[i]
		}
	}
	if zoneB == nil || len(zoneB.TargetIDs) != 2 || zoneB.TargetIDs[0] != b || zoneB.TargetIDs[1] != c ||
		zoneB.Protocol != "TCP" || zoneB.Ports != "8443" || zoneB.AddressFamily != "ipv6" {
		t.Fatalf("zone-b 更新未全量替换: %+v", zoneB)
	}

	if w := e.do(t, http.MethodPut, "/api/edgeone/sites/missing",
		edgeOneSiteBody("missing", []int{a}, "TCP", "80", "ipv4")); w.Code != http.StatusNotFound {
		t.Errorf("更新缺失站点状态码 = %d, want 404", w.Code)
	}
	if w := e.do(t, http.MethodPut, "/api/edgeone/sites/zone-b",
		edgeOneSiteBody("zone-a", []int{a}, "TCP", "80", "ipv4")); w.Code != http.StatusBadRequest {
		t.Errorf("路径与请求体 zone_id 冲突状态码 = %d, want 400", w.Code)
	}
	if got := e.applyCount(); got != 3 {
		t.Errorf("失败更新不得发布，实际 %d", got)
	}

	if w := e.do(t, http.MethodDelete, "/api/edgeone/sites/zone-a", ""); w.Code != http.StatusOK {
		t.Errorf("删除 zone-a 状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 4 {
		t.Errorf("删除站点应发布一次，实际 %d", got)
	}
	if w := e.do(t, http.MethodDelete, "/api/edgeone/sites/zone-a", ""); w.Code != http.StatusNotFound {
		t.Errorf("重复删除站点状态码 = %d, want 404", w.Code)
	}
	if w := e.do(t, http.MethodDelete, pathWithID("/api/targets/", c), ""); w.Code != http.StatusConflict {
		t.Errorf("删除被站点绑定的目标状态码 = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	if w := e.do(t, http.MethodDelete, pathWithID("/api/targets/", a), ""); w.Code != http.StatusOK {
		t.Errorf("删除未被绑定的目标状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if w := e.do(t, http.MethodDelete, "/api/edgeone/sites/zone-b", ""); w.Code != http.StatusOK {
		t.Errorf("删除 zone-b 状态码 = %d, want 200", w.Code)
	}
	if w := e.do(t, http.MethodDelete, pathWithID("/api/targets/", b), ""); w.Code != http.StatusOK {
		t.Errorf("站点删除后应可删除目标: %d; body=%s", w.Code, w.Body.String())
	}
}

func TestEdgeOneSiteValidationAndRollback(t *testing.T) {
	e := newTestEnv(t)
	a := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-a")
	b := e.seedTarget(t, config.CloudAliECS, "cn-hangzhou", "sg-b")
	aliasA := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-a")

	invalidPosts := map[string]string{
		"空目标列表":        edgeOneSiteBody("zone-bad", []int{}, "TCP", "80", "ipv4"),
		"缺目标列表":        `{"zone_id":"zone-bad","protocol":"TCP","ports":"80","address_family":"ipv4"}`,
		"重复本地目标":       edgeOneSiteBody("zone-bad", []int{a, a}, "TCP", "80", "ipv4"),
		"悬空目标":         edgeOneSiteBody("zone-bad", []int{a, 9999}, "TCP", "80", "ipv4"),
		"实际资源别名":       edgeOneSiteBody("zone-bad", []int{a, aliasA}, "TCP", "80", "ipv4"),
		"空端口":          edgeOneSiteBody("zone-bad", []int{a}, "TCP", "", "ipv4"),
		"端口为零":         edgeOneSiteBody("zone-bad", []int{a}, "TCP", "0", "ipv4"),
		"端口超限":         edgeOneSiteBody("zone-bad", []int{a}, "TCP", "65536", "ipv4"),
		"端口范围反向":       edgeOneSiteBody("zone-bad", []int{a}, "TCP", "90-80", "ipv4"),
		"端口空项":         edgeOneSiteBody("zone-bad", []int{a}, "TCP", "80,,443", "ipv4"),
		"ALL 混用":       edgeOneSiteBody("zone-bad", []int{a}, "TCP", "ALL,80", "ipv4"),
		"非法协议":         edgeOneSiteBody("zone-bad", []int{a}, "SCTP", "80", "ipv4"),
		"非法地址族":        edgeOneSiteBody("zone-bad", []int{a}, "TCP", "80", "ipx"),
		"ZoneId 路径分隔符": edgeOneSiteBody("zone/bad", []int{a}, "TCP", "80", "ipv4"),
		"动作不在请求体":      `{"zone_id":"zone-bad","target_ids":[` + fmt.Sprint(a) + `],"protocol":"TCP","ports":"80","address_family":"ipv4","action":"DROP"}`,
	}
	for name, body := range invalidPosts {
		t.Run(name, func(t *testing.T) {
			w := e.do(t, http.MethodPost, "/api/edgeone/sites", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if got := e.applyCount(); got != 0 {
				t.Errorf("非法创建不得发布，实际 %d", got)
			}
			w = e.do(t, http.MethodGet, "/api/edgeone/sites", "")
			if sites := decodeEdgeOneSites(t, w.Body.Bytes()); len(sites) != 0 {
				t.Errorf("非法创建不得写库: %+v", sites)
			}
		})
	}

	// 合法创建后，非法更新必须完整回滚。
	w := e.do(t, http.MethodPost, "/api/edgeone/sites",
		edgeOneSiteBody("zone-1", []int{a}, "TCP", "443", "ipv4"))
	if w.Code != http.StatusCreated {
		t.Fatalf("合法创建状态码 = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	invalidUpdates := map[string]string{
		"空目标":    edgeOneSiteBody("zone-1", []int{}, "TCP", "443", "ipv4"),
		"悬空目标":   edgeOneSiteBody("zone-1", []int{9999}, "TCP", "443", "ipv4"),
		"实际资源别名": edgeOneSiteBody("zone-1", []int{a, aliasA}, "TCP", "443", "ipv4"),
		"空端口":    edgeOneSiteBody("zone-1", []int{b}, "TCP", "", "ipv4"),
	}
	for name, body := range invalidUpdates {
		t.Run("更新/"+name, func(t *testing.T) {
			w := e.do(t, http.MethodPut, "/api/edgeone/sites/zone-1", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if got := e.applyCount(); got != 1 {
				t.Errorf("非法更新不得发布，实际 %d", got)
			}
			w = e.do(t, http.MethodGet, "/api/edgeone/sites", "")
			sites := decodeEdgeOneSites(t, w.Body.Bytes())
			if len(sites) != 1 || len(sites[0].TargetIDs) != 1 || sites[0].TargetIDs[0] != a {
				t.Errorf("非法更新必须保留旧绑定: %+v", sites)
			}
		})
	}

	w = e.do(t, http.MethodPut, "/api/edgeone/sites/zone-1",
		edgeOneSiteBody("zone-1", []int{b}, "UDP", "53", "ipv6"))
	if w.Code != http.StatusOK {
		t.Fatalf("合法更新状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 2 {
		t.Errorf("合法更新应发布一次，实际 %d", got)
	}
	w = e.do(t, http.MethodGet, "/api/edgeone/sites", "")
	sites := decodeEdgeOneSites(t, w.Body.Bytes())
	if len(sites) != 1 || len(sites[0].TargetIDs) != 1 || sites[0].TargetIDs[0] != b {
		t.Errorf("更新未替换绑定: %+v", sites)
	}
}

func TestEdgeOneSiteTransactionRollbackOnBindingFailure(t *testing.T) {
	e := newTestEnv(t)
	a := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-a")
	e.execRaw(t, `CREATE TRIGGER fail_edgeone_binding BEFORE INSERT ON edgeone_site_targets
		BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`)

	w := e.do(t, http.MethodPost, "/api/edgeone/sites",
		edgeOneSiteBody("zone-fail", []int{a}, "TCP", "443", "ipv4"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("绑定写入失败状态码 = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "fixture failure") {
		t.Errorf("500 不得回显底层错误: %s", w.Body.String())
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("事务失败不得发布，实际 %d", got)
	}
	w = e.do(t, http.MethodGet, "/api/edgeone/sites", "")
	if sites := decodeEdgeOneSites(t, w.Body.Bytes()); len(sites) != 0 {
		t.Errorf("站点主行必须随绑定失败回滚: %+v", sites)
	}
}

func TestTargetUpdateAliasRejectedBySiteBindings(t *testing.T) {
	e := newTestEnv(t)
	a := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-1")
	b := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-2")
	w := e.do(t, http.MethodPost, "/api/edgeone/sites",
		edgeOneSiteBody("zone-alias", []int{a, b}, "TCP", "443", "ipv4"))
	if w.Code != http.StatusCreated {
		t.Fatalf("创建站点状态码 = %d, want 201; body=%s", w.Code, w.Body.String())
	}

	// 把 B 改成与 A 相同的实际资源：最终完整快照必须拒绝，且旧状态保持。
	body := `{"cloud_type":"tc_cvm","region":"ap-guangzhou","resource_id":"sg-1"}`
	w = e.do(t, http.MethodPut, pathWithID("/api/targets/", b), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("目标别名更新状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("目标别名更新不得发布，实际 %d", got)
	}
	targets, err := e.store.GetTargets()
	if err != nil || len(targets) != 2 || targets[1].ResourceID != "sg-2" {
		t.Errorf("目标更新必须回滚: %+v err=%v", targets, err)
	}

	body = `{"cloud_type":"tc_cvm","region":"ap-guangzhou","resource_id":"sg-3"}`
	w = e.do(t, http.MethodPut, pathWithID("/api/targets/", b), body)
	if w.Code != http.StatusOK {
		t.Fatalf("目标正常更新状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 2 {
		t.Errorf("目标正常更新应发布一次，实际 %d", got)
	}
}

func TestEdgeOneSitesResetAndSharedTarget(t *testing.T) {
	e := newTestEnv(t)
	a := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-shared")
	for _, zone := range []string{"zone-1", "zone-2"} {
		w := e.do(t, http.MethodPost, "/api/edgeone/sites",
			edgeOneSiteBody(zone, []int{a}, "TCP", "80", "ipv4"))
		if w.Code != http.StatusCreated {
			t.Fatalf("创建 %s 状态码 = %d, want 201; body=%s", zone, w.Code, w.Body.String())
		}
	}
	w := e.do(t, http.MethodPost, "/api/config/reset", `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("reset 状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	w = e.do(t, http.MethodGet, "/api/edgeone/sites", "")
	if sites := decodeEdgeOneSites(t, w.Body.Bytes()); len(sites) != 0 {
		t.Errorf("reset 后站点应为空: %+v", sites)
	}
	if st := e.snapshot(); st == nil || len(st.Config.EdgeOneSites) != 0 {
		t.Errorf("reset 后运行时站点应为空: %+v", st)
	}
}
