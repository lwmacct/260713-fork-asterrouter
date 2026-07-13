<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

declare global {
  interface Window {
    turnstile?: { render: (element: HTMLElement, options: Record<string, unknown>) => string; remove: (id: string) => void; reset: (id: string) => void }
  }
}

const props = defineProps<{ siteKey: string; resetKey?: number }>()
const emit = defineEmits<{ token: [value: string] }>()
const container = ref<HTMLElement | null>(null)
let widgetID = ''

function render() {
  if (!container.value || !window.turnstile || widgetID) return
  widgetID = window.turnstile.render(container.value, {
    sitekey: props.siteKey,
    callback: (token: string) => emit('token', token),
    'expired-callback': () => emit('token', ''),
    'error-callback': () => { emit('token', ''); return true },
    theme: document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light'
  })
}

onMounted(() => {
  const existing = document.querySelector<HTMLScriptElement>('script[data-asterrouter-turnstile]')
  if (window.turnstile) { render(); return }
  if (existing) { existing.addEventListener('load', render, { once: true }); return }
  const script = document.createElement('script'); script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'; script.async = true; script.defer = true; script.dataset.asterrouterTurnstile = 'true'; script.addEventListener('load', render, { once: true }); document.head.appendChild(script)
})

watch(() => props.resetKey, () => { if (widgetID && window.turnstile) { window.turnstile.reset(widgetID); emit('token', '') } })
onBeforeUnmount(() => { if (widgetID && window.turnstile) window.turnstile.remove(widgetID) })
</script>

<template><div ref="container" class="turnstile-widget" aria-label="Cloudflare Turnstile"></div></template>
