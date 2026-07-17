<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Check, CircleDollarSign, Download, Gift, MessageCircle, ReceiptText, RefreshCw, Search, TicketPercent, WalletCards } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import {
  createCustomerRechargeOrder,
  downloadCustomerBillingCSV,
  getCustomerBilling,
  getCustomerBillingEntries,
  redeemCustomerCode,
  type CustomerBillingEntries,
  type CustomerBillingOverview,
  type CustomerBillingQuery
} from '@/api/customer'

const { t } = useI18n()
const pageSize = 20
const loading = ref(true)
const saving = ref(false)
const exporting = ref(false)
const error = ref('')
const notice = ref('')
const overview = ref<CustomerBillingOverview | null>(null)
const entries = ref<CustomerBillingEntries>({ items: [], total: 0, limit: pageSize, offset: 0 })
const selectedPreset = ref(10_000_000)
const customMode = ref(false)
const customAmount = ref<number | null>(null)
const paymentMethod = ref<'wechat' | 'alipay'>('wechat')
const voucherID = ref('')
const redeemCode = ref('')
const filters = reactive({ kind: '', from: '', to: '' })

const amountMicros = computed(() => customMode.value ? Math.round(Number(customAmount.value || 0) * 1_000_000) : selectedPreset.value)
const currentPage = computed(() => Math.floor(entries.value.offset / pageSize) + 1)
const totalPages = computed(() => Math.max(1, Math.ceil(entries.value.total / pageSize)))
const paymentUnavailable = computed(() => !(overview.value?.payment_channels || []).some((item) => item.enabled))
const kindOptions = computed(() => [
  { id: '', label: t('customer.allTypes') },
  { id: 'recharge', label: t('customer.recharge') },
  { id: 'redeem', label: t('customer.redeemKind') },
  { id: 'usage', label: t('customer.usageKind') },
  { id: 'refund', label: t('customer.refund') }
])

function query(offset = entries.value.offset): CustomerBillingQuery {
  return { kind: filters.kind || undefined, from: filters.from || undefined, to: filters.to || undefined, limit: pageSize, offset }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [billingData, entryData] = await Promise.all([getCustomerBilling(), getCustomerBillingEntries(query(0))])
    overview.value = billingData
    entries.value = entryData
    if (!billingData.recharge_options.includes(selectedPreset.value) && billingData.recharge_options[0]) selectedPreset.value = billingData.recharge_options[0]
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function loadEntries(offset = 0) {
  loading.value = true
  error.value = ''
  try {
    entries.value = await getCustomerBillingEntries(query(offset))
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

function selectPreset(value: number) {
  selectedPreset.value = value
  customMode.value = false
  customAmount.value = null
}

function selectCustom() {
  customMode.value = true
  customAmount.value ||= 50
}

async function submitRecharge() {
  error.value = ''
  notice.value = ''
  if (amountMicros.value < 1_000_000) {
    error.value = t('customer.invalidAmount')
    return
  }
  saving.value = true
  try {
    await createCustomerRechargeOrder({ amount_micros: amountMicros.value, payment_method: paymentMethod.value, voucher_id: voucherID.value || undefined })
    notice.value = t('customer.rechargeCreated')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function redeem() {
  if (!redeemCode.value.trim()) return
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await redeemCustomerCode(redeemCode.value.trim())
    overview.value = result.overview
    redeemCode.value = ''
    notice.value = t('customer.redeemSuccess')
    await loadEntries(0)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function exportCSV() {
  exporting.value = true
  error.value = ''
  try {
    await downloadCustomerBillingCSV(query(0))
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    exporting.value = false
  }
}

function resetFilters() {
  filters.kind = ''
  filters.from = ''
  filters.to = ''
  loadEntries(0)
}

function formatMoney(micros = 0): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 6 }).format(micros / 1_000_000)
}

function formatSignedMoney(micros: number): string {
  return `${micros > 0 ? '+' : ''}${formatMoney(micros)}`
}

function formatDate(value?: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

function kindLabel(kind: string): string {
  const labels: Record<string, string> = {
    recharge: t('customer.recharge'), redeem: t('customer.redeemKind'), usage: t('customer.usageKind'),
    refund: t('customer.refund'), gift: t('customer.gift'), profit: t('customer.profit')
  }
  return labels[kind] || kind
}

onMounted(load)
</script>

<template>
  <main class="content customer-billing-page">
    <section class="page-header billing-heading">
      <div><h1>{{ t('customer.billing') }}</h1><p>{{ t('customer.billingHelp') }}</p></div>
      <div class="row-actions"><button class="button secondary" type="button" :disabled="loading" @click="load"><RefreshCw :size="16" />{{ t('common.refresh') }}</button><button class="button secondary" type="button" :disabled="exporting" @click="exportCSV"><Download :size="16" />{{ t('customer.exportCSV') }}</button></div>
    </section>

    <div v-if="error" class="notice">{{ error }}</div>
    <div v-if="notice" class="notice success"><Check :size="17" />{{ notice }}</div>

    <section class="billing-balance-strip">
      <article><span><WalletCards :size="18" /></span><div><small>{{ t('customer.balance') }}</small><strong>{{ formatMoney(overview?.balance_micros) }}</strong></div></article>
      <article><span class="gift"><Gift :size="18" /></span><div><small>{{ t('customer.giftBalance') }}</small><strong>{{ formatMoney(overview?.gift_balance_micros) }}</strong></div></article>
      <article><span class="profit"><CircleDollarSign :size="18" /></span><div><small>{{ t('customer.profitBalance') }}</small><strong>{{ formatMoney(overview?.profit_balance_micros) }}</strong></div></article>
      <article class="total"><span><ReceiptText :size="18" /></span><div><small>{{ t('customer.totalBalance') }}</small><strong>{{ formatMoney(overview?.total_micros) }}</strong></div></article>
    </section>

    <section class="billing-action-grid">
      <section class="panel recharge-panel">
        <div class="panel-header"><div><h2>{{ t('customer.onlineRecharge') }}</h2><p>{{ t('customer.rechargeAmount') }}</p></div><WalletCards :size="18" /></div>
        <form class="panel-body recharge-form" @submit.prevent="submitRecharge">
          <div class="field"><span>{{ t('customer.rechargeAmount') }}</span><div class="recharge-amount-grid"><button v-for="amount in overview?.recharge_options || []" :key="amount" class="amount-option" :class="{ active: !customMode && selectedPreset === amount }" type="button" @click="selectPreset(amount)">{{ formatMoney(amount) }}</button><button class="amount-option" :class="{ active: customMode }" type="button" @click="selectCustom">{{ t('customer.customAmount') }}</button></div></div>
          <label v-if="customMode" class="field custom-amount"><span>{{ t('customer.customAmount') }}</span><div><span>$</span><input v-model.number="customAmount" type="number" min="1" max="100000" step="0.01" :placeholder="t('customer.customAmountPlaceholder')" /></div></label>
          <div class="field"><span>{{ t('customer.paymentMethod') }}</span><div class="payment-methods"><button type="button" :class="{ active: paymentMethod === 'wechat' }" @click="paymentMethod = 'wechat'"><MessageCircle :size="19" /><span><strong>{{ t('customer.wechat') }}</strong><small v-if="paymentUnavailable">{{ t('customer.channelUnavailable') }}</small></span></button><button type="button" :class="{ active: paymentMethod === 'alipay' }" @click="paymentMethod = 'alipay'"><WalletCards :size="19" /><span><strong>{{ t('customer.alipay') }}</strong><small v-if="paymentUnavailable">{{ t('customer.channelUnavailable') }}</small></span></button></div></div>
          <label class="field"><span>{{ t('customer.voucher') }}</span><select v-model="voucherID"><option value="">{{ t('customer.noVoucherOption') }}</option><option v-for="voucher in overview?.vouchers || []" :key="voucher.id" :value="voucher.id">{{ voucher.title }} · {{ formatMoney(voucher.amount_micros) }}</option></select></label>
          <div v-if="paymentUnavailable" class="billing-channel-notice">{{ t('customer.channelUnavailable') }}</div>
          <div class="recharge-submit"><div><small>{{ t('customer.rechargeAmount') }}</small><strong>{{ formatMoney(amountMicros) }}</strong></div><button class="button primary" type="submit" :disabled="saving">{{ saving ? t('customer.submitting') : t('customer.rechargeNow') }}</button></div>
        </form>
      </section>

      <div class="billing-side-stack">
        <section class="panel redeem-panel"><div class="panel-header"><div><h2>{{ t('customer.redeemTitle') }}</h2><p>{{ t('customer.billingHelp') }}</p></div><TicketPercent :size="18" /></div><form class="panel-body" @submit.prevent="redeem"><label class="redeem-input"><Search :size="17" /><input v-model="redeemCode" :placeholder="t('customer.redeemPlaceholder')" autocomplete="off" /></label><button class="button primary" type="submit" :disabled="saving || !redeemCode.trim()">{{ t('customer.redeem') }}</button></form></section>
        <section class="panel voucher-panel"><div class="panel-header"><div><h2>{{ t('customer.availableVouchers') }}</h2><p>{{ overview?.vouchers.length || 0 }} {{ t('customer.voucher') }}</p></div><Gift :size="18" /></div><div class="voucher-list"><div v-for="voucher in overview?.vouchers || []" :key="voucher.id"><span><strong>{{ voucher.title }}</strong><small>{{ t('customer.minimumRecharge', { amount: formatMoney(voucher.minimum_recharge_micros) }) }}</small></span><div><strong>{{ formatMoney(voucher.amount_micros) }}</strong><small v-if="voucher.expires_at">{{ t('customer.expiresAt', { date: formatDate(voucher.expires_at) }) }}</small></div></div><p v-if="!(overview?.vouchers || []).length" class="empty-vouchers">{{ t('customer.noVouchers') }}</p></div></section>
      </div>
    </section>

    <section class="panel billing-detail-panel">
      <div class="panel-header billing-detail-header"><div><h2>{{ t('customer.billingDetails') }}</h2><p>{{ t('customer.pageSummary', { page: currentPage, total: entries.total }) }}</p></div><ReceiptText :size="18" /></div>
      <div class="billing-filters"><div class="billing-kind-tabs"><button v-for="option in kindOptions" :key="option.id" type="button" :class="{ active: filters.kind === option.id }" @click="filters.kind = option.id; loadEntries(0)">{{ option.label }}</button></div><label><span>{{ t('common.from') }}</span><input v-model="filters.from" type="date" /></label><label><span>{{ t('common.to') }}</span><input v-model="filters.to" type="date" /></label><button class="button secondary compact-button" type="button" @click="loadEntries(0)">{{ t('customer.filter') }}</button><button class="button ghost compact-button" type="button" @click="resetFilters">{{ t('customer.reset') }}</button></div>
      <div class="table-scroll"><table class="data-table billing-table"><thead><tr><th>{{ t('customer.time') }}</th><th>{{ t('customer.description') }}</th><th>{{ t('customer.reference') }}</th><th>{{ t('customer.amount') }}</th><th>{{ t('customer.balanceAfter') }}</th></tr></thead><tbody><tr v-for="entry in entries.items" :key="entry.id"><td>{{ formatDate(entry.created_at) }}</td><td><span class="billing-kind pill" :class="entry.amount_micros >= 0 ? 'status-success' : 'status-warning'">{{ kindLabel(entry.kind) }}</span><strong>{{ entry.description || kindLabel(entry.kind) }}</strong></td><td><code>{{ entry.reference || '-' }}</code></td><td><strong :class="entry.amount_micros >= 0 ? 'positive-amount' : 'negative-amount'">{{ formatSignedMoney(entry.amount_micros) }}</strong></td><td>{{ formatMoney(entry.balance_after_micros) }}</td></tr><tr v-if="!entries.items.length"><td colspan="5" class="empty-cell">{{ t('customer.emptyEntries') }}</td></tr></tbody></table></div>
      <div class="billing-pagination"><span>{{ t('customer.pageSummary', { page: currentPage, total: entries.total }) }}</span><div><button class="button secondary compact-button" type="button" :disabled="currentPage <= 1 || loading" @click="loadEntries(entries.offset - pageSize)">{{ t('common.previous') }}</button><button class="button secondary compact-button" type="button" :disabled="currentPage >= totalPages || loading" @click="loadEntries(entries.offset + pageSize)">{{ t('common.next') }}</button></div></div>
    </section>
  </main>
</template>

<style scoped>
.customer-billing-page { gap: 24px; }
.billing-heading { margin-bottom: 0; }
.billing-balance-strip { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); overflow: hidden; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); }
.billing-balance-strip article { display: flex; min-width: 0; align-items: center; gap: 12px; min-height: 92px; padding: 17px 20px; border-left: 1px solid var(--border); }
.billing-balance-strip article:first-child { border-left: 0; }
.billing-balance-strip article > span { display: grid; width: 38px; height: 38px; flex: 0 0 auto; place-items: center; border-radius: 8px; background: var(--info-bg); color: var(--primary); }
.billing-balance-strip article > span.gift { background: #fffbeb; color: #b45309; }
.billing-balance-strip article > span.profit { background: #ecfdf5; color: #047857; }
.billing-balance-strip article.total > span { background: #eef2ff; color: #4338ca; }
.billing-balance-strip article div { display: grid; min-width: 0; gap: 4px; }
.billing-balance-strip small { color: var(--text-muted); font-size: 11px; }
.billing-balance-strip strong { color: var(--text); font-size: 20px; font-weight: 650; }
.billing-action-grid { display: grid; grid-template-columns: minmax(0, 1.55fr) minmax(300px, 0.85fr); gap: 24px; align-items: start; }
.billing-action-grid .panel { margin: 0; }
.recharge-panel .panel-header, .redeem-panel .panel-header, .voucher-panel .panel-header, .billing-detail-panel .panel-header { padding: 18px 22px; }
.recharge-panel .panel-body, .redeem-panel .panel-body { padding: 22px; }
.recharge-panel .panel-header > div, .redeem-panel .panel-header > div, .voucher-panel .panel-header > div, .billing-detail-header > div { display: grid; gap: 3px; }
.recharge-panel .panel-header p, .redeem-panel .panel-header p, .voucher-panel .panel-header p, .billing-detail-header p { margin: 0; color: var(--text-muted); font-size: 11px; }
.recharge-form { gap: 18px; }
.recharge-amount-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 9px; }
.amount-option { min-height: 44px; border: 1px solid var(--border); border-radius: 7px; background: var(--surface); color: var(--text-secondary); cursor: pointer; font-weight: 600; }
.amount-option:hover, .amount-option.active { border-color: var(--primary); background: var(--info-bg); color: var(--primary); }
.custom-amount > div { display: grid; grid-template-columns: 38px minmax(0, 1fr); align-items: center; overflow: hidden; border: 1px solid var(--border); border-radius: 7px; }
.custom-amount > div > span { display: grid; height: 100%; place-items: center; border-right: 1px solid var(--border); background: var(--surface-subtle); color: var(--text-muted); }
.custom-amount input { border: 0; border-radius: 0; }
.payment-methods { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.payment-methods button { display: flex; min-width: 0; align-items: center; gap: 10px; min-height: 58px; padding: 10px 12px; border: 1px solid var(--border); border-radius: 7px; background: var(--surface); color: var(--text-muted); cursor: pointer; text-align: left; }
.payment-methods button.active { border-color: var(--primary); box-shadow: 0 0 0 1px var(--primary); color: var(--primary); }
.payment-methods button span { display: grid; min-width: 0; gap: 2px; }
.payment-methods button strong { color: var(--text); font-size: 12px; }
.payment-methods button small { overflow: hidden; color: var(--text-muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.billing-channel-notice { padding: 10px 12px; border: 1px solid color-mix(in srgb, var(--warning) 30%, var(--border)); border-radius: 7px; background: var(--warning-bg); color: var(--text-secondary); font-size: 11px; line-height: 1.5; }
.recharge-submit { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding-top: 16px; border-top: 1px solid var(--border); }
.recharge-submit > div { display: grid; gap: 2px; }
.recharge-submit small { color: var(--text-muted); font-size: 11px; }
.recharge-submit strong { color: var(--text); font-size: 22px; }
.billing-side-stack { display: grid; gap: 24px; }
.redeem-panel .panel-body { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px; }
.redeem-input { display: flex; align-items: center; gap: 8px; padding: 0 10px; border: 1px solid var(--border); border-radius: 7px; color: var(--text-muted); }
.redeem-input input { width: 100%; min-width: 0; border: 0; outline: 0; background: transparent; }
.voucher-list { display: grid; }
.voucher-list > div { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 18px 22px; border-top: 1px solid var(--border); }
.voucher-list > div:first-child { border-top: 0; }
.voucher-list span, .voucher-list > div > div { display: grid; gap: 3px; }
.voucher-list > div > div { text-align: right; }
.voucher-list strong { color: var(--text); font-size: 12px; }
.voucher-list small { color: var(--text-muted); font-size: 10px; }
.empty-vouchers { margin: 0; padding: 32px 18px; color: var(--text-muted); font-size: 12px; text-align: center; }
.billing-detail-panel { margin: 0; }
.billing-detail-header { justify-content: space-between; }
.billing-filters { display: flex; align-items: end; flex-wrap: wrap; gap: 10px; padding: 12px 16px; border-bottom: 1px solid var(--border); }
.billing-kind-tabs { display: flex; flex-wrap: wrap; gap: 4px; margin-right: auto; padding: 3px; border-radius: 7px; background: var(--surface-subtle); }
.billing-kind-tabs button { min-height: 30px; padding: 0 10px; border: 0; border-radius: 5px; background: transparent; color: var(--text-muted); cursor: pointer; font-size: 11px; }
.billing-kind-tabs button.active { background: var(--surface); box-shadow: var(--shadow-sm); color: var(--text); }
.billing-filters > label { display: grid; gap: 4px; color: var(--text-muted); font-size: 10px; }
.billing-filters input { width: 142px; min-height: 32px; }
.compact-button { min-height: 32px; padding: 0 10px; font-size: 11px; }
.billing-table { min-width: 900px; }
.billing-table td:nth-child(2) { display: flex; align-items: center; gap: 8px; }
.billing-table td code { color: var(--text-muted); font-size: 10px; }
.billing-kind { flex: 0 0 auto; }
.positive-amount { color: var(--success); }
.negative-amount { color: var(--danger); }
.billing-pagination { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 16px; border-top: 1px solid var(--border); color: var(--text-muted); font-size: 11px; }
.billing-pagination > div { display: flex; gap: 8px; }
@media (max-width: 1080px) {
  .billing-balance-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .billing-balance-strip article:nth-child(3) { border-top: 1px solid var(--border); border-left: 0; }
  .billing-balance-strip article:nth-child(4) { border-top: 1px solid var(--border); }
  .billing-action-grid { grid-template-columns: 1fr; }
}
@media (max-width: 640px) {
  .billing-heading .row-actions { width: 100%; }
  .billing-heading .button { flex: 1; }
  .billing-balance-strip { grid-template-columns: 1fr; }
  .billing-balance-strip article { min-height: 78px; border-top: 1px solid var(--border); border-left: 0; }
  .billing-balance-strip article:first-child { border-top: 0; }
  .recharge-amount-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .payment-methods { grid-template-columns: 1fr; }
  .redeem-panel .panel-body { grid-template-columns: 1fr; }
  .recharge-submit { align-items: stretch; flex-direction: column; }
  .recharge-submit .button { width: 100%; }
  .billing-filters { align-items: stretch; }
  .billing-kind-tabs { width: 100%; margin-right: 0; }
  .billing-kind-tabs button { flex: 1; }
  .billing-filters > label { flex: 1; }
  .billing-filters input { width: 100%; }
  .billing-pagination { align-items: stretch; flex-direction: column; }
  .billing-pagination > div .button { flex: 1; }
}
</style>
