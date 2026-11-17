package provider

import (
	"net/netip"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/tag"
)

type compressionRiskPart struct {
	ports      compressionPortSpan
	authorized ipv4Set
	oldExtra   ipv4Set
}

type compressionRiskModel struct {
	byProtocol    map[string][]compressionRiskPart
	oldExtra      ipv4Set
	manualIgnored bool
}

// buildCompressionRiskModel 在精确授权、过渡保护和 S0 TAG 行的端点切分。
func buildCompressionRiskModel(in LighthouseCompressionInput, groups []compressionAuthGroup) compressionRiskModel {
	model := compressionRiskModel{byProtocol: make(map[string][]compressionRiskPart)}
	authorized := make(map[string][]compressionGrant)
	oldGrants := make(map[string][]compressionGrant)
	for _, group := range groups {
		for _, port := range group.ports {
			for _, span := range port.spans {
				authorized[port.protocol] = append(authorized[port.protocol], compressionGrant{span, group.addresses})
			}
		}
	}
	for _, protected := range in.Protected {
		prefix, err := netip.ParsePrefix(protected.Prefix)
		if err != nil {
			continue
		}
		addresses := ipv4SetFromPrefixes([]netip.Prefix{prefix})
		ports, _ := compressionPortSpans(protected.PortItem)
		for _, protocol := range compressionProtocols(protected.Protocol) {
			for _, span := range ports {
				authorized[protocol] = append(authorized[protocol], compressionGrant{span, addresses})
			}
		}
	}
	for _, rule := range in.Snapshot.Rules {
		if !strings.EqualFold(strings.TrimSpace(rule.Action), "ACCEPT") {
			continue
		}
		if !tag.IsOwned(rule.Description, in.EdgeOneTag) {
			model.manualIgnored = true
			continue
		}
		addresses, ports := compressionRuleGrant(rule)
		if len(addresses) == 0 {
			continue
		}
		for _, protocol := range compressionProtocols(rule.Protocol) {
			for _, span := range ports {
				oldGrants[protocol] = append(oldGrants[protocol], compressionGrant{span, addresses})
			}
		}
	}
	for _, protocol := range []string{"TCP", "UDP"} {
		allowed := authorized[protocol]
		old := oldGrants[protocol]
		spans := make([]compressionPortSpan, 0, len(allowed)+len(old))
		for _, grant := range allowed {
			spans = append(spans, grant.ports)
		}
		for _, grant := range old {
			spans = append(spans, grant.ports)
		}
		for _, span := range splitCompressionPorts(spans) {
			part := compressionRiskPart{ports: span, authorized: compressionAddressesAtPort(allowed, span)}
			oldSet := compressionAddressesAtPort(old, span)
			if len(oldSet) == 0 && len(part.authorized) == 0 {
				continue
			}
			part.oldExtra = oldSet.subtract(part.authorized)
			model.oldExtra = model.oldExtra.union(part.oldExtra)
			model.byProtocol[protocol] = append(model.byProtocol[protocol], part)
		}
	}
	return model
}

func compressionAddressesAtPort(grants []compressionGrant, span compressionPortSpan) ipv4Set {
	var addresses ipv4Set
	for _, grant := range grants {
		if grant.ports.overlaps(span) {
			addresses = append(addresses, grant.addresses...)
		}
	}
	return addresses.normalize()
}

func (model compressionRiskModel) candidateExtra(port compressionWirePort, addresses ipv4Set) ipv4Set {
	var extra ipv4Set
	for _, part := range model.byProtocol[port.protocol] {
		for _, span := range port.spans {
			if span.overlaps(part.ports) {
				extra = extra.union(addresses.subtract(part.authorized))
				break
			}
		}
	}
	return extra
}

// summarize 先逐协议端口扣除授权，再跨端口对额外 IPv4 去重。
func (model compressionRiskModel) summarize(limit int64, rep CompressionRepresentation) CompressionRisk {
	out := CompressionRisk{
		Limit: limit, Complete: true, ManualIgnored: model.manualIgnored,
		OldResidue: model.oldExtra.count(), PerPort: []CompressionPortRisk{},
	}
	planned := make(map[string][]compressionGrant)
	for _, row := range rep.Rows {
		prefix := netip.MustParsePrefix(row.CIDR)
		ports, _ := compressionPortSpans(row.Port)
		for _, port := range ports {
			planned[row.Protocol] = append(planned[row.Protocol], compressionGrant{port, ipv4SetFromPrefixes([]netip.Prefix{prefix})})
		}
	}
	var total ipv4Set
	for _, protocol := range []string{"TCP", "UDP"} {
		for _, part := range model.byProtocol[protocol] {
			extra := compressionAddressesAtPort(planned[protocol], part.ports).subtract(part.authorized).union(part.oldExtra)
			total = total.union(extra)
			out.PerPort = append(out.PerPort, CompressionPortRisk{
				Protocol: protocol, Port: part.ports.String(), ExtraIPv4: extra.count(),
				MaxExtraRun: extra.maxRun(), Ranges: ipv4RangeStrings(extra),
			})
		}
	}
	out.ExtraIPv4 = total.count()
	out.MaxExtraRun = total.maxRun()
	return out
}

// expandsExtra checks each protocol/port interval, including ports absent from S0.
func (model compressionRiskModel) expandsExtra(rep CompressionRepresentation) bool {
	for _, row := range rep.Rows {
		ports, _ := compressionPortSpans(row.Port)
		addresses := ipv4SetFromPrefixes([]netip.Prefix{netip.MustParsePrefix(row.CIDR)})
		for _, port := range ports {
			var safe ipv4SetPorts
			for _, part := range model.byProtocol[row.Protocol] {
				if port.overlaps(part.ports) && len(addresses.subtract(part.authorized).subtract(part.oldExtra)) == 0 {
					safe = append(safe, part.ports)
				}
			}
			if !safe.covers(port) {
				return true
			}
		}
	}
	return false
}

// CompressionRiskDoesNotExpand compares the actual extra sets per protocol and
// port. Comparing the total can miss new exposure on a different port.
func CompressionRiskDoesNotExpand(before, after LighthouseCompressionInput) bool {
	groups, blockers, _ := buildCompressionAuthorizations(before, nil)
	if len(blockers) > 0 {
		return false
	}
	previous := buildCompressionRiskModel(before, groups)
	current := buildCompressionRiskModel(after, groups)
	for protocol, parts := range current.byProtocol {
		for _, part := range parts {
			if len(part.oldExtra) == 0 {
				continue
			}
			var safe ipv4SetPorts
			for _, old := range previous.byProtocol[protocol] {
				if part.ports.overlaps(old.ports) && len(part.oldExtra.subtract(old.oldExtra)) == 0 {
					safe = append(safe, old.ports)
				}
			}
			if !safe.covers(part.ports) {
				return false
			}
		}
	}
	return true
}
