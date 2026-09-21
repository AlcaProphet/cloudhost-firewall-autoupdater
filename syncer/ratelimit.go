package syncer

import (
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// rateLimitInterval 频率控制：保留充足余量，确保不易触发限流
func rateLimitInterval(ct config.CloudType) time.Duration {
	switch ct {
	case config.CloudAliSWAS:
		return 5 * time.Second // SWAS 100次/60秒≈1.67/s，取 0.2/s 极度保守
	case config.CloudTCLighthouse:
		return 5 * time.Second // Lighthouse 10/s，取 0.2/s 极度保守
	default:
		return 200 * time.Millisecond // CVM 50/s、ECS 无限制，保守取值
	}
}
