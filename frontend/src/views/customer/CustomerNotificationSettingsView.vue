<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { BellRing, Check, Mail, MessageSquareText, Save } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import {
  getCustomerNotificationSettings,
  updateCustomerNotificationSettings,
  type CustomerNotificationChannel,
  type CustomerNotificationPreference
} from '@/api/customer'

const { t } = useI18n()
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const saved = ref(false)
const preferences = ref<CustomerNotificationPreference[]>([])

const meta = computed<Record<string, { name: string; description: string; threshold?: string; unit?: string }>>(() => ({
  balance_low: { name: t('customer.notificationBalanceLow'), description: t('customer.notificationBalanceLowHelp'), threshold: t('customer.balanceThreshold'), unit: t('customer.yuan') },
  abuse_5xx: { name: t('customer.notificationErrorRate'), description: t('customer.notificationErrorRateHelp'), threshold: t('customer.errorRateThreshold'), unit: '%' },
  payment: { name: t('customer.notificationPayment'), description: t('customer.notificationPaymentHelp') },
  monthly_bill: { name: t('customer.notificationMonthlyBill'), description: t('customer.notificationMonthlyBillHelp') },
  announcement: { name: t('customer.notificationAnnouncement'), description: t('customer.notificationAnnouncementHelp') },
  model_update: { name: t('customer.notificationModelUpdate'), description: t('customer.notificationModelUpdateHelp') },
  account_security: { name: t('customer.notificationSecurity'), description: t('customer.notificationSecurityHelp') },
  marketing: { name: t('customer.notificationMarketing'), description: t('customer.notificationMarketingHelp') },
  product_update: { name: t('customer.notificationProduct'), description: t('customer.notificationProductHelp') }
}))

function hasChannel(preference: CustomerNotificationPreference, channel: CustomerNotificationChannel): boolean {
  return preference.channels.includes(channel)
}

function toggleChannel(preference: CustomerNotificationPreference, channel: CustomerNotificationChannel, enabled: boolean) {
  if (enabled && !preference.channels.includes(channel)) preference.channels.push(channel)
  if (!enabled) preference.channels = preference.channels.filter((item) => item !== channel)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const result = await getCustomerNotificationSettings()
    preferences.value = result.preferences
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  error.value = ''
  saved.value = false
  try {
    const result = await updateCustomerNotificationSettings(preferences.value)
    preferences.value = result.preferences
    saved.value = true
    window.setTimeout(() => { saved.value = false }, 2500)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <main class="content customer-notification-page">
    <section class="page-header">
      <div>
        <h1>{{ t('customer.notificationSettings') }}</h1>
        <p>{{ t('customer.notificationSettingsHelp') }}</p>
      </div>
    </section>

    <div v-if="error" class="alert error">{{ error }}</div>

    <section class="panel notification-settings-panel">
      <div v-if="loading" class="notification-settings-loading">{{ t('common.loading') }}</div>
      <template v-else>
        <div class="notification-table-scroll">
          <table class="notification-settings-table">
            <thead>
              <tr>
                <th>{{ t('customer.notificationType') }}</th>
                <th class="center-column">{{ t('customer.enabled') }}</th>
                <th>{{ t('customer.channels') }}</th>
                <th>{{ t('customer.threshold') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="preference in preferences" :key="preference.event_type" :class="{ disabled: !preference.enabled }">
                <td class="notification-type-cell" :data-label="t('customer.notificationType')">
                  <span class="notification-type-icon"><BellRing :size="17" /></span>
                  <div>
                    <strong>
                      {{ meta[preference.event_type]?.name || preference.event_type }}
                      <span v-if="preference.marketing" class="marketing-badge">{{ t('customer.marketing') }}</span>
                    </strong>
                    <p>{{ meta[preference.event_type]?.description }}</p>
                  </div>
                </td>
                <td class="center-column" :data-label="t('customer.enabled')">
                  <label class="switch-control">
                    <input v-model="preference.enabled" type="checkbox" :aria-label="`${meta[preference.event_type]?.name} ${t('customer.enabled')}`" />
                    <span></span>
                  </label>
                </td>
                <td :data-label="t('customer.channels')">
                  <div class="notification-channels">
                    <label>
                      <input type="checkbox" :checked="hasChannel(preference, 'inapp')" @change="toggleChannel(preference, 'inapp', ($event.target as HTMLInputElement).checked)" />
                      <MessageSquareText :size="15" />{{ t('customer.inApp') }}
                    </label>
                    <label>
                      <input type="checkbox" :checked="hasChannel(preference, 'email')" @change="toggleChannel(preference, 'email', ($event.target as HTMLInputElement).checked)" />
                      <Mail :size="15" />{{ t('customer.email') }}
                    </label>
                  </div>
                </td>
                <td :data-label="t('customer.threshold')">
                  <label v-if="meta[preference.event_type]?.threshold" class="threshold-control">
                    <span>{{ meta[preference.event_type]?.threshold }}</span>
                    <span class="threshold-input-wrap">
                      <input
                        v-model.number="preference.threshold"
                        type="number"
                        :min="preference.event_type === 'abuse_5xx' ? 1 : 0"
                        :max="preference.event_type === 'abuse_5xx' ? 100 : 1000000"
                        step="0.01"
                      />
                      <small>{{ meta[preference.event_type]?.unit }}</small>
                    </span>
                  </label>
                  <span v-else class="no-threshold">-</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <footer class="notification-settings-footer">
          <span v-if="saved" class="saved-status"><Check :size="15" />{{ t('customer.settingsSaved') }}</span>
          <button class="button primary" type="button" :disabled="saving" @click="save">
            <Save :size="16" />{{ saving ? t('common.saving') : t('customer.saveSettings') }}
          </button>
        </footer>
      </template>
    </section>
  </main>
</template>

<style scoped>
.customer-notification-page { display: grid; gap: 24px; }
.notification-settings-panel { overflow: hidden; }
.notification-settings-loading { padding: 42px 24px; color: var(--text-muted); font-size: 13px; text-align: center; }
.notification-table-scroll { overflow-x: auto; }
.notification-settings-table { width: 100%; min-width: 850px; border-collapse: collapse; }
.notification-settings-table th { height: 48px; padding: 0 20px; border-bottom: 1px solid var(--border); background: var(--surface-subtle); color: var(--text-muted); font-size: 12px; font-weight: 650; text-align: left; }
.notification-settings-table th:first-child { width: 43%; }
.notification-settings-table th:nth-child(2) { width: 86px; }
.notification-settings-table th:nth-child(3) { width: 240px; }
.notification-settings-table th:nth-child(4) { width: 220px; }
.notification-settings-table td { min-height: 82px; padding: 17px 20px; border-bottom: 1px solid var(--border); vertical-align: middle; }
.notification-settings-table tbody tr:last-child td { border-bottom: 0; }
.notification-settings-table tbody tr { transition: background .15s ease, opacity .15s ease; }
.notification-settings-table tbody tr:hover { background: var(--surface-hover); }
.notification-settings-table tbody tr.disabled { opacity: .66; }
.center-column { text-align: center !important; }
.notification-type-cell { display: flex; align-items: flex-start; gap: 12px; }
.notification-type-icon { display: grid; width: 34px; height: 34px; flex: 0 0 auto; place-items: center; border-radius: 8px; background: var(--info-bg); color: var(--primary-600); }
.notification-type-cell > div { min-width: 0; }
.notification-type-cell strong { display: flex; flex-wrap: wrap; align-items: center; gap: 7px; color: var(--text); font-size: 13.5px; font-weight: 680; }
.notification-type-cell p { max-width: 520px; margin: 4px 0 0; color: var(--text-muted); font-size: 12px; line-height: 1.5; }
.marketing-badge { padding: 2px 6px; border-radius: 4px; background: var(--surface-subtle); color: var(--text-muted); font-size: 10px; font-weight: 650; }
.switch-control { position: relative; display: inline-flex; width: 36px; height: 21px; cursor: pointer; }
.switch-control input { position: absolute; width: 1px; height: 1px; opacity: 0; }
.switch-control span { position: relative; width: 36px; height: 21px; border-radius: 11px; background: var(--border-strong); transition: background .15s ease; }
.switch-control span::after { position: absolute; top: 3px; left: 3px; width: 15px; height: 15px; border-radius: 50%; background: #fff; box-shadow: 0 1px 3px rgba(0,0,0,.2); content: ''; transition: transform .15s ease; }
.switch-control input:checked + span { background: var(--primary-500); }
.switch-control input:checked + span::after { transform: translateX(15px); }
.switch-control input:focus-visible + span { outline: 2px solid var(--primary-500); outline-offset: 2px; }
.notification-channels { display: flex; flex-wrap: wrap; gap: 8px 18px; }
.notification-channels label { display: inline-flex; min-height: 32px; align-items: center; gap: 6px; color: var(--text-secondary); font-size: 12px; cursor: pointer; }
.notification-channels input { width: 16px; height: 16px; margin: 0; accent-color: var(--primary-600); }
.threshold-control { display: grid; gap: 7px; color: var(--text-muted); font-size: 11.5px; }
.threshold-input-wrap { display: flex; width: 132px; height: 36px; align-items: center; overflow: hidden; border: 1px solid var(--border); border-radius: 7px; background: var(--surface); }
.threshold-input-wrap:focus-within { border-color: var(--primary-500); box-shadow: 0 0 0 2px color-mix(in srgb, var(--primary-500) 16%, transparent); }
.threshold-input-wrap input { width: 92px; height: 100%; padding: 0 10px; border: 0; outline: 0; background: transparent; color: var(--text); font-size: 13px; }
.threshold-input-wrap small { color: var(--text-muted); font-size: 11px; }
.no-threshold { color: var(--text-muted); }
.notification-settings-footer { display: flex; min-height: 70px; align-items: center; justify-content: flex-end; gap: 14px; padding: 14px 20px; border-top: 1px solid var(--border); background: var(--surface-subtle); }
.saved-status { display: inline-flex; align-items: center; gap: 5px; color: var(--success); font-size: 12px; }
@media (max-width: 700px) {
  .notification-table-scroll { overflow: visible; }
  .notification-settings-table { min-width: 0; }
  .notification-settings-table thead { display: none; }
  .notification-settings-table tbody, .notification-settings-table tr, .notification-settings-table td { display: block; width: 100%; }
  .notification-settings-table tr { padding: 18px 16px; border-bottom: 1px solid var(--border); }
  .notification-settings-table tr:last-child { border-bottom: 0; }
  .notification-settings-table td { display: grid; min-height: 0; grid-template-columns: 96px minmax(0, 1fr); align-items: center; padding: 8px 0; border: 0; text-align: left !important; }
  .notification-settings-table td::before { color: var(--text-muted); content: attr(data-label); font-size: 11px; font-weight: 600; }
  .notification-settings-table .notification-type-cell { display: flex; padding: 0 0 12px; }
  .notification-settings-table .notification-type-cell::before { display: none; }
  .notification-channels label { min-height: 44px; }
  .notification-channels input { width: 18px; height: 18px; }
  .switch-control { width: 44px; height: 44px; align-items: center; }
  .switch-control span { width: 44px; height: 26px; border-radius: 13px; }
  .switch-control span::after { width: 20px; height: 20px; }
  .switch-control input:checked + span::after { transform: translateX(18px); }
  .threshold-input-wrap { height: 44px; }
  .notification-settings-footer { min-height: 76px; padding: 16px; }
  .notification-settings-footer .button { flex: 1; justify-content: center; }
}
</style>
