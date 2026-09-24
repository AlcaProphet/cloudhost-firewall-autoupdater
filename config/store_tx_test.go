package config

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// openTestStore 打开临时 SQLite 供事务测试使用
func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "store-tx.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if cerr := s.Close(); cerr != nil {
			t.Errorf("关闭数据库失败: %v", cerr)
		}
	})
	return s
}

// TestAddTargetTxReturnsLastInsertID 事务插入返回数据库分配的自增 ID（Step 5 的 export_id 映射依赖）
func TestAddTargetTxReturnsLastInsertID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 失败: %v", err)
	}
	id1, err := s.AddTargetTx(ctx, tx, TargetConfig{CloudType: CloudTCLighthouse, Region: "gz", ResourceID: "a"})
	if err != nil {
		t.Fatalf("AddTargetTx 失败: %v", err)
	}
	id2, err := s.AddTargetTx(ctx, tx, TargetConfig{CloudType: CloudTCCVM, Region: "gz", ResourceID: "b"})
	if err != nil {
		t.Fatalf("AddTargetTx 失败: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit 失败: %v", err)
	}

	if id1 <= 0 || id2 <= id1 {
		t.Fatalf("自增 ID 不合理: id1=%d id2=%d", id1, id2)
	}
	targets, err := s.GetTargets()
	if err != nil {
		t.Fatalf("GetTargets 失败: %v", err)
	}
	if len(targets) != 2 || int64(targets[0].ID) != id1 || int64(targets[1].ID) != id2 {
		t.Errorf("返回 ID 与数据库不一致: %+v (id1=%d id2=%d)", targets, id1, id2)
	}
}

// TestUpdateDeleteRowsAffected 更新/删除不存在时必须返回 0 行（handler 据此返回 404）
func TestUpdateDeleteRowsAffected(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 失败: %v", err)
	}
	id, err := s.AddTargetTx(ctx, tx, TargetConfig{CloudType: CloudTCLighthouse, Region: "gz", ResourceID: "a"})
	if err != nil {
		t.Fatalf("AddTargetTx 失败: %v", err)
	}
	if err := s.AddRuleTx(ctx, tx, DomainRule{Host: "a.example.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT"}); err != nil {
		t.Fatalf("AddRuleTx 失败: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit 失败: %v", err)
	}

	// AddRuleTx 只返回 error，规则 ID 从数据库读回（普通 CRUD 的 ID 由路径参数提供）
	rules, err := s.GetRules()
	if err != nil || len(rules) != 1 {
		t.Fatalf("GetRules = %d 条, err=%v; want 1", len(rules), err)
	}
	ruleID := rules[0].ID
	tx2, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 失败: %v", err)
	}
	defer func() { _ = tx2.Rollback() }()

	if n, err := s.UpdateTargetTx(ctx, tx2, int(id), TargetConfig{CloudType: CloudTCLighthouse, Region: "bj", ResourceID: "a2"}); err != nil || n != 1 {
		t.Errorf("更新已存在目标: n=%d err=%v, want 1", n, err)
	}
	if n, err := s.UpdateTargetTx(ctx, tx2, 9999, TargetConfig{CloudType: CloudTCLighthouse, Region: "bj", ResourceID: "x"}); err != nil || n != 0 {
		t.Errorf("更新不存在目标: n=%d err=%v, want 0", n, err)
	}
	if n, err := s.UpdateRuleTx(ctx, tx2, ruleID, DomainRule{Host: "b.example.com", Protocol: "UDP", Ports: "53", Action: "DROP"}); err != nil || n != 1 {
		t.Errorf("更新已存在规则: n=%d err=%v, want 1", n, err)
	}
	if n, err := s.UpdateRuleTx(ctx, tx2, 9999, DomainRule{Host: "c", Protocol: "TCP", Ports: "80", Action: "ACCEPT"}); err != nil || n != 0 {
		t.Errorf("更新不存在规则: n=%d err=%v, want 0", n, err)
	}
	if n, err := s.DeleteRuleTx(ctx, tx2, 9999); err != nil || n != 0 {
		t.Errorf("删除不存在规则: n=%d err=%v, want 0", n, err)
	}

	// 存在性查询
	if ok, err := s.TargetExistsTx(ctx, tx2, int(id)); err != nil || !ok {
		t.Errorf("TargetExistsTx(%d) = %v, %v; want true", id, ok, err)
	}
	if ok, err := s.TargetExistsTx(ctx, tx2, 9999); err != nil || ok {
		t.Errorf("TargetExistsTx(9999) = %v, %v; want false", ok, err)
	}
	if ok, err := s.RuleExistsTx(ctx, tx2, ruleID); err != nil || !ok {
		t.Errorf("RuleExistsTx(%d) = %v, %v; want true", ruleID, ok, err)
	}
	if ok, err := s.RuleExistsTx(ctx, tx2, 9999); err != nil || ok {
		t.Errorf("RuleExistsTx(9999) = %v, %v; want false", ok, err)
	}

	if n, err := s.DeleteTargetTx(ctx, tx2, int(id)); err != nil || n != 1 {
		t.Errorf("删除已存在目标: n=%d err=%v, want 1", n, err)
	}
	if n, err := s.DeleteTargetTx(ctx, tx2, int(id)); err != nil || n != 0 {
		t.Errorf("重复删除目标: n=%d err=%v, want 0", n, err)
	}
}

// TestReferencingRuleIDsTx 引用查询必须精确解析 JSON，
// 不得把“引用目标 12”误判为“引用目标 1”（LIKE 子串陷阱）。
func TestReferencingRuleIDsTx(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 失败: %v", err)
	}
	var ids []int
	for i := 0; i < 12; i++ {
		id, aerr := s.AddTargetTx(ctx, tx, TargetConfig{CloudType: CloudTCLighthouse, Region: "gz", ResourceID: fmt.Sprintf("r-%02d", i)})
		if aerr != nil {
			t.Fatalf("AddTargetTx 失败: %v", aerr)
		}
		ids = append(ids, int(id))
	}
	if ids[0] != 1 || ids[11] != 12 {
		t.Fatalf("自增 ID 前提不成立: %v", ids)
	}
	rules := []DomainRule{
		{Host: "one.example.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT", Targets: []int{ids[0]}},
		{Host: "twelve.example.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT", Targets: []int{ids[11]}},
		{Host: "all.example.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT", Targets: []int{}},
		{Host: "both.example.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT", Targets: []int{ids[0], ids[11]}},
	}
	for _, r := range rules {
		if rerr := s.AddRuleTx(ctx, tx, r); rerr != nil {
			t.Fatalf("AddRuleTx 失败: %v", rerr)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit 失败: %v", err)
	}

	tx2, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 失败: %v", err)
	}
	defer func() { _ = tx2.Rollback() }()

	refs1, err := s.ReferencingRuleIDsTx(ctx, tx2, ids[0])
	if err != nil {
		t.Fatalf("引用查询失败: %v", err)
	}
	refs12, err := s.ReferencingRuleIDsTx(ctx, tx2, ids[11])
	if err != nil {
		t.Fatalf("引用查询失败: %v", err)
	}
	none, err := s.ReferencingRuleIDsTx(ctx, tx2, 9999)
	if err != nil {
		t.Fatalf("引用查询失败: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("无引用目标应返回空: %v", none)
	}

	// 按 host 取回规则 ID，精确断言每个目标命中的规则集合
	allRules, err := s.GetRules()
	if err != nil {
		t.Fatalf("GetRules 失败: %v", err)
	}
	byHost := make(map[string]int, len(allRules))
	for _, r := range allRules {
		byHost[r.Host] = r.ID
	}

	if len(refs1) != 2 || !containsInt(refs1, byHost["one.example.com"]) || !containsInt(refs1, byHost["both.example.com"]) {
		t.Errorf("引用目标 1 的规则 = %v, want {one, both}", refs1)
	}
	if containsInt(refs1, byHost["twelve.example.com"]) || containsInt(refs1, byHost["all.example.com"]) {
		t.Errorf("引用目标 1 不应命中 twelve/all（子串误匹配）: %v", refs1)
	}
	if len(refs12) != 2 || !containsInt(refs12, byHost["twelve.example.com"]) || !containsInt(refs12, byHost["both.example.com"]) {
		t.Errorf("引用目标 12 的规则 = %v, want {twelve, both}", refs12)
	}
	if containsInt(refs12, byHost["one.example.com"]) || containsInt(refs12, byHost["all.example.com"]) {
		t.Errorf("引用目标 12 不应命中 one/all: %v", refs12)
	}
}

// containsInt 判断切片是否包含指定值
func containsInt(list []int, want int) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// TestValidateRuleTargetsTx 事务内目标存在性校验
func TestValidateRuleTargetsTx(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 失败: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	id, err := s.AddTargetTx(ctx, tx, TargetConfig{CloudType: CloudAliECS, Region: "cn-hangzhou", ResourceID: "sg-1"})
	if err != nil {
		t.Fatalf("AddTargetTx 失败: %v", err)
	}

	if err := s.ValidateRuleTargetsTx(ctx, tx, nil); err != nil {
		t.Errorf("空数组表示全部目标，应通过: %v", err)
	}
	if err := s.ValidateRuleTargetsTx(ctx, tx, []int{int(id)}); err != nil {
		t.Errorf("存在的目标应通过: %v", err)
	}
	err = s.ValidateRuleTargetsTx(ctx, tx, []int{int(id), 9999})
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Field != "targets" {
		t.Errorf("不存在的引用应返回 targets 校验错误: %v", err)
	}
}

// TestResetAllTxTransaction 事务内清空可回滚、可提交
func TestResetAllTxTransaction(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	seed := func() {
		t.Helper()
		tx, err := s.BeginTx(ctx)
		if err != nil {
			t.Fatalf("BeginTx 失败: %v", err)
		}
		if _, err := s.AddTargetTx(ctx, tx, TargetConfig{CloudType: CloudTCLighthouse, Region: "gz", ResourceID: "keep"}); err != nil {
			t.Fatalf("AddTargetTx 失败: %v", err)
		}
		if err := s.SetSettingTx(ctx, tx, "tag", "keep-tag"); err != nil {
			t.Fatalf("SetSettingTx 失败: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit 失败: %v", err)
		}
	}

	// 回滚：数据必须保持
	seed()
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 失败: %v", err)
	}
	if err := s.ResetAllTx(ctx, tx); err != nil {
		t.Fatalf("ResetAllTx 失败: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback 失败: %v", err)
	}
	if targets, _ := s.GetTargets(); len(targets) != 1 {
		t.Errorf("回滚后目标应保留: %+v", targets)
	}
	if settings, _ := s.GetSettings(); settings["tag"] != "keep-tag" {
		t.Errorf("回滚后设置应保留: %+v", settings)
	}

	// 提交：全部清空
	tx, err = s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 失败: %v", err)
	}
	if err := s.ResetAllTx(ctx, tx); err != nil {
		t.Fatalf("ResetAllTx 失败: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit 失败: %v", err)
	}
	if targets, _ := s.GetTargets(); len(targets) != 0 {
		t.Errorf("提交后目标应清空: %+v", targets)
	}
	if settings, _ := s.GetSettings(); len(settings) != 0 {
		t.Errorf("提交后设置应清空: %+v", settings)
	}
}

// TestLoadConfigSettingValidation 既有数据库非法设置值必须报错，不再静默回退默认值（Build6 §12.10）
func TestLoadConfigSettingValidation(t *testing.T) {
	t.Run("空白按缺失处理并使用默认值", func(t *testing.T) {
		s := openTestStore(t)
		for k, v := range map[string]string{"tag": "  ", "interval": "", "dns": " ", "dns_timeout": "", "dns_fail_threshold": "", "log_level": "", "theme": ""} {
			if err := s.SetSetting(k, v); err != nil {
				t.Fatalf("预置设置失败: %v", err)
			}
		}
		cfg, err := s.LoadConfig()
		if err != nil {
			t.Fatalf("空白值应按缺失处理: %v", err)
		}
		if cfg.Tag != "auto-dns" || cfg.Interval != 5*time.Minute || cfg.DNS != "223.5.5.5" ||
			cfg.DNSTimeout != 10*time.Second || cfg.DNSFailThreshold != 5 || cfg.LogLevel != "info" {
			t.Errorf("默认值不正确: %+v", cfg)
		}
		if !cfg.SyncEnabled {
			t.Error("sync_enabled 默认应为 true")
		}
	})

	t.Run("合法值被归一化加载", func(t *testing.T) {
		s := openTestStore(t)
		for k, v := range map[string]string{
			"tag": " my-tag ", "interval": "7m", "dns": " 1.1.1.1:5353 ",
			"dns_timeout": "3s", "dns_fail_threshold": "9", "log_level": "DEBUG", "theme": "DARK",
			"sync_enabled": "false", "tc_access_id": "AKIDx",
		} {
			if err := s.SetSetting(k, v); err != nil {
				t.Fatalf("预置设置失败: %v", err)
			}
		}
		cfg, err := s.LoadConfig()
		if err != nil {
			t.Fatalf("合法值应通过: %v", err)
		}
		if cfg.Tag != "my-tag" || cfg.Interval != 7*time.Minute || cfg.DNS != "1.1.1.1:5353" ||
			cfg.DNSTimeout != 3*time.Second || cfg.DNSFailThreshold != 9 || cfg.LogLevel != "debug" {
			t.Errorf("归一化结果不正确: %+v", cfg)
		}
		if cfg.SyncEnabled {
			t.Error("sync_enabled=false 应生效")
		}
		if cfg.TCAccessID != "AKIDx" {
			t.Errorf("凭据应按原值加载: %q", cfg.TCAccessID)
		}
	})

	t.Run("非法值返回带键名错误且不回显原值", func(t *testing.T) {
		// hidden 为该用例中“绝不能出现在错误文本里”的原值片段；
		// 像 "0s" 这种同时是提示文案子串（"30s"）的值无法据此判断，留空跳过。
		cases := []struct{ key, value, field, hidden string }{
			{"tag", "bad[tag]", "tag", "bad[tag]"},
			{"interval", "abc", "interval", "abc"},
			{"interval", "-5m", "interval", "-5m"},
			{"dns", "8.8.8.8:99999", "dns", "8.8.8.8"},
			{"dns", "2001:db8::1", "dns", "2001:db8"},
			{"dns_timeout", "0s", "dns_timeout", ""},
			{"dns_fail_threshold", "-3", "dns_fail_threshold", "-3"},
			{"log_level", "trace", "log_level", "trace"},
			{"theme", "blue", "theme", "blue"},
			{"sync_enabled", "yes", "sync_enabled", "yes"},
		}
		for _, tc := range cases {
			s := openTestStore(t)
			if err := s.SetSetting(tc.key, tc.value); err != nil {
				t.Fatalf("预置设置失败: %v", err)
			}
			_, err := s.LoadConfig()
			if err == nil {
				t.Errorf("键 %s 的非法值 %q 应导致 LoadConfig 失败", tc.key, tc.value)
				continue
			}
			var ve *ValidationError
			if !errors.As(err, &ve) || ve.Field != tc.field {
				t.Errorf("键 %s 应返回字段 %s 的校验错误: %v", tc.key, tc.field, err)
			}
			if tc.hidden != "" && strings.Contains(err.Error(), tc.hidden) {
				t.Errorf("错误不得回显原始值 %q: %v", tc.hidden, err)
			}
		}
	})

	t.Run("webui_port 残留键被忽略", func(t *testing.T) {
		s := openTestStore(t)
		if err := s.SetSetting("webui_port", "61234"); err != nil {
			t.Fatalf("预置失败: %v", err)
		}
		if _, err := s.LoadConfig(); err != nil {
			t.Errorf("webui_port 残留不应导致加载失败: %v", err)
		}
	})
}
