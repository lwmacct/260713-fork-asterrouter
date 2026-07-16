package plugins

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

var semverPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+([-.+][0-9A-Za-z.-]+)?$`)

type packageRevocation struct {
	Revoked           bool
	RevokedByAdvisory bool
	AdvisoryID        string
	AdvisoryTitle     string
	AdvisorySeverity  string
	Reason            string
}

func (s *Service) packageRevocation(ctx context.Context, record packageRecord) (packageRevocation, error) {
	if record.Revoked {
		return packageRevocation{Revoked: true, Reason: "package revoked"}, nil
	}
	affected, err := s.revokedAffectedVersions(ctx, record)
	if err != nil {
		return packageRevocation{}, err
	}
	for _, item := range affected {
		if semverRangeMatches(record.Version, item.VersionRange) {
			reason := "revoked by security advisory"
			if strings.TrimSpace(item.AdvisoryID) != "" {
				reason += " " + strings.TrimSpace(item.AdvisoryID)
			}
			return packageRevocation{
				Revoked:           true,
				RevokedByAdvisory: true,
				AdvisoryID:        item.AdvisoryID,
				AdvisoryTitle:     item.AdvisoryTitle,
				AdvisorySeverity:  item.AdvisorySeverity,
				Reason:            reason,
			}, nil
		}
	}
	return packageRevocation{}, nil
}

func (s *Service) revokedAffectedVersions(ctx context.Context, record packageRecord) ([]affectedVersionRecord, error) {
	keys := []string{record.PluginSlug, record.PluginPublicID, record.PluginID}
	seen := map[string]struct{}{}
	out := make([]affectedVersionRecord, 0)
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items, err := s.repo.ListRevokedAffectedVersions(ctx, key)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return out, nil
}

func (s *Service) packageCompatible(record packageRecord, revocation packageRevocation) (bool, string) {
	if revocation.Revoked {
		return false, revocation.Reason
	}
	if !platformMatches(record.OS, s.targetOS) {
		return false, "operating system mismatch"
	}
	if !platformMatches(record.Arch, s.targetArch) {
		return false, "architecture mismatch"
	}
	if record.MinCoreVersion != "" && compareSemver(s.coreVersion, record.MinCoreVersion) < 0 {
		return false, "core version below minimum"
	}
	if record.MaxCoreVersion != "" && compareSemver(s.coreVersion, record.MaxCoreVersion) > 0 {
		return false, "core version above maximum"
	}
	var compatibility []remoteCompatibility
	if err := json.Unmarshal([]byte(defaultString(record.CompatibilityJSON, "[]")), &compatibility); err != nil {
		return false, "compatibility matrix invalid"
	}
	if len(compatibility) == 0 {
		return false, "compatibility matrix missing"
	}
	for _, item := range compatibility {
		if !platformMatches(item.OS, s.targetOS) || !platformMatches(item.Arch, s.targetArch) {
			continue
		}
		if !semverRangeMatches(s.coreVersion, item.CoreVersionRange) {
			continue
		}
		if item.Result == "compatible" {
			return true, ""
		}
		return false, "compatibility matrix rejected this target"
	}
	return false, "no compatible target entry"
}

func semverRangeMatches(version string, constraint string) bool {
	version = strings.TrimSpace(version)
	constraint = strings.TrimSpace(constraint)
	if !semverPattern.MatchString(version) || constraint == "" {
		return false
	}
	for _, term := range strings.Fields(strings.ReplaceAll(constraint, ",", " ")) {
		operator := "="
		value := term
		for _, candidate := range []string{">=", "<=", ">", "<", "=", "^", "~"} {
			if strings.HasPrefix(term, candidate) {
				operator = candidate
				value = strings.TrimSpace(strings.TrimPrefix(term, candidate))
				break
			}
		}
		if !semverPattern.MatchString(value) {
			return false
		}
		cmp := compareSemver(version, value)
		switch operator {
		case ">=":
			if cmp < 0 {
				return false
			}
		case "<=":
			if cmp > 0 {
				return false
			}
		case ">":
			if cmp <= 0 {
				return false
			}
		case "<":
			if cmp >= 0 {
				return false
			}
		case "^":
			upper := semverParts(value)
			upper[0]++
			upper[1], upper[2] = 0, 0
			if cmp < 0 || compareSemverParts(semverParts(version), upper) >= 0 {
				return false
			}
		case "~":
			upper := semverParts(value)
			upper[1]++
			upper[2] = 0
			if cmp < 0 || compareSemverParts(semverParts(version), upper) >= 0 {
				return false
			}
		default:
			if cmp != 0 {
				return false
			}
		}
	}
	return true
}

func compareSemver(left string, right string) int {
	return compareSemverParts(semverParts(left), semverParts(right))
}

func compareSemverParts(left [3]int, right [3]int) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}

func semverParts(value string) [3]int {
	base := value
	if index := strings.IndexAny(base, "-+"); index >= 0 {
		base = base[:index]
	}
	parts := strings.Split(base, ".")
	var result [3]int
	for index := 0; index < len(parts) && index < len(result); index++ {
		for _, char := range parts[index] {
			if char < '0' || char > '9' {
				continue
			}
			result[index] = result[index]*10 + int(char-'0')
		}
	}
	return result
}

func platformMatches(value string, current string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	current = strings.ToLower(strings.TrimSpace(current))
	return value == "" || value == "any" || value == current
}
