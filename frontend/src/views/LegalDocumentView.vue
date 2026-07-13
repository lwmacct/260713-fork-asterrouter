<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ArrowLeft } from '@lucide/vue'
import { useRoute } from 'vue-router'
import { getLegalDocument } from '@/api/settings'
import type { LegalDocument } from '@/types'

const route = useRoute()
const document = ref<LegalDocument | null>(null)
const error = ref('')

onMounted(async () => {
  try { document.value = await getLegalDocument(String(route.params.slug || '')) }
  catch (err) { error.value = err instanceof Error ? err.message : 'Document unavailable' }
})
</script>

<template>
  <main class="legal-page"><article class="legal-document"><a class="legal-back" href="/login"><ArrowLeft :size="16"/>返回登录</a><div v-if="error" class="notice">{{ error }}</div><template v-else-if="document"><h1>{{ document.name }}</h1><div class="legal-content">{{ document.content }}</div></template></article></main>
</template>
