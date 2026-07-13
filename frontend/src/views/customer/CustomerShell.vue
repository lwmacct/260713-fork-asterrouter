<script setup lang="ts">
import { Activity, Code2, Gauge, KeyRound, ReceiptText, UserCog } from '@lucide/vue'
import SurfaceShell from '@/components/SurfaceShell.vue'

const navGroups = [
  {
    label: 'nav.overview',
    items: [{ to: '/customer/overview', label: 'customer.overview', icon: Gauge }]
  },
  {
    label: 'nav.selfService',
    items: [
      { to: '/customer/keys', label: 'customer.keys', icon: KeyRound },
      { to: '/customer/integration', label: 'customer.integration', icon: Code2 }
    ]
  },
  {
    label: 'nav.activity',
    items: [
      { to: '/customer/usage', label: 'customer.usage', icon: Activity },
      { to: '/customer/billing', label: 'customer.billing', icon: ReceiptText }
    ]
  },
  {
    label: 'nav.workspace',
    items: [{ to: '/customer/account', label: 'account.title', icon: UserCog }]
  }
]
</script>

<template>
  <SurfaceShell
    home-to="/customer/overview"
    nav-label="nav.customer"
    surface="customer"
    storage-key="asterrouter_customer_sidebar_collapsed"
    :nav-groups="navGroups"
  />
</template>
