<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowRight, Building2, Check, Laptop, RadioTower } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import TopBar from '@/components/TopBar.vue'
import { applySetupProfiles } from '@/api/settings'
import { setPublicSettingsCache } from '@/router'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const router = useRouter()
const app = useAppStore()
const selectedProfiles = ref<string[]>(['enterprise'])
const defaultProfile = ref('enterprise')
const saving = ref(false)
const error = ref('')

const profiles = [
  { id: 'enterprise', icon: Building2, title: 'setup.enterprise', desc: 'setup.enterpriseDesc' },
  { id: 'personal', icon: Laptop, title: 'setup.personal', desc: 'setup.personalDesc' },
  { id: 'relay_operator', icon: RadioTower, title: 'setup.relay', desc: 'setup.relayDesc' }
]

function hasProfile(profile: string): boolean {
  return selectedProfiles.value.includes(profile)
}

function toggleProfile(profile: string) {
  if (hasProfile(profile)) {
    if (selectedProfiles.value.length === 1) {
      return
    }
    selectedProfiles.value = selectedProfiles.value.filter((item) => item !== profile)
    if (defaultProfile.value === profile) {
      defaultProfile.value = selectedProfiles.value[0] || ''
    }
    return
  }
  selectedProfiles.value = [...selectedProfiles.value, profile]
  if (!defaultProfile.value) {
    defaultProfile.value = profile
  }
}

function defaultRoute(): string {
  if (defaultProfile.value === 'personal') return '/console'
  if (defaultProfile.value === 'relay_operator') return '/operator'
  return '/admin/settings'
}

async function submit() {
  if (!selectedProfiles.value.length) {
    error.value = t('setup.selectAtLeastOne')
    return
  }
  if (!hasProfile(defaultProfile.value)) {
    defaultProfile.value = selectedProfiles.value[0]
  }
  saving.value = true
  error.value = ''
  try {
    const settings = await applySetupProfiles(selectedProfiles.value, defaultProfile.value)
    setPublicSettingsCache(settings)
    await app.loadPublicSettings()
    await router.push(defaultRoute())
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="app-page">
    <TopBar />
    <main class="content">
      <section class="page-header">
        <div>
          <h1>{{ t('setup.title') }}</h1>
          <p>{{ t('setup.subtitle') }}</p>
        </div>
        <button class="button" :disabled="saving || !selectedProfiles.length" @click="submit">
          <ArrowRight :size="17" />
          {{ t('setup.continue') }}
        </button>
      </section>

      <div v-if="error" class="notice">{{ error }}</div>

      <section class="setup-grid">
        <button
          v-for="profile in profiles"
          :key="profile.id"
          type="button"
          class="profile-card"
          :class="{ active: hasProfile(profile.id), primary: defaultProfile === profile.id }"
          @click="toggleProfile(profile.id)"
        >
          <span class="profile-card-topline">
            <component :is="profile.icon" :size="30" />
            <span class="profile-check" :class="{ active: hasProfile(profile.id) }">
              <Check v-if="hasProfile(profile.id)" :size="15" />
            </span>
          </span>
          <h2>{{ t(profile.title) }}</h2>
          <p>{{ t(profile.desc) }}</p>
          <label v-if="hasProfile(profile.id)" class="profile-default" @click.stop>
            <input v-model="defaultProfile" type="radio" name="default_profile" :value="profile.id" />
            <span>{{ t('setup.defaultProfile') }}</span>
          </label>
        </button>
      </section>
    </main>
  </div>
</template>
