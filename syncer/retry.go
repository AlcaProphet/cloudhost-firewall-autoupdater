package syncer

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

const maxRetries = 3

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
// （tc_cvm.go checkRuleLimit 的「安全组入站规则数将达 N（上限 100），停止新增」）
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
