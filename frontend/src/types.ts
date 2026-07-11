export interface ApiResponse<T> {
  code: number
  message: string
  data: T
}

export interface PublicSettings {
  site_name: string
  site_subtitle: string
  public_base_url: string
  api_base_url: string
  gateway_base_path: string
  default_profile: string
  enabled_profiles: string[]
  setup_completed: boolean
  default_locale: string
  enabled_locales: string[]
  oidc_enabled: boolean
  oidc_provider_name: string
  service_center_mode: string
  version: string
  server_timezone: string
  server_utc_offset: string
  storage_mode: string
}

export interface AuthUser {
  username: string
  role: string
}

export interface LoginResult {
  access_token: string
  token_type: string
  expires_at: string
  user: AuthUser
}

export interface AdminSettings extends PublicSettings {
  oidc_issuer_url: string
  oidc_client_id: string
  data_retention_days: number
  prompt_logging_mode: string
  update_channel: string
}

export interface LocaleInfo {
  code: string
  name: string
  native: string
}

export interface ProviderConnection {
  id: string
  name: string
  type: string
  base_url: string
  status: string
  models: string[]
  priority: number
  secret_configured: boolean
  secret_hint: string
  created_at: string
  updated_at: string
}

export interface ProviderRequest {
  name: string
  type: string
  base_url: string
  status: string
  models: string[]
  priority: number
  api_key: string
}

export interface ProviderHealthCheck {
  id: string
  provider_id: string
  status: string
  latency_ms: number
  message: string
  models: string[]
  checked_at: string
}

export interface Project {
  id: string
  name: string
  description: string
  cost_center: string
  monthly_budget_cents: number
  policy_id: string
  current_month_cost_cents: number
  budget_remaining_cents: number
  budget_used_percent: number
  budget_status: string
  status: string
  created_at: string
  updated_at: string
}

export interface ProjectRequest {
  name: string
  description: string
  cost_center: string
  monthly_budget_cents: number
  policy_id: string
  status: string
}

export interface Department {
  id: string
  name: string
  code: string
  parent_id: string
  cost_center: string
  monthly_budget_cents: number
  status: string
  created_at: string
  updated_at: string
}

export interface DepartmentRequest {
  name: string
  code: string
  parent_id: string
  cost_center: string
  monthly_budget_cents: number
  status: string
}

export interface GovernancePolicy {
  id: string
  name: string
  description: string
  version: number
  last_updated_by: string
  scope_type: string
  scope_id: string
  model_allowlist: string[]
  model_denylist: string[]
  qps_limit: number
  monthly_token_limit: number
  monthly_budget_cents: number
  overage_action: string
  prompt_logging_mode: string
  retention_days: number
  tool_call_allowed: boolean
  image_input_allowed: boolean
  web_access_allowed: boolean
  status: string
  created_at: string
  updated_at: string
}

export interface GatewayPolicyCandidate {
  policy_id: string
  policy_name: string
  policy_version: number
  source: string
  scope_type: string
  scope_id: string
  status: string
  matched: boolean
  selected: boolean
  reason: string
}

export interface GatewayPolicyExplanation {
  api_key_id: string
  project_id: string
  selected_policy_id: string
  selected_policy_name: string
  selected_policy_version: number
  selected_source: string
  candidates: GatewayPolicyCandidate[]
}

export interface GovernancePolicyRequest {
  name: string
  description: string
  scope_type: string
  scope_id: string
  model_allowlist: string[]
  model_denylist: string[]
  qps_limit: number
  monthly_token_limit: number
  monthly_budget_cents: number
  overage_action: string
  prompt_logging_mode: string
  retention_days: number
  tool_call_allowed: boolean
  image_input_allowed: boolean
  web_access_allowed: boolean
  status: string
}

export interface Application {
  id: string
  project_id: string
  name: string
  environment: string
  owner: string
  status: string
  created_at: string
  updated_at: string
}

export interface ApplicationRequest {
  project_id: string
  name: string
  environment: string
  owner: string
  status: string
}

export interface WorkspaceUser {
  id: string
  email: string
  display_name: string
  status: string
  role: string
  project_count: number
  created_at: string
  updated_at: string
}

export interface WorkspaceUserRequest {
  email: string
  display_name: string
  status: string
  role: string
}

export interface RoleBinding {
  id: string
  user_id: string
  role: string
  scope_type: string
  scope_id: string
  created_at: string
  updated_at: string
}

export interface RoleBindingRequest {
  user_id: string
  role: string
  scope_type: string
  scope_id: string
}

export interface RoutingGroup {
  id: string
  name: string
  description: string
  platform: string
  rate_multiplier: number
  status: string
  sort_order: number
  account_count: number
  active_account_count: number
  created_at: string
  updated_at: string
}

export interface RoutingGroupRequest {
  name: string
  description: string
  platform: string
  rate_multiplier: number
  status: string
  sort_order: number
}

export interface ProviderAccount {
  id: string
  provider_id: string
  name: string
  platform: string
  auth_type: string
  status: string
  schedulable: boolean
  priority: number
  concurrency: number
  rate_multiplier: number
  models: string[]
  group_ids: string[]
  secret_configured: boolean
  secret_hint: string
  error_message: string
  last_used_at?: string
  expires_at?: string
  created_at: string
  updated_at: string
}

export interface ProviderAccountRequest {
  provider_id: string
  name: string
  platform: string
  auth_type: string
  status: string
  schedulable: boolean
  priority: number
  concurrency: number
  rate_multiplier: number
  models: string[]
  group_ids: string[]
  secret: string
  expires_at: string
}

export interface ProviderAccountHealthCheck {
  id: string
  account_id: string
  provider_id: string
  status: string
  latency_ms: number
  message: string
  models: string[]
  checked_at: string
}

export interface ModelPricing {
  id: string
  model: string
  currency: string
  input_price_cents_per_1m_tokens: number
  output_price_cents_per_1m_tokens: number
  status: string
  created_at: string
  updated_at: string
}

export interface ModelPricingRequest {
  model: string
  currency: string
  input_price_cents_per_1m_tokens: number
  output_price_cents_per_1m_tokens: number
  status: string
}

export interface APIKeyRecord {
  id: string
  project_id: string
  application_id: string
  name: string
  fingerprint: string
  prefix: string
  status: string
  policy_id: string
  model_allowlist: string[]
  qps_limit: number
  monthly_token_limit: number
  expires_at?: string
  last_used_at?: string
  created_at: string
  updated_at: string
}

export interface APIKeyCreateRequest {
  project_id: string
  application_id: string
  name: string
  policy_id: string
  model_allowlist: string[]
  qps_limit: number
  monthly_token_limit: number
  expires_at: string
}

export interface APIKeyUpdateRequest {
  name: string
  policy_id: string
  model_allowlist: string[]
  qps_limit: number
  monthly_token_limit: number
  expires_at: string
  status: string
}

export interface APIKeyCreateResponse {
  record: APIKeyRecord
  key: string
}

export interface AuditLog {
  id: string
  actor: string
  action: string
  resource_type: string
  resource_id: string
  summary: string
  created_at: string
}

export interface AlertEvent {
  id: string
  type: string
  severity: string
  status: string
  title: string
  summary: string
  resource_type: string
  resource_id: string
  project_id: string
  dedupe_key: string
  metadata: Record<string, string>
  first_seen_at: string
  last_seen_at: string
  acknowledged_at?: string
  acknowledged_by: string
  resolved_at?: string
  resolved_by: string
}

export interface AlertSummary {
  total: number
  active: number
  acknowledged: number
  resolved: number
  warning: number
  critical: number
}

export interface Dashboard {
  provider_count: number
  active_provider_count: number
  project_count: number
  application_count: number
  api_key_count: number
  active_api_key_count: number
  models: string[]
  recent_audit: AuditLog[]
}

export interface PortalWorkspace {
  projects: Project[]
  applications: Application[]
  api_keys: APIKeyRecord[]
  usage: UsageReport
  recent_traces: GatewayTrace[]
  alerts: AlertEvent[]
  models: string[]
  gateway_path: string
  can_manage_keys: boolean
  principal: string
}

export interface SystemUpdateAsset {
  name: string
  url: string
  os: string
  arch: string
  sha256: string
  size: number
}

export interface SystemReleaseInfo {
  version: string
  name: string
  notes: string
  published_at: string
  html_url: string
  asset?: SystemUpdateAsset
  assets?: SystemUpdateAsset[]
}

export interface SystemUpdateInfo {
  current_version: string
  latest_version: string
  has_update: boolean
  release_info?: SystemReleaseInfo
  cached: boolean
  warning?: string
  build_type: string
  update_supported: boolean
  manifest_configured: boolean
  restart_supported: boolean
  channel: string
  platform: string
  source: string
  signed_metadata: boolean
}

export interface SystemApplyResult {
  message: string
  operation_id: string
  need_restart: boolean
  already_up_to_date: boolean
  current_version: string
  latest_version: string
  manual_action?: string
}

export interface Plugin {
  id: string
  plugin_id: string
  name: string
  description: string
  category: string
  type: string
  tier: string
  version: string
  vendor: string
  status: string
  entitlement_status: string
  surfaces: string[]
  entry_point: string
  configurable: boolean
  packages?: PluginPackage[]
  created_at: string
  updated_at: string
}

export interface PluginPackage {
  plugin_id: string
  package_id: string
  version: string
  channel: string
  os: string
  arch: string
  sha256: string
  size_bytes: number
  required_entitlement: boolean
  revoked: boolean
  revoked_by_advisory: boolean
  advisory_id?: string
  advisory_title?: string
  advisory_severity?: string
  compatible: boolean
  compatibility_error?: string
  cache_status?: string
  cache_path?: string
  cached_at?: string
  install_status?: string
  installed_at?: string
}

export interface PluginPackageDownloadRequest {
  license_id?: string
  activation_secret?: string
  instance_id?: string
}

export interface PluginPackageImportRequest {
  content_base64?: string
  file_json?: unknown
}

export interface PluginPackageDownloadResult {
  package: PluginPackage
  cache_path: string
  sha256: string
  size_bytes: number
  cached_at: string
}

export interface PluginPackageInstallation {
  plugin_id: string
  package_id: string
  version: string
  os: string
  arch: string
  cache_path: string
  status: string
  installed_at: string
  updated_at: string
}

export interface LicenseEntitlement {
  public_id: string
  type: string
  resource_key: string
  status: string
  starts_at: string
  expires_at?: string
}

export interface OfficialLicenseStatus {
  configured: boolean
  license_id?: string
  customer_id?: string
  instance_id?: string
  snapshot_version?: number
  status: string
  edition?: string
  key_id?: string
  envelope_sha256?: string
  entitlements?: LicenseEntitlement[]
  issued_at?: string
  expires_at?: string
  imported_at?: string
  error?: string
}

export interface LicenseActivateRequest {
  license_id: string
  activation_secret: string
  instance_id?: string
  instance_fingerprint?: string
  display_name?: string
}

export interface LicenseImportRequest {
  envelope?: unknown
  file_json?: unknown
  activation_secret?: string
}

export interface PluginConfig {
  plugin_id: string
  settings: Record<string, string>
  secret_hints: Record<string, string>
  created_at: string
  updated_at: string
}

export interface PluginConfigRequest {
  settings: Record<string, string>
  secrets: Record<string, string>
}

export interface PluginDeliveryAttempt {
  id: string
  plugin_id: string
  alert_id: string
  alert_type: string
  alert_severity: string
  status: string
  target: string
  http_status: number
  error: string
  created_at: string
}

export interface OfficialCatalogStatus {
  mode: string
  bootstrap_url?: string
  source_url: string
  license_url?: string
  trust_configured: boolean
  catalog_version: number
  payload_sha256: string
  key_id: string
  plugin_count: number
  advisory_count: number
  status: string
  error?: string
  synced_at?: string
}

export interface PluginSummary {
  total: number
  enabled: number
  free: number
  paid_locked: number
  configurable: number
}

export interface PluginCatalog {
  summary: PluginSummary
  plugins: Plugin[]
}

export interface SidecarRuntimeStatus {
  plugin_id: string
  enabled: boolean
  installed: boolean
  running: boolean
  supervised: boolean
  version?: string
  endpoint?: string
  supervisor_state?: string
  restart_count?: number
  last_started_at?: string
  last_exited_at?: string
  next_restart_at?: string
  last_error?: string
  error?: string
}

export interface UsageRecord {
  id: string
  project_id: string
  application_id: string
  api_key_id: string
  api_fingerprint: string
  model: string
  provider_id: string
  provider_account_id: string
  status: string
  error_type: string
  latency_ms: number
  input_tokens: number
  output_tokens: number
  cost_cents: number
  created_at: string
}

export interface UsageModelSummary {
  model: string
  requests: number
  errors: number
  tokens: number
  cost_cents: number
  avg_latency_ms: number
}

export interface UsageReport {
 total_requests: number
 error_requests: number
 total_tokens: number
 total_cost_cents: number
  avg_latency_ms: number
  by_model: UsageModelSummary[]
  recent: UsageRecord[]
}

export type CostAllocationDimension = 'project' | 'application' | 'api_key' | 'model'

export interface CostAllocationRow {
  dimension: CostAllocationDimension
  resource_id: string
  resource_name: string
  project_id: string
  project_name: string
  cost_center: string
  application_id: string
  application_name: string
  api_key_id: string
  api_key_name: string
  api_fingerprint: string
  model: string
  requests: number
  error_requests: number
  total_tokens: number
  total_cost_cents: number
  avg_latency_ms: number
  budget_cents: number
  budget_used_percent: number
  cost_share_percent: number
}

export interface CostAllocationReport {
  dimension: CostAllocationDimension
  total_requests: number
  error_requests: number
  total_tokens: number
  total_cost_cents: number
  avg_latency_ms: number
  rows: CostAllocationRow[]
}

export interface RecordListQuery {
  limit?: number
  offset?: number
  q?: string
  dimension?: CostAllocationDimension
  api_key_id?: string
  model?: string
  type?: string
  severity?: string
  status?: string
  project_id?: string
  application_id?: string
  action?: string
  resource_type?: string
  from?: string
  to?: string
}

export interface GatewayTraceSummary {
  total: number
  routed: number
  errors: number
  tokens: number
  avg_latency_ms: number
}

export interface GatewayTrace {
  id: string
  project_id: string
  application_id: string
  api_key_id: string
  api_fingerprint: string
  model: string
  stream: boolean
  message_count: number
  provider_id: string
  provider_account_id: string
  route_source: string
  route_reason: string
  policy_id: string
  policy_name: string
  policy_source: string
  policy_version: number
  policy_snapshot: string
  status: string
  http_status: number
  error_type: string
  latency_ms: number
  input_tokens: number
  output_tokens: number
  request_summary: string
  response_summary: string
  created_at: string
}

export interface AuditLogSummary {
  total: number
  actors: number
  resources: number
  actions: number
}

export type ExportJobKind = 'usage' | 'gateway_traces' | 'audit_logs'

export interface ExportJob {
  id: string
  kind: ExportJobKind
  status: string
  filename: string
  content_type: string
  row_count: number
  size_bytes: number
  error: string
  parameters: Record<string, string>
  created_at: string
  updated_at: string
  expires_at: string
}
