package gatewaycore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var (
	ErrCredentialMissing           = errors.New("gateway credential is required")
	ErrCredentialConflict          = errors.New("multiple gateway credentials are not allowed")
	ErrCredentialTransportRejected = errors.New("credential transport is not allowed for this protocol")
	ErrQueryCredentialRejected     = errors.New("gateway credentials in URL query parameters are not allowed")
	ErrInvalidCanonicalRequest     = errors.New("invalid canonical gateway request")
)

const (
	maxRequestIDBytes   = 128
	maxIdempotencyBytes = 256
	maxStickyKeyBytes   = 256
)

func ExtractCredential(req *http.Request, protocol Protocol) (CredentialEnvelope, error) {
	if req == nil {
		return CredentialEnvelope{}, ErrCredentialMissing
	}
	for _, key := range []string{"api_key", "key", "access_token", "token"} {
		if strings.TrimSpace(req.URL.Query().Get(key)) != "" {
			return CredentialEnvelope{}, ErrQueryCredentialRejected
		}
	}

	candidates := make([]CredentialEnvelope, 0, 3)
	authorization := strings.TrimSpace(req.Header.Get("Authorization"))
	if authorization != "" {
		parts := strings.Fields(authorization)
		if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
			return CredentialEnvelope{}, ErrCredentialTransportRejected
		}
		switch strings.ToLower(parts[0]) {
		case "bearer":
			candidates = append(candidates, CredentialEnvelope{BearerToken: parts[1], Transport: "authorization_bearer"})
		case "aster-context":
			candidates = append(candidates, CredentialEnvelope{SignedContext: parts[1], Transport: "authorization_aster_context"})
		default:
			return CredentialEnvelope{}, ErrCredentialTransportRejected
		}
	}
	if value := strings.TrimSpace(req.Header.Get("X-API-Key")); value != "" {
		if protocol != ProtocolAnthropicMessages {
			return CredentialEnvelope{}, ErrCredentialTransportRejected
		}
		candidates = append(candidates, CredentialEnvelope{BearerToken: value, Transport: "anthropic_x_api_key"})
	}
	if value := strings.TrimSpace(req.Header.Get("X-Goog-API-Key")); value != "" {
		if protocol != ProtocolGeminiGenerate {
			return CredentialEnvelope{}, ErrCredentialTransportRejected
		}
		candidates = append(candidates, CredentialEnvelope{BearerToken: value, Transport: "gemini_x_goog_api_key"})
	}
	if len(candidates) == 0 {
		return CredentialEnvelope{}, ErrCredentialMissing
	}
	if len(candidates) != 1 {
		return CredentialEnvelope{}, ErrCredentialConflict
	}
	return candidates[0], nil
}

func CanonicalizeOpenAIChat(raw []byte, header http.Header) (CanonicalRequest, error) {
	var payload struct {
		Model    string            `json:"model"`
		Messages []json.RawMessage `json:"messages"`
		Stream   bool              `json:"stream"`
		User     string            `json:"user"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return CanonicalRequest{}, ErrInvalidCanonicalRequest
	}
	payload.Model = strings.TrimSpace(payload.Model)
	if payload.Model == "" {
		return CanonicalRequest{}, fmt.Errorf("%w: model is required", ErrInvalidCanonicalRequest)
	}
	requestID := strings.TrimSpace(header.Get("X-Request-ID"))
	if len(requestID) > maxRequestIDBytes {
		return CanonicalRequest{}, fmt.Errorf("%w: request id is too long", ErrInvalidCanonicalRequest)
	}
	if requestID == "" {
		generated, err := randomRequestID()
		if err != nil {
			return CanonicalRequest{}, err
		}
		requestID = generated
	}
	idempotencyKey := strings.TrimSpace(header.Get("Idempotency-Key"))
	if len(idempotencyKey) > maxIdempotencyBytes {
		return CanonicalRequest{}, fmt.Errorf("%w: idempotency key is too long", ErrInvalidCanonicalRequest)
	}
	stickyKey := strings.TrimSpace(header.Get("X-AsterRouter-Sticky-Key"))
	if stickyKey == "" {
		stickyKey = strings.TrimSpace(payload.User)
	}
	if len(stickyKey) > maxStickyKeyBytes {
		stickyKey = stickyKey[:maxStickyKeyBytes]
	}
	fingerprint := sha256.Sum256(raw)
	return CanonicalRequest{
		ID:              "op_" + requestID,
		ClientRequestID: requestID,
		Fingerprint:     hex.EncodeToString(fingerprint[:]),
		Protocol:        ProtocolOpenAIChat,
		Operation:       "chat_completion",
		Modality:        "text",
		Lane:            LaneDirect,
		Model:           payload.Model,
		Stream:          payload.Stream,
		MessageCount:    len(payload.Messages),
		IdempotencyKey:  idempotencyKey,
		StickyKey:       stickyKey,
		Payload:         append(json.RawMessage(nil), raw...),
	}, nil
}

func randomRequestID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate gateway request id: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
