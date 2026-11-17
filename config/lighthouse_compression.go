package config

import (
	"fmt"
	"strings"
)

// Lighthouse 实例级 EdgeOne 策略压缩配置（Build9 Step 1）。
//
// 身份键固定为 cloud_type + region + resource_id（TargetIdentity）：同实例的
// 本地别名与全部绑定站点共享同一份开关与额度，其他实例相互独立。
type LighthouseCompression struct {
	CloudType      CloudType `json:"cloud_type"`
	Region         string    `json:"region"`
	ResourceID     string    `json:"resource_id"`
	Enabled        bool      `json:"enabled"`
	ExtraIPv4Allow int64     `json:"extra_ipv4_allow"`
}

// MaxLighthouseExtraIPv4 是额外放行额度上限：IPv4 全地址数 2^32。
//
// 额度按“同一实例在共存峰值额外放行的不同 IPv4 地址数”计数，因此不可能超过
// 2^32；更大的值没有业务含义，直接拒绝。
const MaxLighthouseExtraIPv4 int64 = 1 << 32

// LighthouseCompressionState 是运行观察，不属于 v5 业务配置。
//
// 目前只持久化“启用转关闭”后的待复查标记；Step 3 的可信回读负责清除。
type LighthouseCompressionState struct {
	CloudType     CloudType
	Region        string
	ResourceID    string
	PendingReview bool
	PendingReason string
	PendingSince  string
}

// NormalizeLighthouseCompression 归一化并校验一条实例压缩配置。
//
// 只接受腾讯云 Lighthouse；额度允许 0（仅无损），上限为 IPv4 全地址数。
// 不检查该实例当前是否仍有目标引用，引用闭包由完整业务快照统一校验。
func NormalizeLighthouseCompression(c LighthouseCompression) (LighthouseCompression, error) {
	cloudType, err := NormalizeCloudType(string(c.CloudType))
	if err != nil {
		return LighthouseCompression{}, err
	}
	if cloudType != CloudTCLighthouse {
		return LighthouseCompression{}, invalidField("cloud_type", "策略压缩仅支持 tc_lighthouse")
	}
	region, err := NormalizeRegion(c.Region)
	if err != nil {
		return LighthouseCompression{}, err
	}
	resourceID, err := NormalizeResourceID(c.ResourceID)
	if err != nil {
		return LighthouseCompression{}, err
	}
	if c.ExtraIPv4Allow < 0 || c.ExtraIPv4Allow > MaxLighthouseExtraIPv4 {
		return LighthouseCompression{}, invalidField("extra_ipv4_allow",
			fmt.Sprintf("必须是 0～%d 的整数", MaxLighthouseExtraIPv4))
	}
	return LighthouseCompression{
		CloudType:      cloudType,
		Region:         region,
		ResourceID:     resourceID,
		Enabled:        c.Enabled,
		ExtraIPv4Allow: c.ExtraIPv4Allow,
	}, nil
}

// Identity 返回物理实例身份键；字段已归一化时与 TargetIdentity 一致。
func (c LighthouseCompression) Identity() string {
	return TargetIdentity(TargetConfig{
		CloudType:  c.CloudType,
		Region:     c.Region,
		ResourceID: c.ResourceID,
	})
}

// Identity 返回运行观察对应的物理实例身份键。
func (s LighthouseCompressionState) Identity() string {
	return TargetIdentity(TargetConfig{
		CloudType:  s.CloudType,
		Region:     s.Region,
		ResourceID: s.ResourceID,
	})
}

// ValidateLighthouseCompressionTarget 校验配置身份命中了至少一个现有目标。
func ValidateLighthouseCompressionTarget(c LighthouseCompression, targets []TargetConfig) error {
	identity := c.Identity()
	for _, target := range targets {
		if TargetIdentity(target) == identity {
			return nil
		}
	}
	return fmt.Errorf("Lighthouse 压缩配置引用了不存在的实例 %s/%s/%s",
		c.CloudType, c.Region, strings.TrimSpace(c.ResourceID))
}
