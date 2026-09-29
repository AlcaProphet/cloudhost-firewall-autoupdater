package syncer

import (
	"context"
	"errors"
	"log/slog"
	"net"
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
// 返回实际写入计数 (added, deleted, skipped)：added 只累计 Provider 报告的
// Written（Issue6 A11），skipped 累计 Provider 明确跳过的期望规则条数；
// 重试轮重新 Diff（云端状态已更新），已生效规则不重复出现，天然避免重复计数；
// 幂等跳过（规则已存在/已不存在）不计入，与 Dry Run 的 to_add/to_delete 口径一致
func (s *Syncer) retrySync(p provider.Provider, rule config.DomainRule, resolved []dns.ResolvedIP, tagStr string) (added, deleted, skipped int, err error) {
	added, deleted, skipped, _, err = s.retrySyncDetailed(p, rule, resolved, tagStr)
	return
}

// retrySyncDetailed 在既有计数之外返回可安全展示的跳过详情。
// 详情只取最终成功 attempt；需要重试的失败 attempt 不得重复累加。
func (s *Syncer) retrySyncDetailed(p provider.Provider, rule config.DomainRule, resolved []dns.ResolvedIP, tagStr string) (added, deleted, skipped int, skippedDetails []provider.RuleChange, err error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		attemptSkipped := 0
		var attemptDetails []provider.RuleChange
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
				return added, deleted, attemptSkipped, attemptDetails, err
			}
			continue
		}

		// 2. 筛选本工具规则 + Diff（只使用本轮 TAG 快照）
		owned := provider.OwnedRules(allRules, tagStr)
		desc := truncateDesc(tag.Format(tagStr, rule.Comment), p.CloudType())
		diff := provider.Diff(resolved, rule, desc, owned, p)
		// Diff 阶段已识别的「无法实施」规则（未进入 to_add）必须无条件计数：
		// 它们与 Provider 在写入阶段报告的 Skipped 是**互斥**的两类
		// （前者不在 to_add 中，后者在 to_add 中），因此同一轮内相加不会重复；
		// 放在 to_add 判断之外，保证「全部都是无法实施」时也能如实报告（Issue6 A11）。
		//
		// 注意本轮次内只计一次：成功返回即结束，需要重试时本轮次的计数会被
		// 丢弃（与 added/deleted 的既有语义一致——它们同样只累计成功轮）。
		attemptSkipped += len(diff.Skipped)
		for _, item := range diff.Skipped {
			detail := provider.RuleChangeFromAction(item.Action)
			detail.SkipReason = item.Reason
			attemptDetails = append(attemptDetails, detail)
		}

		// 3. 执行删除（成功才计数；幂等"已不存在"视为成功但不计数）
		if len(diff.ToDelete) > 0 {
			if _, err := p.DeleteRules(provider.RuleSnapshot{}, diff.ToDelete); err != nil {
				// 逐条删除 Provider 可能在后续请求失败前已有确认成功项；先累计其
				// 明确进度，再按原始错误继续幂等/重试判定。
				var partial *provider.PartialDeleteError
				if errors.As(err, &partial) {
					deleted += partial.Deleted
				}
				if isIdempotentDelete(err) {
					slog.Warn("规则已不存在，跳过", "provider", p.Name())
				} else {
					lastErr = err
					if !isRetryable(err) {
						return added, deleted, attemptSkipped, attemptDetails, err
					}
					continue
				}
			} else {
				deleted += len(diff.ToDelete)
			}
		}

		// 4. 执行添加（成功才计数；幂等"已存在"视为成功但不计数）
		if len(diff.ToAdd) > 0 {
			res, err := p.CreateRules(provider.RuleSnapshot{}, diff.ToAdd)
			// 即使最终返回错误，Written 仍可表示此前已成功的独立子批次；
			// 当前失败且提交状态未知的子批次由 Provider 保持为 0。
			added += res.Written
			if err != nil {
				if isIdempotentCreate(err) {
					slog.Warn("规则已存在，跳过", "provider", p.Name())
				} else {
					lastErr = err
					if !isRetryable(err) {
						return added, deleted, attemptSkipped, attemptDetails, err
					}
					continue
				}
			} else {
				// Provider 明确跳过的期望规则（如 SWAS 无法表达 DROP）计入
				// skipped，绝不虚增为新增成功。
				attemptSkipped += res.Skipped
			}
		}

		return added, deleted, attemptSkipped, attemptDetails, nil // 成功
	}
	return added, deleted, skipped, skippedDetails, lastErr
}

// isRetryable 判断是否可重试（Issue6 A12，2026-09-27 用户裁决：结构化与兜底两者都做）。
//
// 依序三段判断：
//  1. 结构化：context.DeadlineExceeded 及其包装（阿里云 tea/dara 原样返回 *url.Error，
//     可用 errors.Is/errors.As 正确识别）；
//  2. 结构化：net.Error.Timeout()（覆盖 i/o timeout、真实 http.Client.Timeout）；
//  3. 字符串兜底（大小写不敏感）：腾讯 SDK 会把网络错误重新包装成
//     *TencentCloudSDKError{Code:"ClientError.NetworkError", Message:"... context deadline
//     exceeded (Client.Timeout exceeded while awaiting headers)"}，而该类型**没有
//     Unwrap**（common/errors/errors.go 全文无 Unwrap），因此结构化判断对腾讯路径不成立，
//     只能靠云错误码与 message 关键字兜底。
//
// 语义：腾讯 SDK 的网络类错误（含超时）进入重试；判定顺序与幂等优先级不变
// （幂等「已存在/已不存在」先于本函数判定，且不计数不重试）。
// 放宽带宽后仍不得让有意不可重试的错误变成可重试——CVM 规则上限
// （tc_cvm.go checkRuleLimit 的「安全组规则总数将达 N（上限 100），停止新增」）
// 不含任何下列关键字，因此保持不可重试。
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// 1. 结构化：超时上下文
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// 2. 结构化：网络超时
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// 3. 字符串兜底（大小写不敏感）
	msg := strings.ToLower(err.Error())
	retryable := []string{
		// 腾讯云版本竞争：Lighthouse UnsupportedOperation.FirewallVersionMismatch /
		// CVM UnsupportedOperation.VersionMismatch。必须整目标重试，绝不降级为无版本写入。
		"firewallversionmismatch",
		"versionmismatch",
		"requestlimitexceeded",
		"internalerror",
		"firewallbusy",
		"timeout",
		"connection refused",
		"clienterror.networkerror", // 腾讯 SDK 网络类错误码（netretry.go 重新包装的形状）
	}
	for _, r := range retryable {
		if strings.Contains(msg, r) {
			return true
		}
	}
	return false
}

// isPartialDelete 判断删除是否部分成功（云端已确认部分批次）。
func isPartialDelete(err error) bool {
	var partial *provider.PartialDeleteError
	return errors.As(err, &partial)
}

// isVersionMismatch 判断是否为腾讯云版本竞争错误。
//
// Lighthouse = UnsupportedOperation.FirewallVersionMismatch；CVM = UnsupportedOperation.VersionMismatch。
// 必须显式识别为可重试条件，绝不允许去掉版本重发。
func isVersionMismatch(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "firewallversionmismatch") || strings.Contains(msg, "versionmismatch")
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

// truncateDesc 按云厂商描述字段长度限制截断（保证 [TAG] 前缀完整）。
//
// 唯一实现已收口到 provider.TruncateDescription，本函数只是旧逐规则路径的兼容包装
// （Issue7 Step 1：避免截断逻辑出现第二套实现）。
func truncateDesc(desc string, ct config.CloudType) string {
	return provider.TruncateDescription(ct, desc)
}
