<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Bell, CheckCheck, CircleAlert, CreditCard, Megaphone, ShieldCheck, Sparkles } from '@lucide/vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  getCustomerNotifications,
  markAllCustomerNotificationsRead,
  markCustomerNotificationRead,
  type CustomerNotification,
  type CustomerNotificationList
} from '@/api/customer'

const { t, locale } = useI18n()
const router = useRouter()
const root = ref<HTMLElement | null>(null)
const open = ref(false)
const loading = ref(false)
const markingAll = ref(false)
const error = ref('')
const data = ref<CustomerNotificationList>({ items: [], total: 0, unread: 0, limit: 20, offset: 0 })
let poller: number | undefined

const badge = computed(() => data.value.unread > 99 ? '99+' : String(data.value.unread))

function notificationIcon(type: string) {
  if (type === 'balance_low' || type === 'abuse_5xx') return CircleAlert
  if (type === 'payment') return CreditCard
  if (type === 'announcement') return Megaphone
  if (type === 'account_security') return ShieldCheck
  return Sparkles
}

function formatTime(value: string): string {
  return new Intl.DateTimeFormat(locale.value, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(value))
}

async function load(silent = false) {
  if (!silent) loading.value = true
  error.value = ''
  try {
    data.value = await getCustomerNotifications()
  } catch (err) {
    if (!silent) error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    if (!silent) loading.value = false
  }
}

async function toggle() {
  open.value = !open.value
  if (open.value) await load()
}

async function openNotification(notification: CustomerNotification) {
  if (!notification.is_read) {
    await markCustomerNotificationRead(notification.id)
    notification.is_read = true
    data.value.unread = Math.max(0, data.value.unread - 1)
  }
  if (notification.link?.startsWith('/')) {
    open.value = false
    await router.push(notification.link)
  }
}

async function markAll() {
  if (!data.value.unread) return
  markingAll.value = true
  error.value = ''
  try {
    await markAllCustomerNotificationsRead()
    data.value.items.forEach((item) => { item.is_read = true })
    data.value.unread = 0
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    markingAll.value = false
  }
}

function closeOutside(event: MouseEvent) {
  if (root.value && !root.value.contains(event.target as Node)) open.value = false
}

onMounted(() => {
  document.addEventListener('click', closeOutside)
  void load(true)
  poller = window.setInterval(() => void load(true), 30000)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', closeOutside)
  if (poller) window.clearInterval(poller)
})
</script>

<template>
  <div ref="root" class="customer-notification-bell">
    <button class="notification-bell-trigger" type="button" :aria-label="t('customer.notifications')" :aria-expanded="open" @click.stop="toggle">
      <Bell :size="18" />
      <span v-if="data.unread" class="notification-count">{{ badge }}</span>
    </button>

    <section v-if="open" class="notification-dropdown" :aria-label="t('customer.notifications')">
      <header>
        <strong>{{ t('customer.notifications') }}</strong>
        <button type="button" :disabled="markingAll || !data.unread" @click="markAll">
          <CheckCheck :size="15" />{{ t('customer.markAllRead') }}
        </button>
      </header>
      <div class="notification-list">
        <div v-if="loading" class="notification-empty">{{ t('common.loading') }}</div>
        <div v-else-if="error" class="notification-empty error">{{ error }}</div>
        <template v-else>
          <button
            v-for="notification in data.items"
            :key="notification.id"
            class="notification-item"
            :class="{ unread: !notification.is_read }"
            type="button"
            @click="openNotification(notification)"
          >
            <span class="notification-item-icon"><component :is="notificationIcon(notification.type)" :size="17" /></span>
            <span class="notification-item-copy">
              <strong>{{ notification.title }}</strong>
              <span v-if="notification.content">{{ notification.content }}</span>
              <small>{{ formatTime(notification.created_at) }}</small>
            </span>
            <i v-if="!notification.is_read" aria-hidden="true"></i>
          </button>
        </template>
        <div v-if="!loading && !error && !data.items.length" class="notification-empty">{{ t('customer.noNotifications') }}</div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.customer-notification-bell { position: relative; }
.notification-bell-trigger { position: relative; display: grid; width: 38px; height: 38px; padding: 0; place-items: center; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); color: var(--text-secondary); cursor: pointer; }
.notification-bell-trigger:hover { border-color: var(--border-strong); background: var(--surface-hover); color: var(--text); }
.notification-count { position: absolute; top: -5px; right: -5px; display: grid; min-width: 17px; height: 17px; padding: 0 4px; place-items: center; border: 2px solid var(--surface); border-radius: 10px; background: var(--danger); color: #fff; font-size: 9px; font-weight: 750; line-height: 1; }
.notification-dropdown { position: absolute; z-index: 60; top: calc(100% + 9px); right: 0; width: min(360px, calc(100vw - 28px)); overflow: hidden; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); box-shadow: 0 18px 42px rgba(15, 23, 42, .16); }
.notification-dropdown header { display: flex; min-height: 52px; align-items: center; justify-content: space-between; gap: 14px; padding: 0 16px; border-bottom: 1px solid var(--border); }
.notification-dropdown header strong { color: var(--text); font-size: 14px; }
.notification-dropdown header button { display: inline-flex; min-height: 34px; align-items: center; gap: 5px; padding: 0; border: 0; background: none; color: var(--primary-600); cursor: pointer; font-size: 12px; }
.notification-dropdown header button:disabled { cursor: default; opacity: .45; }
.notification-list { max-height: min(480px, calc(100vh - 130px)); overflow-y: auto; }
.notification-item { display: grid; width: 100%; min-height: 82px; grid-template-columns: 32px minmax(0, 1fr) 8px; gap: 10px; align-items: start; padding: 13px 16px; border: 0; border-bottom: 1px solid var(--border); background: transparent; color: inherit; text-align: left; cursor: pointer; }
.notification-item:last-child { border-bottom: 0; }
.notification-item:hover { background: var(--surface-hover); }
.notification-item.unread { background: color-mix(in srgb, var(--primary-500) 5%, var(--surface)); }
.notification-item-icon { display: grid; width: 32px; height: 32px; place-items: center; border-radius: 8px; background: var(--surface-subtle); color: var(--text-secondary); }
.notification-item.unread .notification-item-icon { background: var(--info-bg); color: var(--primary-600); }
.notification-item-copy { display: grid; min-width: 0; gap: 3px; }
.notification-item-copy strong { overflow: hidden; color: var(--text); font-size: 13px; font-weight: 550; text-overflow: ellipsis; white-space: nowrap; }
.notification-item.unread .notification-item-copy strong { font-weight: 700; }
.notification-item-copy > span { display: -webkit-box; overflow: hidden; color: var(--text-muted); font-size: 12px; line-height: 1.45; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
.notification-item-copy small { color: var(--text-muted); font-size: 11px; }
.notification-item > i { width: 6px; height: 6px; margin-top: 5px; border-radius: 50%; background: var(--primary-500); }
.notification-empty { padding: 32px 18px; color: var(--text-muted); font-size: 13px; text-align: center; }
.notification-empty.error { color: var(--danger); }
@media (max-width: 640px) {
  .notification-bell-trigger { width: 44px; height: 44px; }
  .notification-dropdown { position: fixed; top: 66px; right: 14px; left: 14px; width: auto; }
  .notification-dropdown header button { min-height: 44px; }
  .notification-item { min-height: 88px; }
}
</style>
