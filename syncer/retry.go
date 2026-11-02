package syncer

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alibabacloud-go/tea/tea"
)

const maxRetries = 3

// isRetryable 判断目标是否应重新读取快照并规划。
// 先保留结构化 deadline/net.Error 超时语义，再对阿里云 SDK 业务错误按精确 Code 分类。
// 即使 Code 缺失或未知，也不读取该业务错误的 Message、Detail、Data 或外层文本。
// 其他错误沿用原有字符串兼容判断；腾讯无 cause 的错误不受本项影响。
// 幂等处理由调用方先执行，不能用重试分类替代 S1/S2 覆盖核验。
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

	// 3. 阿里云业务错误：只接受已定型的五个码，精确匹配且不作文本兜底。
	if code, ok := aliSDKErrorCode(err); ok {
		switch code {
		case "Throttling", "Throttling.User", "Throttling.Api", "ServiceUnavailable", "InternalError":
			return true
		default:
			return false
		}
	}
	// 4. 其他错误保留原有字符串兜底（大小写不敏感）。
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

// aliSDKErrorCode 区分“阿里云业务错误没有 Code”与“不是阿里云业务错误”。
// 当前固定 SDK 默认经 TeaSDKError 转换，使用 errors.As 穿透 Provider/目标层包装。
func aliSDKErrorCode(err error) (string, bool) {
	var sdkErr *tea.SDKError
	if !errors.As(err, &sdkErr) {
		return "", false
	}
	return tea.StringValue(sdkErr.Code), true
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
	// 阿里云业务消息不得进入腾讯版本竞争分支。
	if _, ok := aliSDKErrorCode(err); ok {
		return false
	}
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "firewallversionmismatch") || strings.Contains(msg, "versionmismatch")
}

// isIdempotentCreate 判断"规则已存在"
func isIdempotentCreate(err error) bool {
	// 保留既有幂等码集合，阿里云仅精确匹配 Code。
	if code, ok := aliSDKErrorCode(err); ok {
		switch code {
		case "FirewallRulesExist", "FirewallRuleAlreadyExist", "DuplicatePolicy", "FirewallRulesDuplicated":
			return true
		default:
			return false
		}
	}
	msg := err.Error()
	return strings.Contains(msg, "FirewallRulesExist") ||
		strings.Contains(msg, "FirewallRuleAlreadyExist") ||
		strings.Contains(msg, "DuplicatePolicy") ||
		strings.Contains(msg, "FirewallRulesDuplicated")
}

// isIdempotentDelete 判断"规则已不存在"
func isIdempotentDelete(err error) bool {
	// 保留既有幂等码集合，阿里云仅精确匹配 Code。
	if code, ok := aliSDKErrorCode(err); ok {
		switch code {
		case "FirewallRulesNotFound", "InvalidParam.SecurityGroupRuleId", "InvalidSecurityGroupRuleId.NotFound", "InvalidSecurityGroupRule.RuleNotExist", "InvalidInstanceId.NotFound":
			return true
		default:
			return false
		}
	}
	msg := err.Error()
	return strings.Contains(msg, "FirewallRulesNotFound") ||
		strings.Contains(msg, "InvalidParam.SecurityGroupRuleId") ||
		strings.Contains(msg, "InvalidSecurityGroupRuleId.NotFound") ||
		strings.Contains(msg, "InvalidSecurityGroupRule.RuleNotExist") ||
		strings.Contains(msg, "InvalidInstanceId.NotFound")
}
