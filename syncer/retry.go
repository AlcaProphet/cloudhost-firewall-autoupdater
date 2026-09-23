package syncer

import (
	"log/slog"
	"strings"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/tag"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

const maxRetries = 3

// retrySync 带重试的完整同步流程（Describe → Diff → Create/Delete）
// tagStr 为本轮同步捕获的 TAG 快照：OwnedRules 筛选、描述生成和全部重试都只使用该参数，
// 不再读取可被热重载替换的 s.cfg，保证一轮同步内不混用新旧 TAG
// 返回实际写入计数 (added, deleted)：累计各轮次中云 API 调用成功的写入量；
// 重试轮重新 Diff（云端状态已更新），已生效规则不重复出现，天然避免重复计数；
// 幂等跳过（规则已存在/已不存在）不计入，与 Dry Run 的 to_add/to_delete 口径一致
func (s *Syncer) retrySync(p provider.Provider, rule config.DomainRule, resolved []dns.ResolvedIP, tagStr string) (added, deleted int, err error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			backoff := time.Duration(1<<uint(i-1)) * time.Second
			slog.Warn("重试同步", "attempt", i+1, "backoff", backoff, "provider", p.Name())
			time.Sleep(backoff)
		}

		// 1. 重新获取当前规则（乐观锁核心）
		allRules, err := p.GetRules()
		if err != nil {
			lastErr = err
			if !isRetryable(err) {
				return added, deleted, err
			}
			continue
		}

		// 2. 筛选本工具规则 + Diff（只使用本轮 TAG 快照）
		owned := provider.OwnedRules(allRules, tagStr)
		desc := truncateDesc(tag.Format(tagStr, rule.Comment), p.CloudType())
		diff := provider.Diff(resolved, rule, desc, owned, p)

		// 3. 执行删除（成功才计数；幂等"已不存在"视为成功但不计数）
		if len(diff.ToDelete) > 0 {
			if err := p.DeleteRules(diff.ToDelete); err != nil {
				if isIdempotentDelete(err) {
					slog.Warn("规则已不存在，跳过", "provider", p.Name())
				} else {
					lastErr = err
					if !isRetryable(err) {
						return added, deleted, err
					}
					continue
				}
			} else {
				deleted += len(diff.ToDelete)
			}
		}

		// 4. 执行添加（成功才计数；幂等"已存在"视为成功但不计数）
		if len(diff.ToAdd) > 0 {
			if err := p.CreateRules(diff.ToAdd); err != nil {
				if isIdempotentCreate(err) {
					slog.Warn("规则已存在，跳过", "provider", p.Name())
				} else {
					lastErr = err
					if !isRetryable(err) {
						return added, deleted, err
					}
					continue
				}
			} else {
				added += len(diff.ToAdd)
			}
		}

		return added, deleted, nil // 成功
	}
	return added, deleted, lastErr
}

// isRetryable 判断是否可重试
func isRetryable(err error) bool {
	msg := err.Error()
	retryable := []string{
		"RequestLimitExceeded",
		"InternalError",
		"FirewallBusy",
		"timeout",
		"connection refused",
	}
	for _, r := range retryable {
		if strings.Contains(msg, r) {
			return true
		}
	}
	return false
}

// isIdempotentCreate 判断"规则已存在"
func isIdempotentCreate(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "FirewallRulesExist") ||
		strings.Contains(msg, "FirewallRuleAlreadyExist") ||
		strings.Contains(msg, "DuplicatePolicy") ||
		strings.Contains(msg, "FirewallRulesDuplicated")
}

// isIdempotentDelete 判断"规则已不存在"
func isIdempotentDelete(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "FirewallRulesNotFound") ||
		strings.Contains(msg, "InvalidParam.SecurityGroupRuleId") ||
		strings.Contains(msg, "InvalidSecurityGroupRuleId.NotFound") ||
		strings.Contains(msg, "InvalidSecurityGroupRule.RuleNotExist") ||
		strings.Contains(msg, "InvalidInstanceId.NotFound")
}

// truncateDesc 按云厂商描述字段长度限制截断（保证 [TAG] 前缀完整）
// 使用 rune 切片避免截断 UTF-8 多字节字符（如中文）
func truncateDesc(desc string, ct config.CloudType) string {
	maxLen := 0
	switch ct {
	case config.CloudTCLighthouse:
		maxLen = 64 // FirewallRuleDescription ≤ 64 字符
	case config.CloudAliSWAS:
		maxLen = 50 // Remark ≤ 50 字符（阿里云 SWAS API）
	default:
		return desc // 其他云厂商限制宽松，无需截断
	}
	runes := []rune(desc)
	if len(runes) <= maxLen {
		return desc
	}
	return string(runes[:maxLen])
}
