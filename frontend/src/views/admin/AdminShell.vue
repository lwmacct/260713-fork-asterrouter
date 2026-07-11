<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import {
  Activity,
  BellRing,
  Building2,
  ChevronLeft,
  ChevronRight,
  BadgeDollarSign,
  FileClock,
  FileOutput,
  Gauge,
  KeyRound,
  Laptop,
  Moon,
  PieChart,
  Plug,
  RadioTower,
  Settings,
  Server,
  ShieldCheck,
  Sun,
  UsersRound,
  WalletCards,
  X
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import TopBar from '@/components/TopBar.vue'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const app = useAppStore()
const route = useRoute()
const collapsed = ref(localStorage.getItem('asterrouter_sidebar_collapsed') === 'true')
const mobileOpen = ref(false)
const darkMode = ref(document.documentElement.dataset.theme === 'dark')

const version = computed(() => app.publicSettings?.version || 'Dev')
const enabledProfiles = computed(() => app.publicSettings?.enabled_profiles || [])

const navGroups = [
  {
    label: 'nav.governance',
    items: [
      { to: '/admin/dashboard', label: 'admin.overview', icon: Gauge },
      { to: '/admin/providers', label: 'admin.providers', icon: Server },
      { to: '/admin/model-pricings', label: 'admin.modelPricings', icon: BadgeDollarSign },
      { to: '/admin/users', label: 'admin.users', icon: UsersRound },
      { to: '/admin/departments', label: 'admin.departments', icon: Building2 },
      { to: '/admin/policies', label: 'admin.policies', icon: ShieldCheck },
      { to: '/admin/api-keys', label: 'admin.apiKeys', icon: KeyRound },
      { to: '/admin/usage', label: 'admin.usage', icon: WalletCards },
      { to: '/admin/cost-allocation', label: 'admin.costAllocation', icon: PieChart },
      { to: '/admin/traces', label: 'admin.traces', icon: Activity },
      { to: '/admin/alerts', label: 'admin.alerts', icon: BellRing },
      { to: '/admin/audit', label: 'admin.audit', icon: FileClock },
      { to: '/admin/exports', label: 'admin.exports', icon: FileOutput }
    ]
  },
  {
    label: 'nav.platform',
    items: [
      { to: '/admin/plugins', label: 'admin.plugins', icon: Plug },
      { to: '/admin/settings', label: 'admin.settings', icon: Settings }
    ]
  }
]

function toggleCollapsed() {
  collapsed.value = !collapsed.value
  localStorage.setItem('asterrouter_sidebar_collapsed', String(collapsed.value))
}

function toggleTheme() {
  darkMode.value = !darkMode.value
  document.documentElement.dataset.theme = darkMode.value ? 'dark' : 'light'
  localStorage.setItem('asterrouter_theme', darkMode.value ? 'dark' : 'light')
}

watch(
  () => route.fullPath,
  () => {
    mobileOpen.value = false
  }
)
</script>

<template>
  <div class="admin-layout" :class="{ 'sidebar-is-collapsed': collapsed }">
    <aside class="admin-sidebar" :class="{ collapsed, 'mobile-open': mobileOpen }">
      <div class="sidebar-brand-row">
        <RouterLink class="sidebar-brand-link" to="/admin/dashboard">
          <span class="brand-mark">AR</span>
          <span class="sidebar-brand-copy">
            <strong>{{ app.siteName }}</strong>
            <small>v{{ version }}</small>
          </span>
        </RouterLink>
        <button
          class="icon-button sidebar-mobile-close"
          type="button"
          :aria-label="t('nav.closeMenu')"
          @click="mobileOpen = false"
        >
          <X :size="19" />
        </button>
      </div>

      <nav class="sidebar-nav" :aria-label="t('nav.admin')">
        <section v-for="group in navGroups" :key="group.label" class="sidebar-section">
          <p class="sidebar-section-title">{{ t(group.label) }}</p>
          <RouterLink
            v-for="item in group.items"
            :key="item.to"
            class="nav-item"
            :to="item.to"
            :title="collapsed ? t(item.label) : undefined"
          >
            <component :is="item.icon" :size="19" />
            <span>{{ t(item.label) }}</span>
          </RouterLink>
        </section>
      </nav>

      <div class="sidebar-footer">
        <RouterLink v-if="enabledProfiles.includes('personal')" class="nav-item" to="/console" :title="collapsed ? t('nav.console') : undefined">
          <Laptop :size="19" />
          <span>{{ t('nav.console') }}</span>
        </RouterLink>
        <RouterLink v-if="enabledProfiles.includes('relay_operator')" class="nav-item" to="/operator" :title="collapsed ? t('nav.operator') : undefined">
          <RadioTower :size="19" />
          <span>{{ t('nav.operator') }}</span>
        </RouterLink>
        <RouterLink v-if="enabledProfiles.includes('enterprise')" class="nav-item" to="/portal" :title="collapsed ? t('nav.portal') : undefined">
          <KeyRound :size="19" />
          <span>{{ t('nav.portal') }}</span>
        </RouterLink>
        <button
          class="nav-item"
          type="button"
          :title="darkMode ? t('nav.lightMode') : t('nav.darkMode')"
          @click="toggleTheme"
        >
          <Sun v-if="darkMode" :size="19" />
          <Moon v-else :size="19" />
          <span>{{ darkMode ? t('nav.lightMode') : t('nav.darkMode') }}</span>
        </button>
        <button
          class="nav-item sidebar-collapse"
          type="button"
          :title="collapsed ? t('nav.expand') : t('nav.collapse')"
          @click="toggleCollapsed"
        >
          <ChevronRight v-if="collapsed" :size="19" />
          <ChevronLeft v-else :size="19" />
          <span>{{ t('nav.collapse') }}</span>
        </button>
      </div>
    </aside>

    <button
      v-if="mobileOpen"
      class="sidebar-overlay"
      type="button"
      :aria-label="t('nav.closeMenu')"
      @click="mobileOpen = false"
    ></button>

    <div class="admin-main">
      <TopBar show-menu @toggle-menu="mobileOpen = true" />
      <RouterView />
    </div>
  </div>
</template>
