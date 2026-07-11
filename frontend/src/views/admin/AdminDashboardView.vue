<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Activity, Fingerprint, KeyRound, PlugZap, Server } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { getDashboard } from '@/api/control'
import type { Dashboard } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const dashboard = ref<Dashboard | null>(null)

const metrics = computed(() => {
  const data = dashboard.value
  return [
    { label: t('dashboard.providers'), value: data?.provider_count ?? 0, sub: `${data?.active_provider_count ?? 0} ${t('dashboard.active')}`, icon: Server },
    { label: t('dashboard.apiKeys'), value: data?.api_key_count ?? 0, sub: `${data?.active_api_key_count ?? 0} ${t('dashboard.active')}`, icon: KeyRound },
    { label: t('dashboard.models'), value: data?.models.length ?? 0, sub: t('dashboard.gatewayCatalog'), icon: PlugZap },
    { label: t('dashboard.recentAudit'), value: data?.recent_audit.length ?? 0, sub: t('audit.events'), icon: Fingerprint }
  ]
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    dashboard.value = await getDashboard()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <main class="content">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.overview') }}</h1>
        <p>{{ t('dashboard.subtitle') }}</p>
      </div>
      <button class="button secondary" :disabled="loading" @click="load">
        <Activity :size="17" />
        {{ t('common.refresh') }}
      </button>
    </section>

    <div v-if="error" class="notice">{{ error }}</div>

    <section class="metric-grid">
      <article v-for="item in metrics" :key="item.label" class="metric-card">
        <span class="metric-icon"><component :is="item.icon" :size="20" /></span>
        <div>
          <span>{{ item.label }}</span>
          <strong>{{ item.value }}</strong>
          <small>{{ item.sub }}</small>
        </div>
      </article>
    </section>

    <section class="grid section-gap">
      <div class="panel">
        <div class="panel-header">
          <PlugZap :size="18" />
          <h2>{{ t('dashboard.models') }}</h2>
        </div>
        <div class="panel-body">
          <div class="status-line">
            <span v-for="model in dashboard?.models || []" :key="model" class="pill">{{ model }}</span>
            <span v-if="!dashboard?.models.length" class="hint">{{ t('dashboard.noModels') }}</span>
          </div>
        </div>
      </div>

      <div class="panel">
        <div class="panel-header">
          <Fingerprint :size="18" />
          <h2>{{ t('dashboard.recentAudit') }}</h2>
        </div>
        <div class="panel-body">
          <div v-for="event in dashboard?.recent_audit || []" :key="event.id" class="audit-row">
            <strong>{{ event.action }} / {{ event.resource_type }}</strong>
            <span>{{ event.summary }}</span>
          </div>
          <span v-if="!dashboard?.recent_audit.length" class="hint">{{ t('dashboard.noAudit') }}</span>
        </div>
      </div>
    </section>
  </main>
</template>
