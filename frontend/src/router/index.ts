import { createRouter, createWebHistory } from 'vue-router'
import { getPublicSettings } from '@/api/settings'
import EntryRedirectView from '@/views/EntryRedirectView.vue'
import LoginView from '@/views/LoginView.vue'
import SetupView from '@/views/SetupView.vue'
import AdminShell from '@/views/admin/AdminShell.vue'
import AdminApiKeysView from '@/views/admin/AdminApiKeysView.vue'
import AdminAlertsView from '@/views/admin/AdminAlertsView.vue'
import AdminAuditView from '@/views/admin/AdminAuditView.vue'
import AdminCostAllocationView from '@/views/admin/AdminCostAllocationView.vue'
import AdminDashboardView from '@/views/admin/AdminDashboardView.vue'
import AdminDepartmentsView from '@/views/admin/AdminDepartmentsView.vue'
import AdminExportJobsView from '@/views/admin/AdminExportJobsView.vue'
import AdminGatewayTracesView from '@/views/admin/AdminGatewayTracesView.vue'
import AdminModelPricingsView from '@/views/admin/AdminModelPricingsView.vue'
import AdminPluginsView from '@/views/admin/AdminPluginsView.vue'
import AdminPoliciesView from '@/views/admin/AdminPoliciesView.vue'
import AdminProviderAccountsView from '@/views/admin/AdminProviderAccountsView.vue'
import AdminProvidersView from '@/views/admin/AdminProvidersView.vue'
import AdminRoutingGroupsView from '@/views/admin/AdminRoutingGroupsView.vue'
import AdminSettingsView from '@/views/admin/AdminSettingsView.vue'
import AdminUsageView from '@/views/admin/AdminUsageView.vue'
import AdminUsersView from '@/views/admin/AdminUsersView.vue'
import ConsoleHomeView from '@/views/console/ConsoleHomeView.vue'
import OperatorHomeView from '@/views/operator/OperatorHomeView.vue'
import PortalHomeView from '@/views/portal/PortalHomeView.vue'
import type { PublicSettings } from '@/types'

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

function profileRoute(profile: string): string {
  if (profile === 'personal') return '/console'
  if (profile === 'relay_operator') return '/operator'
  return '/admin/dashboard'
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
  if (path.startsWith('/portal')) return settings.enabled_profiles.includes('enterprise')
  if (path.startsWith('/admin/settings')) return true
  if (path.startsWith('/admin')) return settings.enabled_profiles.includes('enterprise')
  return true
}

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: EntryRedirectView },
    { path: '/login', component: LoginView, meta: { titleKey: 'auth.signIn', descriptionKey: 'auth.signInToAccount' } },
    { path: '/setup', component: SetupView, meta: { titleKey: 'setup.title', descriptionKey: 'setup.subtitle' } },
    { path: '/console', component: ConsoleHomeView, meta: { titleKey: 'console.title', descriptionKey: 'console.subtitle' } },
    { path: '/operator', component: OperatorHomeView, meta: { titleKey: 'operator.title', descriptionKey: 'operator.subtitle' } },
    {
      path: '/admin',
      component: AdminShell,
      children: [
        { path: '', redirect: '/admin/dashboard' },
        { path: 'dashboard', component: AdminDashboardView, meta: { titleKey: 'admin.overview', descriptionKey: 'dashboard.subtitle' } },
        { path: 'providers', component: AdminProvidersView, meta: { titleKey: 'admin.providers', descriptionKey: 'providers.subtitle' } },
        { path: 'routing-groups', component: AdminRoutingGroupsView, meta: { titleKey: 'admin.routingGroups', descriptionKey: 'routingGroups.subtitle' } },
        { path: 'provider-accounts', component: AdminProviderAccountsView, meta: { titleKey: 'admin.providerAccounts', descriptionKey: 'providerAccounts.subtitle' } },
        { path: 'model-pricings', component: AdminModelPricingsView, meta: { titleKey: 'admin.modelPricings', descriptionKey: 'modelPricings.subtitle' } },
        { path: 'users', component: AdminUsersView, meta: { titleKey: 'admin.users', descriptionKey: 'users.subtitle' } },
        { path: 'departments', component: AdminDepartmentsView, meta: { titleKey: 'admin.departments', descriptionKey: 'departments.subtitle' } },
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
        { path: ':pathMatch(.*)*', redirect: '/admin/dashboard' }
      ]
    },
    { path: '/portal', component: PortalHomeView, meta: { titleKey: 'portal.title', descriptionKey: 'portal.subtitle' } },
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
  if (to.path === '/login' || to.path === '/setup') {
    return true
  }
  if (!settings?.setup_completed) {
    return '/setup'
  }
  if (!surfaceAllowed(to.path, settings)) {
    return entry
  }
  if ((to.path.startsWith('/admin') || to.path.startsWith('/portal') || to.path.startsWith('/console') || to.path.startsWith('/operator')) && !token) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  return true
})

export default router
