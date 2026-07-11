<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ChevronDown, Globe2, Laptop, LogOut, Menu, PanelsTopLeft, RadioTower, UserRound } from '@lucide/vue'
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

const userInitials = computed(() => auth.user?.username.slice(0, 2).toUpperCase() || 'AR')
const enabledProfiles = computed(() => app.publicSettings?.enabled_profiles || [])

function changeLocale(event: Event) {
  setLocale((event.target as HTMLSelectElement).value as LocaleCode)
}

async function openSurface(path: string) {
  accountOpen.value = false
  await router.push(path)
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
  if (auth.isAuthenticated && !auth.user) {
    auth.loadCurrentUser()
  }
})

onBeforeUnmount(() => document.removeEventListener('click', closeOnOutsideClick))
</script>

<template>
  <header class="topbar">
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
          <span class="account-avatar">{{ userInitials }}</span>
          <span class="account-copy">
            <strong>{{ auth.user.username }}</strong>
            <small>{{ auth.user.role }}</small>
          </span>
          <ChevronDown :size="15" />
        </button>

        <div v-if="accountOpen" class="account-dropdown">
          <div class="account-dropdown-header">
            <strong>{{ auth.user.username }}</strong>
            <span>{{ auth.user.role }}</span>
          </div>
          <button v-if="enabledProfiles.includes('personal')" type="button" @click="openSurface('/console')">
            <Laptop :size="16" />
            {{ t('nav.console') }}
          </button>
          <button v-if="enabledProfiles.includes('relay_operator')" type="button" @click="openSurface('/operator')">
            <RadioTower :size="16" />
            {{ t('nav.operator') }}
          </button>
          <button v-if="enabledProfiles.includes('enterprise')" type="button" @click="openSurface('/portal')">
            <PanelsTopLeft :size="16" />
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
  </header>
</template>
