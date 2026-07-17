<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PricingRulesView from '@/components/pricing/PricingRulesView.vue'

const { t } = useI18n()
</script>

<template>
  <PricingRulesView surface="operator" :title="t('pricingRules.operatorTitle')" :subtitle="t('pricingRules.operatorSubtitle')" />
</template>
