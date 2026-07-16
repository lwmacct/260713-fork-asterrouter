package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

const (
	ArtifactS3SinkPluginID      = "com.asterrouter.artifact.s3-compatible-sink"
	artifactSinkDestinationsKey = "artifact_sink_destinations"
	artifactSinkAccessKey       = "access_key"
	artifactSinkSecretKey       = "secret_key"
	artifactSinkSessionToken    = "session_token"
)

type ArtifactSinkRegistry interface {
	SetArtifactSink(controlplane.ArtifactSink) error
	RemoveArtifactSink(string)
}

type ArtifactSinkObjectStore interface {
	Put(context.Context, string, io.Reader, int64, string) (int64, error)
	Delete(context.Context, string) error
}

type ArtifactSinkStoreFactory func(context.Context, controlplane.S3ArtifactStoreConfig) (ArtifactSinkObjectStore, error)

type artifactSinkDestinationRecord struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Provider            string `json:"provider"`
	Endpoint            string `json:"endpoint,omitempty"`
	Region              string `json:"region"`
	Bucket              string `json:"bucket"`
	Prefix              string `json:"prefix,omitempty"`
	ReferenceBaseURL    string `json:"reference_base_url,omitempty"`
	AllowedProfileScope string `json:"allowed_profile_scope,omitempty"`
	AllowedTenantID     string `json:"allowed_tenant_id,omitempty"`
	PathStyle           bool   `json:"path_style"`
	Enabled             bool   `json:"enabled"`
}

type s3ArtifactSink struct {
	destination artifactSinkDestinationRecord
	store       ArtifactSinkObjectStore
}

func (s *Service) ArtifactSinkDestinations(ctx context.Context, pluginID string) ([]ArtifactSinkDestination, error) {
	plugin, err := s.artifactSinkPlugin(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	record, _, err := s.artifactSinkConfigRecord(ctx, plugin.ID)
	if err != nil {
		return nil, err
	}
	destinations, err := artifactSinkDestinationRecords(record)
	if err != nil {
		return nil, err
	}
	result := make([]ArtifactSinkDestination, 0, len(destinations))
	for _, destination := range destinations {
		result = append(result, artifactSinkDestinationFromRecord(destination, record))
	}
	return result, nil
}

func (s *Service) UpsertArtifactSinkDestination(ctx context.Context, pluginID, sinkID string, request ArtifactSinkDestinationRequest) (ArtifactSinkDestination, error) {
	plugin, err := s.artifactSinkPlugin(ctx, pluginID)
	if err != nil {
		return ArtifactSinkDestination{}, err
	}
	if plugin.Status == StatusLocked {
		return ArtifactSinkDestination{}, ErrPluginLocked
	}
	s.artifactSinkMu.Lock()
	defer s.artifactSinkMu.Unlock()
	record, configFound, err := s.artifactSinkConfigRecord(ctx, plugin.ID)
	if err != nil {
		return ArtifactSinkDestination{}, err
	}
	previousRecord := cloneConfigRecord(record)
	destinations, err := artifactSinkDestinationRecords(record)
	if err != nil {
		return ArtifactSinkDestination{}, err
	}
	destination := normalizeArtifactSinkDestination(sinkID, request)
	if err := validateArtifactSinkDestination(destination); err != nil {
		return ArtifactSinkDestination{}, err
	}
	index := -1
	for currentIndex := range destinations {
		if destinations[currentIndex].ID == destination.ID {
			index = currentIndex
			break
		}
	}
	if index >= 0 {
		destinations[index] = destination
	} else {
		destinations = append(destinations, destination)
	}
	if err := s.applyArtifactSinkSecrets(&record, destination.ID, request); err != nil {
		return ArtifactSinkDestination{}, err
	}
	if err := requireArtifactSinkCredentials(record, destination.ID); err != nil {
		return ArtifactSinkDestination{}, err
	}
	if err := saveArtifactSinkDestinations(&record, destinations, s.now().UTC()); err != nil {
		return ArtifactSinkDestination{}, err
	}
	if plugin.Status == StatusEnabled {
		if err := s.syncArtifactSinksLocked(ctx, plugin, record); err != nil {
			return ArtifactSinkDestination{}, err
		}
	}
	if err := s.repo.SaveConfig(ctx, record); err != nil {
		if plugin.Status != StatusEnabled {
			return ArtifactSinkDestination{}, err
		}
		rollbackErr := s.syncArtifactSinksLocked(ctx, plugin, previousArtifactSinkConfig(plugin.ID, previousRecord, configFound))
		return ArtifactSinkDestination{}, errors.Join(err, rollbackErr)
	}
	return artifactSinkDestinationFromRecord(destination, record), nil
}

func (s *Service) DeleteArtifactSinkDestination(ctx context.Context, pluginID, sinkID string) error {
	plugin, err := s.artifactSinkPlugin(ctx, pluginID)
	if err != nil {
		return err
	}
	s.artifactSinkMu.Lock()
	defer s.artifactSinkMu.Unlock()
	record, configFound, err := s.artifactSinkConfigRecord(ctx, plugin.ID)
	if err != nil {
		return err
	}
	previousRecord := cloneConfigRecord(record)
	destinations, err := artifactSinkDestinationRecords(record)
	if err != nil {
		return err
	}
	normalizedID := strings.TrimSpace(sinkID)
	filtered := make([]artifactSinkDestinationRecord, 0, len(destinations))
	destinationFound := false
	for _, destination := range destinations {
		if destination.ID == normalizedID {
			destinationFound = true
			continue
		}
		filtered = append(filtered, destination)
	}
	if !destinationFound {
		return ErrArtifactSinkDestinationNotFound
	}
	for _, name := range []string{artifactSinkAccessKey, artifactSinkSecretKey, artifactSinkSessionToken} {
		delete(record.SecretCiphertexts, artifactSinkConfigSecretKey(normalizedID, name))
		delete(record.SecretHints, artifactSinkConfigSecretKey(normalizedID, name))
	}
	if err := saveArtifactSinkDestinations(&record, filtered, s.now().UTC()); err != nil {
		return err
	}
	if plugin.Status == StatusEnabled {
		if err := s.syncArtifactSinksLocked(ctx, plugin, record); err != nil {
			return err
		}
	}
	if err := s.repo.SaveConfig(ctx, record); err != nil {
		if plugin.Status != StatusEnabled {
			return err
		}
		rollbackErr := s.syncArtifactSinksLocked(ctx, plugin, previousArtifactSinkConfig(plugin.ID, previousRecord, configFound))
		return errors.Join(err, rollbackErr)
	}
	return nil
}

func (s *Service) StartEnabledArtifactSinks(ctx context.Context) error {
	plugin, found, err := s.repo.FindPlugin(ctx, ArtifactS3SinkPluginID)
	if err != nil || !found {
		return err
	}
	record, _, err := s.artifactSinkConfigRecord(ctx, plugin.ID)
	if err != nil {
		return err
	}
	s.artifactSinkMu.Lock()
	defer s.artifactSinkMu.Unlock()
	return s.syncArtifactSinksLocked(ctx, plugin, record)
}

func (s *Service) enableArtifactSinkPlugin(ctx context.Context, plugin Plugin) error {
	record, _, err := s.artifactSinkConfigRecord(ctx, plugin.ID)
	if err != nil {
		return err
	}
	destinations, err := artifactSinkDestinationRecords(record)
	if err != nil {
		return err
	}
	enabled := 0
	for _, destination := range destinations {
		if destination.Enabled {
			enabled++
		}
	}
	if enabled == 0 {
		return fmt.Errorf("%w: at least one enabled artifact sink destination is required", ErrPluginConfigInvalid)
	}
	return s.syncArtifactSinkPluginWithRecord(ctx, plugin, record)
}

func (s *Service) syncArtifactSinkPlugin(ctx context.Context, plugin Plugin) error {
	record, _, err := s.artifactSinkConfigRecord(ctx, plugin.ID)
	if err != nil {
		return err
	}
	return s.syncArtifactSinkPluginWithRecord(ctx, plugin, record)
}

func (s *Service) syncArtifactSinkPluginWithRecord(ctx context.Context, plugin Plugin, record configRecord) error {
	s.artifactSinkMu.Lock()
	defer s.artifactSinkMu.Unlock()
	return s.syncArtifactSinksLocked(ctx, plugin, record)
}

func (s *Service) stopArtifactSinkPlugins() {
	s.artifactSinkMu.Lock()
	defer s.artifactSinkMu.Unlock()
	if s.artifactSinkRegistry != nil {
		for id := range s.registeredArtifactSinks {
			s.artifactSinkRegistry.RemoveArtifactSink(id)
		}
	}
	s.registeredArtifactSinks = map[string]controlplane.ArtifactSink{}
}

func (s *Service) artifactSinkPlugin(ctx context.Context, pluginID string) (Plugin, error) {
	plugin, found, err := s.repo.FindPlugin(ctx, strings.TrimSpace(pluginID))
	if err != nil {
		return Plugin{}, err
	}
	if !found {
		return Plugin{}, ErrPluginNotFound
	}
	if plugin.ID != ArtifactS3SinkPluginID {
		return Plugin{}, ErrPluginNotConfigurable
	}
	return plugin, nil
}

func (s *Service) artifactSinkConfigRecord(ctx context.Context, pluginID string) (configRecord, bool, error) {
	record, found, err := s.repo.FindConfig(ctx, pluginID)
	if err != nil {
		return configRecord{}, false, err
	}
	if !found {
		now := s.now().UTC()
		record = configRecord{
			PluginID: pluginID, Settings: map[string]string{}, SecretCiphertexts: map[string]string{},
			SecretHints: map[string]string{}, CreatedAt: now, UpdatedAt: now,
		}
	}
	return record, found, nil
}

func (s *Service) applyArtifactSinkSecrets(record *configRecord, sinkID string, request ArtifactSinkDestinationRequest) error {
	if record.SecretCiphertexts == nil {
		record.SecretCiphertexts = map[string]string{}
	}
	if record.SecretHints == nil {
		record.SecretHints = map[string]string{}
	}
	secrets := cleanStringMap(request.Secrets)
	if request.ClearSessionToken {
		delete(record.SecretCiphertexts, artifactSinkConfigSecretKey(sinkID, artifactSinkSessionToken))
		delete(record.SecretHints, artifactSinkConfigSecretKey(sinkID, artifactSinkSessionToken))
	}
	for _, name := range []string{artifactSinkAccessKey, artifactSinkSecretKey, artifactSinkSessionToken} {
		value := strings.TrimSpace(secrets[name])
		if value == "" {
			continue
		}
		ciphertext, err := encryptSecret(s.secretKey, value)
		if err != nil {
			return err
		}
		key := artifactSinkConfigSecretKey(sinkID, name)
		record.SecretCiphertexts[key] = ciphertext
		record.SecretHints[key] = maskSecret(value)
	}
	return nil
}

func (s *Service) syncArtifactSinksLocked(ctx context.Context, plugin Plugin, record configRecord) error {
	desired := map[string]controlplane.ArtifactSink{}
	if plugin.Status == StatusEnabled {
		destinations, err := artifactSinkDestinationRecords(record)
		if err != nil {
			return err
		}
		for _, destination := range destinations {
			if !destination.Enabled {
				continue
			}
			sink, err := s.buildArtifactSink(ctx, destination, record)
			if err != nil {
				return err
			}
			desired[destination.ID] = sink
		}
	}
	if len(desired) > 0 && s.artifactSinkRegistry == nil {
		return ErrArtifactSinkRegistryRequired
	}
	desiredIDs := make([]string, 0, len(desired))
	for id := range desired {
		desiredIDs = append(desiredIDs, id)
	}
	sort.Strings(desiredIDs)
	appliedIDs := make([]string, 0, len(desiredIDs))
	for _, id := range desiredIDs {
		appliedIDs = append(appliedIDs, id)
		if err := s.artifactSinkRegistry.SetArtifactSink(desired[id]); err != nil {
			return errors.Join(err, s.rollbackArtifactSinks(appliedIDs))
		}
	}
	if s.artifactSinkRegistry != nil {
		for id := range s.registeredArtifactSinks {
			if _, keep := desired[id]; !keep {
				s.artifactSinkRegistry.RemoveArtifactSink(id)
			}
		}
	}
	s.registeredArtifactSinks = desired
	return nil
}

func (s *Service) rollbackArtifactSinks(appliedIDs []string) error {
	if s.artifactSinkRegistry == nil {
		return nil
	}
	var rollbackErr error
	for index := len(appliedIDs) - 1; index >= 0; index-- {
		id := appliedIDs[index]
		if previous, found := s.registeredArtifactSinks[id]; found {
			rollbackErr = errors.Join(rollbackErr, s.artifactSinkRegistry.SetArtifactSink(previous))
			continue
		}
		s.artifactSinkRegistry.RemoveArtifactSink(id)
	}
	return rollbackErr
}

func (s *Service) buildArtifactSink(ctx context.Context, destination artifactSinkDestinationRecord, config configRecord) (controlplane.ArtifactSink, error) {
	accessKey, err := s.decryptArtifactSinkSecret(config, destination.ID, artifactSinkAccessKey)
	if err != nil {
		return nil, err
	}
	secretKey, err := s.decryptArtifactSinkSecret(config, destination.ID, artifactSinkSecretKey)
	if err != nil {
		return nil, err
	}
	sessionToken, err := s.decryptArtifactSinkSecret(config, destination.ID, artifactSinkSessionToken)
	if err != nil {
		return nil, err
	}
	factory := s.artifactSinkStoreFactory
	if factory == nil {
		factory = func(ctx context.Context, config controlplane.S3ArtifactStoreConfig) (ArtifactSinkObjectStore, error) {
			return controlplane.NewS3ArtifactStore(ctx, config)
		}
	}
	store, err := factory(ctx, controlplane.S3ArtifactStoreConfig{
		Endpoint: destination.Endpoint, Region: destination.Region, Bucket: destination.Bucket, Prefix: destination.Prefix,
		AccessKey: accessKey, SecretKey: secretKey, SessionToken: sessionToken, PathStyle: destination.PathStyle,
	})
	if err != nil {
		return nil, err
	}
	return &s3ArtifactSink{destination: destination, store: store}, nil
}

func (s *Service) decryptArtifactSinkSecret(config configRecord, sinkID, name string) (string, error) {
	ciphertext := strings.TrimSpace(config.SecretCiphertexts[artifactSinkConfigSecretKey(sinkID, name)])
	if ciphertext == "" {
		return "", nil
	}
	return decryptSecret(s.secretKey, ciphertext)
}

func normalizeArtifactSinkDestination(sinkID string, request ArtifactSinkDestinationRequest) artifactSinkDestinationRecord {
	return artifactSinkDestinationRecord{
		ID: strings.TrimSpace(sinkID), Name: strings.TrimSpace(request.Name), Provider: strings.ToLower(strings.TrimSpace(request.Provider)),
		Endpoint: strings.TrimRight(strings.TrimSpace(request.Endpoint), "/"), Region: strings.TrimSpace(request.Region),
		Bucket: strings.TrimSpace(request.Bucket), Prefix: strings.Trim(strings.TrimSpace(request.Prefix), "/"),
		ReferenceBaseURL:    strings.TrimRight(strings.TrimSpace(request.ReferenceBaseURL), "/"),
		AllowedProfileScope: strings.TrimSpace(request.AllowedProfileScope), AllowedTenantID: strings.TrimSpace(request.AllowedTenantID),
		PathStyle: request.PathStyle, Enabled: request.Enabled,
	}
}

func validateArtifactSinkDestination(destination artifactSinkDestinationRecord) error {
	if !validPluginArtifactSinkID(destination.ID) || destination.Name == "" || len(destination.Name) > 160 || invalidArtifactSinkText(destination.Name) ||
		!oneOfString(destination.Provider, "s3", "r2", "oss") || destination.Bucket == "" || len(destination.Bucket) > 255 ||
		destination.Region == "" || invalidArtifactSinkText(destination.Region) || invalidArtifactSinkText(destination.Bucket) || invalidArtifactSinkText(destination.AllowedProfileScope) ||
		!oneOfString(destination.AllowedProfileScope, "", "personal", "relay_operator", "enterprise", "platform") ||
		invalidArtifactSinkText(destination.AllowedTenantID) || strings.Contains(destination.Bucket, "/") ||
		strings.Contains(destination.Prefix, "..") || strings.Contains(destination.Prefix, "\\") || invalidArtifactSinkText(destination.Prefix) {
		return ErrPluginConfigInvalid
	}
	if destination.Provider != "s3" && destination.Endpoint == "" {
		return fmt.Errorf("%w: endpoint is required for %s", ErrPluginConfigInvalid, destination.Provider)
	}
	if destination.Endpoint != "" {
		parsed, err := url.Parse(destination.Endpoint)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
			parsed.Opaque != "" || parsed.RawPath != "" || (parsed.Path != "" && parsed.Path != "/") {
			return fmt.Errorf("%w: endpoint must be an HTTPS origin", ErrPluginConfigInvalid)
		}
	}
	if destination.ReferenceBaseURL != "" {
		parsed, err := url.Parse(destination.ReferenceBaseURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("%w: reference_base_url must be HTTPS", ErrPluginConfigInvalid)
		}
	}
	return nil
}

func requireArtifactSinkCredentials(config configRecord, sinkID string) error {
	for _, name := range []string{artifactSinkAccessKey, artifactSinkSecretKey} {
		if strings.TrimSpace(config.SecretCiphertexts[artifactSinkConfigSecretKey(sinkID, name)]) == "" {
			return fmt.Errorf("%w: %s is required", ErrPluginConfigInvalid, name)
		}
	}
	return nil
}

func artifactSinkDestinationRecords(config configRecord) ([]artifactSinkDestinationRecord, error) {
	raw := strings.TrimSpace(config.Settings[artifactSinkDestinationsKey])
	if raw == "" {
		return []artifactSinkDestinationRecord{}, nil
	}
	var destinations []artifactSinkDestinationRecord
	if err := json.Unmarshal([]byte(raw), &destinations); err != nil {
		return nil, fmt.Errorf("%w: artifact sink destinations are invalid", ErrPluginConfigInvalid)
	}
	if destinations == nil {
		destinations = []artifactSinkDestinationRecord{}
	}
	seen := map[string]struct{}{}
	for _, destination := range destinations {
		if err := validateArtifactSinkDestination(destination); err != nil {
			return nil, err
		}
		if _, duplicate := seen[destination.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate artifact sink id", ErrPluginConfigInvalid)
		}
		seen[destination.ID] = struct{}{}
	}
	sort.Slice(destinations, func(i, j int) bool { return destinations[i].ID < destinations[j].ID })
	return destinations, nil
}

func saveArtifactSinkDestinations(config *configRecord, destinations []artifactSinkDestinationRecord, updatedAt time.Time) error {
	sort.Slice(destinations, func(i, j int) bool { return destinations[i].ID < destinations[j].ID })
	raw, err := json.Marshal(destinations)
	if err != nil {
		return err
	}
	if config.Settings == nil {
		config.Settings = map[string]string{}
	}
	config.Settings[artifactSinkDestinationsKey] = string(raw)
	config.UpdatedAt = updatedAt.UTC()
	if config.CreatedAt.IsZero() {
		config.CreatedAt = config.UpdatedAt
	}
	return nil
}

func previousArtifactSinkConfig(pluginID string, record configRecord, found bool) configRecord {
	if found {
		return record
	}
	return configRecord{
		PluginID: pluginID, Settings: map[string]string{}, SecretCiphertexts: map[string]string{}, SecretHints: map[string]string{},
	}
}

func artifactSinkDestinationFromRecord(destination artifactSinkDestinationRecord, config configRecord) ArtifactSinkDestination {
	hints := map[string]string{}
	for _, name := range []string{artifactSinkAccessKey, artifactSinkSecretKey, artifactSinkSessionToken} {
		if hint := strings.TrimSpace(config.SecretHints[artifactSinkConfigSecretKey(destination.ID, name)]); hint != "" {
			hints[name] = hint
		}
	}
	return ArtifactSinkDestination{
		ID: destination.ID, Name: destination.Name, Provider: destination.Provider, Endpoint: destination.Endpoint, Region: destination.Region,
		Bucket: destination.Bucket, Prefix: destination.Prefix, ReferenceBaseURL: destination.ReferenceBaseURL,
		AllowedProfileScope: destination.AllowedProfileScope, AllowedTenantID: destination.AllowedTenantID,
		PathStyle: destination.PathStyle, Enabled: destination.Enabled, SecretHints: hints,
	}
}

func artifactSinkConfigSecretKey(sinkID, name string) string {
	return "sink:" + base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(sinkID))) + ":" + name
}

func validPluginArtifactSinkID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 160 {
		return false
	}
	for _, character := range id {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("-_.:", character) {
			continue
		}
		return false
	}
	return true
}

func invalidArtifactSinkText(value string) bool {
	return len(value) > 512 || strings.ContainsAny(value, "\x00\r\n")
}

func oneOfString(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func (s *s3ArtifactSink) ID() string { return s.destination.ID }

func (s *s3ArtifactSink) Accepts(owner controlplane.ArtifactOwner) bool {
	return (s.destination.AllowedProfileScope == "" || owner.ProfileScope == s.destination.AllowedProfileScope) &&
		(s.destination.AllowedTenantID == "" || owner.TenantID == s.destination.AllowedTenantID)
}

func (s *s3ArtifactSink) DeliverArtifact(ctx context.Context, request controlplane.ArtifactSinkRequest, body io.Reader) (controlplane.ArtifactSinkResult, error) {
	if body == nil || request.SinkID != s.ID() || request.IdempotencyKey != request.ArtifactID || request.ArtifactID == "" {
		return controlplane.ArtifactSinkResult{}, controlplane.ErrArtifactSinkInvalid
	}
	if !s.Accepts(request.Owner) {
		return controlplane.ArtifactSinkResult{}, controlplane.ErrArtifactSinkForbidden
	}
	key := s.objectKey(request)
	if _, err := s.store.Put(ctx, key, body, request.ExpectedSizeBytes, request.MediaType); err != nil {
		return controlplane.ArtifactSinkResult{}, err
	}
	return controlplane.ArtifactSinkResult{ExternalReference: s.externalReference(key)}, nil
}

func (s *s3ArtifactSink) DeleteArtifact(ctx context.Context, request controlplane.ArtifactSinkRequest) error {
	if request.SinkID != s.ID() || request.IdempotencyKey != request.ArtifactID || request.ArtifactID == "" || !s.Accepts(request.Owner) {
		return controlplane.ErrArtifactSinkInvalid
	}
	return s.store.Delete(ctx, s.objectKey(request))
}

func (s *s3ArtifactSink) objectKey(request controlplane.ArtifactSinkRequest) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		request.Owner.ProfileScope, request.Owner.TenantID, request.Owner.IntegrationID, request.Owner.PrincipalType,
		request.Owner.PrincipalID, request.Owner.ExternalSubjectReference,
	}, "\x00")))
	return "owners/" + hex.EncodeToString(digest[:16]) + "/" + request.ArtifactID
}

func (s *s3ArtifactSink) externalReference(key string) string {
	fullKey := key
	if s.destination.Prefix != "" {
		fullKey = s.destination.Prefix + "/" + key
	}
	escaped := escapeArtifactObjectKey(fullKey)
	if s.destination.ReferenceBaseURL != "" {
		return strings.TrimRight(s.destination.ReferenceBaseURL, "/") + "/" + escaped
	}
	return s.destination.Provider + "://" + url.PathEscape(s.destination.Bucket) + "/" + escaped
}

func escapeArtifactObjectKey(key string) string {
	parts := strings.Split(key, "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}

var _ controlplane.ArtifactSink = (*s3ArtifactSink)(nil)
