<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Activity, Code2, KeyRound, ReceiptText, RefreshCw, WalletCards } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { getPortalWorkspace } from '@/api/control'
import { getCustomerBilling, type CustomerBillingOverview } from '@/api/customer'
import type { PortalWorkspace } from '@/types'

const { t } = useI18n()
const route = useRoute()
const loading = ref(true)
const error = ref('')
const billing = ref<CustomerBillingOverview | null>(null)
const workspace = ref<PortalWorkspace | null>(null)
const activePanel = computed(() => route.meta.customerPanel === 'usage' ? 'usage' : 'overview')
const activeKeys = computed(() => (workspace.value?.api_keys || []).filter((item) => item.status === 'active').length)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [billingData, workspaceData] = await Promise.all([getCustomerBilling(), getPortalWorkspace()])
    billing.value = billingData
    workspace.value = workspaceData
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

function formatMoney(cents = 0): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'CNY' }).format(cents / 100)
}

function formatNumber(value = 0): string {
  return new Intl.NumberFormat().format(value)
}

function formatDate(value: string): string {
  return new Date(value).toLocaleString()
}

onMounted(load)
</script>

<template>
  <main class="content customer-home-page">
    <section class="page-header">
      <div>
        <h1>{{ activePanel === 'usage' ? t('customer.usage') : t('customer.overview') }}</h1>
        <p>{{ activePanel === 'usage' ? t('customer.usageHelp') : t('customer.subtitle') }}</p>
      </div>
      <button class="button secondary" type="button" :disabled="loading" @click="load">
        <RefreshCw :size="16" />{{ t('common.refresh') }}
      </button>
    </section>

    <div v-if="error" class="notice">{{ error }}</div>

    <section v-if="activePanel === 'overview'" class="metric-grid customer-balance-grid">
      <article class="metric-card"><span class="metric-icon"><WalletCards :size="18" /></span><div><span>{{ t('customer.balance') }}</span><strong>{{ formatMoney(billing?.balance_cents) }}</strong><small>{{ t('customer.billing') }}</small></div></article>
      <article class="metric-card"><span class="metric-icon gift-icon"><WalletCards :size="18" /></span><div><span>{{ t('customer.giftBalance') }}</span><strong>{{ formatMoney(billing?.gift_balance_cents) }}</strong><small>{{ t('customer.availableVouchers') }}</small></div></article>
      <article class="metric-card"><span class="metric-icon profit-icon"><Activity :size="18" /></span><div><span>{{ t('customer.profitBalance') }}</span><strong>{{ formatMoney(billing?.profit_balance_cents) }}</strong><small>{{ t('customer.profit') }}</small></div></article>
      <article class="metric-card"><span class="metric-icon total-icon"><ReceiptText :size="18" /></span><div><span>{{ t('customer.totalBalance') }}</span><strong>{{ formatMoney(billing?.total_cents) }}</strong><small>{{ t('customer.balance') }}</small></div></article>
    </section>

    <section v-else class="metric-grid customer-balance-grid">
      <article class="metric-card"><span class="metric-icon"><Activity :size="18" /></span><div><span>{{ t('customer.requestCount') }}</span><strong>{{ formatNumber(workspace?.usage.total_requests) }}</strong><small>{{ formatNumber(workspace?.usage.error_requests) }} {{ t('usage.errors') }}</small></div></article>
      <article class="metric-card"><span class="metric-icon gift-icon"><Activity :size="18" /></span><div><span>{{ t('customer.tokenCount') }}</span><strong>{{ formatNumber(workspace?.usage.total_tokens) }}</strong><small>Token</small></div></article>
      <article class="metric-card"><span class="metric-icon profit-icon"><WalletCards :size="18" /></span><div><span>{{ t('customer.totalCost') }}</span><strong>{{ formatMoney(workspace?.usage.total_cost_cents) }}</strong><small>{{ t('customer.usage') }}</small></div></article>
      <article class="metric-card"><span class="metric-icon total-icon"><KeyRound :size="18" /></span><div><span>{{ t('customer.activeKeys') }}</span><strong>{{ activeKeys }}</strong><small>API Keys</small></div></article>
    </section>

    <section v-if="activePanel === 'overview'" class="panel customer-quick-panel">
      <div class="panel-header"><div><h2>{{ t('customer.quickActions') }}</h2><p>{{ t('customer.subtitle') }}</p></div></div>
      <div class="customer-quick-grid">
        <RouterLink to="/customer/keys"><KeyRound :size="20" /><span><strong>{{ t('customer.keys') }}</strong><small>{{ t('customer.keySummary') }}</small></span></RouterLink>
        <RouterLink to="/customer/integration"><Code2 :size="20" /><span><strong>{{ t('customer.integration') }}</strong><small>{{ t('customer.integrationHelp') }}</small></span></RouterLink>
        <RouterLink to="/customer/billing"><ReceiptText :size="20" /><span><strong>{{ t('customer.billing') }}</strong><small>{{ t('customer.billingHelp') }}</small></span></RouterLink>
      </div>
    </section>

    <section v-else class="panel">
      <div class="panel-header"><h2>{{ t('customer.recentUsage') }}</h2></div>
      <div class="panel-body table-scroll">
        <table class="data-table customer-usage-table">
          <thead><tr><th>{{ t('customer.time') }}</th><th>{{ t('customer.model') }}</th><th>{{ t('customer.status') }}</th><th>{{ t('customer.tokens') }}</th><th>{{ t('customer.cost') }}</th></tr></thead>
          <tbody>
            <tr v-for="item in workspace?.usage.recent || []" :key="item.id"><td>{{ formatDate(item.created_at) }}</td><td><strong>{{ item.model }}</strong></td><td><span class="pill" :class="item.status === 'forwarded' ? 'status-success' : 'status-warning'">{{ item.status }}</span></td><td>{{ formatNumber(item.input_tokens + item.output_tokens) }}</td><td>{{ formatMoney(item.cost_cents) }}</td></tr>
            <tr v-if="!(workspace?.usage.recent || []).length"><td colspan="5" class="empty-cell">{{ t('customer.noUsage') }}</td></tr>
          </tbody>
        </table>
      </div>
    </section>
  </main>
</template>

<style scoped>
.customer-home-page { gap: 16px; }
.customer-balance-grid { margin-bottom: 0; }
.gift-icon { color: #b45309; background: #fffbeb; }
.profit-icon { color: #047857; background: #ecfdf5; }
.total-icon { color: #4338ca; background: #eef2ff; }
.customer-quick-panel .panel-header > div { display: grid; gap: 3px; }
.customer-quick-panel .panel-header p { margin: 0; color: var(--text-muted); font-size: 12px; }
.customer-quick-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); }
.customer-quick-grid a { display: flex; min-width: 0; align-items: center; gap: 12px; min-height: 92px; padding: 18px 20px; border-left: 1px solid var(--border); color: var(--text-secondary); text-decoration: none; }
.customer-quick-grid a:first-child { border-left: 0; }
.customer-quick-grid a:hover { background: var(--surface-subtle); color: var(--text); }
.customer-quick-grid span { display: grid; min-width: 0; gap: 4px; }
.customer-quick-grid strong { color: var(--text); font-size: 13px; }
.customer-quick-grid small { overflow: hidden; color: var(--text-muted); font-size: 11px; line-height: 1.5; text-overflow: ellipsis; }
.customer-usage-table { min-width: 720px; }
@media (max-width: 760px) {
  .customer-quick-grid { grid-template-columns: 1fr; }
  .customer-quick-grid a { border-top: 1px solid var(--border); border-left: 0; }
  .customer-quick-grid a:first-child { border-top: 0; }
}
</style>
