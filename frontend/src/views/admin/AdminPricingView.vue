<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import PricingRulesView from '@/components/pricing/PricingRulesView.vue'
import type { PricingSurface } from '@/types'

const props = withDefaults(defineProps<{ surface?: PricingSurface }>(), { surface: 'admin' })
const { t } = useI18n()
const title = computed(() => props.surface === 'platform' ? t('pricingRules.platformTitle') : t('pricingRules.adminTitle'))
const subtitle = computed(() => props.surface === 'platform' ? t('pricingRules.platformSubtitle') : t('pricingRules.adminSubtitle'))
</script>

<template>
  <PricingRulesView :surface="surface" :title="title" :subtitle="subtitle" />
</template>
