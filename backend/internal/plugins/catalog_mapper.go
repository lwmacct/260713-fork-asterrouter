package plugins

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

var catalogSlugPattern = regexp.MustCompile(`[^a-z0-9._-]+`)

func mapRemoteCatalogPlugins(index remoteCatalogIndex, now time.Time) []Plugin {
	out := make([]Plugin, 0, len(index.Plugins))
	for _, item := range index.Plugins {
		slug := sanitizeCatalogSlug(item.Slug)
		if slug == "" {
			continue
		}
		pluginID := catalogPluginID(item)
		version, requiresEntitlement := latestCatalogVersion(item.Versions)
		if version == "" {
			continue
		}
		tier, entitlement, status := remoteTier(item.Tier, requiresEntitlement)
		out = append(out, Plugin{
			ID:                pluginID,
			PluginID:          pluginID,
			Name:              defaultText(item.Name, slug),
			Description:       strings.TrimSpace(item.Summary),
			Category:          defaultText(item.Category, "official"),
			Type:              "remote",
			Tier:              tier,
			Version:           version,
			Vendor:            defaultText(item.VendorName, "AsterCloud"),
			Status:            status,
			EntitlementStatus: entitlement,
			Surfaces:          []string{"admin"},
			EntryPoint:        "/admin/plugins",
			Configurable:      false,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
	}
	sortPlugins(out)
	return out
}

func mapRemoteCatalogPackages(index remoteCatalogIndex, now time.Time) []packageRecord {
	out := make([]packageRecord, 0)
	for _, item := range index.Plugins {
		slug := sanitizeCatalogSlug(item.Slug)
		if slug == "" {
			continue
		}
		pluginID := catalogPluginID(item)
		for _, version := range item.Versions {
			if version.Status != "published" && version.Status != "deprecated" {
				continue
			}
			compatibilityJSON := jsonString(version.Compatibility, "[]")
			for _, pkg := range version.Packages {
				packageID := strings.TrimSpace(pkg.PublicID)
				if packageID == "" {
					continue
				}
				packageURI := packageURIFromEnvelope(pkg.Signature)
				out = append(out, packageRecord{
					PluginID:            pluginID,
					PluginSlug:          slug,
					PluginPublicID:      strings.TrimSpace(item.PublicID),
					VersionPublicID:     strings.TrimSpace(version.PublicID),
					Version:             strings.TrimSpace(version.Version),
					Channel:             strings.TrimSpace(version.Channel),
					RequiredEntitlement: version.RequiredEntitlement,
					MinCoreVersion:      strings.TrimSpace(version.MinCoreVersion),
					MaxCoreVersion:      strings.TrimSpace(version.MaxCoreVersion),
					PackageID:           packageID,
					PackageURI:          packageURI,
					OS:                  strings.ToLower(strings.TrimSpace(pkg.OS)),
					Arch:                strings.ToLower(strings.TrimSpace(pkg.Arch)),
					SHA256:              strings.ToLower(strings.TrimSpace(pkg.SHA256)),
					SizeBytes:           pkg.SizeBytes,
					SignatureJSON:       jsonString(pkg.Signature, "{}"),
					Revoked:             pkg.Revoked,
					CompatibilityJSON:   compatibilityJSON,
					CreatedAt:           now,
					UpdatedAt:           now,
				})
			}
		}
	}
	sortPackageRecords(out)
	return out
}

func packageURIFromEnvelope(envelope catalogEnvelope) string {
	var payload packageSignaturePayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.URI)
}

func officialPluginID(slug string) string {
	slug = sanitizeCatalogSlug(slug)
	return "com.astercloud.catalog." + slug
}

func catalogPluginID(item remoteCatalogPlugin) string {
	if strings.TrimSpace(item.PluginID) != "" {
		return strings.TrimSpace(item.PluginID)
	}
	return officialPluginID(item.Slug)
}

func sanitizeCatalogSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = catalogSlugPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-._")
	return value
}

func defaultText(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func jsonString(value any, fallback string) string {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) == 0 {
		return fallback
	}
	return string(raw)
}
