package controlplane

import (
	"errors"
	"net/netip"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

const gatewayDefaultTenantID = "workspace"

var errInvalidAPIKeyPolicy = errors.New("invalid api key policy")

var (
	ErrAPIKeyAlreadyRotated        = errors.New("api key has already been rotated")
	ErrAPIKeyChangedDuringRotation = errors.New("api key changed while rotation was in progress")
)

type apiKeyPolicyFields struct {
	scopes                   []string
	allowedModalities        []string
	allowedOperations        []string
	qpsLimit                 int
	rpmLimit                 int
	tpmLimit                 int
	concurrencyLimit         int
	monthlyTokenLimit        int
	monthlyBudgetMicros      int64
	monthlyImageLimit        int
	monthlyVideoSecondsLimit int
	monthlyAudioSecondsLimit int
	allowedCIDRs             []string
	lanePolicy               string
	artifactPolicy           string
	artifactSinkID           string
}

func apiKeyPolicyFromCreateRequest(req APIKeyCreateRequest) apiKeyPolicyFields {
	return apiKeyPolicyFields{
		scopes: req.Scopes, allowedModalities: req.AllowedModalities, allowedOperations: req.AllowedOperations,
		qpsLimit: req.QPSLimit, rpmLimit: req.RPMLimit, tpmLimit: req.TPMLimit, concurrencyLimit: req.ConcurrencyLimit,
		monthlyTokenLimit: req.MonthlyTokenLimit, monthlyBudgetMicros: req.MonthlyBudgetMicros,
		monthlyImageLimit: req.MonthlyImageLimit, monthlyVideoSecondsLimit: req.MonthlyVideoSecondsLimit,
		monthlyAudioSecondsLimit: req.MonthlyAudioSecondsLimit, allowedCIDRs: req.AllowedCIDRs,
		lanePolicy: req.LanePolicy, artifactPolicy: req.ArtifactPolicy, artifactSinkID: req.ArtifactSinkID,
	}
}

func apiKeyPolicyFromUpdateRequest(req APIKeyUpdateRequest) apiKeyPolicyFields {
	return apiKeyPolicyFields{
		scopes: req.Scopes, allowedModalities: req.AllowedModalities, allowedOperations: req.AllowedOperations,
		qpsLimit: req.QPSLimit, rpmLimit: req.RPMLimit, tpmLimit: req.TPMLimit, concurrencyLimit: req.ConcurrencyLimit,
		monthlyTokenLimit: req.MonthlyTokenLimit, monthlyBudgetMicros: req.MonthlyBudgetMicros,
		monthlyImageLimit: req.MonthlyImageLimit, monthlyVideoSecondsLimit: req.MonthlyVideoSecondsLimit,
		monthlyAudioSecondsLimit: req.MonthlyAudioSecondsLimit, allowedCIDRs: req.AllowedCIDRs,
		lanePolicy: req.LanePolicy, artifactPolicy: req.ArtifactPolicy, artifactSinkID: req.ArtifactSinkID,
	}
}

func apiKeyPolicyForUpdate(record APIKeyRecord, req APIKeyUpdateRequest) apiKeyPolicyFields {
	requested := apiKeyPolicyFromUpdateRequest(req)
	if req.Scopes != nil || req.AllowedModalities != nil || req.AllowedOperations != nil || req.AllowedCIDRs != nil ||
		req.RPMLimit != 0 || req.TPMLimit != 0 || req.ConcurrencyLimit != 0 || req.MonthlyBudgetMicros != 0 ||
		req.MonthlyImageLimit != 0 || req.MonthlyVideoSecondsLimit != 0 || req.MonthlyAudioSecondsLimit != 0 ||
		strings.TrimSpace(req.LanePolicy) != "" || strings.TrimSpace(req.ArtifactPolicy) != "" || strings.TrimSpace(req.ArtifactSinkID) != "" {
		return requested
	}
	requested = apiKeyPolicyFromRecord(record)
	requested.qpsLimit = req.QPSLimit
	requested.monthlyTokenLimit = req.MonthlyTokenLimit
	return requested
}

func normalizeAPIKeyPolicy(fields apiKeyPolicyFields) (apiKeyPolicyFields, error) {
	limits := []int{
		fields.qpsLimit, fields.rpmLimit, fields.tpmLimit, fields.concurrencyLimit,
		fields.monthlyTokenLimit, fields.monthlyImageLimit,
		fields.monthlyVideoSecondsLimit, fields.monthlyAudioSecondsLimit,
	}
	for _, limit := range limits {
		if limit < 0 {
			return apiKeyPolicyFields{}, errors.New("api key limits must be greater than or equal to 0")
		}
	}
	if fields.monthlyBudgetMicros < 0 {
		return apiKeyPolicyFields{}, errors.New("api key limits must be greater than or equal to 0")
	}

	var err error
	fields.scopes, err = normalizeGatewayPolicyTokens(fields.scopes, []string{GatewayScopeInvoke, GatewayScopeModelsRead})
	if err != nil {
		return apiKeyPolicyFields{}, err
	}
	fields.allowedModalities, err = normalizeGatewayPolicyTokens(fields.allowedModalities, []string{GatewayModalityMetadata, GatewayModalityText})
	if err != nil {
		return apiKeyPolicyFields{}, err
	}
	fields.allowedOperations, err = normalizeGatewayPolicyTokens(fields.allowedOperations, []string{GatewayOperationListModels, GatewayOperationChatCompletion})
	if err != nil {
		return apiKeyPolicyFields{}, err
	}
	fields.allowedCIDRs, err = normalizeAllowedCIDRs(fields.allowedCIDRs)
	if err != nil {
		return apiKeyPolicyFields{}, err
	}
	fields.lanePolicy = strings.TrimSpace(fields.lanePolicy)
	if fields.lanePolicy == "" {
		fields.lanePolicy = GatewayLanePolicyDirectOnly
	}
	if !oneOf(fields.lanePolicy, GatewayLanePolicyDirectOnly, GatewayLanePolicyDurableOnly, GatewayLanePolicyDirectAndDurable) {
		return apiKeyPolicyFields{}, errors.New("lane_policy must be direct_only, durable_only, or direct_and_durable")
	}
	fields.artifactPolicy = strings.TrimSpace(fields.artifactPolicy)
	if fields.artifactPolicy == "" {
		fields.artifactPolicy = GatewayArtifactPolicyProxyOnly
	}
	if !oneOf(fields.artifactPolicy, GatewayArtifactPolicyProxyOnly, GatewayArtifactPolicyTemporary, GatewayArtifactPolicyManaged, GatewayArtifactPolicyCustomerSink, GatewayArtifactPolicyMetadataOnly) {
		return apiKeyPolicyFields{}, errors.New("artifact_policy is not supported")
	}
	requestedSinkID := strings.TrimSpace(fields.artifactSinkID)
	fields.artifactSinkID = artifactSinkSnapshot(fields.artifactPolicy, requestedSinkID)
	if fields.artifactPolicy != GatewayArtifactPolicyCustomerSink && requestedSinkID != "" {
		return apiKeyPolicyFields{}, errors.New("artifact_sink_id is only supported with customer_sink policy")
	}
	if !validArtifactSinkBinding(fields.artifactPolicy, fields.artifactSinkID) {
		return apiKeyPolicyFields{}, errors.New("artifact_sink_id is required and must be valid for customer_sink policy")
	}
	return fields, nil
}

func applyAPIKeyPolicy(record *APIKeyRecord, fields apiKeyPolicyFields) {
	record.Scopes = fields.scopes
	record.AllowedModalities = fields.allowedModalities
	record.AllowedOperations = fields.allowedOperations
	record.QPSLimit = fields.qpsLimit
	record.RPMLimit = fields.rpmLimit
	record.TPMLimit = fields.tpmLimit
	record.ConcurrencyLimit = fields.concurrencyLimit
	record.MonthlyTokenLimit = fields.monthlyTokenLimit
	record.MonthlyBudgetMicros = fields.monthlyBudgetMicros
	record.MonthlyImageLimit = fields.monthlyImageLimit
	record.MonthlyVideoSecondsLimit = fields.monthlyVideoSecondsLimit
	record.MonthlyAudioSecondsLimit = fields.monthlyAudioSecondsLimit
	record.AllowedCIDRs = fields.allowedCIDRs
	record.LanePolicy = fields.lanePolicy
	record.ArtifactPolicy = fields.artifactPolicy
	record.ArtifactSinkID = fields.artifactSinkID
}

func applyAPIKeyPrincipal(record *APIKeyRecord, platformIdentity *platformCredentialIdentity) {
	if platformIdentity != nil {
		record.ProfileScope = ProfileScopePlatform
		record.PlatformTenantID = platformIdentity.tenant.ID
		record.GatewayPrincipalID = platformIdentity.principal.ID
		record.TenantID = platformIdentity.tenant.ID
		record.PrincipalType = platformIdentity.principal.PrincipalType
		record.PrincipalReference = platformIdentity.principal.ID
	} else {
		record.TenantID = gatewayDefaultTenantID
		record.PrincipalType = record.KeyType
		record.PrincipalReference = record.ID
		if record.KeyType == APIKeyTypeCustomer {
			record.TenantID = record.CustomerID
			record.PrincipalReference = record.CustomerID
		}
		if record.KeyType == APIKeyTypeUser {
			record.PrincipalReference = record.OwnerUserID
		}
	}
	if record.RotationFamilyID == "" {
		record.RotationFamilyID = "key_family_" + randomID(10)
	}
}

func effectiveAPIKeyPolicy(record APIKeyRecord) apiKeyPolicyFields {
	fields, err := normalizeAPIKeyPolicy(apiKeyPolicyFromRecord(record))
	if err != nil {
		return apiKeyPolicyFields{}
	}
	return fields
}

func apiKeyPolicyFromRecord(record APIKeyRecord) apiKeyPolicyFields {
	return apiKeyPolicyFields{
		scopes: record.Scopes, allowedModalities: record.AllowedModalities, allowedOperations: record.AllowedOperations,
		qpsLimit: record.QPSLimit, rpmLimit: record.RPMLimit, tpmLimit: record.TPMLimit, concurrencyLimit: record.ConcurrencyLimit,
		monthlyTokenLimit: record.MonthlyTokenLimit, monthlyBudgetMicros: record.MonthlyBudgetMicros,
		monthlyImageLimit: record.MonthlyImageLimit, monthlyVideoSecondsLimit: record.MonthlyVideoSecondsLimit,
		monthlyAudioSecondsLimit: record.MonthlyAudioSecondsLimit, allowedCIDRs: record.AllowedCIDRs,
		lanePolicy: record.LanePolicy, artifactPolicy: record.ArtifactPolicy, artifactSinkID: record.ArtifactSinkID,
	}
}

func normalizeGatewayPolicyTokens(values, defaults []string) ([]string, error) {
	values = cleanStringList(values)
	if len(values) == 0 {
		return append([]string(nil), defaults...), nil
	}
	for index, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !validGatewayPolicyToken(value) {
			return nil, errInvalidAPIKeyPolicy
		}
		values[index] = value
	}
	return cleanStringList(values), nil
}

func validGatewayPolicyToken(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			continue
		}
		if index > 0 && (char == ':' || char == '_' || char == '-') {
			continue
		}
		return false
	}
	return true
}

func normalizeAllowedCIDRs(values []string) ([]string, error) {
	values = cleanStringList(values)
	out := make([]string, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			address, addressErr := netip.ParseAddr(value)
			if addressErr != nil {
				return nil, errors.New("allowed_cidrs must contain valid IP addresses or CIDR prefixes")
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		normalized := prefix.Masked().String()
		if !contains(out, normalized) {
			out = append(out, normalized)
		}
	}
	return out, nil
}

func apiKeyAllowsCanonicalRequest(record APIKeyRecord, request gatewaycore.CanonicalRequest) bool {
	policy := effectiveAPIKeyPolicy(record)
	if len(policy.scopes) == 0 || len(policy.allowedOperations) == 0 || len(policy.allowedModalities) == 0 {
		return false
	}
	requiredScope := GatewayScopeInvoke
	if request.Operation == GatewayOperationListModels {
		requiredScope = GatewayScopeModelsRead
	}
	if !contains(policy.scopes, requiredScope) || !contains(policy.allowedOperations, request.Operation) || !contains(policy.allowedModalities, request.Modality) {
		return false
	}
	switch request.Lane {
	case gatewaycore.LaneDirect:
		if policy.lanePolicy == GatewayLanePolicyDurableOnly {
			return false
		}
	case gatewaycore.LaneDurable:
		if policy.lanePolicy == GatewayLanePolicyDirectOnly {
			return false
		}
	default:
		return false
	}
	return apiKeyAllowsSourceIP(policy, request.SourceIP)
}

func apiKeyAllowsSourceIP(policy apiKeyPolicyFields, sourceIP string) bool {
	if len(policy.allowedCIDRs) == 0 {
		return true
	}
	address, err := netip.ParseAddr(strings.TrimSpace(sourceIP))
	if err != nil {
		return false
	}
	for _, value := range policy.allowedCIDRs {
		prefix, parseErr := netip.ParsePrefix(value)
		if parseErr == nil && prefix.Contains(address) {
			return true
		}
	}
	return false
}
