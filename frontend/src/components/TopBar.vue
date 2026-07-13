<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ChevronDown, Globe2, KeyRound, Laptop, LogOut, Menu, PanelsTopLeft, RadioTower, UserCog, UserRound } from '@lucide/vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { availableLocales, getLocale, setLocale, type LocaleCode } from '@/i18n'

withDefaults(defineProps<{ showMenu?: boolean }>(), {
  showMenu: false
})

const emit = defineEmits<{ toggleMenu: [] }>()
const { t } = useI18n()
const app = useAppStore()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const accountOpen = ref(false)
const accountRef = ref<HTMLElement | null>(null)

const pageTitle = computed(() => {
  const key = route.meta.titleKey
  return typeof key === 'string' ? t(key) : app.siteName
})

const pageDescription = computed(() => {
  const key = route.meta.descriptionKey
  return typeof key === 'string' ? t(key) : app.siteSubtitle
})

const userInitials = computed(() => (auth.user?.display_name || auth.user?.email || auth.user?.username || 'AR').slice(0, 2).toUpperCase())
const enabledProfiles = computed(() => app.publicSettings?.enabled_profiles || [])
const demoMode = computed(() => Boolean(app.publicSettings?.demo_mode))
const isConsoleSurface = computed(() => route.path.startsWith('/console'))
const isOperatorSurface = computed(() => route.path.startsWith('/operator'))
const isCustomerSurface = computed(() => route.path.startsWith('/customer'))
const isAdminSurface = computed(() => route.path.startsWith('/admin'))
const isPortalSurface = computed(() => route.path.startsWith('/portal'))
const canOperateRelay = computed(() => ['super_admin', 'platform_admin', 'demo_admin'].includes(auth.user?.role || ''))

function changeLocale(event: Event) {
  setLocale((event.target as HTMLSelectElement).value as LocaleCode)
}

async function openSurface(path: string) {
  accountOpen.value = false
  await router.push(path)
}

async function openAccount() {
	const surface = route.path.split('/')[1]
	accountOpen.value = false
	await router.push(`/${['console', 'operator', 'admin', 'portal', 'customer'].includes(surface) ? surface : 'admin'}/account`)
}

async function logout() {
  accountOpen.value = false
  auth.logout()
  await router.push('/login')
}

function closeOnOutsideClick(event: MouseEvent) {
  if (accountRef.value && !accountRef.value.contains(event.target as Node)) {
    accountOpen.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', closeOnOutsideClick)
	if (auth.isAuthenticated) {
    auth.loadCurrentUser()
  }
})

onBeforeUnmount(() => document.removeEventListener('click', closeOnOutsideClick))
</script>

<template>
  <header class="app-header glass topbar">
    <div class="app-header-inner">
      <div class="topbar-context">
      <button
        v-if="showMenu"
        class="icon-button mobile-menu-button"
        type="button"
        :aria-label="t('nav.openMenu')"
        :title="t('nav.openMenu')"
        @click="emit('toggleMenu')"
      >
        <Menu :size="20" />
      </button>

      <div>
        <p class="topbar-title">{{ pageTitle }}</p>
        <p class="topbar-description">{{ pageDescription }}</p>
      </div>
      </div>

      <div class="topbar-actions">
      <span v-if="demoMode" class="pill status-warning">{{ t('nav.demoMode') }}</span>
      <label class="locale-control">
        <Globe2 :size="17" aria-hidden="true" />
        <select :value="getLocale()" :aria-label="t('nav.language')" @change="changeLocale">
          <option v-for="locale in availableLocales" :key="locale.code" :value="locale.code">
            {{ locale.label }}
          </option>
        </select>
      </label>

      <div v-if="auth.user" ref="accountRef" class="account-menu">
        <button
          class="account-trigger"
          type="button"
          :aria-expanded="accountOpen"
          :aria-label="t('nav.accountMenu')"
          @click="accountOpen = !accountOpen"
        >
		  <span class="account-avatar">
			<img v-if="auth.user.avatar_data_url" :src="auth.user.avatar_data_url" alt="" />
			<template v-else>{{ userInitials }}</template>
		  </span>
          <span class="account-copy">
			<strong>{{ auth.user.display_name || auth.user.username }}</strong>
            <small>{{ auth.user.role }}</small>
          </span>
          <ChevronDown :size="15" />
        </button>

        <div v-if="accountOpen" class="account-dropdown">
          <div class="account-dropdown-header">
			<strong>{{ auth.user.display_name || auth.user.username }}</strong>
            <span>{{ auth.user.role }}</span>
          </div>
		  <button type="button" @click="openAccount">
			<UserCog :size="16" />
			{{ t('account.title') }}
		  </button>
          <button v-if="enabledProfiles.includes('personal') && !isConsoleSurface" type="button" @click="openSurface('/console/overview')">
            <Laptop :size="16" />
            {{ t('nav.console') }}
          </button>
          <button v-if="enabledProfiles.includes('relay_operator') && canOperateRelay && !isOperatorSurface" type="button" @click="openSurface('/operator/overview')">
            <RadioTower :size="16" />
            {{ t('nav.operator') }}
          </button>
          <button v-if="enabledProfiles.includes('relay_operator') && !isCustomerSurface" type="button" @click="openSurface('/customer/overview')">
            <UserRound :size="16" />
            {{ t('nav.customer') }}
          </button>
          <button v-if="enabledProfiles.includes('enterprise') && !isAdminSurface" type="button" @click="openSurface('/admin/dashboard')">
            <PanelsTopLeft :size="16" />
            {{ t('nav.admin') }}
          </button>
          <button v-if="enabledProfiles.includes('enterprise') && !isPortalSurface" type="button" @click="openSurface('/portal/overview')">
            <KeyRound :size="16" />
            {{ t('nav.portal') }}
          </button>
          <button class="danger-item" type="button" @click="logout">
            <LogOut :size="16" />
            {{ t('nav.logout') }}
          </button>
        </div>
      </div>

      <span v-else class="guest-avatar" aria-hidden="true">
        <UserRound :size="18" />
      </span>
      </div>
    </div>
  </header>
</template>
