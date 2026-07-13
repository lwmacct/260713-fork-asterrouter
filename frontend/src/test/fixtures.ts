import type { AuthUser, PublicSettings } from '@/types'

export function makeAuthUser(overrides: Partial<AuthUser> = {}): AuthUser {
  return {
    username: 'test@example.com',
    role: 'super_admin',
    display_name: 'Test User',
    email: 'test@example.com',
    ...overrides
  }
}

export function makePublicSettings(overrides: Partial<PublicSettings> = {}): PublicSettings {
  return {
    site_name: 'AsterRouter',
    site_subtitle: 'AI Gateway Control Plane',
    site_logo: '',
    public_base_url: 'http://localhost:8080',
    api_base_url: '/api/v1',
    gateway_base_path: '/v1',
    default_profile: 'enterprise',
    enabled_profiles: ['enterprise'],
    setup_completed: true,
    default_locale: 'en-US',
    enabled_locales: ['en-US', 'zh-CN'],
    oidc_enabled: false,
    oidc_provider_name: 'OIDC',
    oidc_require_verified_email: true,
    feishu_enabled: false,
    feishu_region: 'cn',
    github_oauth_enabled: false,
    google_oauth_enabled: false,
    dingtalk_enabled: false,
    registration_enabled: false,
    email_verify_enabled: false,
    totp_enabled: true,
    turnstile_enabled: false,
    turnstile_site_key: '',
    invitation_required: false,
    login_agreement_enabled: false,
    login_agreement_mode: 'checkbox',
    login_agreement_updated_at: '',
    legal_documents: [],
    backend_mode: false,
    support_contact: '',
    documentation_url: '',
    custom_endpoints: [],
    custom_menu_items: [],
    channel_monitor_enabled: false,
    available_channels_enabled: false,
    risk_control_enabled: false,
    cyber_session_block_enabled: false,
    backup_s3_enabled: false,
    service_center_mode: 'private',
    version: '0.3.0-test',
    server_timezone: 'UTC',
    server_utc_offset: '+00:00',
    storage_mode: 'memory',
    demo_mode: false,
    ...overrides
  }
}
