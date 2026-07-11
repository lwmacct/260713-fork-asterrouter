<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Activity, KeyRound, RadioTower, RefreshCw, Route, Server, WalletCards } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { getAPIKeys, getDashboard, getProviderAccounts, getRoutingGroups, getUsageReport } from '@/api/control'
import TopBar from '@/components/TopBar.vue'
import type { APIKeyRecord, Dashboard, ProviderAccount, RoutingGroup, UsageReport } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const dashboard = ref<Dashboard | null>(null)
const routingGroups = ref<RoutingGroup[]>([])
const routeResources = ref<ProviderAccount[]>([])
const apiKeys = ref<APIKeyRecord[]>([])
const usage = ref<UsageReport | null>(null)

const activeResources = computed(() => routeResources.value.filter((item) => item.status === 'active').length)
const schedulableResources = computed(() => routeResources.value.filter((item) => item.schedulable).length)
const activeKeys = computed(() => apiKeys.value.filter((item) => item.status === 'active').length)

function formatNumber(value?: number): string {
  return new Intl.NumberFormat().format(value || 0)
}

function formatCost(cents?: number): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD' }).format((cents || 0) / 100)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [dashboardData, groups, resources, keys, usageReport] = await Promise.all([
      getDashboard(),
      getRoutingGroups(),
      getProviderAccounts(),
      getAPIKeys(),
      getUsageReport()
    ])
    dashboard.value = dashboardData
    routingGroups.value = groups
    routeResources.value = resources
    apiKeys.value = keys
    usage.value = usageReport
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="app-page">
    <TopBar />
    <main class="content">
      <section class="page-header">
        <div>
          <h1>{{ t('operator.title') }}</h1>
          <p>{{ t('operator.subtitle') }}</p>
        </div>
        <div class="row-actions">
          <button class="button secondary" type="button" :disabled="loading" @click="load">
            <RefreshCw :size="17" />
            {{ t('common.refresh') }}
          </button>
          <RouterLink class="button secondary" to="/admin/settings">{{ t('admin.settings') }}</RouterLink>
        </div>
      </section>

      <div v-if="error" class="notice">{{ error }}</div>

      <section class="metric-grid">
        <article class="metric-card">
          <span class="metric-icon"><Server :size="18" /></span>
          <div>
            <span>{{ t('operator.providers') }}</span>
            <strong>{{ dashboard?.provider_count || 0 }}</strong>
            <small>{{ dashboard?.active_provider_count || 0 }} {{ t('providers.active') }}</small>
          </div>
        </article>
        <article class="metric-card">
          <span class="metric-icon"><Route :size="18" /></span>
          <div>
            <span>{{ t('operator.routeResources') }}</span>
            <strong>{{ activeResources }}</strong>
            <small>{{ schedulableResources }} {{ t('providerAccounts.schedulable') }}</small>
          </div>
        </article>
        <article class="metric-card">
          <span class="metric-icon"><KeyRound :size="18" /></span>
          <div>
            <span>{{ t('operator.customerKeys') }}</span>
            <strong>{{ activeKeys }}</strong>
            <small>{{ apiKeys.length }} {{ t('admin.apiKeys') }}</small>
          </div>
        </article>
        <article class="metric-card">
          <span class="metric-icon"><WalletCards :size="18" /></span>
          <div>
            <span>{{ t('operator.cost') }}</span>
            <strong>{{ formatCost(usage?.total_cost_cents) }}</strong>
            <small>{{ formatNumber(usage?.total_requests) }} {{ t('usage.requests') }}</small>
          </div>
        </article>
      </section>

      <section class="grid section-gap">
        <section class="panel">
          <div class="panel-header split-header">
            <div>
              <h2>{{ t('operator.dispatch') }}</h2>
              <p>{{ t('operator.dispatchHelp') }}</p>
            </div>
            <RadioTower :size="18" />
          </div>
          <div class="panel-body">
            <div class="status-line">
              <span class="pill">{{ routingGroups.length }} {{ t('admin.routingGroups') }}</span>
              <span class="pill">{{ routeResources.length }} {{ t('admin.providerAccounts') }}</span>
              <span class="pill">{{ dashboard?.models.length || 0 }} {{ t('dashboard.models') }}</span>
            </div>
            <div class="row-actions">
              <RouterLink class="button secondary" to="/admin/routing-groups">{{ t('admin.routingGroups') }}</RouterLink>
              <RouterLink class="button secondary" to="/admin/provider-accounts">{{ t('admin.providerAccounts') }}</RouterLink>
              <RouterLink class="button secondary" to="/admin/plugins">{{ t('admin.plugins') }}</RouterLink>
            </div>
          </div>
        </section>

        <section class="panel">
          <div class="panel-header split-header">
            <div>
              <h2>{{ t('operator.traffic') }}</h2>
              <p>{{ t('operator.trafficHelp') }}</p>
            </div>
            <Activity :size="18" />
          </div>
          <div class="panel-body">
            <div class="status-line">
              <span class="pill">{{ formatNumber(usage?.total_tokens) }} {{ t('usage.tokens') }}</span>
              <span class="pill">{{ formatNumber(usage?.error_requests) }} {{ t('usage.errors') }}</span>
            </div>
            <div class="row-actions">
              <RouterLink class="button secondary" to="/admin/api-keys">{{ t('admin.apiKeys') }}</RouterLink>
              <RouterLink class="button secondary" to="/admin/usage">{{ t('admin.usage') }}</RouterLink>
              <RouterLink class="button secondary" to="/admin/traces">{{ t('admin.traces') }}</RouterLink>
            </div>
          </div>
        </section>
      </section>
    </main>
  </div>
</template>
