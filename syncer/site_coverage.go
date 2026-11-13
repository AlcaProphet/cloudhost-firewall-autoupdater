package syncer

import (
	"crypto/sha256"
	"net/netip"
	"sort"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// SiteCoverage 是一个站点在一个物理目标上的本轮回读证明。
//
// 只有 Complete 且 HasConflicts=false，并且与站点要求的当前/待更新版本、
// 规范前缀摘要和配置代次一致时，才可作为站点确认的输入。
type SiteCoverage struct {
	ZoneID         string
	TargetKey      string
	Current        string
	Pending        string
	SnapshotDigest [32]byte
	ConfigEpoch    uint64
	Complete       bool
	HasConflicts   bool
	Reasons        []string
}

// snapshotDigest 对当前版与待更新版的版本号和规范前缀做稳定摘要。
func snapshotDigest(snapshot provider.OriginACLSnapshot) [32]byte {
	h := sha256.New()
	writeVersion := func(prefix string, version provider.OriginACLVersion) {
		_, _ = h.Write([]byte(prefix))
		_, _ = h.Write([]byte(version.Version))
		_, _ = h.Write([]byte{0})
		prefixes := append([]netip.Prefix(nil), version.Prefixes...)
		sort.SliceStable(prefixes, func(i, j int) bool {
			return prefixes[i].Masked().String() < prefixes[j].Masked().String()
		})
		for _, p := range prefixes {
			_, _ = h.Write([]byte(p.Masked().String()))
			_, _ = h.Write([]byte{0})
		}
	}
	writeVersion("current|", snapshot.Current)
	if snapshot.Next != nil {
		writeVersion("next|", *snapshot.Next)
	} else {
		_, _ = h.Write([]byte("next|none\x00"))
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// currentFingerprint 覆盖当前版版本号与规范前缀，用于确认前重查。
func currentFingerprint(current provider.OriginACLVersion) [32]byte {
	return snapshotDigest(provider.OriginACLSnapshot{Current: current})
}

// pendingFingerprint 只覆盖待更新版本与规范前缀，用于确认去重。
func pendingFingerprint(next *provider.OriginACLVersion) [32]byte {
	h := sha256.New()
	if next == nil {
		_, _ = h.Write([]byte("none"))
	} else {
		_, _ = h.Write([]byte(next.Version))
		_, _ = h.Write([]byte{0})
		prefixes := append([]netip.Prefix(nil), next.Prefixes...)
		sort.SliceStable(prefixes, func(i, j int) bool {
			return prefixes[i].Masked().String() < prefixes[j].Masked().String()
		})
		for _, p := range prefixes {
			_, _ = h.Write([]byte(p.Masked().String()))
			_, _ = h.Write([]byte{0})
		}
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// siteCoverageForExec 从最新目标回读的规划生成每个适用站点的证明。
func siteCoverageForExec(state *RuntimeState, exec targetExecution, plan provider.TargetPlan) map[string]SiteCoverage {
	out := make(map[string]SiteCoverage)
	if len(exec.edgeSites) == 0 {
		return out
	}
	covered := coveredKeySet(plan)
	unsupportedKeys := make(map[provider.FunctionalKey]bool)
	for _, issue := range plan.Unsupported {
		if issue.Key != nil {
			unsupportedKeys[*issue.Key] = true
		}
	}
	for _, site := range exec.edgeSites {
		zone := strings.TrimSpace(site.ZoneID)
		if zone == "" {
			continue
		}
		data := exec.edgeData[zone]
		proof := SiteCoverage{
			ZoneID:      zone,
			TargetKey:   exec.identity,
			ConfigEpoch: exec.epoch,
			Reasons:     []string{},
		}
		if data.unavailable != "" || strings.TrimSpace(data.snapshot.ZoneID) == "" ||
			strings.TrimSpace(data.snapshot.Current.Version) == "" ||
			!provider.OriginACLSnapshotPrefixesValid(data.snapshot) {
			proof.Reasons = append(proof.Reasons, "snapshot_unavailable")
		} else {
			proof.Current = data.snapshot.Current.Version
			if data.snapshot.Next != nil {
				proof.Pending = data.snapshot.Next.Version
			}
			proof.SnapshotDigest = snapshotDigest(data.snapshot)
			if !hasRequiredAddressFamilies(data.snapshot, site.AddressFamily) {
				proof.Reasons = append(proof.Reasons, "address_family_missing")
			}
		}
		for _, issue := range plan.EdgeOneErrors {
			if strings.Contains(issue.Message, zone) {
				proof.Reasons = append(proof.Reasons, "edgeone_unavailable")
				break
			}
		}
		if len(plan.Conflicts) > 0 {
			proof.Reasons = append(proof.Reasons, "conflict")
		}
		siteMissing := false
		for _, d := range plan.Desired {
			if !containsString(d.SiteIDs, zone) {
				continue
			}
			if !d.Implementable {
				// 当前平台无法实施该站点期望：站点覆盖不可用，确认必须为零。
				proof.Reasons = append(proof.Reasons, "unsupported")
				continue
			}
			if !covered[d.Key] {
				siteMissing = true
			}
			if unsupportedKeys[d.Key] {
				proof.Reasons = append(proof.Reasons, "unsupported")
			}
		}
		if siteMissing {
			proof.Reasons = append(proof.Reasons, "s1_uncovered")
		}
		proof.Reasons = stableUniqueStrings(proof.Reasons)
		proof.HasConflicts = len(plan.Conflicts) > 0 || len(plan.EdgeOneErrors) > 0 || containsString(proof.Reasons, "unsupported")
		proof.Complete = len(proof.Reasons) == 0
		out[zone] = proof
	}
	return out
}

func hasRequiredAddressFamilies(snapshot provider.OriginACLSnapshot, family string) bool {
	if family == config.EdgeOneAddressFamilyDual {
		return len(snapshot.PrefixesForFamily(config.EdgeOneAddressFamilyIPv4)) > 0 &&
			len(snapshot.PrefixesForFamily(config.EdgeOneAddressFamilyIPv6)) > 0
	}
	return len(snapshot.PrefixesForFamily(family)) > 0
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func stableUniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
