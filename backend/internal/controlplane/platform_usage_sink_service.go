package controlplane

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	platformUsageSinkDefaultMaxAttempts = 10
	platformUsageSinkMaximumAttempts    = 100
	platformUsageDeliveryLease          = 30 * time.Second
)

var ErrPlatformUsageDeliveryNotFound = errors.New("platform usage delivery event not found")

type platformUsageEventPayload struct {
	EventID            string          `json:"event_id"`
	IntegrationID      string          `json:"integration_id"`
	TenantID           string          `json:"tenant_id"`
	ExternalSubjectRef string          `json:"external_subject_ref"`
	UsageRecordID      string          `json:"usage_record_id"`
	Model              string          `json:"model"`
	InputTokens        int             `json:"input_tokens"`
	OutputTokens       int             `json:"output_tokens"`
	UsageDimensions    UsageDimensions `json:"usage_dimensions"`
	UsageCostMicros    *int64          `json:"usage_cost_micros,omitempty"`
	UsageCostCurrency  string          `json:"usage_cost_currency"`
	PricingStatus      string          `json:"pricing_status"`
	Status             string          `json:"status"`
	OccurredAt         time.Time       `json:"occurred_at"`
}

func (s *Service) ListPlatformUsageSinks(ctx context.Context) ([]PlatformUsageSink, error) {
	sinks, err := s.repo.ListPlatformUsageSinks(ctx)
	if err != nil {
		return nil, err
	}
	for index := range sinks {
		sinks[index] = platformUsageSinkPublic(sinks[index])
	}
	return sinks, nil
}

func (s *Service) CreatePlatformUsageSink(ctx context.Context, actor string, req PlatformUsageSinkRequest) (PlatformUsageSinkCreateResponse, error) {
	sink, secret, integration, err := s.platformUsageSinkFromRequest(ctx, req, nil, true)
	if err != nil {
		return PlatformUsageSinkCreateResponse{}, err
	}
	sink.ID = "pus_" + randomID(10)
	sink.CreatedAt = s.nowUTC()
	sink.UpdatedAt = sink.CreatedAt
	if err := s.requirePlatformUsageSinkUnique(ctx, sink, ""); err != nil {
		return PlatformUsageSinkCreateResponse{}, err
	}
	if err := s.repo.SavePlatformUsageSink(ctx, sink); err != nil {
		return PlatformUsageSinkCreateResponse{}, err
	}
	identity, err := s.platformCredentialIdentity(ctx, sink.TenantID, integration.GatewayPrincipalID)
	if err != nil {
		return PlatformUsageSinkCreateResponse{}, err
	}
	if err := s.auditPlatform(ctx, actor, "create", "platform_usage_sink", sink.ID, "Created platform usage sink "+sink.Name, &identity.tenant, &identity.principal); err != nil {
		return PlatformUsageSinkCreateResponse{}, err
	}
	return PlatformUsageSinkCreateResponse{Record: platformUsageSinkPublic(sink), SigningSecret: secret}, nil
}

func (s *Service) UpdatePlatformUsageSink(ctx context.Context, actor, id string, req PlatformUsageSinkRequest) (PlatformUsageSink, error) {
	existing, err := s.platformUsageSinkByID(ctx, id)
	if err != nil {
		return PlatformUsageSink{}, err
	}
	if strings.TrimSpace(req.TenantID) == "" {
		req.TenantID = existing.TenantID
	}
	if strings.TrimSpace(req.ExternalAuthIntegrationID) == "" {
		req.ExternalAuthIntegrationID = existing.ExternalAuthIntegrationID
	}
	if req.TenantID != existing.TenantID || req.ExternalAuthIntegrationID != existing.ExternalAuthIntegrationID {
		return PlatformUsageSink{}, errors.New("platform usage sink tenant_id and external_auth_integration_id are immutable")
	}
	if strings.TrimSpace(req.EndpointURL) != "" || strings.TrimSpace(req.SigningSecret) != "" {
		return PlatformUsageSink{}, errors.New("rotate the platform usage sink endpoint and signing secret through its dedicated endpoint")
	}
	sink, _, integration, err := s.platformUsageSinkFromRequest(ctx, req, &existing, false)
	if err != nil {
		return PlatformUsageSink{}, err
	}
	sink.ID = existing.ID
	sink.CreatedAt = existing.CreatedAt
	sink.UpdatedAt = s.nowUTC()
	if err := s.requirePlatformUsageSinkUnique(ctx, sink, existing.ID); err != nil {
		return PlatformUsageSink{}, err
	}
	if err := s.repo.SavePlatformUsageSink(ctx, sink); err != nil {
		return PlatformUsageSink{}, err
	}
	identity, err := s.platformCredentialIdentity(ctx, sink.TenantID, integration.GatewayPrincipalID)
	if err != nil {
		return PlatformUsageSink{}, err
	}
	if err := s.auditPlatform(ctx, actor, "update", "platform_usage_sink", sink.ID, "Updated platform usage sink "+sink.Name, &identity.tenant, &identity.principal); err != nil {
		return PlatformUsageSink{}, err
	}
	return platformUsageSinkPublic(sink), nil
}

func (s *Service) RotatePlatformUsageSinkEndpoint(ctx context.Context, actor, id, endpointURL, signingSecret string) (PlatformUsageSinkCreateResponse, error) {
	sink, err := s.platformUsageSinkByID(ctx, id)
	if err != nil {
		return PlatformUsageSinkCreateResponse{}, err
	}
	endpointURL = strings.TrimSpace(endpointURL)
	if err := validatePlatformUsageSinkEndpoint(endpointURL); err != nil {
		return PlatformUsageSinkCreateResponse{}, err
	}
	if strings.TrimSpace(signingSecret) == "" {
		signingSecret = "aus_" + randomToken(32)
	}
	if len(signingSecret) < 24 || len(signingSecret) > 4096 {
		return PlatformUsageSinkCreateResponse{}, errors.New("platform usage sink signing secret must contain 24 to 4096 characters")
	}
	endpointCiphertext, err := encryptSecret(s.secretKey, endpointURL)
	if err != nil {
		return PlatformUsageSinkCreateResponse{}, err
	}
	secretCiphertext, err := encryptSecret(s.secretKey, signingSecret)
	if err != nil {
		return PlatformUsageSinkCreateResponse{}, err
	}
	sink.EndpointURLCiphertext = endpointCiphertext
	sink.EndpointURLHint = maskSecret(endpointURL)
	sink.SigningSecretCiphertext = secretCiphertext
	sink.SigningSecretHint = maskSecret(signingSecret)
	sink.UpdatedAt = s.nowUTC()
	if err := s.repo.SavePlatformUsageSink(ctx, sink); err != nil {
		return PlatformUsageSinkCreateResponse{}, err
	}
	integration, err := s.externalAuthIntegrationByID(ctx, sink.ExternalAuthIntegrationID)
	if err != nil {
		return PlatformUsageSinkCreateResponse{}, err
	}
	identity, err := s.platformCredentialIdentity(ctx, sink.TenantID, integration.GatewayPrincipalID)
	if err != nil {
		return PlatformUsageSinkCreateResponse{}, err
	}
	if err := s.auditPlatform(ctx, actor, "rotate_endpoint", "platform_usage_sink", sink.ID, "Rotated platform usage sink endpoint and signing secret", &identity.tenant, &identity.principal); err != nil {
		return PlatformUsageSinkCreateResponse{}, err
	}
	return PlatformUsageSinkCreateResponse{Record: platformUsageSinkPublic(sink), SigningSecret: signingSecret}, nil
}

func (s *Service) ListPlatformUsageDeliveryEvents(ctx context.Context, query PlatformUsageDeliveryQuery) ([]PlatformUsageDeliveryEvent, error) {
	return s.repo.QueryPlatformUsageDeliveryEvents(ctx, query)
}

func (s *Service) RequeuePlatformUsageDeliveryEvent(ctx context.Context, actor, sinkID, id string) error {
	events, err := s.repo.QueryPlatformUsageDeliveryEvents(ctx, PlatformUsageDeliveryQuery{SinkID: strings.TrimSpace(sinkID), DeliveryID: strings.TrimSpace(id), Limit: 1})
	if err != nil {
		return err
	}
	var event PlatformUsageDeliveryEvent
	found := false
	for _, item := range events {
		if item.ID == id {
			event = item
			found = true
			break
		}
	}
	if !found || event.SinkID != strings.TrimSpace(sinkID) {
		return ErrPlatformUsageDeliveryNotFound
	}
	if err := s.repo.RequeuePlatformUsageDeliveryEvent(ctx, id, s.nowUTC()); err != nil {
		return err
	}
	sink, err := s.platformUsageSinkByID(ctx, event.SinkID)
	if err != nil {
		return err
	}
	integration, err := s.externalAuthIntegrationByID(ctx, sink.ExternalAuthIntegrationID)
	if err != nil {
		return err
	}
	identity, err := s.platformCredentialIdentity(ctx, sink.TenantID, integration.GatewayPrincipalID)
	if err != nil {
		return err
	}
	return s.auditPlatform(ctx, actor, "requeue", "platform_usage_delivery", event.ID, "Requeued platform usage delivery event", &identity.tenant, &identity.principal)
}

func (s *Service) platformUsageSinkFromRequest(ctx context.Context, req PlatformUsageSinkRequest, existing *PlatformUsageSink, create bool) (PlatformUsageSink, string, ExternalAuthIntegration, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 120 {
		return PlatformUsageSink{}, "", ExternalAuthIntegration{}, errors.New("platform usage sink name must contain 1 to 120 characters")
	}
	integration, err := s.externalAuthIntegrationByID(ctx, req.ExternalAuthIntegrationID)
	if err != nil || integration.TenantID != req.TenantID {
		return PlatformUsageSink{}, "", ExternalAuthIntegration{}, errors.New("platform usage sink integration must belong to the selected platform tenant")
	}
	maxAttempts := req.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = platformUsageSinkDefaultMaxAttempts
	}
	if maxAttempts < 1 || maxAttempts > platformUsageSinkMaximumAttempts {
		return PlatformUsageSink{}, "", ExternalAuthIntegration{}, fmt.Errorf("platform usage sink max_attempts must be between 1 and %d", platformUsageSinkMaximumAttempts)
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = PlatformUsageSinkStatusActive
	}
	if !oneOf(status, PlatformUsageSinkStatusActive, PlatformUsageSinkStatusDisabled) {
		return PlatformUsageSink{}, "", ExternalAuthIntegration{}, errors.New("platform usage sink status must be active or disabled")
	}
	sink := PlatformUsageSink{TenantID: req.TenantID, ExternalAuthIntegrationID: integration.ID, Name: name, Status: status, MaxAttempts: maxAttempts}
	secret := ""
	if existing != nil {
		sink.EndpointURLCiphertext = existing.EndpointURLCiphertext
		sink.EndpointURLHint = existing.EndpointURLHint
		sink.SigningSecretCiphertext = existing.SigningSecretCiphertext
		sink.SigningSecretHint = existing.SigningSecretHint
	} else if create {
		endpointURL := strings.TrimSpace(req.EndpointURL)
		if err := validatePlatformUsageSinkEndpoint(endpointURL); err != nil {
			return PlatformUsageSink{}, "", ExternalAuthIntegration{}, err
		}
		secret = strings.TrimSpace(req.SigningSecret)
		if secret == "" {
			secret = "aus_" + randomToken(32)
		}
		if len(secret) < 24 || len(secret) > 4096 {
			return PlatformUsageSink{}, "", ExternalAuthIntegration{}, errors.New("platform usage sink signing secret must contain 24 to 4096 characters")
		}
		endpointCiphertext, encryptErr := encryptSecret(s.secretKey, endpointURL)
		if encryptErr != nil {
			return PlatformUsageSink{}, "", ExternalAuthIntegration{}, encryptErr
		}
		secretCiphertext, encryptErr := encryptSecret(s.secretKey, secret)
		if encryptErr != nil {
			return PlatformUsageSink{}, "", ExternalAuthIntegration{}, encryptErr
		}
		sink.EndpointURLCiphertext = endpointCiphertext
		sink.EndpointURLHint = maskSecret(endpointURL)
		sink.SigningSecretCiphertext = secretCiphertext
		sink.SigningSecretHint = maskSecret(secret)
	}
	if sink.Status == PlatformUsageSinkStatusActive && (sink.EndpointURLCiphertext == "" || sink.SigningSecretCiphertext == "") {
		return PlatformUsageSink{}, "", ExternalAuthIntegration{}, errors.New("active platform usage sink requires an endpoint and signing secret")
	}
	return sink, secret, integration, nil
}

func validatePlatformUsageSinkEndpoint(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("platform usage sink endpoint_url must be an absolute https URL without credentials or fragment")
	}
	if len([]rune(value)) > 2048 {
		return errors.New("platform usage sink endpoint_url must be at most 2048 characters")
	}
	return nil
}

func (s *Service) platformUsageSinkByID(ctx context.Context, id string) (PlatformUsageSink, error) {
	sinks, err := s.repo.ListPlatformUsageSinks(ctx)
	if err != nil {
		return PlatformUsageSink{}, err
	}
	for _, sink := range sinks {
		if sink.ID == strings.TrimSpace(id) {
			return sink, nil
		}
	}
	return PlatformUsageSink{}, errors.New("platform usage sink not found")
}

func (s *Service) requirePlatformUsageSinkUnique(ctx context.Context, sink PlatformUsageSink, exceptID string) error {
	sinks, err := s.repo.ListPlatformUsageSinks(ctx)
	if err != nil {
		return err
	}
	for _, item := range sinks {
		if item.ID != exceptID && item.ExternalAuthIntegrationID == sink.ExternalAuthIntegrationID && strings.EqualFold(item.Name, sink.Name) {
			return errors.New("platform usage sink name already exists for external auth integration")
		}
	}
	return nil
}

func platformUsageSinkPublic(sink PlatformUsageSink) PlatformUsageSink {
	sink.EndpointURLCiphertext = ""
	sink.SigningSecretCiphertext = ""
	return sink
}

func (s *Service) platformUsageDeliveryEventsForRecord(ctx context.Context, record UsageRecord) ([]PlatformUsageDeliveryEvent, error) {
	if record.ProfileScope != ProfileScopePlatform || record.ExternalAuthIntegrationID == "" || record.ExternalSubjectReference == "" {
		return nil, nil
	}
	sinks, err := s.repo.ListPlatformUsageSinks(ctx)
	if err != nil {
		return nil, err
	}
	events := []PlatformUsageDeliveryEvent{}
	for _, sink := range sinks {
		if sink.Status != PlatformUsageSinkStatusActive || sink.ExternalAuthIntegrationID != record.ExternalAuthIntegrationID || sink.TenantID != record.PlatformTenantID {
			continue
		}
		eventID := "usage_evt_" + hashAPIKey(sink.ID + "\x00" + record.ID)[:24]
		payload, err := json.Marshal(platformUsageEventPayload{EventID: eventID, IntegrationID: record.ExternalAuthIntegrationID, TenantID: record.PlatformTenantID, ExternalSubjectRef: record.ExternalSubjectReference, UsageRecordID: record.ID, Model: record.Model, InputTokens: record.InputTokens, OutputTokens: record.OutputTokens, UsageDimensions: record.UsageDimensions, UsageCostMicros: record.UsageCostMicros, UsageCostCurrency: record.UsageCostCurrency, PricingStatus: record.PricingStatus, Status: record.Status, OccurredAt: record.CreatedAt})
		if err != nil {
			return nil, err
		}
		now := s.nowUTC()
		events = append(events, PlatformUsageDeliveryEvent{ID: "pud_" + randomID(12), SinkID: sink.ID, UsageRecordID: record.ID, EventID: eventID, PayloadJSON: string(payload), Status: PlatformUsageDeliveryStatusPending, MaxAttempts: sink.MaxAttempts, NextAttemptAt: now, TargetHint: sink.EndpointURLHint, CreatedAt: now, UpdatedAt: now})
	}
	return events, nil
}

func (s *Service) DeliverDuePlatformUsage(ctx context.Context, limit int) error {
	now := s.nowUTC()
	leaseToken := "lease_" + randomToken(16)
	events, err := s.repo.ClaimDuePlatformUsageDeliveryEvents(ctx, now, now.Add(platformUsageDeliveryLease), leaseToken, limit)
	if err != nil {
		return err
	}
	var firstErr error
	for _, event := range events {
		sink, sinkErr := s.platformUsageSinkByID(ctx, event.SinkID)
		if sinkErr != nil {
			if err := s.reschedulePlatformUsageDelivery(ctx, event, leaseToken, 0, sinkErr.Error()); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		endpoint, endpointErr := decryptSecret(s.secretKey, sink.EndpointURLCiphertext)
		secret, secretErr := decryptSecret(s.secretKey, sink.SigningSecretCiphertext)
		if endpointErr != nil || secretErr != nil {
			if err := s.reschedulePlatformUsageDelivery(ctx, event, leaseToken, 0, "usage sink secret is unavailable"); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		httpStatus, deliverErr := s.deliverPlatformUsageEvent(ctx, endpoint, secret, event)
		if deliverErr == nil {
			if err := s.repo.CompletePlatformUsageDeliveryEvent(ctx, event.ID, leaseToken, s.nowUTC(), httpStatus); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := s.reschedulePlatformUsageDelivery(ctx, event, leaseToken, httpStatus, deliverErr.Error()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Service) deliverPlatformUsageEvent(ctx context.Context, endpoint, secret string, event PlatformUsageDeliveryEvent) (int, error) {
	timestamp := s.nowUTC().Format(time.RFC3339)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "." + event.PayloadJSON))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(event.PayloadJSON))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AsterRouter-UsageSink/0.1")
	req.Header.Set("X-Aster-Event-ID", event.EventID)
	req.Header.Set("X-Aster-Timestamp", timestamp)
	req.Header.Set("X-Aster-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	client := s.platformUsageHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("platform usage sink redirects are not allowed")
		}}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp.StatusCode, fmt.Errorf("usage sink delivery failed with HTTP %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

func (s *Service) reschedulePlatformUsageDelivery(ctx context.Context, event PlatformUsageDeliveryEvent, leaseToken string, httpStatus int, message string) error {
	now := s.nowUTC()
	deadLetter := event.AttemptCount >= event.MaxAttempts
	next := now.Add(platformUsageDeliveryRetryDelay(event.AttemptCount))
	if deadLetter {
		next = now
	}
	return s.repo.ReschedulePlatformUsageDeliveryEvent(ctx, event.ID, leaseToken, next, httpStatus, message, deadLetter, now)
}

func platformUsageDeliveryRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second * time.Duration(1<<(attempt-1))
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}
