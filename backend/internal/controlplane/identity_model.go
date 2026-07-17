package controlplane

import "time"

const (
	WorkspaceUserStatusActive   = "active"
	WorkspaceUserStatusDisabled = "disabled"

	RoleSuperAdmin      = "super_admin"
	RolePlatformAdmin   = "platform_admin"
	RoleKeyManager      = "key_manager"
	RoleReadOnlyAuditor = "read_only_auditor"
	RoleDeveloper       = "developer"

	RoleScopeGlobal     = "global"
	RoleScopeResource   = "resource"
	RoleScopeSurface    = "surface"
	RoleScopeDepartment = "department"

	RBACResourceDashboard       = "dashboard"
	RBACResourceRouting         = "routing"
	RBACResourceProviders       = "providers"
	RBACResourceAPIKeys         = "api_keys"
	RBACResourceUsage           = "usage"
	RBACResourceTraces          = "traces"
	RBACResourceAIJobs          = "ai_jobs"
	RBACResourceArtifacts       = "artifacts"
	RBACResourceAlerts          = "alerts"
	RBACResourceIdentity        = "identity"
	RBACResourcePlatformTenants = "platform_tenants"
	RBACResourcePolicies        = "policies"
	RBACResourceAudit           = "audit"
	RBACResourceExports         = "exports"
	RBACResourcePlugins         = "plugins"
	RBACResourceSettings        = "settings"
	RBACResourceSystem          = "system"

	SurfacePersonal      = "personal"
	SurfaceRelayOperator = "relay_operator"
	SurfaceEnterprise    = "enterprise"
	SurfacePlatform      = "platform"
	SurfacePortal        = "portal"
	SurfaceCustomer      = "customer"
)

type WorkspaceUser struct {
	ID                     string     `json:"id"`
	Email                  string     `json:"email"`
	DisplayName            string     `json:"display_name"`
	AvatarDataURL          string     `json:"avatar_data_url,omitempty"`
	Status                 string     `json:"status"`
	Role                   string     `json:"role"`
	BalanceMicros          int64      `json:"balance_micros"`
	ConcurrencyLimit       int        `json:"concurrency_limit"`
	RPMLimit               int        `json:"rpm_limit"`
	ExternalIssuer         string     `json:"external_issuer,omitempty"`
	ExternalSubject        string     `json:"external_subject,omitempty"`
	DepartmentID           string     `json:"department_id,omitempty"`
	TOTPEnabled            bool       `json:"totp_enabled"`
	TOTPSecretCiphertext   string     `json:"-"`
	TOTPRecoveryHashes     []string   `json:"-"`
	PasswordHash           string     `json:"-"`
	EmailVerified          bool       `json:"email_verified"`
	EmailVerifyHash        string     `json:"-"`
	EmailVerifyExpiresAt   *time.Time `json:"-"`
	PasswordResetHash      string     `json:"-"`
	PasswordResetExpiresAt *time.Time `json:"-"`
	SessionVersion         int64      `json:"-"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type AuthIdentity struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Issuer    string    `json:"issuer"`
	Subject   string    `json:"subject"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AccountProfile struct {
	ID               string         `json:"id"`
	Email            string         `json:"email"`
	DisplayName      string         `json:"display_name"`
	AvatarDataURL    string         `json:"avatar_data_url,omitempty"`
	Status           string         `json:"status"`
	Role             string         `json:"role"`
	BalanceMicros    int64          `json:"balance_micros"`
	ConcurrencyLimit int            `json:"concurrency_limit"`
	RPMLimit         int            `json:"rpm_limit"`
	ExternalIssuer   string         `json:"external_issuer,omitempty"`
	AuthIdentities   []AuthIdentity `json:"auth_identities"`
	EmailVerified    bool           `json:"email_verified"`
	PasswordEnabled  bool           `json:"password_enabled"`
	TOTPEnabled      bool           `json:"totp_enabled"`
	ManagedByConfig  bool           `json:"managed_by_config"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type AccountProfileUpdateRequest struct {
	DisplayName   string `json:"display_name"`
	AvatarDataURL string `json:"avatar_data_url"`
}

type AccountPasswordUpdateRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type WorkspaceUserDefaults struct {
	BalanceMicros    int64
	ConcurrencyLimit int
	RPMLimit         int
}

type WorkspaceUserRequest struct {
	Email        string  `json:"email"`
	DisplayName  string  `json:"display_name"`
	Status       string  `json:"status"`
	Role         string  `json:"role"`
	DepartmentID *string `json:"department_id"`
}

type RoleBinding struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	ScopeType string    `json:"scope_type"`
	ScopeID   string    `json:"scope_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type RoleBindingRequest struct {
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
}
