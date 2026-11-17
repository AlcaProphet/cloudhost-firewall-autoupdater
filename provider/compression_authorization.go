package provider

import (
	"net/netip"
	"slices"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

type compressionGrant struct {
	ports     compressionPortSpan
	addresses ipv4Set
}

type compressionSource struct {
	grant     compressionGrant
	ref       CompressionAuthorization
	fullPorts bool
}

type compressionWirePort struct {
	protocol, expression string
	spans                []compressionPortSpan
}

type compressionAuthGroup struct {
	addresses ipv4Set
	prefixes  []netip.Prefix
	ports     []compressionWirePort
	sources   []compressionSource
	exactOnly bool
}

// buildCompressionAuthorizations 按每个协议端口的完整 IPv4 授权集合分组。
func buildCompressionAuthorizations(in LighthouseCompressionInput, external map[FunctionalKey][]config.RuleInfo) ([]compressionAuthGroup, []CompressionBlocker, []CompressionBlocker) {
	byProtocol := make(map[string][]compressionSource)
	var blockers, warnings []CompressionBlocker
	for _, input := range in.Sites {
		site := input.Site
		zone := strings.TrimSpace(site.ZoneID)
		snapshot := input.Snapshot
		if zone == "" || input.UnavailableReason != "" || snapshot.ZoneID != zone ||
			strings.TrimSpace(snapshot.Current.Version) == "" ||
			(snapshot.Next != nil && strings.TrimSpace(snapshot.Next.Version) == "") ||
			!OriginACLSnapshotPrefixesValid(snapshot) {
			blockers = append(blockers, CompressionBlocker{
				Code: CompressionBlockedSnapshotIncomplete, Message: "站点快照不可用或缺少版本/网段", ZoneID: zone,
			})
			continue
		}
		family := strings.ToLower(strings.TrimSpace(site.AddressFamily))
		if family == "ipv6" {
			continue
		}
		if family != "ipv4" && family != "dual" {
			blockers = append(blockers, CompressionBlocker{
				Code: CompressionBlockedSnapshotIncomplete, Message: "站点地址族无效", ZoneID: zone,
			})
			continue
		}
		protocol := strings.ToUpper(strings.TrimSpace(site.Protocol))
		if protocol == "ICMP" {
			warnings = append(warnings, CompressionBlocker{
				Code: "icmp_not_compressed", Message: "ICMP 沿用原规划", ZoneID: zone,
			})
			continue
		}
		protocols := compressionProtocols(protocol)
		spans, ok := compressionPortSpans(site.Ports)
		if !ok || len(protocols) == 0 || protocol == "ALL" {
			blockers = append(blockers, CompressionBlocker{
				Code: CompressionBlockedInvalidPort, Message: "站点协议或端口无效", ZoneID: zone,
			})
			continue
		}
		full := compressionPortsFull(spans)
		items := strings.Split(site.Ports, ",")
		addVersion := func(version OriginACLVersion, kind string) {
			for _, prefix := range version.Prefixes {
				r, ok := prefixIPv4Range(prefix)
				if !ok {
					continue
				}
				for _, protocol := range protocols {
					for i, span := range spans {
						ref := CompressionAuthorization{
							ZoneID: zone, Version: version.Version, VersionKind: kind,
							Prefix: prefix.Masked().String(), Protocol: protocol, PortItem: strings.ToUpper(strings.TrimSpace(items[i])),
						}
						if compressionExternalCovers(external, ref) {
							continue
						}
						byProtocol[protocol] = append(byProtocol[protocol], compressionSource{
							grant: compressionGrant{span, ipv4Set{r}}, ref: ref, fullPorts: full,
						})
					}
				}
			}
		}
		addVersion(snapshot.Current, "current")
		if snapshot.Next != nil {
			addVersion(*snapshot.Next, "next")
		}
	}
	if len(blockers) > 0 {
		return nil, blockers, warnings
	}

	groups := make(map[string]*compressionAuthGroup)
	var order []string
	for _, protocol := range []string{"TCP", "UDP"} {
		sources := byProtocol[protocol]
		spans := make([]compressionPortSpan, 0, len(sources))
		for _, source := range sources {
			spans = append(spans, source.grant.ports)
		}
		groupPorts := make(map[string][]compressionPortSpan)
		rangedParts := make(map[compressionPortSpan]bool)
		var groupOrder []string
		seen := make(map[string]map[CompressionAuthorization]bool)
		for _, part := range splitCompressionPorts(spans) {
			var addresses ipv4Set
			var active []compressionSource
			for _, source := range sources {
				if part.overlaps(source.grant.ports) {
					addresses = append(addresses, source.grant.addresses...)
					active = append(active, source)
					if source.grant.ports.first != source.grant.ports.last {
						rangedParts[part] = true
					}
				}
			}
			if len(addresses) == 0 {
				continue
			}
			addresses = addresses.normalize()
			prefixes := ipv4SetPrefixes(addresses)
			key := protocol + "\x00" + compressionCoverKey(prefixes)
			if groups[key] == nil {
				groups[key] = &compressionAuthGroup{addresses: addresses, prefixes: prefixes}
				order = append(order, key)
			}
			group := groups[key]
			if seen[key] == nil {
				seen[key] = make(map[CompressionAuthorization]bool)
				groupOrder = append(groupOrder, key)
			}
			for _, source := range active {
				group.exactOnly = group.exactOnly || source.fullPorts
				if !seen[key][source.ref] {
					group.sources = append(group.sources, source)
					seen[key][source.ref] = true
				}
			}
			groupPorts[key] = append(groupPorts[key], part)
		}
		for _, key := range groupOrder {
			group := groups[key]
			parts := groupPorts[key]
			group.exactOnly = group.exactOnly || compressionPortsFull(parts)
			for _, expr := range portExpressions(parts, rangedParts) {
				spans, _ := compressionPortSpans(expr)
				group.ports = append(group.ports, compressionWirePort{protocol, expr, spans})
			}
		}
	}
	slices.Sort(order)
	out := make([]compressionAuthGroup, 0, len(order))
	for _, key := range order {
		out = append(out, *groups[key])
	}
	return out, nil, warnings
}

func compressionProtocols(protocol string) []string {
	switch strings.ToUpper(strings.TrimSpace(protocol)) {
	case "TCP":
		return []string{"TCP"}
	case "UDP":
		return []string{"UDP"}
	case "ALL", "TCP+UDP":
		return []string{"TCP", "UDP"}
	default:
		return nil
	}
}

func compressionRuleGrant(rule config.RuleInfo) (ipv4Set, []compressionPortSpan) {
	prefix, ok := CanonicalCIDR(rule.CidrBlock)
	if !ok {
		return nil, nil
	}
	r, ok := prefixIPv4Range(netip.MustParsePrefix(prefix))
	if !ok {
		return nil, nil
	}
	port := rule.Port
	if strings.TrimSpace(port) == "" {
		port = "ALL"
	}
	ports, _ := compressionPortSpans(port)
	return ipv4Set{r}, ports
}
