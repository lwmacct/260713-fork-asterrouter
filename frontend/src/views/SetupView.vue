<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowLeft, ArrowRight, Building2, Check, Laptop, PanelsTopLeft, RadioTower, ShieldCheck } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { applySetupProfile } from '@/api/settings'
import { ApiClientError } from '@/api/client'
import { setPublicSettingsCache } from '@/router'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const router = useRouter()
const app = useAppStore()
const auth = useAuthStore()
const selectedProfile = ref('')
const currentStep = ref(0)
const saving = ref(false)
const error = ref('')

const profiles = [
  {
    id: 'enterprise',
    icon: Building2,
    title: 'setup.enterprise',
    desc: 'setup.enterpriseDesc',
    owner: 'setup.enterpriseOwner',
    route: '/admin + /portal',
    includes: ['setup.enterpriseIncludes1', 'setup.enterpriseIncludes2', 'setup.enterpriseIncludes3'],
    excludes: ['setup.enterpriseExcludes1', 'setup.enterpriseExcludes2', 'setup.enterpriseExcludes3']
  },
  {
    id: 'personal',
    icon: Laptop,
    title: 'setup.personal',
    desc: 'setup.personalDesc',
    owner: 'setup.personalOwner',
    route: '/console/overview',
    includes: ['setup.personalIncludes1', 'setup.personalIncludes2', 'setup.personalIncludes3'],
    excludes: ['setup.personalExcludes1', 'setup.personalExcludes2', 'setup.personalExcludes3']
  },
  {
    id: 'relay_operator',
    icon: RadioTower,
    title: 'setup.relay',
    desc: 'setup.relayDesc',
    owner: 'setup.relayOwner',
    route: '/operator + /customer',
    includes: ['setup.relayIncludes1', 'setup.relayIncludes2', 'setup.relayIncludes3'],
    excludes: ['setup.relayExcludes1', 'setup.relayExcludes2', 'setup.relayExcludes3']
  },
  {
    id: 'platform',
    icon: PanelsTopLeft,
    title: 'setup.platform',
    desc: 'setup.platformDesc',
    owner: 'setup.platformOwner',
    route: '/platform/overview',
    includes: ['setup.platformIncludes1', 'setup.platformIncludes2', 'setup.platformIncludes3'],
    excludes: ['setup.platformExcludes1', 'setup.platformExcludes2', 'setup.platformExcludes3']
  }
]

const steps = computed(() => [
  { id: 'profiles', title: t('setup.stepProfiles') },
  { id: 'ready', title: t('setup.stepReady') }
])
const selectedProfileItem = computed(() => profiles.find((profile) => profile.id === selectedProfile.value) || profiles[0])
const canProceed = computed(() => Boolean(selectedProfile.value))

function profileRoute(): string {
  if (selectedProfile.value === 'personal') return '/console/overview'
  if (selectedProfile.value === 'relay_operator') return '/operator/overview'
  if (selectedProfile.value === 'platform') return '/platform/overview'
  return '/admin/dashboard'
}

function nextStep() {
  if (!canProceed.value) {
    error.value = t('setup.selectOne')
    return
  }
  error.value = ''
  currentStep.value = 1
}

function previousStep() {
  error.value = ''
  currentStep.value = 0
}

async function submit() {
  if (!selectedProfile.value) {
    error.value = t('setup.selectOne')
    return
  }
  saving.value = true
  error.value = ''
  try {
    const settings = await applySetupProfile(selectedProfile.value)
    setPublicSettingsCache(settings)
    await app.loadPublicSettings()
    auth.logout()
    await router.push({ path: '/login', query: { redirect: profileRoute() } })
  } catch (err) {
    if (err instanceof ApiClientError && (err.status === 0 || err.status === 404)) {
      error.value = t('setup.serviceUnavailable')
    } else {
      error.value = err instanceof Error ? err.message : t('common.failed')
    }
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="setup-page">
    <main class="setup-shell">
      <section class="setup-brand">
        <span class="setup-brand-mark">
          <ShieldCheck :size="28" />
        </span>
        <h1>{{ t('setup.title') }}</h1>
        <p>{{ t('setup.subtitle') }}</p>
      </section>

      <nav class="setup-steps" :aria-label="t('setup.steps')">
        <template v-for="(step, index) in steps" :key="step.id">
          <div class="setup-step" :class="{ active: currentStep === index, done: currentStep > index }">
            <span class="setup-step-index">
              <Check v-if="currentStep > index" :size="14" />
              <span v-else>{{ index + 1 }}</span>
            </span>
            <span>{{ step.title }}</span>
          </div>
          <span v-if="index < steps.length - 1" class="setup-step-line" :class="{ done: currentStep > index }"></span>
        </template>
      </nav>

      <section class="setup-card">
        <div v-if="currentStep === 0" class="setup-step-panel">
          <div class="setup-section-header">
            <div>
              <h2>{{ t('setup.profileTitle') }}</h2>
              <p>{{ t('setup.profileHelp') }}</p>
            </div>
            <span class="pill">{{ t('setup.singleProfile') }}</span>
          </div>

          <section class="setup-grid">
            <button
              v-for="profile in profiles"
              :key="profile.id"
              type="button"
              class="profile-card"
              :class="{ active: selectedProfile === profile.id, primary: selectedProfile === profile.id }"
              :aria-pressed="selectedProfile === profile.id"
              @click="selectedProfile = profile.id"
            >
              <span class="profile-card-topline">
                <component :is="profile.icon" :size="30" />
                <span class="profile-check" :class="{ active: selectedProfile === profile.id }">
                  <Check v-if="selectedProfile === profile.id" :size="15" />
                </span>
              </span>
              <h2>{{ t(profile.title) }}</h2>
              <p>{{ t(profile.desc) }}</p>
              <span class="profile-owner">{{ t(profile.owner) }}</span>
              <span class="profile-route">{{ profile.route }}</span>
            </button>
          </section>
        </div>

        <div v-if="currentStep === 1" class="setup-step-panel">
          <div class="setup-section-header">
            <div>
              <h2>{{ t('setup.readyTitle') }}</h2>
              <p>{{ t('setup.readyHelp') }}</p>
            </div>
          </div>

          <div class="setup-review-grid">
            <div>
              <label>{{ t('setup.profile') }}</label>
              <div class="chip-list">
                <span class="pill">{{ t(selectedProfileItem.title) }}</span>
              </div>
            </div>
            <div>
              <label>{{ t('setup.businessOwner') }}</label>
              <span>{{ t(selectedProfileItem.owner) }}</span>
            </div>
            <div>
              <label>{{ t('setup.initialEntry') }}</label>
              <strong>{{ profileRoute() }}</strong>
              <span>{{ selectedProfileItem.route }}</span>
            </div>
            <div>
              <label>{{ t('setup.expansion') }}</label>
              <span>{{ t('setup.expansionHelp') }}</span>
            </div>
          </div>

          <div class="setup-review-boundaries">
            <section>
              <h3>{{ t('setup.includedScope') }}</h3>
              <ul>
                <li v-for="item in selectedProfileItem.includes" :key="item">{{ t(item) }}</li>
              </ul>
            </section>
            <section>
              <h3>{{ t('setup.excludedScope') }}</h3>
              <ul>
                <li v-for="item in selectedProfileItem.excludes" :key="item">{{ t(item) }}</li>
              </ul>
            </section>
          </div>
        </div>

        <div v-if="error" class="notice setup-notice">{{ error }}</div>

        <footer class="setup-actions">
          <button v-if="currentStep > 0" class="button secondary" type="button" :disabled="saving" @click="previousStep">
            <ArrowLeft :size="17" />
            {{ t('common.previous') }}
          </button>
          <span v-else></span>

          <button
            v-if="currentStep < steps.length - 1"
            class="button"
            type="button"
            :disabled="!canProceed"
            @click="nextStep"
          >
            {{ t('common.next') }}
            <ArrowRight :size="17" />
          </button>
          <button v-else class="button" type="button" :disabled="saving || !selectedProfile" @click="submit">
            <ArrowRight :size="17" />
            {{ saving ? t('common.saving') : t('setup.completeInstallation') }}
          </button>
        </footer>
      </section>
    </main>
  </div>
</template>
