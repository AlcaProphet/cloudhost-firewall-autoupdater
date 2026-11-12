package config

import (
	"fmt"
	"strconv"
	"strings"
)

// EdgeOneSite 是一个 ZoneId 的站点配置（Build8 Step 3）。
//
// 首版一个站点使用同一组协议、端口和地址族绑定多个现有目标；动作由后端
// 固定为 ACCEPT，不进入配置。发布到运行时前必须由 Store 深拷贝 TargetIDs。
type EdgeOneSite struct {
	ZoneID        string `json:"zone_id"`
	TargetIDs     []int  `json:"target_ids"`
	Protocol      string `json:"protocol"`
	Ports         string `json:"ports"`
	AddressFamily string `json:"address_family"` // ipv4 / ipv6 / dual
}

// EdgeOne 地址族枚举（Build8 Step 3、Design6 §7.1）。
const (
	EdgeOneAddressFamilyIPv4 = "ipv4"
	EdgeOneAddressFamilyIPv6 = "ipv6"
	EdgeOneAddressFamilyDual = "dual"
)

// NormalizeZoneID 归一化并校验站点 ZoneId：
// 去除首尾空白后非空、无控制字符、无路径分隔符；保留腾讯云 ID 的大小写。
func NormalizeZoneID(v string) (string, error) {
	s := strings.TrimSpace(v)
	if s == "" {
		return "", invalidField("zone_id", "不能为空")
	}
	if hasControlChar(s) {
		return "", invalidField("zone_id", "不能包含控制字符")
	}
	if strings.ContainsAny(s, `/\`) {
		return "", invalidField("zone_id", "不能包含路径分隔符")
	}
	return s, nil
}

// NormalizeEdgeOneSite 归一化并校验站点配置本身（不检查目标存在性与相互别名）。
//
//   - target_ids 必须显式提供且至少一个，元素为正数且组内不重复；
//   - protocol 只允许 TCP / UDP / TCP+UDP / ICMP；
//   - ICMP 沿用既有语义归一化为 ALL；其他协议接受单独 ALL 或逗号分隔的端口/范围；
//   - address_family 只允许 ipv4 / ipv6 / dual（按小写归一化）。
func NormalizeEdgeOneSite(site EdgeOneSite) (EdgeOneSite, error) {
	zoneID, err := NormalizeZoneID(site.ZoneID)
	if err != nil {
		return EdgeOneSite{}, err
	}
	targetIDs, err := NormalizeTargetIDs(site.TargetIDs)
	if err != nil {
		return EdgeOneSite{}, err
	}
	if len(targetIDs) == 0 {
		return EdgeOneSite{}, invalidField("target_ids", "至少需要一个目标")
	}

	protocol := strings.ToUpper(strings.TrimSpace(site.Protocol))
	if !validProtocols[protocol] {
		return EdgeOneSite{}, invalidField("protocol", "只允许 TCP / UDP / TCP+UDP / ICMP")
	}
	ports, err := normalizeEdgeOnePorts(protocol, site.Ports)
	if err != nil {
		return EdgeOneSite{}, err
	}
	addressFamily := strings.ToLower(strings.TrimSpace(site.AddressFamily))
	switch addressFamily {
	case EdgeOneAddressFamilyIPv4, EdgeOneAddressFamilyIPv6, EdgeOneAddressFamilyDual:
	default:
		return EdgeOneSite{}, invalidField("address_family", "只允许 ipv4 / ipv6 / dual")
	}

	return EdgeOneSite{
		ZoneID:        zoneID,
		TargetIDs:     append([]int(nil), targetIDs...),
		Protocol:      protocol,
		Ports:         ports,
		AddressFamily: addressFamily,
	}, nil
}

// normalizeEdgeOnePorts 校验并规范化 EdgeOne 端口表达式。
//
// 与 DNS 规则不同，这里不允许空项被静默丢弃，也不允许把空输入回退成 ALL：
// 空项、非法项、ALL 与其他端口混用都整份拒绝。
func normalizeEdgeOnePorts(protocol, ports string) (string, error) {
	if protocol == "ICMP" {
		return "ALL", nil
	}
	s := strings.TrimSpace(ports)
	if s == "" {
		return "", invalidField("ports", "非 ICMP 协议不能为空")
	}
	items := strings.Split(s, ",")
	result := make([]string, 0, len(items))
	for _, item := range items {
		part := strings.TrimSpace(item)
		if part == "" {
			return "", invalidField("ports", "不能包含空项")
		}
		if strings.EqualFold(part, "ALL") {
			if len(items) != 1 {
				return "", invalidField("ports", "ALL 不能与其他端口混用")
			}
			return "ALL", nil
		}
		normalized, err := normalizeEdgeOnePort(part)
		if err != nil {
			return "", err
		}
		result = append(result, normalized)
	}
	return strings.Join(result, ","), nil
}

// normalizeEdgeOnePort 校验单个端口或端口范围，并输出规范十进制形式。
func normalizeEdgeOnePort(v string) (string, error) {
	if !strings.Contains(v, "-") {
		n, err := parseEdgeOnePortNumber(v)
		if err != nil {
			return "", err
		}
		return strconv.Itoa(n), nil
	}
	parts := strings.SplitN(v, "-", 2)
	if len(parts) != 2 {
		return "", invalidField("ports", "端口范围格式无效")
	}
	start, err := parseEdgeOnePortNumber(parts[0])
	if err != nil {
		return "", err
	}
	end, err := parseEdgeOnePortNumber(parts[1])
	if err != nil {
		return "", err
	}
	if start > end {
		return "", invalidField("ports", "端口范围起点不能大于终点")
	}
	return fmt.Sprintf("%d-%d", start, end), nil
}

// parseEdgeOnePortNumber 只接受 1～65535 的十进制数字（允许首尾空白）。
func parseEdgeOnePortNumber(v string) (int, error) {
	s := strings.TrimSpace(v)
	if s == "" {
		return 0, invalidField("ports", "端口不能为空")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, invalidField("ports", "端口必须是 1～65535 的整数")
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, invalidField("ports", "端口必须是 1～65535 的整数")
	}
	return n, nil
}

// TargetIdentity 是跨本地 ID 的物理目标身份：云类型 + 地域 + 资源 ID。
//
// 仅用于同一站点内的别名检测；不同站点可以共享同一实际目标。
func TargetIdentity(t TargetConfig) string {
	return string(t.CloudType) + "\x00" + t.Region + "\x00" + t.ResourceID
}

// DeepCopyEdgeOneSites 深拷贝站点切片及每个 TargetIDs 数组。
func DeepCopyEdgeOneSites(sites []EdgeOneSite) []EdgeOneSite {
	out := make([]EdgeOneSite, len(sites))
	for i, site := range sites {
		cp := site
		cp.TargetIDs = append([]int(nil), site.TargetIDs...)
		out[i] = cp
	}
	return out
}
