package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/tag"
	"github.com/alibabacloud-go/tea/tea"
)

// 本文件承载 Build9 Step 4～5 的 EdgeOne TAG 清理归类、执行复核和错误摘要。
//
// 与自动清理 planner 的 cleanupLocatorIssue 刻意分开：
//   - 自动清理只对本次 planner 的候选负责，CVM/阿里云定位只需证明字段存在，
//     索引/规则 ID 重复由删除层在提交批次时拒绝；
//   - 手动清理预览必须证明“全快照唯一定位”，因为一次确认会提交整批候选，
//     不能把“字段存在”误当成“全局唯一”。
//
// 本函数不访问网络、数据库、时钟、日志或事件总线，仅消费一次完整快照。

// 预览逐行状态。
const (
	// FirewallCleanupDeletable 表示该物理行属于当前 TAG 且定位唯一，可进入本次预览候选。
	FirewallCleanupDeletable = "deletable"
	// FirewallCleanupSkipped 表示该物理行属于当前 TAG，但字段不足或定位不唯一，必须保留。
	FirewallCleanupSkipped = "skipped"
)

// 预览逐行原因码（稳定、可展示；不包含敏感值）。
const (
	// FirewallCleanupReasonRuleInvalid 云端规则字段不足以安全判定。
	FirewallCleanupReasonRuleInvalid = "rule_fields_invalid"
	// FirewallCleanupReasonKeyAmbiguous Lighthouse 功能 key 在完整快照中不唯一。
	FirewallCleanupReasonKeyAmbiguous = "key_ambiguous"
	// FirewallCleanupReasonLocatorMissing 缺少可用的删除定位字段。
	FirewallCleanupReasonLocatorMissing = "locator_missing"
	// FirewallCleanupReasonLocatorDuplicate 删除定位在完整快照中不唯一。
	FirewallCleanupReasonLocatorDuplicate = "locator_duplicate"
	// FirewallCleanupReasonSnapshotIncomplete 腾讯云快照缺少可用的版本号。
	FirewallCleanupReasonSnapshotIncomplete = "snapshot_incomplete"
	// FirewallCleanupReasonUnsupportedPlatform 未知平台，无法安全归类。
	FirewallCleanupReasonUnsupportedPlatform = "unsupported_platform"
)

// FirewallCleanupRow 是一条当前 TAG 匹配规则的只读归类结果。
//
// Rule 保留云端快照的原始字段；Index 是它在完整快照中的位置，供预览层在
// 计算 DNS 最后覆盖时区分同值物理行。Keys 是该物理行展开后的 canonical
// 功能 key（多端口行会有多个），任一 key 歧义都会使整行不可删。
type FirewallCleanupRow struct {
	Index      int
	Rule       config.RuleInfo
	Keys       []FunctionalKey
	Status     string
	ReasonCode string
	Reason     string
}

// Deletable 返回该行是否可进入本次预览候选。
func (r FirewallCleanupRow) Deletable() bool {
	return r.Status == FirewallCleanupDeletable
}

// FirewallCleanupAssessment 是一次完整快照的只读归类结果。
//
// Rows 只包含当前 TAG 匹配行；非匹配行保留在云端，不进入 Rows，仅通过
// RetainedCount 表达数量。调用方不得把 Rows 之外的行纳入删除候选。
type FirewallCleanupAssessment struct {
	RulesRead int
	Rows      []FirewallCleanupRow
}

// MatchedCount 返回当前 TAG 匹配的物理行数（一个云端对象算一行）。
func (a FirewallCleanupAssessment) MatchedCount() int {
	return len(a.Rows)
}

// DeletableCount 返回可安全删除的物理行数。
func (a FirewallCleanupAssessment) DeletableCount() int {
	n := 0
	for _, row := range a.Rows {
		if row.Deletable() {
			n++
		}
	}
	return n
}

// SkippedCount 返回匹配但必须保留的物理行数。
func (a FirewallCleanupAssessment) SkippedCount() int {
	return len(a.Rows) - a.DeletableCount()
}

// RetainedCount 返回不属于当前 TAG、因此保留的物理行数。
func (a FirewallCleanupAssessment) RetainedCount() int {
	return a.RulesRead - len(a.Rows)
}

// DeletableRows 返回可删除行的副本，供预览记录与 Step 5 重新核对使用。
func (a FirewallCleanupAssessment) DeletableRows() []FirewallCleanupRow {
	out := make([]FirewallCleanupRow, 0, a.DeletableCount())
	for _, row := range a.Rows {
		if row.Deletable() {
			out = append(out, row)
		}
	}
	return out
}

// AssessFirewallCleanup 在完整快照上按当前 EdgeOne TAG 归类清理候选。
//
// 定位规则（Build9 Step 4 冻结）：
//   - Lighthouse 无稳定 RuleID，必须使用同一快照的 FirewallVersion 与整行原值；
//     该行每个 canonical key 在**全部快照规则**中恰好出现一次才可删除，任一端口
//     不唯一都跳过整行；
//   - CVM 使用同一快照的 Version 与入站 PolicyIndex；索引必须非空、非负、可解析，
//     且在全部快照规则中唯一；
//   - SWAS/ECS 使用同一快照的 RuleId/SecurityGroupRuleId；必须非空且在全部快照
//     规则中唯一。
//
// 匹配只看 tag.IsOwned，手工填写了同一合法 TAG 前缀的行同样匹配；相似 TAG、
// 历史 TAG、DNS TAG 与无 TAG 手工规则均不进入 Rows。
func AssessFirewallCleanup(cloudType config.CloudType, snapshot RuleSnapshot, tagValue string) FirewallCleanupAssessment {
	assessment := FirewallCleanupAssessment{
		RulesRead: len(snapshot.Rules),
		Rows:      []FirewallCleanupRow{},
	}
	tagValue = strings.TrimSpace(tagValue)

	// 先建立全快照定位索引：即使重复项本身不是当前 TAG 规则，也会让匹配行
	// 无法唯一定位，因此必须遍历全部规则。
	keyRows := make(map[FunctionalKey]int)
	policyRows := make(map[int64]int)
	ruleIDRows := make(map[string]int)
	revisionOK := true

	switch cloudType {
	case config.CloudTCLighthouse:
		_, err := strconv.ParseUint(strings.TrimSpace(snapshot.Revision), 10, 64)
		revisionOK = err == nil
		if revisionOK {
			for _, rule := range snapshot.Rules {
				keys, ok := SnapshotRuleKeys(rule)
				if !ok {
					continue
				}
				seen := make(map[FunctionalKey]bool, len(keys))
				for _, key := range keys {
					if seen[key] {
						continue
					}
					seen[key] = true
					keyRows[key]++
				}
			}
		}
	case config.CloudTCCVM:
		revisionOK = strings.TrimSpace(snapshot.Revision) != ""
		for _, rule := range snapshot.Rules {
			if index, ok := parseCVMIndex(rule.PolicyIndex); ok {
				policyRows[index]++
			}
		}
	case config.CloudAliSWAS, config.CloudAliECS:
		for _, rule := range snapshot.Rules {
			id := strings.TrimSpace(rule.RuleID)
			if id != "" {
				ruleIDRows[id]++
			}
		}
	}

	for index, rule := range snapshot.Rules {
		if !tag.IsOwned(rule.Description, tagValue) {
			continue
		}

		keys, ok := SnapshotRuleKeys(rule)
		row := FirewallCleanupRow{
			Index: index,
			Rule:  rule,
			Keys:  append([]FunctionalKey(nil), keys...),
		}
		if !ok || len(keys) == 0 {
			row.Status = FirewallCleanupSkipped
			row.ReasonCode = FirewallCleanupReasonRuleInvalid
			row.Reason = "云端规则字段不足以安全判定，整行保留"
			assessment.Rows = append(assessment.Rows, row)
			continue
		}

		reasonCode, reason := cleanupSkipReason(cloudType, rule, keys, revisionOK, keyRows, policyRows, ruleIDRows)
		if reasonCode != "" {
			row.Status = FirewallCleanupSkipped
			row.ReasonCode = reasonCode
			row.Reason = reason
			assessment.Rows = append(assessment.Rows, row)
			continue
		}

		row.Status = FirewallCleanupDeletable
		assessment.Rows = append(assessment.Rows, row)
	}

	return assessment
}

// cleanupSkipReason 返回该匹配行为何不能删除；可删除时返回空原因码。
func cleanupSkipReason(
	cloudType config.CloudType,
	rule config.RuleInfo,
	keys []FunctionalKey,
	revisionOK bool,
	keyRows map[FunctionalKey]int,
	policyRows map[int64]int,
	ruleIDRows map[string]int,
) (string, string) {
	switch cloudType {
	case config.CloudTCLighthouse:
		if !revisionOK {
			return FirewallCleanupReasonSnapshotIncomplete, "腾讯云快照缺少可用的防火墙版本号，整目标不可验证"
		}
		for _, key := range keys {
			if keyRows[key] != 1 {
				return FirewallCleanupReasonKeyAmbiguous, "该行任一功能 key 在完整快照中不唯一，整行保留"
			}
		}
		return "", ""
	case config.CloudTCCVM:
		if !revisionOK {
			return FirewallCleanupReasonSnapshotIncomplete, "CVM 快照缺少可用的安全组版本号，整目标不可验证"
		}
		index, ok := parseCVMIndex(rule.PolicyIndex)
		if !ok {
			return FirewallCleanupReasonLocatorMissing, "候选缺少可用的入站 PolicyIndex，整行保留"
		}
		if policyRows[index] != 1 {
			return FirewallCleanupReasonLocatorDuplicate, "入站 PolicyIndex 在完整快照中不唯一，整行保留"
		}
		return "", ""
	case config.CloudAliSWAS, config.CloudAliECS:
		id := strings.TrimSpace(rule.RuleID)
		if id == "" {
			return FirewallCleanupReasonLocatorMissing, "候选缺少可用的规则 ID，整行保留"
		}
		if ruleIDRows[id] != 1 {
			return FirewallCleanupReasonLocatorDuplicate, "规则 ID 在完整快照中不唯一，整行保留"
		}
		return "", ""
	default:
		return FirewallCleanupReasonUnsupportedPlatform, "未知云平台，无法安全归类"
	}
}

// parseCVMIndex 解析 CVM PolicyIndex；只接受非负十进制整数。
func parseCVMIndex(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	index, err := strconv.ParseInt(value, 10, 64)
	if err != nil || index < 0 {
		return 0, false
	}
	return index, true
}

// ValidateFirewallCleanup 把新快照限定到已确认的整行候选；绝不扩大到新规则。
// 腾讯版本变化、同一定位内容变化或任一候选歧义均拒绝整个目标。
// absent 只表示旧身份已消失，不表示本操作实际删除。
func ValidateFirewallCleanup(cloudType config.CloudType, snapshot RuleSnapshot, tagValue, revision string, authorized []config.RuleInfo) (rows []config.RuleInfo, absent int, err error) {
	if (cloudType == config.CloudTCLighthouse || cloudType == config.CloudTCCVM) && snapshot.Revision != revision {
		return nil, 0, fmt.Errorf("云端版本已变化，请重新预览")
	}
	assessment := AssessFirewallCleanup(cloudType, snapshot, tagValue)
	rows = []config.RuleInfo{}
	for _, old := range authorized {
		found := false
		for index, current := range snapshot.Rules {
			sameLocator := cleanupAuthorizedIdentityEqual(current, old)
			switch cloudType {
			case config.CloudTCLighthouse:
				oldKeys, _ := SnapshotRuleKeys(old)
				currentKeys, _ := SnapshotRuleKeys(current)
				for _, a := range oldKeys {
					for _, b := range currentKeys {
						if a == b {
							sameLocator = true
						}
					}
				}
			case config.CloudTCCVM:
				a, aok := parseCVMIndex(old.PolicyIndex)
				b, bok := parseCVMIndex(current.PolicyIndex)
				sameLocator = aok && bok && a == b
				withoutIndex := current
				withoutIndex.PolicyIndex = old.PolicyIndex
				if !sameLocator && cleanupAuthorizedIdentityEqual(withoutIndex, old) {
					return nil, 0, fmt.Errorf("候选入站索引已漂移，请重新预览")
				}
			case config.CloudAliSWAS, config.CloudAliECS:
				sameLocator = strings.TrimSpace(current.RuleID) == strings.TrimSpace(old.RuleID)
				// 同一旧行丢失 ID 不是可验证的“已不存在”。
				withoutID := current
				withoutID.RuleID = old.RuleID
				if strings.TrimSpace(current.RuleID) == "" && cleanupAuthorizedIdentityEqual(withoutID, old) {
					return nil, 0, fmt.Errorf("候选规则 ID 缺失，请重新预览")
				}
			}
			if !sameLocator {
				continue
			}
			if found || !cleanupAuthorizedIdentityEqual(current, old) {
				return nil, 0, fmt.Errorf("候选身份或定位已变化，请重新预览")
			}
			safe := false
			for _, row := range assessment.Rows {
				if row.Index == index && row.Deletable() {
					safe = true
					break
				}
			}
			if !safe {
				return nil, 0, fmt.Errorf("候选定位或归属无法验证，请重新预览")
			}
			found = true
			rows = append(rows, current)
		}
		if !found {
			absent++
		}
	}
	return rows, absent, nil
}

// FirewallCleanupErrorInfo 仅提取可用于核查的 RequestId 与结果未知标记，
// 不把业务错误正文、请求参数或凭据复制到任务响应/日志。
func FirewallCleanupErrorInfo(err error) (requestID string, unknown bool) {
	if err == nil {
		return "", false
	}
	var network net.Error
	unknown = errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &network)
	var tc interface {
		GetCode() string
		GetRequestId() string
	}
	if errors.As(err, &tc) {
		code := tc.GetCode()
		return tc.GetRequestId(), unknown || code == "ClientError.NetworkError" || code == "InternalError" || strings.Contains(code, "Timeout")
	}
	var ali *tea.SDKError
	if errors.As(err, &ali) {
		code := tea.StringValue(ali.Code)
		unknown = unknown || code == "InternalError" || strings.Contains(code, "Timeout")
		if ali.Data != nil {
			var data struct {
				RequestID string `json:"RequestId"`
			}
			if jsonErr := json.Unmarshal([]byte(*ali.Data), &data); jsonErr == nil {
				requestID = data.RequestID
			}
		}
	}
	return requestID, unknown
}

// cleanupAuthorizedIdentityEqual 不把人类备注纳入身份；当前 TAG 归属另由全快照归类核对。
func cleanupAuthorizedIdentityEqual(a, b config.RuleInfo) bool {
	a.Description = ""
	b.Description = ""
	return a == b
}
