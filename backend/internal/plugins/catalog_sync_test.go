package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestServiceSyncOfficialCatalogVerifiesCachesAndMapsPlugins(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(): %v", err)
	}
	now := time.Date(2026, 7, 11, 1, 30, 0, 0, time.UTC)
	payload := testCatalogPayload()
	envelope := signedCatalogEnvelope(t, privateKey, "catalog-key-v1", payload, now)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(wrappedCatalogEnvelope(t, envelope))
	}))
	defer server.Close()

	repo := NewMemoryRepository()
	svc := NewServiceWithOptions(repo, ServiceOptions{
		OfficialCatalog: OfficialCatalogConfig{
			Mode:            CatalogModeOnline,
			URL:             server.URL,
			PublicKeyID:     "catalog-key-v1",
			PublicKeyBase64: base64.StdEncoding.EncodeToString(publicKey),
		},
		Now: func() time.Time { return now },
	})

	status, err := svc.SyncOfficialCatalog(context.Background())
	if err != nil {
		t.Fatalf("SyncOfficialCatalog(): %v", err)
	}
	if status.Status != catalogSyncSucceeded || status.CatalogVersion != 7 || status.PluginCount != 2 || status.PayloadSHA256 == "" {
		t.Fatalf("unexpected sync status: %+v", status)
	}

	catalog, err := svc.Catalog(context.Background())
	if err != nil {
		t.Fatalf("Catalog(): %v", err)
	}
	freePlugin := findPlugin(catalog.Plugins, "com.astercloud.catalog.provider-intelligence")
	if freePlugin.ID == "" || freePlugin.Status != StatusDisabled || freePlugin.EntitlementStatus != EntitlementFree || freePlugin.Version != "1.2.3" {
		t.Fatalf("free plugin mismatch: %+v", freePlugin)
	}
	paidPlugin := findPlugin(catalog.Plugins, "com.astercloud.catalog.enterprise-guard")
	if paidPlugin.ID == "" || paidPlugin.Status != StatusLocked || paidPlugin.EntitlementStatus != EntitlementMissing {
		t.Fatalf("paid plugin mismatch: %+v", paidPlugin)
	}

	cached, err := svc.OfficialCatalogStatus(context.Background())
	if err != nil {
		t.Fatalf("OfficialCatalogStatus(): %v", err)
	}
	if cached.CatalogVersion != status.CatalogVersion || cached.PayloadSHA256 != status.PayloadSHA256 {
		t.Fatalf("cached status mismatch: %+v vs %+v", cached, status)
	}
}

func TestOfficialPluginIDUsesCatalogNamespace(t *testing.T) {
	if got := officialPluginID("provider-trust-evidence"); got != "com.astercloud.catalog.provider-trust-evidence" {
		t.Fatalf("officialPluginID(provider-trust-evidence) = %q", got)
	}
	if got := officialPluginID("provider-intelligence"); got != "com.astercloud.catalog.provider-intelligence" {
		t.Fatalf("officialPluginID(provider-intelligence) = %q", got)
	}
}

func TestServiceSyncOfficialCatalogRejectsTamperedPayload(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(): %v", err)
	}
	now := time.Date(2026, 7, 11, 1, 30, 0, 0, time.UTC)
	envelope := signedCatalogEnvelope(t, privateKey, "catalog-key-v1", testCatalogPayload(), now)
	envelope.Payload = json.RawMessage(`{"schema_version":"astercloud.catalog-index.v1","catalog_version":8,"generated_at":"2026-07-11T01:30:00Z","plugins":[]}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(wrappedCatalogEnvelope(t, envelope))
	}))
	defer server.Close()

	svc := NewServiceWithOptions(NewMemoryRepository(), ServiceOptions{
		OfficialCatalog: OfficialCatalogConfig{
			Mode:            CatalogModeOnline,
			URL:             server.URL,
			PublicKeyID:     "catalog-key-v1",
			PublicKeyBase64: base64.StdEncoding.EncodeToString(publicKey),
		},
		Now: func() time.Time { return now },
	})

	status, err := svc.SyncOfficialCatalog(context.Background())
	if !errors.Is(err, ErrCatalogSignature) {
		t.Fatalf("err = %v, want ErrCatalogSignature", err)
	}
	if status.Status != catalogSyncFailed || status.Error == "" {
		t.Fatalf("unexpected failed status: %+v", status)
	}
	catalog, err := svc.Catalog(context.Background())
	if err != nil {
		t.Fatalf("Catalog(): %v", err)
	}
	if len(catalog.Plugins) != 0 {
		t.Fatalf("tampered catalog should not save plugins: %+v", catalog.Plugins)
	}
}

func TestServiceDownloadOfficialPackageVerifiesAndCaches(t *testing.T) {
	svc, repo, packageID, content := newPackageDownloadTestService(t, []byte("signed plugin package"), false)

	status, err := svc.SyncOfficialCatalog(context.Background())
	if err != nil {
		t.Fatalf("SyncOfficialCatalog(): %v", err)
	}
	if status.PluginCount != 1 {
		t.Fatalf("PluginCount = %d, want 1", status.PluginCount)
	}
	result, err := svc.DownloadPackage(context.Background(), "com.astercloud.catalog.provider-intelligence", packageID, PackageDownloadRequest{})
	if err != nil {
		t.Fatalf("DownloadPackage(): %v", err)
	}
	if result.Package.PackageID != packageID || result.SHA256 == "" || result.CachePath == "" {
		t.Fatalf("unexpected download result: %+v", result)
	}
	cached, err := os.ReadFile(result.CachePath)
	if err != nil {
		t.Fatalf("ReadFile(cache): %v", err)
	}
	if string(cached) != string(content) {
		t.Fatalf("cached content = %q, want %q", cached, content)
	}
	record, ok, err := repo.FindPackageCache(context.Background(), packageID)
	if err != nil {
		t.Fatalf("FindPackageCache(): %v", err)
	}
	if !ok || record.Status != PackageCacheStatusCached {
		t.Fatalf("cache record mismatch: ok=%v record=%+v", ok, record)
	}
	installation, err := svc.InstallPackage(context.Background(), "com.astercloud.catalog.provider-intelligence", packageID)
	if err != nil {
		t.Fatalf("InstallPackage(): %v", err)
	}
	if installation.Status != PackageInstallInstalled || installation.PackageID != packageID || installation.CachePath != result.CachePath {
		t.Fatalf("installation mismatch: %+v", installation)
	}
	packages, err := svc.Packages(context.Background(), "com.astercloud.catalog.provider-intelligence")
	if err != nil {
		t.Fatalf("Packages(): %v", err)
	}
	if len(packages) != 1 || packages[0].InstallStatus != PackageInstallInstalled || packages[0].InstalledAt == nil {
		t.Fatalf("package install status mismatch: %+v", packages)
	}
	uninstalled, err := svc.UninstallPackage(context.Background(), "com.astercloud.catalog.provider-intelligence", packageID)
	if err != nil {
		t.Fatalf("UninstallPackage(): %v", err)
	}
	if uninstalled.Status != PackageInstallUninstalled {
		t.Fatalf("uninstall mismatch: %+v", uninstalled)
	}
}

func TestServiceBootstrapOnlySyncDownloadAndActivateLicense(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(): %v", err)
	}
	now := time.Date(2026, 7, 11, 6, 0, 0, 0, time.UTC)
	expiresAt := now.Add(24 * time.Hour)
	content := []byte("bootstrap provider package")
	checksumBytes := sha256.Sum256(content)
	checksum := hex.EncodeToString(checksumBytes[:])
	packageID := "pkg_bootstrap_provider_darwin_arm64"
	packageSignature := signedPackageEnvelope(t, privateKey, "catalog-key-v1", packageSignaturePayload{
		SchemaVersion: packagePayloadSchema,
		Plugin:        "provider-intelligence",
		Version:       "1.2.3",
		OS:            "darwin",
		Arch:          "arm64",
		SHA256:        checksum,
		SizeBytes:     int64(len(content)),
		URI:           "object://provider-intelligence/1.2.3/darwin-arm64.pkg",
	}, now)
	catalogEnvelope := signedCatalogEnvelope(t, privateKey, "catalog-key-v1", remoteCatalogIndex{
		SchemaVersion:  catalogIndexSchema,
		CatalogVersion: 13,
		GeneratedAt:    now,
		Plugins: []remoteCatalogPlugin{
			{
				PublicID:   "plg_provider",
				Slug:       "provider-intelligence",
				Name:       "Provider Intelligence",
				Summary:    "Signed provider intelligence plugin.",
				Category:   "official",
				VendorName: "AsterCloud",
				Tier:       "free",
				Versions: []remoteCatalogVersion{
					{
						PublicID:            "plgv_provider",
						Version:             "1.2.3",
						Channel:             "stable",
						Status:              "published",
						MinCoreVersion:      "1.0.0",
						RequiredEntitlement: false,
						Compatibility: []remoteCompatibility{
							{CoreVersionRange: ">=1.0.0 <2.0.0", OS: "darwin", Arch: "arm64", Result: "compatible"},
						},
						Packages: []remoteCatalogPackage{
							{
								PublicID:  packageID,
								OS:        "darwin",
								Arch:      "arm64",
								SHA256:    checksum,
								SizeBytes: int64(len(content)),
								Signature: packageSignature,
							},
						},
					},
				},
			},
		},
	}, now)
	licenseEnvelope := signedLicenseEnvelope(t, privateKey, "catalog-key-v1", licenseSnapshotPayload{
		SchemaVersion:   licenseSnapshotSchema,
		SnapshotID:      "lss_bootstrap",
		SnapshotVersion: 1,
		License: snapshotLicense{
			PublicID:  "lic_bootstrap",
			Edition:   "enterprise",
			Status:    LicenseStatusActive,
			Seats:     5,
			StartsAt:  now.Add(-time.Hour),
			ExpiresAt: &expiresAt,
		},
		Customer: snapshotCustomer{PublicID: "cus_bootstrap"},
		SKU:      snapshotSKU{PublicID: "sku_bootstrap", Code: "ASTER-ENT", Features: json.RawMessage(`{}`), Limits: json.RawMessage(`{}`)},
		Instance: snapshotInstance{
			PublicID:         "inst_bootstrap",
			Fingerprint:      "sha256:00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
			DisplayName:      "router-bootstrap",
			FirstActivatedAt: now,
		},
		Entitlements: []Entitlement{
			{PublicID: "ent_bootstrap", Type: "plugin", ResourceKey: "provider-intelligence", Status: LicenseStatusActive, StartsAt: now.Add(-time.Hour), ExpiresAt: &expiresAt},
		},
		IssuedAt:  now,
		ExpiresAt: expiresAt,
	}, now, expiresAt)

	var serverURL string
	licenseActivationHit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/official/v1/catalog/bootstrap":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": catalogBootstrap{
					SchemaVersion: catalogBootstrapSchema,
					CatalogURL:    serverURL + "/official/v1/catalog/index",
					LicenseURL:    serverURL + "/custom/licenses/activate",
					SigningKeys: []catalogBootstrapKey{
						{
							KeyID:     "catalog-key-v1",
							Purpose:   "*",
							Algorithm: "Ed25519",
							Status:    "active",
							PublicKey: base64.RawURLEncoding.EncodeToString(publicKey),
							Encoding:  "base64url",
							NotBefore: now.Add(-time.Hour),
						},
					},
					GeneratedAt: now,
				},
			})
		case "/official/v1/catalog/index":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(wrappedCatalogEnvelope(t, catalogEnvelope))
		case "/official/v1/packages/" + packageID + "/download":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": packageDownloadGrant{
					ID:              "dgr_bootstrap",
					PublicID:        "dgr_bootstrap_public",
					PackageID:       "internal-package-id",
					PackagePublicID: packageID,
					DownloadURL:     serverURL + "/objects/provider.pkg",
					Headers:         map[string]string{"X-Test-Download": "bootstrap"},
					SHA256:          checksum,
					Signature:       packageSignature,
					ExpiresAt:       now.Add(10 * time.Minute),
					CreatedAt:       now,
				},
			})
		case "/objects/provider.pkg":
			if r.Header.Get("X-Test-Download") != "bootstrap" {
				t.Fatalf("download grant headers were not forwarded")
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		case "/custom/licenses/activate":
			licenseActivationHit = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": activationResponse{Envelope: licenseEnvelope},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	serverURL = server.URL

	svc := NewServiceWithOptions(NewMemoryRepository(), ServiceOptions{
		SecretKey: "test-secret",
		OfficialCatalog: OfficialCatalogConfig{
			Mode:         CatalogModeOnline,
			BootstrapURL: server.URL + "/official/v1/catalog/bootstrap",
		},
		PackageCacheDir: t.TempDir(),
		CoreVersion:     "1.2.0",
		TargetOS:        "darwin",
		TargetArch:      "arm64",
		Now:             func() time.Time { return now },
	})

	status, err := svc.SyncOfficialCatalog(context.Background())
	if err != nil {
		t.Fatalf("SyncOfficialCatalog(): %v", err)
	}
	if status.Status != catalogSyncSucceeded || status.SourceURL != server.URL+"/official/v1/catalog/index" || !status.TrustConfigured || status.LicenseURL != server.URL+"/custom/licenses/activate" {
		t.Fatalf("unexpected bootstrap status: %+v", status)
	}
	result, err := svc.DownloadPackage(context.Background(), "com.astercloud.catalog.provider-intelligence", packageID, PackageDownloadRequest{})
	if err != nil {
		t.Fatalf("DownloadPackage(): %v", err)
	}
	cached, err := os.ReadFile(result.CachePath)
	if err != nil {
		t.Fatalf("ReadFile(cache): %v", err)
	}
	if string(cached) != string(content) {
		t.Fatalf("cached content = %q, want %q", cached, content)
	}
	licenseStatus, err := svc.ActivateLicense(context.Background(), LicenseActivateRequest{
		LicenseID:        "lic_bootstrap",
		ActivationSecret: "activation-secret",
		InstanceID:       "inst_bootstrap",
	})
	if err != nil {
		t.Fatalf("ActivateLicense(): %v", err)
	}
	if !licenseActivationHit || licenseStatus.LicenseID != "lic_bootstrap" || licenseStatus.Status != LicenseStatusActive {
		t.Fatalf("license activation mismatch: hit=%v status=%+v", licenseActivationHit, licenseStatus)
	}
}

func TestServiceInstallPackageRequiresCachedPackage(t *testing.T) {
	svc, _, packageID, _ := newPackageDownloadTestService(t, []byte("signed plugin package"), false)

	if _, err := svc.SyncOfficialCatalog(context.Background()); err != nil {
		t.Fatalf("SyncOfficialCatalog(): %v", err)
	}
	_, err := svc.InstallPackage(context.Background(), "com.astercloud.catalog.provider-intelligence", packageID)
	if !errors.Is(err, ErrPackageNotCached) {
		t.Fatalf("InstallPackage() error = %v, want ErrPackageNotCached", err)
	}
}

func TestServiceImportLicenseUnlocksPaidPackageDownload(t *testing.T) {
	svc, packageID, content, licenseEnvelope := newPaidPackageDownloadTestService(t)

	if _, err := svc.SyncOfficialCatalog(context.Background()); err != nil {
		t.Fatalf("SyncOfficialCatalog(): %v", err)
	}
	before, err := svc.Catalog(context.Background())
	if err != nil {
		t.Fatalf("Catalog(before): %v", err)
	}
	locked := findPlugin(before.Plugins, "com.astercloud.catalog.enterprise-guard")
	if locked.Status != StatusLocked || locked.EntitlementStatus != EntitlementMissing {
		t.Fatalf("paid plugin should start locked: %+v", locked)
	}
	rawEnvelope, err := json.Marshal(licenseEnvelope)
	if err != nil {
		t.Fatalf("marshal license envelope: %v", err)
	}
	status, err := svc.ImportLicense(context.Background(), LicenseImportRequest{
		Envelope:         rawEnvelope,
		ActivationSecret: "activation-secret",
	})
	if err != nil {
		t.Fatalf("ImportLicense(): %v", err)
	}
	if status.LicenseID != "lic_enterprise" || status.Status != LicenseStatusActive || len(status.Entitlements) != 1 {
		t.Fatalf("license status mismatch: %+v", status)
	}
	after, err := svc.Catalog(context.Background())
	if err != nil {
		t.Fatalf("Catalog(after): %v", err)
	}
	unlocked := findPlugin(after.Plugins, "com.astercloud.catalog.enterprise-guard")
	if unlocked.Status != StatusDisabled || unlocked.EntitlementStatus != EntitlementIncluded {
		t.Fatalf("paid plugin should be unlocked by local license: %+v", unlocked)
	}
	result, err := svc.DownloadPackage(context.Background(), "com.astercloud.catalog.enterprise-guard", packageID, PackageDownloadRequest{})
	if err != nil {
		t.Fatalf("DownloadPackage(paid): %v", err)
	}
	cached, err := os.ReadFile(result.CachePath)
	if err != nil {
		t.Fatalf("ReadFile(cache): %v", err)
	}
	if string(cached) != string(content) {
		t.Fatalf("cached content = %q, want %q", cached, content)
	}
}

func TestServiceImportLicenseRejectsTamperedEnvelope(t *testing.T) {
	svc, _, _, licenseEnvelope := newPaidPackageDownloadTestService(t)
	licenseEnvelope.Payload = json.RawMessage(`{"schema_version":"astercloud.license-snapshot.v1","snapshot_version":2}`)
	rawEnvelope, err := json.Marshal(licenseEnvelope)
	if err != nil {
		t.Fatalf("marshal license envelope: %v", err)
	}
	_, err = svc.ImportLicense(context.Background(), LicenseImportRequest{Envelope: rawEnvelope})
	if !errors.Is(err, ErrLicenseSignature) {
		t.Fatalf("ImportLicense(tampered) error = %v, want ErrLicenseSignature", err)
	}
}

func TestServiceDownloadOfficialPackageRejectsChecksumMismatch(t *testing.T) {
	svc, repo, packageID, _ := newPackageDownloadTestService(t, []byte("signed plugin packagf"), true)

	if _, err := svc.SyncOfficialCatalog(context.Background()); err != nil {
		t.Fatalf("SyncOfficialCatalog(): %v", err)
	}
	_, err := svc.DownloadPackage(context.Background(), "com.astercloud.catalog.provider-intelligence", packageID, PackageDownloadRequest{})
	if !errors.Is(err, ErrPackageChecksum) {
		t.Fatalf("DownloadPackage() error = %v, want ErrPackageChecksum", err)
	}
	if record, ok, err := repo.FindPackageCache(context.Background(), packageID); err != nil {
		t.Fatalf("FindPackageCache(): %v", err)
	} else if ok {
		t.Fatalf("checksum failure should not save cache: %+v", record)
	}
}

func TestServicePrivateMirrorDownloadsSignedPackageURI(t *testing.T) {
	svc, _, packageID, content := newPrivateMirrorPackageTestService(t)

	status, err := svc.SyncOfficialCatalog(context.Background())
	if err != nil {
		t.Fatalf("SyncOfficialCatalog(): %v", err)
	}
	if status.Mode != CatalogModePrivateMirror || status.Status != catalogSyncSucceeded {
		t.Fatalf("unexpected mirror status: %+v", status)
	}
	result, err := svc.DownloadPackage(context.Background(), "com.astercloud.catalog.provider-intelligence", packageID, PackageDownloadRequest{})
	if err != nil {
		t.Fatalf("DownloadPackage(private mirror): %v", err)
	}
	cached, err := os.ReadFile(result.CachePath)
	if err != nil {
		t.Fatalf("ReadFile(cache): %v", err)
	}
	if string(cached) != string(content) {
		t.Fatalf("cached content = %q, want %q", cached, content)
	}
}

func TestServiceImportOfflinePackageVerifiesAndCaches(t *testing.T) {
	svc, repo, packageID, content := newPrivateMirrorPackageTestService(t)
	if _, err := svc.SyncOfficialCatalog(context.Background()); err != nil {
		t.Fatalf("SyncOfficialCatalog(): %v", err)
	}
	offlineSvc := NewServiceWithOptions(repo, ServiceOptions{
		OfficialCatalog: OfficialCatalogConfig{
			Mode:            CatalogModeOffline,
			PublicKeyID:     svc.catalogConfig.PublicKeyID,
			PublicKeyBase64: svc.catalogConfig.PublicKeyBase64,
		},
		PackageCacheDir: t.TempDir(),
		CoreVersion:     "1.2.0",
		TargetOS:        "darwin",
		TargetArch:      "arm64",
		Now:             svc.now,
	})
	sum := sha256.Sum256(content)
	file := map[string]any{
		"package_id":     packageID,
		"content_base64": base64.StdEncoding.EncodeToString(content),
		"sha256":         hex.EncodeToString(sum[:]),
		"size_bytes":     len(content),
	}
	rawFile, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("marshal package file: %v", err)
	}
	result, err := offlineSvc.ImportPackage(context.Background(), "com.astercloud.catalog.provider-intelligence", packageID, PackageImportRequest{FileJSON: rawFile})
	if err != nil {
		t.Fatalf("ImportPackage(): %v", err)
	}
	cached, err := os.ReadFile(result.CachePath)
	if err != nil {
		t.Fatalf("ReadFile(cache): %v", err)
	}
	if string(cached) != string(content) {
		t.Fatalf("cached content = %q, want %q", cached, content)
	}
	if _, err := offlineSvc.InstallPackage(context.Background(), "com.astercloud.catalog.provider-intelligence", packageID); err != nil {
		t.Fatalf("InstallPackage(imported): %v", err)
	}
}

func TestServiceImportOfflinePackageRejectsChecksumMismatch(t *testing.T) {
	svc, repo, packageID, _ := newPrivateMirrorPackageTestService(t)
	if _, err := svc.SyncOfficialCatalog(context.Background()); err != nil {
		t.Fatalf("SyncOfficialCatalog(): %v", err)
	}
	offlineSvc := NewServiceWithOptions(repo, ServiceOptions{
		OfficialCatalog: OfficialCatalogConfig{
			Mode:            CatalogModeOffline,
			PublicKeyID:     svc.catalogConfig.PublicKeyID,
			PublicKeyBase64: svc.catalogConfig.PublicKeyBase64,
		},
		PackageCacheDir: t.TempDir(),
		CoreVersion:     "1.2.0",
		TargetOS:        "darwin",
		TargetArch:      "arm64",
		Now:             svc.now,
	})
	rawFile, err := json.Marshal(map[string]any{
		"package_id":     packageID,
		"content_base64": base64.StdEncoding.EncodeToString([]byte("tampered package")),
	})
	if err != nil {
		t.Fatalf("marshal package file: %v", err)
	}
	_, err = offlineSvc.ImportPackage(context.Background(), "com.astercloud.catalog.provider-intelligence", packageID, PackageImportRequest{FileJSON: rawFile})
	if !errors.Is(err, ErrPackageChecksum) {
		t.Fatalf("ImportPackage() error = %v, want ErrPackageChecksum", err)
	}
}

func TestServiceSyncOfficialCatalogBlocksRevokedAdvisoryVersion(t *testing.T) {
	svc, repo, packageID := newRevokedAdvisoryPackageTestService(t, false)

	status, err := svc.SyncOfficialCatalog(context.Background())
	if err != nil {
		t.Fatalf("SyncOfficialCatalog(): %v", err)
	}
	if status.AdvisoryCount != 1 {
		t.Fatalf("AdvisoryCount = %d, want 1", status.AdvisoryCount)
	}
	packages, err := svc.Packages(context.Background(), "com.astercloud.catalog.provider-intelligence")
	if err != nil {
		t.Fatalf("Packages(): %v", err)
	}
	if len(packages) != 1 || !packages[0].Revoked || !packages[0].RevokedByAdvisory || packages[0].AdvisoryID != "AST-2026-0001" {
		t.Fatalf("expected advisory-revoked package: %+v", packages)
	}
	if _, err := svc.DownloadPackage(context.Background(), "com.astercloud.catalog.provider-intelligence", packageID, PackageDownloadRequest{}); !errors.Is(err, ErrPackageRevoked) {
		t.Fatalf("DownloadPackage() error = %v, want ErrPackageRevoked", err)
	}
	now := time.Date(2026, 7, 11, 4, 0, 0, 0, time.UTC)
	if err := repo.SavePackageCache(context.Background(), packageCacheRecord{
		PackageID: packageID,
		PluginID:  "com.astercloud.catalog.provider-intelligence",
		Version:   "1.2.3",
		OS:        "darwin",
		Arch:      "arm64",
		SHA256:    packages[0].SHA256,
		SizeBytes: packages[0].SizeBytes,
		CachePath: "/tmp/provider.pkg",
		Status:    PackageCacheStatusCached,
		CachedAt:  now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SavePackageCache(): %v", err)
	}
	if _, err := svc.InstallPackage(context.Background(), "com.astercloud.catalog.provider-intelligence", packageID); !errors.Is(err, ErrPackageRevoked) {
		t.Fatalf("InstallPackage() error = %v, want ErrPackageRevoked", err)
	}
}

func TestServiceSyncOfficialCatalogRejectsTamperedAdvisorySignature(t *testing.T) {
	svc, _, _ := newRevokedAdvisoryPackageTestService(t, true)

	status, err := svc.SyncOfficialCatalog(context.Background())
	if !errors.Is(err, ErrCatalogSignature) {
		t.Fatalf("SyncOfficialCatalog() error = %v, want ErrCatalogSignature", err)
	}
	if status.Status != catalogSyncFailed || status.Error == "" {
		t.Fatalf("unexpected failed status: %+v", status)
	}
	catalog, err := svc.Catalog(context.Background())
	if err != nil {
		t.Fatalf("Catalog(): %v", err)
	}
	if len(catalog.Plugins) != 0 {
		t.Fatalf("tampered advisory should not save catalog data: %+v", catalog.Plugins)
	}
}

func testCatalogPayload() remoteCatalogIndex {
	return remoteCatalogIndex{
		SchemaVersion:  catalogIndexSchema,
		CatalogVersion: 7,
		GeneratedAt:    time.Date(2026, 7, 11, 1, 29, 0, 0, time.UTC),
		Plugins: []remoteCatalogPlugin{
			{
				PublicID:   "plg_provider",
				Slug:       "provider-intelligence",
				Name:       "Provider Intelligence",
				Summary:    "Signed provider intelligence plugin.",
				Category:   "official",
				VendorName: "AsterCloud",
				Tier:       "free",
				Versions: []remoteCatalogVersion{
					{PublicID: "plgv_provider", Version: "1.2.3", Status: "published", RequiredEntitlement: false},
				},
			},
			{
				PublicID:   "plg_guard",
				Slug:       "enterprise-guard",
				Name:       "Enterprise Guard",
				Summary:    "Governance plugin.",
				Category:   "governance",
				VendorName: "AsterCloud",
				Tier:       "enterprise",
				Versions: []remoteCatalogVersion{
					{PublicID: "plgv_guard", Version: "2.0.0", Status: "published", RequiredEntitlement: true},
				},
			},
		},
	}
}

func newRevokedAdvisoryPackageTestService(t *testing.T, tamperAdvisory bool) (*Service, *MemoryRepository, string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(): %v", err)
	}
	now := time.Date(2026, 7, 11, 4, 0, 0, 0, time.UTC)
	content := []byte("revoked provider package")
	checksumBytes := sha256.Sum256(content)
	checksum := hex.EncodeToString(checksumBytes[:])
	packageID := "pkg_provider_revoked_darwin_arm64"
	packageSignature := signedPackageEnvelope(t, privateKey, "catalog-key-v1", packageSignaturePayload{
		SchemaVersion: packagePayloadSchema,
		Plugin:        "provider-intelligence",
		Version:       "1.2.3",
		OS:            "darwin",
		Arch:          "arm64",
		SHA256:        checksum,
		SizeBytes:     int64(len(content)),
		URI:           "object://provider-intelligence/1.2.3/darwin-arm64.pkg",
	}, now)
	advisoryPayload := advisorySignaturePayload{
		SchemaVersion: advisoryPayloadSchema,
		AdvisoryID:    "AST-2026-0001",
		Severity:      "critical",
		Title:         "Provider package revoked",
		Summary:       "Revokes vulnerable provider package versions.",
		PublishedAt:   now,
	}
	advisorySignature := signedAdvisoryEnvelope(t, privateKey, "catalog-key-v1", advisoryPayload, now)
	if tamperAdvisory {
		tampered := advisoryPayload
		tampered.Title = "Tampered advisory title"
		rawPayload, err := json.Marshal(tampered)
		if err != nil {
			t.Fatalf("marshal tampered advisory: %v", err)
		}
		advisorySignature.Payload = rawPayload
	}
	payload := remoteCatalogIndex{
		SchemaVersion:  catalogIndexSchema,
		CatalogVersion: 11,
		GeneratedAt:    now,
		Plugins: []remoteCatalogPlugin{
			{
				PublicID:   "plg_provider",
				Slug:       "provider-intelligence",
				Name:       "Provider Intelligence",
				Summary:    "Signed provider intelligence plugin.",
				Category:   "official",
				VendorName: "AsterCloud",
				Tier:       "free",
				Versions: []remoteCatalogVersion{
					{
						PublicID:            "plgv_provider",
						Version:             "1.2.3",
						Channel:             "stable",
						Status:              "published",
						MinCoreVersion:      "1.0.0",
						RequiredEntitlement: false,
						Compatibility: []remoteCompatibility{
							{CoreVersionRange: ">=1.0.0 <2.0.0", OS: "darwin", Arch: "arm64", Result: "compatible"},
						},
						Packages: []remoteCatalogPackage{
							{
								PublicID:  packageID,
								OS:        "darwin",
								Arch:      "arm64",
								SHA256:    checksum,
								SizeBytes: int64(len(content)),
								Signature: packageSignature,
							},
						},
					},
				},
			},
		},
		Advisories: []remoteCatalogAdvisory{
			{
				PublicID:    "adv_2026_0001",
				AdvisoryID:  "AST-2026-0001",
				Severity:    "critical",
				Title:       advisoryPayload.Title,
				Summary:     advisoryPayload.Summary,
				PublishedAt: &now,
				Affected: []remoteAffectedVersion{
					{
						PublicID:     "aff_provider_123",
						AdvisoryID:   "adv_internal",
						PluginID:     "plg_provider",
						PluginSlug:   "provider-intelligence",
						VersionRange: ">=1.2.0 <1.2.4",
						FixedVersion: "1.2.4",
						Revoked:      true,
						CreatedAt:    now,
					},
				},
				Signature: advisorySignature,
			},
		},
	}
	catalogEnvelope := signedCatalogEnvelope(t, privateKey, "catalog-key-v1", payload, now)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/official/v1/catalog/index":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(wrappedCatalogEnvelope(t, catalogEnvelope))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	repo := NewMemoryRepository()
	svc := NewServiceWithOptions(repo, ServiceOptions{
		OfficialCatalog: OfficialCatalogConfig{
			Mode:            CatalogModeOnline,
			URL:             server.URL + "/official/v1/catalog/index",
			PublicKeyID:     "catalog-key-v1",
			PublicKeyBase64: base64.StdEncoding.EncodeToString(publicKey),
		},
		PackageCacheDir: t.TempDir(),
		CoreVersion:     "1.2.0",
		TargetOS:        "darwin",
		TargetArch:      "arm64",
		Now:             func() time.Time { return now },
	})
	return svc, repo, packageID
}

func newPrivateMirrorPackageTestService(t *testing.T) (*Service, *MemoryRepository, string, []byte) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(): %v", err)
	}
	now := time.Date(2026, 7, 11, 5, 0, 0, 0, time.UTC)
	content := []byte("private mirror provider package")
	checksumBytes := sha256.Sum256(content)
	checksum := hex.EncodeToString(checksumBytes[:])
	packageID := "pkg_provider_mirror_darwin_arm64"
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/official/v1/catalog/index":
			packageSignature := signedPackageEnvelope(t, privateKey, "catalog-key-v1", packageSignaturePayload{
				SchemaVersion: packagePayloadSchema,
				Plugin:        "provider-intelligence",
				Version:       "1.2.3",
				OS:            "darwin",
				Arch:          "arm64",
				SHA256:        checksum,
				SizeBytes:     int64(len(content)),
				URI:           serverURL + "/mirror/provider.pkg",
			}, now)
			payload := remoteCatalogIndex{
				SchemaVersion:  catalogIndexSchema,
				CatalogVersion: 12,
				GeneratedAt:    now,
				Plugins: []remoteCatalogPlugin{
					{
						PublicID:   "plg_provider",
						Slug:       "provider-intelligence",
						Name:       "Provider Intelligence",
						Summary:    "Signed provider intelligence plugin.",
						Category:   "official",
						VendorName: "AsterCloud",
						Tier:       "free",
						Versions: []remoteCatalogVersion{
							{
								PublicID:            "plgv_provider",
								Version:             "1.2.3",
								Channel:             "stable",
								Status:              "published",
								MinCoreVersion:      "1.0.0",
								RequiredEntitlement: false,
								Compatibility: []remoteCompatibility{
									{CoreVersionRange: ">=1.0.0 <2.0.0", OS: "darwin", Arch: "arm64", Result: "compatible"},
								},
								Packages: []remoteCatalogPackage{
									{
										PublicID:  packageID,
										OS:        "darwin",
										Arch:      "arm64",
										SHA256:    checksum,
										SizeBytes: int64(len(content)),
										Signature: packageSignature,
									},
								},
							},
						},
					},
				},
			}
			catalogEnvelope := signedCatalogEnvelope(t, privateKey, "catalog-key-v1", payload, now)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(wrappedCatalogEnvelope(t, catalogEnvelope))
		case "/mirror/provider.pkg":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		case "/official/v1/packages/" + packageID + "/download":
			t.Fatalf("private mirror download must not call official package authorization endpoint")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	serverURL = server.URL
	repo := NewMemoryRepository()
	svc := NewServiceWithOptions(repo, ServiceOptions{
		OfficialCatalog: OfficialCatalogConfig{
			Mode:            CatalogModePrivateMirror,
			URL:             server.URL + "/official/v1/catalog/index",
			PublicKeyID:     "catalog-key-v1",
			PublicKeyBase64: base64.StdEncoding.EncodeToString(publicKey),
		},
		PackageCacheDir: t.TempDir(),
		CoreVersion:     "1.2.0",
		TargetOS:        "darwin",
		TargetArch:      "arm64",
		Now:             func() time.Time { return now },
	})
	return svc, repo, packageID, content
}

func newPackageDownloadTestService(t *testing.T, servedContent []byte, forceChecksumMismatch bool) (*Service, *MemoryRepository, string, []byte) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(): %v", err)
	}
	now := time.Date(2026, 7, 11, 2, 30, 0, 0, time.UTC)
	expectedContent := servedContent
	if forceChecksumMismatch {
		expectedContent = []byte("signed plugin package")
	}
	checksumBytes := sha256.Sum256(expectedContent)
	checksum := hex.EncodeToString(checksumBytes[:])
	packageID := "pkg_provider_darwin_arm64"
	packageSignature := signedPackageEnvelope(t, privateKey, "catalog-key-v1", packageSignaturePayload{
		SchemaVersion: packagePayloadSchema,
		Plugin:        "provider-intelligence",
		Version:       "1.2.3",
		OS:            "darwin",
		Arch:          "arm64",
		SHA256:        checksum,
		SizeBytes:     int64(len(expectedContent)),
		URI:           "object://provider-intelligence/1.2.3/darwin-arm64.pkg",
	}, now)
	payload := remoteCatalogIndex{
		SchemaVersion:  catalogIndexSchema,
		CatalogVersion: 9,
		GeneratedAt:    now,
		Plugins: []remoteCatalogPlugin{
			{
				PublicID:   "plg_provider",
				Slug:       "provider-intelligence",
				Name:       "Provider Intelligence",
				Summary:    "Signed provider intelligence plugin.",
				Category:   "official",
				VendorName: "AsterCloud",
				Tier:       "free",
				Versions: []remoteCatalogVersion{
					{
						PublicID:            "plgv_provider",
						Version:             "1.2.3",
						Channel:             "stable",
						Status:              "published",
						MinCoreVersion:      "1.0.0",
						RequiredEntitlement: false,
						Compatibility: []remoteCompatibility{
							{CoreVersionRange: ">=1.0.0 <2.0.0", OS: "darwin", Arch: "arm64", Result: "compatible"},
						},
						Packages: []remoteCatalogPackage{
							{
								PublicID:  packageID,
								OS:        "darwin",
								Arch:      "arm64",
								SHA256:    checksum,
								SizeBytes: int64(len(expectedContent)),
								Signature: packageSignature,
							},
						},
					},
				},
			},
		},
	}
	catalogEnvelope := signedCatalogEnvelope(t, privateKey, "catalog-key-v1", payload, now)
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/official/v1/catalog/index":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(wrappedCatalogEnvelope(t, catalogEnvelope))
		case "/official/v1/packages/" + packageID + "/download":
			if r.Header.Get("X-Aster-Core-Version") != "1.2.0" || r.Header.Get("X-Aster-OS") != "darwin" || r.Header.Get("X-Aster-Arch") != "arm64" {
				t.Fatalf("missing compatibility headers: %+v", r.Header)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": packageDownloadGrant{
					ID:              "dgr_test",
					PublicID:        "dgr_test_public",
					PackageID:       "internal-package-id",
					PackagePublicID: packageID,
					DownloadURL:     serverURL + "/objects/provider.pkg",
					Headers:         map[string]string{"X-Test-Download": "ok"},
					SHA256:          checksum,
					Signature:       packageSignature,
					ExpiresAt:       now.Add(10 * time.Minute),
					CreatedAt:       now,
				},
			})
		case "/objects/provider.pkg":
			if r.Header.Get("X-Test-Download") != "ok" {
				t.Fatalf("download grant headers were not forwarded")
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(servedContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	serverURL = server.URL

	repo := NewMemoryRepository()
	svc := NewServiceWithOptions(repo, ServiceOptions{
		OfficialCatalog: OfficialCatalogConfig{
			Mode:            CatalogModeOnline,
			URL:             server.URL + "/official/v1/catalog/index",
			PublicKeyID:     "catalog-key-v1",
			PublicKeyBase64: base64.StdEncoding.EncodeToString(publicKey),
		},
		PackageCacheDir: t.TempDir(),
		CoreVersion:     "1.2.0",
		TargetOS:        "darwin",
		TargetArch:      "arm64",
		Now:             func() time.Time { return now },
	})
	return svc, repo, packageID, servedContent
}

func newPaidPackageDownloadTestService(t *testing.T) (*Service, string, []byte, catalogEnvelope) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(): %v", err)
	}
	now := time.Date(2026, 7, 11, 3, 15, 0, 0, time.UTC)
	expiresAt := now.Add(24 * time.Hour)
	content := []byte("paid enterprise guard package")
	checksumBytes := sha256.Sum256(content)
	checksum := hex.EncodeToString(checksumBytes[:])
	packageID := "pkg_guard_darwin_arm64"
	packageSignature := signedPackageEnvelope(t, privateKey, "catalog-key-v1", packageSignaturePayload{
		SchemaVersion: packagePayloadSchema,
		Plugin:        "enterprise-guard",
		Version:       "2.0.0",
		OS:            "darwin",
		Arch:          "arm64",
		SHA256:        checksum,
		SizeBytes:     int64(len(content)),
		URI:           "object://enterprise-guard/2.0.0/darwin-arm64.pkg",
	}, now)
	payload := remoteCatalogIndex{
		SchemaVersion:  catalogIndexSchema,
		CatalogVersion: 10,
		GeneratedAt:    now,
		Plugins: []remoteCatalogPlugin{
			{
				PublicID:   "plg_guard",
				Slug:       "enterprise-guard",
				Name:       "Enterprise Guard",
				Summary:    "Governance plugin.",
				Category:   "governance",
				VendorName: "AsterCloud",
				Tier:       "enterprise",
				Versions: []remoteCatalogVersion{
					{
						PublicID:            "plgv_guard",
						Version:             "2.0.0",
						Channel:             "stable",
						Status:              "published",
						MinCoreVersion:      "1.0.0",
						RequiredEntitlement: true,
						Compatibility: []remoteCompatibility{
							{CoreVersionRange: ">=1.0.0 <2.0.0", OS: "darwin", Arch: "arm64", Result: "compatible"},
						},
						Packages: []remoteCatalogPackage{
							{
								PublicID:  packageID,
								OS:        "darwin",
								Arch:      "arm64",
								SHA256:    checksum,
								SizeBytes: int64(len(content)),
								Signature: packageSignature,
							},
						},
					},
				},
			},
		},
	}
	catalogEnvelope := signedCatalogEnvelope(t, privateKey, "catalog-key-v1", payload, now)
	licenseEnvelope := signedLicenseEnvelope(t, privateKey, "catalog-key-v1", licenseSnapshotPayload{
		SchemaVersion:   licenseSnapshotSchema,
		SnapshotID:      "lss_enterprise",
		SnapshotVersion: 1,
		License: snapshotLicense{
			PublicID:  "lic_enterprise",
			Edition:   "enterprise",
			Status:    LicenseStatusActive,
			Seats:     5,
			StartsAt:  now.Add(-time.Hour),
			ExpiresAt: &expiresAt,
		},
		Customer: snapshotCustomer{PublicID: "cus_enterprise"},
		SKU:      snapshotSKU{PublicID: "sku_enterprise", Code: "ASTER-ENT", Features: json.RawMessage(`{}`), Limits: json.RawMessage(`{}`)},
		Instance: snapshotInstance{
			PublicID:         "inst_router",
			Fingerprint:      "sha256:00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
			DisplayName:      "router-a",
			FirstActivatedAt: now,
		},
		Entitlements: []Entitlement{
			{PublicID: "ent_guard", Type: "plugin", ResourceKey: "enterprise-guard", Status: LicenseStatusActive, StartsAt: now.Add(-time.Hour), ExpiresAt: &expiresAt},
		},
		IssuedAt:  now,
		ExpiresAt: expiresAt,
	}, now, expiresAt)
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/official/v1/catalog/index":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(wrappedCatalogEnvelope(t, catalogEnvelope))
		case "/official/v1/packages/" + packageID + "/download":
			if r.Header.Get("X-Aster-License-ID") != "lic_enterprise" || r.Header.Get("X-Aster-Activation-Secret") != "activation-secret" || r.Header.Get("X-Aster-Instance-ID") != "inst_router" {
				t.Fatalf("missing license headers: %+v", r.Header)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": packageDownloadGrant{
					ID:              "dgr_paid",
					PublicID:        "dgr_paid_public",
					PackageID:       "internal-package-id",
					PackagePublicID: packageID,
					DownloadURL:     serverURL + "/objects/guard.pkg",
					Headers:         map[string]string{"X-Test-Download": "paid"},
					SHA256:          checksum,
					Signature:       packageSignature,
					ExpiresAt:       now.Add(10 * time.Minute),
					CreatedAt:       now,
				},
			})
		case "/objects/guard.pkg":
			if r.Header.Get("X-Test-Download") != "paid" {
				t.Fatalf("download grant headers were not forwarded")
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	serverURL = server.URL
	repo := NewMemoryRepository()
	svc := NewServiceWithOptions(repo, ServiceOptions{
		SecretKey: "test-secret",
		OfficialCatalog: OfficialCatalogConfig{
			Mode:            CatalogModeOnline,
			URL:             server.URL + "/official/v1/catalog/index",
			PublicKeyID:     "catalog-key-v1",
			PublicKeyBase64: base64.StdEncoding.EncodeToString(publicKey),
		},
		PackageCacheDir: t.TempDir(),
		CoreVersion:     "1.2.0",
		TargetOS:        "darwin",
		TargetArch:      "arm64",
		Now:             func() time.Time { return now },
	})
	return svc, packageID, content, licenseEnvelope
}

func signedCatalogEnvelope(t *testing.T, privateKey ed25519.PrivateKey, keyID string, payload any, issuedAt time.Time) catalogEnvelope {
	t.Helper()
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	envelope := catalogEnvelope{
		SchemaVersion: catalogEnvelopeSchema,
		Purpose:       catalogIndexPurpose,
		KeyID:         keyID,
		Algorithm:     "Ed25519",
		IssuedAt:      issuedAt.UTC().Format(time.RFC3339Nano),
		Payload:       rawPayload,
	}
	message, err := catalogEnvelopeSigningMessage(envelope)
	if err != nil {
		t.Fatalf("signing message: %v", err)
	}
	envelope.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, message))
	return envelope
}

func signedPackageEnvelope(t *testing.T, privateKey ed25519.PrivateKey, keyID string, payload packageSignaturePayload, issuedAt time.Time) catalogEnvelope {
	t.Helper()
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal package payload: %v", err)
	}
	envelope := catalogEnvelope{
		SchemaVersion: catalogEnvelopeSchema,
		Purpose:       packageEnvelopePurpose,
		KeyID:         keyID,
		Algorithm:     "Ed25519",
		IssuedAt:      issuedAt.UTC().Format(time.RFC3339Nano),
		Payload:       rawPayload,
	}
	message, err := catalogEnvelopeSigningMessage(envelope)
	if err != nil {
		t.Fatalf("signing package message: %v", err)
	}
	envelope.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, message))
	return envelope
}

func signedAdvisoryEnvelope(t *testing.T, privateKey ed25519.PrivateKey, keyID string, payload advisorySignaturePayload, issuedAt time.Time) catalogEnvelope {
	t.Helper()
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal advisory payload: %v", err)
	}
	envelope := catalogEnvelope{
		SchemaVersion: catalogEnvelopeSchema,
		Purpose:       advisoryPurpose,
		KeyID:         keyID,
		Algorithm:     "Ed25519",
		IssuedAt:      issuedAt.UTC().Format(time.RFC3339Nano),
		Payload:       rawPayload,
	}
	message, err := catalogEnvelopeSigningMessage(envelope)
	if err != nil {
		t.Fatalf("signing advisory message: %v", err)
	}
	envelope.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, message))
	return envelope
}

func signedLicenseEnvelope(t *testing.T, privateKey ed25519.PrivateKey, keyID string, payload licenseSnapshotPayload, issuedAt time.Time, expiresAt time.Time) catalogEnvelope {
	t.Helper()
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal license payload: %v", err)
	}
	envelope := catalogEnvelope{
		SchemaVersion: catalogEnvelopeSchema,
		Purpose:       licenseEnvelopePurpose,
		KeyID:         keyID,
		Algorithm:     "Ed25519",
		IssuedAt:      issuedAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt:     expiresAt.UTC().Format(time.RFC3339Nano),
		Payload:       rawPayload,
	}
	message, err := catalogEnvelopeSigningMessage(envelope)
	if err != nil {
		t.Fatalf("signing license message: %v", err)
	}
	envelope.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, message))
	return envelope
}

func wrappedCatalogEnvelope(t *testing.T, envelope catalogEnvelope) []byte {
	t.Helper()
	raw, err := json.Marshal(struct {
		Data catalogEnvelope `json:"data"`
	}{Data: envelope})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return raw
}

func findPlugin(items []Plugin, id string) Plugin {
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	return Plugin{}
}
