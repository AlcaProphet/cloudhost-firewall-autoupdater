package tag

import (
	"fmt"
	"strings"
)

// Format 生成规则描述："[TAG] comment"
func Format(tag, comment string) string {
	if comment == "" {
		return fmt.Sprintf("[%s]", tag)
	}
	return fmt.Sprintf("[%s] %s", tag, comment)
}

// IsOwned 是本工具唯一的 TAG 所有权判定（Issue7 §2.2、AGENTS §三）。
//
// 只有 description 精确等于 "[TAG]"、或以 "[TAG] "（右方恰好一个空格）开头时才属于
// 当前命名空间；"[TAG]foo"、"x[TAG] foo"、"[TAG-old] foo" 均**不属于**，
// 因此永远不参与修改、删除或接管。Parse 与 provider.OwnedRules 必须共用本判定，
// 禁止任何地方再写一套前缀比较。
func IsOwned(description, tag string) bool {
	prefix := "[" + tag + "]"
	return description == prefix || strings.HasPrefix(description, prefix+" ")
}

// HasPrefix 判断规则描述是否属于本工具管理（IsOwned 的兼容别名，语义完全一致）。
func HasPrefix(description, tag string) bool {
	return IsOwned(description, tag)
}

// Parse 从描述中提取 comment 部分，非本工具规则返回 ok=false。
func Parse(description, tag string) (comment string, ok bool) {
	if !IsOwned(description, tag) {
		return "", false
	}
	prefix := "[" + tag + "]"
	return strings.TrimSpace(description[len(prefix):]), true
}
