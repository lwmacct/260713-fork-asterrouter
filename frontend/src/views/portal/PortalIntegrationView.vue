<script setup lang="ts">
import AccountAccessConfig from '@/components/account/AccountAccessConfig.vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>

<template>
  <main class="content portal-integration-page">
    <section class="page-header">
      <div>
        <h1>{{ t('accountAccess.exportTitle') }}</h1>
        <p>{{ t('accountAccess.exportSubtitle') }}</p>
      </div>
    </section>
    <AccountAccessConfig />
  </main>
</template>

<style scoped>
.portal-integration-page { gap: 16px; }
</style>
