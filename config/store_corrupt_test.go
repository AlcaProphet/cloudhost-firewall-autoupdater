package config

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// ─── Issue6 A9：损坏的 rules.targets 必须 fail-closed 且错误文本不含原值 ───

// insertRawRule 直接写入原始 targets 文本（绕过 API 校验，模拟外部改库/损坏）。
func insertRawRule(t *testing.T, s *Store, host, targetsExpr string) int {
	t.Helper()
	res, err := s.db.Exec(
		"INSERT INTO rules (host, protocol, ports, action, targets, comment, enable_ipv6) VALUES (?, 'TCP', '443', 'ACCEPT', "+targetsExpr+", '', 0)",
		host,
	)
	if err != nil {
		t.Fatalf("直插规则失败: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("取规则 ID 失败: %v", err)
	}
	return int(id)
}

// TestLoadRulesTargetsFourStateContract 四态严格口径（F1 裁决）。
func TestLoadRulesTargetsFourStateContract(t *testing.T) {
	tests := []struct {
		name      string
		expr      string
		wantAll   bool // 期望解析为「全部目标」（Targets 为 nil/空）
		wantIDs   []int
		wantError bool
		// forbidden 是该用例错误文本中不得出现的「原始值回显」片段
		// （注意：不动值本身可以出现在说明里，例如 “JSON null”，
		// 因此不能用全局列表一概而论）
		forbidden []string
	}{
		{name: "历史空串=全部目标", expr: "''", wantAll: true},
		{name: "空数组=合法的全部目标", expr: "'[]'", wantAll: true},
		{name: "null=内部错误", expr: "'null'", wantError: true},
		{name: "对象=内部错误", expr: "'{}'", wantError: true, forbidden: []string{"{}"}},
		{name: "标量=内部错误", expr: "'1'", wantError: true},
		{name: "非整数数组=内部错误", expr: "'[1,\"a\"]'", wantError: true, forbidden: []string{`[1,"a"]`}},
		{name: "解析失败=内部错误", expr: "'[1,'", wantError: true, forbidden: []string{"[1,"}},
		{name: "物理 NULL=内部错误", expr: "NULL", wantError: true},
		{name: "正常整数数组", expr: "'[1]'", wantIDs: []int{1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := OpenStore(filepath.Join(t.TempDir(), "rules.db"))
			if err != nil {
				t.Fatalf("OpenStore 失败: %v", err)
			}
			defer s.Close()

			id := insertRawRule(t, s, "example.com", tt.expr)

			rules, err := s.GetRules()
			if tt.wantError {
				if err == nil {
					t.Fatalf("期望内部错误，实际 rules=%+v", rules)
				}
				if !strings.Contains(err.Error(), "#"+itoa(id)) {
					t.Errorf("错误文本必须包含规则 ID #%d，实际: %v", id, err)
				}
				// 不得回显原始损坏值
				for _, raw := range tt.forbidden {
					if strings.Contains(err.Error(), raw) {
						t.Errorf("错误文本不得回显原值 %q: %v", raw, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("期望成功，实际错误: %v", err)
			}
			if len(rules) != 1 {
				t.Fatalf("规则数 = %d, want 1", len(rules))
			}
			if tt.wantAll {
				if len(rules[0].Targets) != 0 {
					t.Errorf("Targets = %v, want 空（全部目标）", rules[0].Targets)
				}
				return
			}
			if len(rules[0].Targets) != len(tt.wantIDs) {
				t.Fatalf("Targets = %v, want %v", rules[0].Targets, tt.wantIDs)
			}
			for i := range tt.wantIDs {
				if rules[0].Targets[i] != tt.wantIDs[i] {
					t.Errorf("Targets = %v, want %v", rules[0].Targets, tt.wantIDs)
				}
			}
		})
	}
}

// TestCorruptTargetsBlocksAllFourPaths 损坏必须阻断四条调用链（同步快照/导出/引用检查/启动加载）。
func TestCorruptTargetsBlocksAllFourPaths(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "block.db"))
	if err != nil {
		t.Fatalf("OpenStore 失败: %v", err)
	}
	defer s.Close()

	insertRawRule(t, s, "example.com", "'null'")

	// 1) 完整业务快照（同步与导出共用）
	if _, err := s.LoadBusinessSnapshot(); err == nil {
		t.Error("LoadBusinessSnapshot 必须因损坏 targets 失败（同步/导出路径）")
	}

	// 2) 启动加载
	if _, err := s.LoadConfig(); err == nil {
		t.Error("LoadConfig 必须因损坏 targets 失败（启动路径）")
	}

	// 3) 引用检查（目标删除的 409 判定）
	tx, err := s.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("BeginTx 失败: %v", err)
	}
	if _, err := s.ReferencingRuleIDsTx(context.Background(), tx, 1); err == nil {
		t.Error("ReferencingRuleIDsTx 必须因损坏 targets 失败（引用检查路径）")
	}
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("Rollback 失败: %v", err)
	}

	// 4) 只读事务导出路径
	rtx, err := s.BeginReadOnlyTx(context.Background())
	if err != nil {
		t.Fatalf("BeginReadOnlyTx 失败: %v", err)
	}
	if _, err := loadRules(context.Background(), rtx); err == nil {
		t.Error("只读事务内 loadRules 必须因损坏 targets 失败（导出路径）")
	}
	if err := rtx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("只读事务 Rollback 失败: %v", err)
	}
}

// itoa 是最小整数转字符串（避免为此引入 strconv 到测试头部）
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		return "-" + string(buf)
	}
	return string(buf)
}
