package plugins

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/cryptoutil"
	"golang.org/x/crypto/hkdf"
)

const (
	officialFeedEnvelopePurpose = "official_data_feed"
	officialFeedPackageSchema   = "astercloud.encrypted-feed-package.v1"
	officialFeedKeyWrap         = "X25519-HKDF-SHA256+A256GCM"
	officialFeedCipher          = "AES-256-GCM"
	maxOfficialFeedBytes        = 32 * 1024 * 1024
)

var (
	ErrOfficialFeedInvalid     = errors.New("official data feed is invalid")
	ErrOfficialFeedEntitlement = errors.New("official data feed entitlement is missing")
	ErrOfficialFeedBinding     = errors.New("official data feed is bound to another instance")
	ErrOfficialFeedDecrypt     = errors.New("official data feed decryption failed")
	ErrOfficialFeedReplay      = errors.New("official data feed would roll back a newer snapshot")
	ErrOfficialFeedNotFound    = errors.New("official data feed is not imported")
	ErrOfficialFeedExpired     = errors.New("official data feed is expired")
)

type encryptedFeedPackage struct {
	SchemaVersion     string               `json:"schema_version"`
	ServiceKey        string               `json:"service_key"`
	FeedID            string               `json:"feed_id"`
	FeedVersion       string               `json:"feed_version"`
	DataSchemaVersion string               `json:"data_schema_version"`
	LicenseID         string               `json:"license_id"`
	InstanceID        string               `json:"instance_id"`
	IssuedAt          time.Time            `json:"issued_at"`
	ExpiresAt         time.Time            `json:"expires_at"`
	Payload           encryptedFeedPayload `json:"payload"`
	Revocations       []feedRevocation     `json:"revocations"`
}

type encryptedFeedPayload struct {
	Cipher                string `json:"cipher"`
	KeyWrap               string `json:"key_wrap"`
	EphemeralPublicKey    string `json:"ephemeral_public_key"`
	EncryptedDataKeyNonce string `json:"encrypted_data_key_nonce"`
	EncryptedDataKey      string `json:"encrypted_data_key"`
	Nonce                 string `json:"nonce"`
	Ciphertext            string `json:"ciphertext"`
	SHA256                string `json:"sha256"`
	SizeBytes             int64  `json:"size_bytes"`
}

type feedRevocation struct {
	FeedID    string    `json:"feed_id"`
	Reason    string    `json:"reason"`
	RevokedAt time.Time `json:"revoked_at"`
}

func (s *Service) OfficialFeedClientInfo(ctx context.Context) (OfficialFeedClientInfo, error) {
	license, ok, err := s.repo.LatestLicense(ctx)
	if err != nil {
		return OfficialFeedClientInfo{}, err
	}
	if !ok || !licenseRecordActive(license, s.now().UTC()) {
		return OfficialFeedClientInfo{}, ErrLicenseNotFound
	}
	_, publicKey, err := s.officialFeedKeyPair()
	if err != nil {
		return OfficialFeedClientInfo{}, err
	}
	return OfficialFeedClientInfo{
		InstanceID:          license.InstanceID,
		LicenseID:           license.LicenseID,
		EncryptionAlgorithm: officialFeedKeyWrap,
		EncryptionPublicKey: base64.RawURLEncoding.EncodeToString(publicKey.Bytes()),
	}, nil
}

func (s *Service) ImportOfficialFeed(ctx context.Context, request OfficialFeedImportRequest) (OfficialFeedStatus, error) {
	envelope, err := decodeOfficialFeedEnvelope(request)
	if err != nil {
		return OfficialFeedStatus{}, err
	}
	cfg, err := s.effectiveCatalogConfig(ctx)
	if err != nil {
		return OfficialFeedStatus{}, err
	}
	if cfg.PublicKeyID == "" || cfg.PublicKeyBase64 == "" {
		return OfficialFeedStatus{}, ErrCatalogNotConfigured
	}
	if err := verifySignedEnvelope(envelope, cfg, officialFeedEnvelopePurpose, s.now().UTC()); err != nil {
		return OfficialFeedStatus{}, ErrCatalogSignature
	}
	var feed encryptedFeedPackage
	if err := json.Unmarshal(envelope.Payload, &feed); err != nil {
		return OfficialFeedStatus{}, ErrOfficialFeedInvalid
	}
	if err := validateEncryptedFeedPackage(feed, s.now().UTC()); err != nil {
		return OfficialFeedStatus{}, err
	}
	license, ok, err := s.repo.LatestLicense(ctx)
	if err != nil {
		return OfficialFeedStatus{}, err
	}
	if !ok || !licenseRecordActive(license, s.now().UTC()) {
		return OfficialFeedStatus{}, ErrLicenseNotFound
	}
	if feed.LicenseID != "" && feed.LicenseID != license.LicenseID {
		return OfficialFeedStatus{}, ErrOfficialFeedEntitlement
	}
	if feed.InstanceID != license.InstanceID {
		return OfficialFeedStatus{}, ErrOfficialFeedBinding
	}
	if !licenseAllowsResource(license, "data_feed", feed.ServiceKey, s.now().UTC()) {
		return OfficialFeedStatus{}, ErrOfficialFeedEntitlement
	}
	latest, exists, err := s.repo.LatestOfficialFeed(ctx, feed.ServiceKey)
	if err != nil {
		return OfficialFeedStatus{}, err
	}
	if exists && latest.FeedID != feed.FeedID && feed.IssuedAt.Before(latest.IssuedAt) {
		return OfficialFeedStatus{}, ErrOfficialFeedReplay
	}
	plaintext, err := s.decryptOfficialFeed(feed)
	if err != nil {
		return OfficialFeedStatus{}, err
	}
	payloadCiphertext, err := encryptSecret(s.secretKey, string(plaintext))
	if err != nil {
		return OfficialFeedStatus{}, err
	}
	rawEnvelope, err := json.Marshal(envelope)
	if err != nil {
		return OfficialFeedStatus{}, err
	}
	now := s.now().UTC()
	record := officialFeedRecord{
		ServiceKey:        feed.ServiceKey,
		FeedID:            feed.FeedID,
		FeedVersion:       feed.FeedVersion,
		DataSchemaVersion: feed.DataSchemaVersion,
		Status:            "active",
		SignatureVerified: true,
		PayloadSHA256:     normalizeSHA256(feed.Payload.SHA256),
		SizeBytes:         int64(len(plaintext)),
		PayloadCiphertext: payloadCiphertext,
		EnvelopeJSON:      string(rawEnvelope),
		IssuedAt:          feed.IssuedAt,
		ExpiresAt:         feed.ExpiresAt,
		ImportedAt:        now,
		UpdatedAt:         now,
	}
	if err := s.repo.SaveOfficialFeed(ctx, record); err != nil {
		return OfficialFeedStatus{}, err
	}
	if err := s.applyOfficialFeedRevocations(ctx, feed.ServiceKey, feed.Revocations, now); err != nil {
		return OfficialFeedStatus{}, err
	}
	return officialFeedStatusFromRecord(record, now), nil
}

func (s *Service) OfficialFeedStatuses(ctx context.Context, serviceKey string) ([]OfficialFeedStatus, error) {
	records, err := s.repo.ListOfficialFeeds(ctx, strings.TrimSpace(serviceKey))
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	out := make([]OfficialFeedStatus, 0, len(records))
	for _, record := range records {
		out = append(out, officialFeedStatusFromRecord(record, now))
	}
	return out, nil
}

func (s *Service) OfficialFeedPayload(ctx context.Context, serviceKey string) (json.RawMessage, error) {
	serviceKey = strings.TrimSpace(serviceKey)
	record, ok, err := s.latestUsableOfficialFeed(ctx, serviceKey)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrOfficialFeedNotFound
	}
	license, ok, err := s.repo.LatestLicense(ctx)
	if err != nil {
		return nil, err
	}
	if !ok || !licenseRecordActive(license, s.now().UTC()) {
		return nil, ErrLicenseNotFound
	}
	if !licenseAllowsResource(license, "data_feed", serviceKey, s.now().UTC()) {
		return nil, ErrOfficialFeedEntitlement
	}
	var envelope catalogEnvelope
	if err := json.Unmarshal([]byte(record.EnvelopeJSON), &envelope); err != nil {
		return nil, ErrOfficialFeedInvalid
	}
	var feed encryptedFeedPackage
	if err := json.Unmarshal(envelope.Payload, &feed); err != nil {
		return nil, ErrOfficialFeedInvalid
	}
	if feed.InstanceID != license.InstanceID || feed.LicenseID != "" && feed.LicenseID != license.LicenseID {
		return nil, ErrOfficialFeedBinding
	}
	plaintext, err := decryptSecret(s.secretKey, record.PayloadCiphertext)
	if err != nil {
		return nil, err
	}
	if !json.Valid([]byte(plaintext)) {
		return nil, ErrOfficialFeedInvalid
	}
	return json.RawMessage(plaintext), nil
}

func (s *Service) latestUsableOfficialFeed(ctx context.Context, serviceKey string) (officialFeedRecord, bool, error) {
	records, err := s.repo.ListOfficialFeeds(ctx, serviceKey)
	if err != nil {
		return officialFeedRecord{}, false, err
	}
	now := s.now().UTC()
	for _, record := range records {
		if record.Status == "active" && record.ExpiresAt.After(now) {
			return record, true, nil
		}
	}
	if len(records) > 0 {
		return officialFeedRecord{}, false, ErrOfficialFeedExpired
	}
	return officialFeedRecord{}, false, nil
}

func (s *Service) applyOfficialFeedRevocations(ctx context.Context, serviceKey string, revocations []feedRevocation, now time.Time) error {
	for _, revocation := range revocations {
		feedID := strings.TrimSpace(revocation.FeedID)
		if feedID == "" || revocation.RevokedAt.After(now.Add(5*time.Minute)) {
			return ErrOfficialFeedInvalid
		}
		if err := s.repo.UpdateOfficialFeedStatus(ctx, serviceKey, feedID, "revoked", now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) officialFeedKeyPair() (*ecdh.PrivateKey, *ecdh.PublicKey, error) {
	seed, err := cryptoutil.DeriveKey(s.secretKey, "asterrouter:official-feed:x25519:v2")
	if err != nil {
		return nil, nil, err
	}
	privateKey, err := ecdh.X25519().NewPrivateKey(seed)
	if err != nil {
		return nil, nil, err
	}
	return privateKey, privateKey.PublicKey(), nil
}

func (s *Service) decryptOfficialFeed(feed encryptedFeedPackage) ([]byte, error) {
	privateKey, _, err := s.officialFeedKeyPair()
	if err != nil {
		return nil, err
	}
	ephemeralRaw, err := decodeBase64Value(feed.Payload.EphemeralPublicKey)
	if err != nil {
		return nil, ErrOfficialFeedDecrypt
	}
	ephemeralKey, err := ecdh.X25519().NewPublicKey(ephemeralRaw)
	if err != nil {
		return nil, ErrOfficialFeedDecrypt
	}
	sharedSecret, err := privateKey.ECDH(ephemeralKey)
	if err != nil {
		return nil, ErrOfficialFeedDecrypt
	}
	keyEncryptionKey := make([]byte, 32)
	kdf := hkdf.New(sha256.New, sharedSecret, []byte(feed.FeedID), []byte("astercloud:official-data-feed:key-wrap:v1"))
	if _, err := io.ReadFull(kdf, keyEncryptionKey); err != nil {
		return nil, ErrOfficialFeedDecrypt
	}
	wrappedKey, err := decodeBase64Value(feed.Payload.EncryptedDataKey)
	if err != nil {
		return nil, ErrOfficialFeedDecrypt
	}
	keyNonce, err := decodeBase64Value(feed.Payload.EncryptedDataKeyNonce)
	if err != nil {
		return nil, ErrOfficialFeedDecrypt
	}
	dataKey, err := openAESGCM(keyEncryptionKey, keyNonce, wrappedKey, []byte(feed.FeedID+"|"+feed.ServiceKey))
	if err != nil || len(dataKey) != 32 {
		return nil, ErrOfficialFeedDecrypt
	}
	ciphertext, err := decodeBase64Value(feed.Payload.Ciphertext)
	if err != nil || int64(len(ciphertext)) > maxOfficialFeedBytes {
		return nil, ErrOfficialFeedDecrypt
	}
	nonce, err := decodeBase64Value(feed.Payload.Nonce)
	if err != nil {
		return nil, ErrOfficialFeedDecrypt
	}
	plaintext, err := openAESGCM(dataKey, nonce, ciphertext, []byte(feed.ServiceKey+"|"+feed.FeedID+"|"+feed.DataSchemaVersion))
	if err != nil || int64(len(plaintext)) > maxOfficialFeedBytes || !json.Valid(plaintext) {
		return nil, ErrOfficialFeedDecrypt
	}
	sum := sha256.Sum256(plaintext)
	if hex.EncodeToString(sum[:]) != normalizeSHA256(feed.Payload.SHA256) || int64(len(plaintext)) != feed.Payload.SizeBytes {
		return nil, ErrOfficialFeedInvalid
	}
	return plaintext, nil
}

func validateEncryptedFeedPackage(feed encryptedFeedPackage, now time.Time) error {
	if feed.SchemaVersion != officialFeedPackageSchema || strings.TrimSpace(feed.ServiceKey) == "" || strings.TrimSpace(feed.FeedID) == "" || strings.TrimSpace(feed.FeedVersion) == "" || strings.TrimSpace(feed.DataSchemaVersion) == "" {
		return ErrOfficialFeedInvalid
	}
	if feed.Payload.Cipher != officialFeedCipher || feed.Payload.KeyWrap != officialFeedKeyWrap || feed.Payload.SizeBytes <= 0 || !feed.ExpiresAt.After(now) || feed.IssuedAt.After(now.Add(5*time.Minute)) {
		return ErrOfficialFeedInvalid
	}
	if err := validateFeedRevocations(feed.Revocations, feed.FeedID, now); err != nil {
		return err
	}
	return nil
}

func validateFeedRevocations(revocations []feedRevocation, currentFeedID string, now time.Time) error {
	for _, revocation := range revocations {
		if strings.TrimSpace(revocation.FeedID) == "" ||
			strings.TrimSpace(revocation.FeedID) == strings.TrimSpace(currentFeedID) ||
			revocation.RevokedAt.IsZero() ||
			revocation.RevokedAt.After(now.Add(5*time.Minute)) {
			return ErrOfficialFeedInvalid
		}
	}
	return nil
}

func decodeOfficialFeedEnvelope(request OfficialFeedImportRequest) (catalogEnvelope, error) {
	if len(request.Envelope) > 0 && strings.TrimSpace(string(request.Envelope)) != "" && string(request.Envelope) != "null" {
		return decodeLicenseEnvelopeBytes(request.Envelope)
	}
	if len(request.FileJSON) > 0 && strings.TrimSpace(string(request.FileJSON)) != "" && string(request.FileJSON) != "null" {
		return decodeLicenseEnvelopeBytes(request.FileJSON)
	}
	return catalogEnvelope{}, ErrOfficialFeedInvalid
}

func officialFeedStatusFromRecord(record officialFeedRecord, now time.Time) OfficialFeedStatus {
	status := record.Status
	if !record.ExpiresAt.After(now) && status == "active" {
		status = "expired"
	}
	return OfficialFeedStatus{
		ServiceKey:        record.ServiceKey,
		FeedID:            record.FeedID,
		FeedVersion:       record.FeedVersion,
		DataSchemaVersion: record.DataSchemaVersion,
		Status:            status,
		SignatureVerified: record.SignatureVerified,
		PayloadSHA256:     record.PayloadSHA256,
		SizeBytes:         record.SizeBytes,
		IssuedAt:          record.IssuedAt,
		ExpiresAt:         record.ExpiresAt,
		ImportedAt:        record.ImportedAt,
	}
}

func openAESGCM(key []byte, nonce []byte, ciphertext []byte, additionalData []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, ErrOfficialFeedDecrypt
	}
	return gcm.Open(nil, nonce, ciphertext, additionalData)
}

func decodeBase64Value(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		decoded, err := encoding.DecodeString(value)
		if err == nil {
			return decoded, nil
		}
	}
	return nil, ErrOfficialFeedInvalid
}

func normalizeSHA256(value string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), "sha256:"))
}
