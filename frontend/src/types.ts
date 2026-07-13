export interface ApiResponse<T> {
  code: number
  message: string
  data: T
}

export interface PublicSettings {
  site_name: string
  site_subtitle: string
	site_logo: string
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
	oidc_require_verified_email: boolean
	feishu_enabled: boolean
	feishu_region: 'cn' | 'global'
	github_oauth_enabled: boolean
	google_oauth_enabled: boolean
	dingtalk_enabled: boolean
	registration_enabled: boolean
	email_verify_enabled: boolean
	totp_enabled: boolean
	turnstile_enabled: boolean
	turnstile_site_key: string
	invitation_required: boolean
	login_agreement_enabled: boolean
	login_agreement_mode: 'modal' | 'checkbox'
	login_agreement_updated_at: string
	legal_documents: LegalDocument[]
	backend_mode: boolean
	support_contact: string
	documentation_url: string
	custom_endpoints: CustomEndpoint[]
	custom_menu_items: CustomMenuItem[]
	channel_monitor_enabled: boolean
	available_channels_enabled: boolean
	risk_control_enabled: boolean
	cyber_session_block_enabled: boolean
	backup_s3_enabled: boolean
  service_center_mode: string
  version: string
  server_timezone: string
  server_utc_offset: string
  storage_mode: string
  demo_mode: boolean
}

export interface AuthUser {
  username: string
  role: string
	display_name?: string
	email?: string
	avatar_data_url?: string
}

export interface AccountLoginMethod {
	id: 'email' | 'local' | 'oidc' | 'feishu' | 'github' | 'google' | 'dingtalk'
	label: string
	available: boolean
	bound: boolean
	detail?: string
}

export interface AccountProfile {
	id: string
	email: string
	display_name: string
	avatar_data_url?: string
	status: string
	role: string
	balance_cents: number
	concurrency_limit: number
	rpm_limit: number
	external_issuer?: string
	auth_identities: Array<{id:string;user_id:string;issuer:string;subject:string;email:string;created_at:string;updated_at:string}>
	email_verified: boolean
	password_enabled: boolean
	totp_enabled: boolean
	totp_available: boolean
	managed_by_config: boolean
	created_at: string
	updated_at: string
	login_methods: AccountLoginMethod[]
}

export interface TOTPSetup {
	secret: string
	provisioning_uri: string
}

export interface LoginResult {
  access_token: string
  token_type: string
  expires_at: string
  user: AuthUser
}

export interface AdminSettings extends PublicSettings {
	runtime_restart_required: boolean
	runtime_restart_reasons: string[]
	oidc_issuer_url: string
  oidc_client_id: string
	feishu_app_id: string
	feishu_app_secret?: string
	feishu_configured: boolean
	github_oauth_client_id: string
	github_oauth_client_secret?: string
	github_oauth_configured: boolean
	google_oauth_client_id: string
	google_oauth_client_secret?: string
	google_oauth_configured: boolean
	dingtalk_client_id: string
	dingtalk_client_secret?: string
	dingtalk_configured: boolean
	allowed_email_domains: string[]
	invitation_codes: string[]
	trusted_proxy_headers: boolean
	turnstile_secret_key?: string
	turnstile_configured: boolean
	default_balance_cents: number
	default_concurrency: number
	default_rpm: number
	auth_source_defaults: Record<string, AuthSourceDefault>
	smtp_host: string
	smtp_port: number
	smtp_username: string
	smtp_password?: string
	smtp_from: string
	smtp_configured: boolean
	email_templates: EmailTemplate[]
	login_agreement_title: string
	login_agreement_content: string
	default_page_size: number
	page_size_options: number[]
	home_content: string
	hide_import_button: boolean
	channel_monitor_interval_seconds: number
	cyber_session_block_ttl_seconds: number
	backup_s3_endpoint: string
	backup_s3_region: string
	backup_s3_bucket: string
	backup_s3_prefix: string
	backup_s3_access_key: string
	backup_s3_secret_key?: string
	backup_s3_configured: boolean
	backup_s3_path_style: boolean
	backup_retention_days: number
	backup_max_retained: number
	backup_schedule_enabled: boolean
	backup_interval_hours: number
  data_retention_days: number
  prompt_logging_mode: string
  update_channel: string
}

export interface RetentionCleanupResult {
	before: string
	usage_records: number
	gateway_traces: number
	alert_events: number
	audit_logs: number
}

export interface LegalDocument {
	id: string
	name: string
	slug: string
	content: string
}

export interface EmailTemplate {
	event: string
	locale: 'en-US' | 'zh-CN'
	subject: string
	html: string
}

export interface CustomEndpoint { name: string; endpoint: string; description: string }
export interface CustomMenuItem { id: string; label: string; url: string; open_in_new_tab: boolean }
export interface AuthSourceDefault { enabled: boolean; balance_cents: number; concurrency: number; rpm: number }

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

export interface OrganizationGroup {
	id: string
	name: string
	description: string
	status: string
	member_ids: string[]
	created_at: string
	updated_at: string
}

export interface OrganizationGroupRequest {
	name: string
	description: string
	status: string
	member_ids: string[]
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

export interface WorkspaceUser {
  id: string
  email: string
  display_name: string
  status: string
  role: string
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
  group_type: string
  rate_multiplier: number
  rpm_limit: number
  is_exclusive: boolean
  daily_budget_cents: number
  weekly_budget_cents: number
  monthly_budget_cents: number
  image_enabled: boolean
  batch_image_enabled: boolean
  image_rate_multiplier: number
  batch_image_discount_multiplier: number
  image_price_1k_cents: number
  image_price_2k_cents: number
  image_price_4k_cents: number
  video_enabled: boolean
  video_rate_multiplier: number
  video_price_480p_cents: number
  video_price_720p_cents: number
  video_price_1080p_cents: number
  peak_rate_enabled: boolean
  peak_start: string
  peak_end: string
  peak_rate_multiplier: number
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
  group_type: string
  rate_multiplier: number
  rpm_limit: number
  is_exclusive: boolean
  daily_budget_cents: number
  weekly_budget_cents: number
  monthly_budget_cents: number
  image_enabled: boolean
  batch_image_enabled: boolean
  image_rate_multiplier: number
  batch_image_discount_multiplier: number
  image_price_1k_cents: number
  image_price_2k_cents: number
  image_price_4k_cents: number
  video_enabled: boolean
  video_rate_multiplier: number
  video_price_480p_cents: number
  video_price_720p_cents: number
  video_price_1080p_cents: number
  peak_rate_enabled: boolean
  peak_start: string
  peak_end: string
  peak_rate_multiplier: number
  status: string
  sort_order: number
}

export interface ProviderAccountTempUnschedulableRule {
  status_code: number
  keywords: string[]
  duration_minutes: number
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
  weight: number
  concurrency: number
  rpm_limit: number
  tpm_limit: number
  load_factor?: number
  rate_multiplier: number
  models: string[]
  group_ids: string[]
  secret_configured: boolean
  secret_hint: string
  error_message: string
  last_used_at?: string
  expires_at?: string
  cooldown_until?: string
  circuit_state: string
  circuit_failure_threshold: number
  circuit_open_seconds: number
  consecutive_failures: number
  circuit_opened_until?: string
  last_failure_at?: string
  temp_unschedulable_rules: ProviderAccountTempUnschedulableRule[]
  temp_unschedulable_reason: string
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
  weight: number
  concurrency: number
  rpm_limit: number
  tpm_limit: number
  load_factor?: number | null
  rate_multiplier: number
  models: string[]
  group_ids: string[]
  secret: string
  expires_at: string
  circuit_failure_threshold: number
  circuit_open_seconds: number
  temp_unschedulable_rules: ProviderAccountTempUnschedulableRule[]
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

export interface GatewayModel {
  id: string
  model_id: string
  name: string
  description: string
  modality: string
  default_route_group: string
  sticky_enabled: boolean
  sticky_ttl_seconds: number
  status: string
  route_count: number
  created_at: string
  updated_at: string
}

export interface GatewayModelRequest {
  model_id: string
  name: string
  description: string
  modality: string
  default_route_group: string
  sticky_enabled: boolean
  sticky_ttl_seconds: number
  status: string
}

export interface ModelRoute {
  id: string
  gateway_model_id: string
  route_group: string
  provider_account_id: string
  upstream_model: string
  priority: number
  weight: number
  status: string
  created_at: string
  updated_at: string
}

export interface ModelRouteRequest {
  gateway_model_id: string
  route_group: string
  provider_account_id: string
  upstream_model: string
  priority: number
  weight: number
  status: string
}

export interface GatewaySimulationCandidate {
  rank: number
  route_id: string
  route_group: string
  provider_id: string
  provider_account_id: string
  upstream_model: string
  headroom: number
  rpm_limit: number
  tpm_limit: number
  concurrency: number
  circuit_state: string
  eligible: boolean
  reason: string
}

export interface GatewaySimulation {
  requested_model: string
  resolved_model: string
  route_group: string
  status: string
  summary: string
  candidates: GatewaySimulationCandidate[]
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
  name: string
  fingerprint: string
  prefix: string
  status: string
  key_type: string
  customer_id: string
  owner_user_id: string
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
  name: string
  policy_id: string
  model_allowlist: string[]
  qps_limit: number
  monthly_token_limit: number
  expires_at: string
  key_type?: string
  customer_id?: string
  owner_user_id?: string
}

export interface APIKeyUpdateRequest {
  name: string
  policy_id: string
  model_allowlist: string[]
  qps_limit: number
  monthly_token_limit: number
  expires_at: string
  status: string
  key_type?: string
  customer_id?: string
  owner_user_id?: string
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
  api_key_count: number
  active_api_key_count: number
  models: string[]
  recent_audit: AuditLog[]
}

export interface PortalWorkspace {
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

export interface SystemArchiveInfo {
  id: string
  path: string
  size_bytes: number
  created_at: string
}

export interface S3BackupObject { key: string; id: string; size_bytes: number; last_modified: string }

export interface SystemRestoreResult {
  operation_id: string
  backup_id: string
  need_restart: boolean
  message: string
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

export interface LicenseRedeemRequest {
  code: string
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

export interface PluginAPIToken {
  id: string
  name: string
  plugin_id?: string
  token_prefix: string
  scopes: string[]
  surfaces: string[]
  status: string
  expires_at?: string
  last_used_at?: string
  created_at: string
  updated_at: string
}

export interface PluginAPITokenCreateRequest {
  name: string
  plugin_id?: string
  scopes: string[]
  surfaces: string[]
  expires_at?: string
}

export interface PluginAPITokenCreateResult {
  token: PluginAPIToken
  secret: string
}

export interface OfficialFeedStatus {
  service_key: string
  feed_id: string
  feed_version: string
  data_schema_version: string
  status: string
  signature_verified: boolean
  payload_sha256: string
  size_bytes: number
  issued_at: string
  expires_at: string
  imported_at: string
}

export interface OfficialFeedClientInfo {
  instance_id: string
  license_id: string
  encryption_algorithm: string
  encryption_public_key: string
}

export interface OfficialFeedImportRequest {
  envelope?: unknown
  file_json?: unknown
}

export interface OfficialFeedSyncRun {
  id: string
  service_key: string
  feed_id?: string
  mode: string
  status: string
  request_id?: string
  source_url?: string
  error_code?: string
  error?: string
  started_at: string
  finished_at: string
}

export interface OfficialFeedSyncResult {
  feed: OfficialFeedStatus
  run: OfficialFeedSyncRun
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
  redeem_url?: string
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
  api_key_id: string
  customer_id: string
  api_fingerprint: string
  model: string
  upstream_model: string
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

export interface OperatorCustomerGroup { id:string; name:string; description:string; status:string; created_at:string; updated_at:string }
export interface OperatorPlan { id:string; name:string; description:string; monthly_fee_cents:number; included_tokens:number; monthly_limit_cents:number; rate_multiplier:number; status:string; created_at:string; updated_at:string }
export interface OperatorCustomer { id:string; name:string; email:string; group_id:string; plan_id:string; status:string; balance_cents:number; credit_cents:number; notes:string; created_at:string; updated_at:string }
export interface OperatorPricingRule { id:string; name:string; plan_id:string; model:string; input_price_cents_per_1m_tokens:number; output_price_cents_per_1m_tokens:number; rate_multiplier:number; status:string; created_at:string; updated_at:string }
export interface OperatorBalanceEntry { id:string; customer_id:string; kind:string; amount_cents:number; balance_after_cents:number; reference:string; note:string; actor:string; created_at:string }
export interface OperatorRiskRule { id:string; name:string; rule_type:string; threshold:number; window_minutes:number; action:string; description:string; status:string; created_at:string; updated_at:string }
export interface GatewayRiskBlock { api_key_id:string; rule_id:string; reason:string; expires_at:string; created_at:string }
export interface OperatorNotice { id:string; title:string; content:string; audience:string; status:string; publish_at?:string; created_at:string; updated_at:string }
export interface OperatorDashboard { customers:number; active_customers:number; plans:number; balance_cents:number; risk_rules:number; published_notices:number }

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

export type CostAllocationDimension = 'api_key' | 'user' | 'department' | 'group' | 'model'

export interface CostAllocationRow {
  dimension: CostAllocationDimension
  resource_id: string
  resource_name: string
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
  provider_id?: string
  provider_account_id?: string
  type?: string
  severity?: string
  status?: string
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
  api_key_id: string
  api_fingerprint: string
  model: string
  stream: boolean
  message_count: number
  provider_id: string
  provider_account_id: string
  gateway_model_id: string
  route_id: string
  route_group: string
  upstream_model: string
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
