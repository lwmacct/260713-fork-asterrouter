import { createRouter, createWebHistory } from 'vue-router'
import { getPublicSettings } from '@/api/settings'
import type { PublicSettings } from '@/types'

const EntryRedirectView = () => import('@/views/EntryRedirectView.vue')
const LoginView = () => import('@/views/LoginView.vue')
const LegalDocumentView = () => import('@/views/LegalDocumentView.vue')
const AccountProfileView = () => import('@/views/AccountProfileView.vue')
const SetupView = () => import('@/views/SetupView.vue')
const AdminShell = () => import('@/views/admin/AdminShell.vue')
const AdminApiKeysView = () => import('@/views/admin/AdminApiKeysView.vue')
const AdminAlertsView = () => import('@/views/admin/AdminAlertsView.vue')
const AdminAuditView = () => import('@/views/admin/AdminAuditView.vue')
const AdminCostAllocationView = () => import('@/views/admin/AdminCostAllocationView.vue')
const AdminDashboardView = () => import('@/views/admin/AdminDashboardView.vue')
const AdminDepartmentsView = () => import('@/views/admin/AdminDepartmentsView.vue')
const AdminOrganizationGroupsView = () => import('@/views/admin/AdminOrganizationGroupsView.vue')
const AdminExportJobsView = () => import('@/views/admin/AdminExportJobsView.vue')
const AdminGatewayTracesView = () => import('@/views/admin/AdminGatewayTracesView.vue')
const AdminGatewayModelsView = () => import('@/views/admin/AdminGatewayModelsView.vue')
const AdminGatewaySimulatorView = () => import('@/views/admin/AdminGatewaySimulatorView.vue')
const AdminModelPricingsView = () => import('@/views/admin/AdminModelPricingsView.vue')
const AdminModelRoutesView = () => import('@/views/admin/AdminModelRoutesView.vue')
const AdminPluginsView = () => import('@/views/admin/AdminPluginsView.vue')
const AdminPoliciesView = () => import('@/views/admin/AdminPoliciesView.vue')
const AdminProviderAccountsView = () => import('@/views/admin/AdminProviderAccountsView.vue')
const AdminProvidersView = () => import('@/views/admin/AdminProvidersView.vue')
const AdminRoutingGroupsView = () => import('@/views/admin/AdminRoutingGroupsView.vue')
const AdminSettingsView = () => import('@/views/admin/AdminSettingsView.vue')
const AdminUsageView = () => import('@/views/admin/AdminUsageView.vue')
const AdminUsersView = () => import('@/views/admin/AdminUsersView.vue')
const ConsoleHomeView = () => import('@/views/console/ConsoleHomeView.vue')
const ConsoleShell = () => import('@/views/console/ConsoleShell.vue')
const CustomerBillingView = () => import('@/views/customer/CustomerBillingView.vue')
const CustomerHomeView = () => import('@/views/customer/CustomerHomeView.vue')
const CustomerNotificationSettingsView = () => import('@/views/customer/CustomerNotificationSettingsView.vue')
const CustomerShell = () => import('@/views/customer/CustomerShell.vue')
const OperatorHomeView = () => import('@/views/operator/OperatorHomeView.vue')
const OperatorShell = () => import('@/views/operator/OperatorShell.vue')
const OperatorCustomersView = () => import('@/views/operator/OperatorCustomersView.vue')
const OperatorGroupsView = () => import('@/views/operator/OperatorGroupsView.vue')
const OperatorPlansView = () => import('@/views/operator/OperatorPlansView.vue')
const OperatorBalancesView = () => import('@/views/operator/OperatorBalancesView.vue')
const OperatorPricingView = () => import('@/views/operator/OperatorPricingView.vue')
const OperatorRiskView = () => import('@/views/operator/OperatorRiskView.vue')
const OperatorNoticesView = () => import('@/views/operator/OperatorNoticesView.vue')
const OperatorUsageView = () => import('@/views/operator/OperatorUsageView.vue')
const OperatorKeysView = () => import('@/views/operator/OperatorKeysView.vue')
const PortalHomeView = () => import('@/views/portal/PortalHomeView.vue')
const PortalIntegrationView = () => import('@/views/portal/PortalIntegrationView.vue')
const PortalKeysView = () => import('@/views/portal/PortalKeysView.vue')
const PortalShell = () => import('@/views/portal/PortalShell.vue')

let publicSettingsCache: PublicSettings | null = null

export function setPublicSettingsCache(settings: PublicSettings | null) {
  publicSettingsCache = settings?.setup_completed ? settings : null
}

export function clearPublicSettingsCache() {
  publicSettingsCache = null
}

async function loadPublicSettings(): Promise<PublicSettings | null> {
  if (publicSettingsCache) return publicSettingsCache
  try {
    const settings = await getPublicSettings()
    setPublicSettingsCache(settings)
    return settings
  } catch {
    return null
  }
}

function profileRoute(profile: string, role = storedRole()): string {
  if (profile === 'personal') return '/console/overview'
  if (profile === 'relay_operator') return canOperateRelay(role) ? '/operator/overview' : '/customer/overview'
  return '/admin/dashboard'
}

function storedRole(): string {
  try {
    return JSON.parse(localStorage.getItem('asterrouter_admin_user') || '{}').role || ''
  } catch {
    return ''
  }
}

function canOperateRelay(role: string): boolean {
  return ['super_admin', 'platform_admin', 'demo_admin'].includes(role)
}

function defaultEntry(settings: PublicSettings | null): string {
  if (!settings?.setup_completed) return '/setup'
  if (settings.default_profile && settings.enabled_profiles.includes(settings.default_profile)) {
    return profileRoute(settings.default_profile)
  }
  return profileRoute(settings.enabled_profiles[0] || 'enterprise')
}

function surfaceAllowed(path: string, settings: PublicSettings | null): boolean {
  if (!settings?.setup_completed) return path === '/setup'
  if (path.startsWith('/console')) return settings.enabled_profiles.includes('personal')
  if (path.startsWith('/operator')) return settings.enabled_profiles.includes('relay_operator')
  if (path.startsWith('/customer')) return settings.enabled_profiles.includes('relay_operator')
  if (path.startsWith('/portal')) return settings.enabled_profiles.includes('enterprise')
  if (path.startsWith('/admin')) return settings.enabled_profiles.includes('enterprise')
  return true
}

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: EntryRedirectView },
    { path: '/login', component: LoginView, meta: { titleKey: 'auth.signIn', descriptionKey: 'auth.signInToAccount' } },
    { path: '/setup', component: SetupView, meta: { titleKey: 'setup.title', descriptionKey: 'setup.subtitle' } },
    {
      path: '/console',
      component: ConsoleShell,
      children: [
        { path: '', redirect: '/console/overview' },
        { path: 'overview', component: ConsoleHomeView, meta: { titleKey: 'console.overview', descriptionKey: 'console.subtitle', consolePanel: 'overview' } },
        { path: 'providers', component: AdminProvidersView, meta: { titleKey: 'admin.providers', descriptionKey: 'providers.subtitle' } },
        { path: 'models', component: AdminGatewayModelsView, meta: { titleKey: 'admin.gatewayModels', descriptionKey: 'gatewayModels.subtitle' } },
        { path: 'model-routes', component: AdminModelRoutesView, meta: { titleKey: 'admin.modelRoutes', descriptionKey: 'modelRoutes.subtitle' } },
        { path: 'gateway-simulator', component: AdminGatewaySimulatorView, meta: { titleKey: 'admin.gatewaySimulator', descriptionKey: 'gatewaySimulator.subtitle' } },
        { path: 'plugins', component: AdminPluginsView, meta: { titleKey: 'admin.plugins', descriptionKey: 'plugins.subtitle' } },
        { path: 'routing-groups', component: AdminRoutingGroupsView, meta: { titleKey: 'admin.routingGroups', descriptionKey: 'routingGroups.subtitle' } },
        { path: 'resources', component: AdminProviderAccountsView, meta: { titleKey: 'admin.providerAccounts', descriptionKey: 'providerAccounts.subtitle' } },
        { path: 'keys', component: AdminApiKeysView, meta: { titleKey: 'console.keys', descriptionKey: 'console.keySummary' } },
        { path: 'usage', component: AdminUsageView, meta: { titleKey: 'console.usage', descriptionKey: 'console.usageHelp' } },
        { path: 'settings', component: AdminSettingsView, meta: { titleKey: 'admin.settings', descriptionKey: 'admin.subtitle' } },
		{ path: 'account', component: AccountProfileView, meta: { titleKey: 'account.title', descriptionKey: 'account.subtitle' } },
        { path: ':pathMatch(.*)*', redirect: '/console/overview' }
      ]
    },
    {
      path: '/operator',
      component: OperatorShell,
      children: [
        { path: '', redirect: '/operator/overview' },
        { path: 'overview', component: OperatorHomeView, meta: { titleKey: 'operator.overview', descriptionKey: 'operator.subtitle', operatorPanel: 'overview' } },
        { path: 'customers', component: OperatorCustomersView, meta: { titleKey: 'operatorDomain.customers', descriptionKey: 'operatorDomain.customersHelp' } },
        { path: 'customer-keys', component: OperatorKeysView, meta: { titleKey: 'operatorDomain.keyList', descriptionKey: 'operatorDomain.keySummary' } },
        { path: 'customer-groups', component: OperatorGroupsView, meta: { titleKey: 'operatorDomain.groups', descriptionKey: 'operatorDomain.groupsHelp' } },
        { path: 'plans', component: OperatorPlansView, meta: { titleKey: 'operatorDomain.plans', descriptionKey: 'operatorDomain.plansHelp' } },
        { path: 'balances', component: OperatorBalancesView, meta: { titleKey: 'operatorDomain.balances', descriptionKey: 'operatorDomain.balancesHelp' } },
        { path: 'pricing', component: OperatorPricingView, meta: { titleKey: 'operatorDomain.pricing', descriptionKey: 'operatorDomain.pricingHelp' } },
        { path: 'risk', component: OperatorRiskView, meta: { titleKey: 'operatorDomain.risk', descriptionKey: 'operatorDomain.riskHelp' } },
        { path: 'notices', component: OperatorNoticesView, meta: { titleKey: 'operatorDomain.notices', descriptionKey: 'operatorDomain.noticesHelp' } },
        { path: 'providers', component: AdminProvidersView, meta: { titleKey: 'admin.providers', descriptionKey: 'providers.subtitle' } },
        { path: 'models', component: AdminGatewayModelsView, meta: { titleKey: 'admin.gatewayModels', descriptionKey: 'gatewayModels.subtitle' } },
        { path: 'model-routes', component: AdminModelRoutesView, meta: { titleKey: 'admin.modelRoutes', descriptionKey: 'modelRoutes.subtitle' } },
        { path: 'gateway-simulator', component: AdminGatewaySimulatorView, meta: { titleKey: 'admin.gatewaySimulator', descriptionKey: 'gatewaySimulator.subtitle' } },
        { path: 'routing-groups', component: AdminRoutingGroupsView, meta: { titleKey: 'operator.groupList', descriptionKey: 'operator.groupSummary' } },
        { path: 'resources', component: AdminProviderAccountsView, meta: { titleKey: 'operator.resourceList', descriptionKey: 'operator.resourceSummary' } },
        { path: 'usage', component: OperatorUsageView, meta: { titleKey: 'operator.traffic', descriptionKey: 'operator.trafficHelp' } },
        { path: 'plugins', component: AdminPluginsView, meta: { titleKey: 'admin.plugins', descriptionKey: 'plugins.subtitle' } },
        { path: 'settings', component: AdminSettingsView, meta: { titleKey: 'admin.settings', descriptionKey: 'admin.subtitle' } },
		{ path: 'account', component: AccountProfileView, meta: { titleKey: 'account.title', descriptionKey: 'account.subtitle' } },
        { path: ':pathMatch(.*)*', redirect: '/operator/overview' }
      ]
    },
    {
      path: '/admin',
      component: AdminShell,
      children: [
        { path: '', redirect: '/admin/dashboard' },
        { path: 'dashboard', component: AdminDashboardView, meta: { titleKey: 'admin.overview', descriptionKey: 'dashboard.subtitle' } },
        { path: 'providers', component: AdminProvidersView, meta: { titleKey: 'admin.providers', descriptionKey: 'providers.subtitle' } },
        { path: 'gateway-models', component: AdminGatewayModelsView, meta: { titleKey: 'admin.gatewayModels', descriptionKey: 'gatewayModels.subtitle' } },
        { path: 'model-routes', component: AdminModelRoutesView, meta: { titleKey: 'admin.modelRoutes', descriptionKey: 'modelRoutes.subtitle' } },
        { path: 'gateway-simulator', component: AdminGatewaySimulatorView, meta: { titleKey: 'admin.gatewaySimulator', descriptionKey: 'gatewaySimulator.subtitle' } },
        { path: 'routing-groups', component: AdminRoutingGroupsView, meta: { titleKey: 'admin.routingGroups', descriptionKey: 'routingGroups.subtitle' } },
        { path: 'provider-accounts', component: AdminProviderAccountsView, meta: { titleKey: 'admin.providerAccounts', descriptionKey: 'providerAccounts.subtitle' } },
        { path: 'model-pricings', component: AdminModelPricingsView, meta: { titleKey: 'admin.modelPricings', descriptionKey: 'modelPricings.subtitle' } },
        { path: 'users', component: AdminUsersView, meta: { titleKey: 'admin.users', descriptionKey: 'users.subtitle' } },
        { path: 'departments', component: AdminDepartmentsView, meta: { titleKey: 'admin.departments', descriptionKey: 'departments.subtitle' } },
				{ path: 'organization-groups', component: AdminOrganizationGroupsView, meta: { titleKey: 'organizationGroups.title', descriptionKey: 'organizationGroups.subtitle' } },
        { path: 'policies', component: AdminPoliciesView, meta: { titleKey: 'admin.policies', descriptionKey: 'policies.subtitle' } },
        { path: 'api-keys', component: AdminApiKeysView, meta: { titleKey: 'admin.apiKeys', descriptionKey: 'apiKeys.subtitle' } },
        { path: 'usage', component: AdminUsageView, meta: { titleKey: 'admin.usage', descriptionKey: 'usage.subtitle' } },
        { path: 'cost-allocation', component: AdminCostAllocationView, meta: { titleKey: 'admin.costAllocation', descriptionKey: 'costAllocation.subtitle' } },
        { path: 'traces', component: AdminGatewayTracesView, meta: { titleKey: 'admin.traces', descriptionKey: 'traces.subtitle' } },
        { path: 'alerts', component: AdminAlertsView, meta: { titleKey: 'admin.alerts', descriptionKey: 'alerts.subtitle' } },
        { path: 'exports', component: AdminExportJobsView, meta: { titleKey: 'admin.exports', descriptionKey: 'exports.subtitle' } },
        { path: 'plugins', component: AdminPluginsView, meta: { titleKey: 'admin.plugins', descriptionKey: 'plugins.subtitle' } },
        { path: 'audit', component: AdminAuditView, meta: { titleKey: 'admin.audit', descriptionKey: 'audit.subtitle' } },
        { path: 'settings', component: AdminSettingsView, meta: { titleKey: 'admin.settings', descriptionKey: 'admin.subtitle' } },
		{ path: 'account', component: AccountProfileView, meta: { titleKey: 'account.title', descriptionKey: 'account.subtitle' } },
        { path: ':pathMatch(.*)*', redirect: '/admin/dashboard' }
      ]
    },
    {
      path: '/customer',
      component: CustomerShell,
      children: [
        { path: '', redirect: '/customer/overview' },
        { path: 'overview', component: CustomerHomeView, meta: { titleKey: 'customer.overview', descriptionKey: 'customer.subtitle', customerPanel: 'overview' } },
        { path: 'keys', component: PortalKeysView, meta: { titleKey: 'customer.keys', descriptionKey: 'customer.keySummary' } },
        { path: 'integration', component: PortalIntegrationView, meta: { titleKey: 'customer.integration', descriptionKey: 'customer.integrationHelp' } },
        { path: 'usage', component: CustomerHomeView, meta: { titleKey: 'customer.usage', descriptionKey: 'customer.usageHelp', customerPanel: 'usage' } },
        { path: 'billing', component: CustomerBillingView, meta: { titleKey: 'customer.billing', descriptionKey: 'customer.billingHelp' } },
		{ path: 'notifications', component: CustomerNotificationSettingsView, meta: { titleKey: 'customer.notificationSettings', descriptionKey: 'customer.notificationSettingsHelp' } },
		{ path: 'account', component: AccountProfileView, meta: { titleKey: 'account.title', descriptionKey: 'account.subtitle' } },
        { path: ':pathMatch(.*)*', redirect: '/customer/overview' }
      ]
    },
    {
      path: '/portal',
      component: PortalShell,
      children: [
        { path: '', redirect: '/portal/overview' },
        { path: 'overview', component: PortalHomeView, meta: { titleKey: 'portal.overview', descriptionKey: 'portal.subtitle', portalPanel: 'overview' } },
        { path: 'integration', component: PortalIntegrationView, meta: { titleKey: 'portal.integrationGuide', descriptionKey: 'portal.gatewayHelp', portalPanel: 'integration' } },
        { path: 'keys', component: PortalKeysView, meta: { titleKey: 'portal.myKeys', descriptionKey: 'portal.keySummary', portalPanel: 'keys' } },
        { path: 'usage', component: PortalHomeView, meta: { titleKey: 'portal.usage', descriptionKey: 'portal.usageHelp', portalPanel: 'usage' } },
        { path: 'alerts', component: PortalHomeView, meta: { titleKey: 'portal.alerts', descriptionKey: 'portal.alertsHelp', portalPanel: 'alerts' } },
        { path: 'traces', component: PortalHomeView, meta: { titleKey: 'portal.recentTraces', descriptionKey: 'portal.traceHelp', portalPanel: 'traces' } },
		{ path: 'account', component: AccountProfileView, meta: { titleKey: 'account.title', descriptionKey: 'account.subtitle' } },
        { path: ':pathMatch(.*)*', redirect: '/portal/overview' }
      ]
    },
    { path: '/legal/:slug', component: LegalDocumentView },
    { path: '/:pathMatch(.*)*', redirect: '/' }
  ]
})

router.beforeEach(async (to) => {
  const token = localStorage.getItem('asterrouter_admin_token')
  const settings = await loadPublicSettings()
  const entry = defaultEntry(settings)
  if (to.path === '/') {
    return entry
  }
  if (to.path === '/login' && token) {
    return entry
  }
  if (to.path === '/setup') {
    if (settings?.setup_completed) {
      return entry
    }
    return true
  }
  if (to.path === '/login') {
    return true
  }
	if (to.path.startsWith('/legal/')) return true
  if (!settings?.setup_completed) {
    return '/setup'
  }
  if (!surfaceAllowed(to.path, settings)) {
    return entry
  }
  if ((to.path.startsWith('/admin') || to.path.startsWith('/portal') || to.path.startsWith('/console') || to.path.startsWith('/operator') || to.path.startsWith('/customer')) && !token) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  return true
})

export default router
