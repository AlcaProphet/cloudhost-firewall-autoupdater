package config

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
)

// 本文件是 Build8 Step 3 的领域与 Store 回归：站点参数校验、绑定闭包、
// 站内实际资源别名、完整快照/运行时深拷贝、共享目标与 reset 清理。

func TestNormalizeEdgeOneSite(t *testing.T) {
	valid := EdgeOneSite{
		ZoneID:        " zone-AbC ",
		TargetIDs:     []int{2, 1},
		Protocol:      " tcp ",
		Ports:         " 80, 443,8000-8010 ",
		AddressFamily: " IPv4 ",
	}
	got, err := NormalizeEdgeOneSite(valid)
	if err != nil {
		t.Fatalf("合法站点被拒绝: %v", err)
	}
	want := EdgeOneSite{
		ZoneID: "zone-AbC", TargetIDs: []int{2, 1}, Protocol: "TCP",
		Ports: "80,443,8000-8010", AddressFamily: EdgeOneAddressFamilyIPv4,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("归一化站点 = %+v, want %+v", got, want)
	}
	// 返回的 TargetIDs 必须是独立切片。
	got.TargetIDs[0] = 99
	if valid.TargetIDs[0] != 2 {
		t.Errorf("TargetIDs 未深拷贝: %+v", valid.TargetIDs)
	}

	icmp, err := NormalizeEdgeOneSite(EdgeOneSite{
		ZoneID: "z", TargetIDs: []int{1}, Protocol: "icmp", Ports: "not-a-port", AddressFamily: "dual",
	})
	if err != nil || icmp.Ports != "ALL" || icmp.Protocol != "ICMP" {
		t.Errorf("ICMP 应归一化为 ALL: %+v err=%v", icmp, err)
	}

	base := EdgeOneSite{ZoneID: "z", TargetIDs: []int{1}, Protocol: "TCP", Ports: "80", AddressFamily: "ipv4"}
	cases := map[string]func(*EdgeOneSite){
		"空 ZoneId": func(s *EdgeOneSite) { s.ZoneID = "   " },
		"路径分隔符":    func(s *EdgeOneSite) { s.ZoneID = "a/b" },
		"反斜杠":      func(s *EdgeOneSite) { s.ZoneID = `a\b` },
		"控制字符":     func(s *EdgeOneSite) { s.ZoneID = "a\x00b" },
		"空目标列表":    func(s *EdgeOneSite) { s.TargetIDs = nil },
		"目标为零":     func(s *EdgeOneSite) { s.TargetIDs = []int{0} },
		"目标重复":     func(s *EdgeOneSite) { s.TargetIDs = []int{1, 1} },
		"非法协议":     func(s *EdgeOneSite) { s.Protocol = "SCTP" },
		"非法地址族":    func(s *EdgeOneSite) { s.AddressFamily = "ipx" },
		"空端口":      func(s *EdgeOneSite) { s.Ports = "  " },
		"端口零":      func(s *EdgeOneSite) { s.Ports = "0" },
		"端口超限":     func(s *EdgeOneSite) { s.Ports = "65536" },
		"端口非数字":    func(s *EdgeOneSite) { s.Ports = "abc" },
		"端口空项":     func(s *EdgeOneSite) { s.Ports = "80,,443" },
		"端口范围反向":   func(s *EdgeOneSite) { s.Ports = "90-80" },
		"端口范围缺终点":  func(s *EdgeOneSite) { s.Ports = "80-" },
		"ALL 混用":   func(s *EdgeOneSite) { s.Ports = "ALL,80" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			site := base
			site.TargetIDs = append([]int(nil), base.TargetIDs...)
			mutate(&site)
			if _, err := NormalizeEdgeOneSite(site); err == nil {
				t.Fatalf("非法站点被接受: %+v", site)
			}
		})
	}
}

func addConfigTarget(t *testing.T, store *Store, ct CloudType, region, resource string) int {
	t.Helper()
	var id int64
	if err := store.WithTransaction(func(tx *sql.Tx) error {
		newID, err := store.AddTargetTx(context.Background(), tx, TargetConfig{
			CloudType: ct, Region: region, ResourceID: resource,
		})
		id = newID
		return err
	}); err != nil {
		t.Fatalf("添加目标失败: %v", err)
	}
	return int(id)
}

func addConfigSite(t *testing.T, store *Store, site EdgeOneSite) {
	t.Helper()
	if err := store.WithTransaction(func(tx *sql.Tx) error {
		return store.AddEdgeOneSiteTx(context.Background(), tx, site)
	}); err != nil {
		t.Fatalf("添加站点失败: %v", err)
	}
}

func TestEdgeOneSiteStoreSnapshotDeepCopyAndReset(t *testing.T) {
	store := newSnapshotStore(t)
	first := addConfigTarget(t, store, CloudTCCVM, "ap-guangzhou", "sg-a")
	second := addConfigTarget(t, store, CloudAliECS, "cn-hangzhou", "sg-b")
	addConfigSite(t, store, EdgeOneSite{
		ZoneID: "zone-2", TargetIDs: []int{second, first}, Protocol: "TCP",
		Ports: "443", AddressFamily: "dual",
	})
	addConfigSite(t, store, EdgeOneSite{
		ZoneID: "zone-1", TargetIDs: []int{first}, Protocol: "ICMP",
		Ports: "ignored", AddressFamily: "ipv4",
	})

	snapshot, err := store.LoadBusinessSnapshot()
	if err != nil {
		t.Fatalf("LoadBusinessSnapshot 失败: %v", err)
	}
	if len(snapshot.EdgeOneSites) != 2 {
		t.Fatalf("站点数量 = %d, want 2: %+v", len(snapshot.EdgeOneSites), snapshot.EdgeOneSites)
	}
	if snapshot.EdgeOneSites[0].ZoneID != "zone-1" || snapshot.EdgeOneSites[1].ZoneID != "zone-2" {
		t.Errorf("站点必须按 zone_id 稳定排序: %+v", snapshot.EdgeOneSites)
	}
	if got := snapshot.EdgeOneSites[1].TargetIDs; !reflect.DeepEqual(got, []int{first, second}) {
		t.Errorf("绑定目标应按 target_id 升序: %v", got)
	}
	if got := snapshot.EdgeOneSites[0].Ports; got != "ALL" {
		t.Errorf("ICMP 端口应归一化为 ALL: %q", got)
	}

	rc := snapshot.ToRuntimeConfig()
	if len(rc.EdgeOneSites) != 2 {
		t.Fatalf("运行时站点数量 = %d, want 2", len(rc.EdgeOneSites))
	}
	// 运行时快照必须与业务快照、DeepCopy 结果相互独立。
	rc.EdgeOneSites[1].TargetIDs[0] = 999
	if snapshot.EdgeOneSites[1].TargetIDs[0] == 999 {
		t.Errorf("ToRuntimeConfig 未深拷贝站点")
	}
	copied := rc.DeepCopy()
	copied.EdgeOneSites[0].TargetIDs[0] = 777
	if rc.EdgeOneSites[0].TargetIDs[0] == 777 {
		t.Errorf("DeepCopy 未深拷贝站点")
	}

	// reset 必须同时清空站点与绑定行。
	if err := store.WithTransaction(func(tx *sql.Tx) error {
		return store.ResetAllTx(context.Background(), tx)
	}); err != nil {
		t.Fatalf("ResetAllTx 失败: %v", err)
	}
	sites, err := store.LoadEdgeOneSites()
	if err != nil || len(sites) != 0 {
		t.Errorf("reset 后站点为空: %+v err=%v", sites, err)
	}
}

func TestEdgeOneSiteAliasRejected(t *testing.T) {
	store := newSnapshotStore(t)
	first := addConfigTarget(t, store, CloudTCCVM, "ap-guangzhou", "sg-same")
	second := addConfigTarget(t, store, CloudTCCVM, "ap-guangzhou", "sg-same")
	addConfigSite(t, store, EdgeOneSite{
		ZoneID: "zone-alias", TargetIDs: []int{first, second}, Protocol: "TCP",
		Ports: "443", AddressFamily: "ipv4",
	})
	_, err := store.LoadBusinessSnapshot()
	if err == nil {
		t.Fatal("同一站点的两个实际资源别名必须被完整快照拒绝")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Field != "edgeone_sites" {
		t.Fatalf("期望 edgeone_sites 领域错误，实际 %v", err)
	}
}

func TestEdgeOneSiteDanglingBindingRejected(t *testing.T) {
	store := newSnapshotStore(t)
	first := addConfigTarget(t, store, CloudTCCVM, "ap-guangzhou", "sg-a")
	addConfigSite(t, store, EdgeOneSite{
		ZoneID: "zone-dangling", TargetIDs: []int{first}, Protocol: "TCP",
		Ports: "443", AddressFamily: "ipv4",
	})
	if err := store.WithTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO edgeone_site_targets (zone_id, target_id) VALUES ('zone-dangling', 9999)")
		return err
	}); err != nil {
		t.Fatalf("注入悬空绑定失败: %v", err)
	}
	_, err := store.LoadBusinessSnapshot()
	if err == nil {
		t.Fatal("悬空绑定必须被完整快照拒绝")
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		t.Errorf("悬空引用属于持久化完整性错误，不应包装为请求侧校验: %v", err)
	}
}

func TestEdgeOneSiteEmptyBindingRejected(t *testing.T) {
	store := newSnapshotStore(t)
	if err := store.WithTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO edgeone_sites (zone_id, protocol, ports, address_family) VALUES ('zone-empty', 'TCP', '443', 'ipv4')")
		return err
	}); err != nil {
		t.Fatalf("注入空绑定站点失败: %v", err)
	}
	_, err := store.LoadBusinessSnapshot()
	if err == nil {
		t.Fatal("没有绑定目标的站点必须被完整快照拒绝")
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		t.Errorf("空绑定属于持久化完整性错误，不得包装为请求侧校验: %v", err)
	}
}

func TestTargetIdentityTrimsStoredFields(t *testing.T) {
	normalized := TargetConfig{CloudType: CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-a"}
	padded := TargetConfig{CloudType: " tc_cvm ", Region: " ap-guangzhou ", ResourceID: " sg-a "}
	if TargetIdentity(normalized) != TargetIdentity(padded) {
		t.Errorf("TargetIdentity 未归一化首尾空白: %q vs %q", TargetIdentity(normalized), TargetIdentity(padded))
	}
	other := TargetConfig{CloudType: CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-b"}
	if TargetIdentity(normalized) == TargetIdentity(other) {
		t.Errorf("不同资源不应得到相同身份")
	}
}

func TestEdgeOneSitesMayShareTarget(t *testing.T) {
	store := newSnapshotStore(t)
	target := addConfigTarget(t, store, CloudTCCVM, "ap-guangzhou", "sg-shared")
	addConfigSite(t, store, EdgeOneSite{ZoneID: "zone-a", TargetIDs: []int{target}, Protocol: "TCP", Ports: "80", AddressFamily: "ipv4"})
	addConfigSite(t, store, EdgeOneSite{ZoneID: "zone-b", TargetIDs: []int{target}, Protocol: "UDP", Ports: "53", AddressFamily: "ipv6"})
	snapshot, err := store.LoadBusinessSnapshot()
	if err != nil {
		t.Fatalf("不同站点共享目标应合法: %v", err)
	}
	if len(snapshot.EdgeOneSites) != 2 {
		t.Errorf("站点数量 = %d, want 2", len(snapshot.EdgeOneSites))
	}
	zones, err := store.EdgeOneSitesReferencingTargetTx(context.Background(), mustTx(t, store), target)
	if err != nil {
		t.Fatalf("查询目标引用失败: %v", err)
	}
	if !reflect.DeepEqual(zones, []string{"zone-a", "zone-b"}) {
		t.Errorf("目标引用站点 = %v", zones)
	}
}

func mustTx(t *testing.T, store *Store) *sql.Tx {
	t.Helper()
	tx, err := store.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("BeginTx 失败: %v", err)
	}
	t.Cleanup(func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			t.Errorf("回滚测试事务失败: %v", rbErr)
		}
	})
	return tx
}
