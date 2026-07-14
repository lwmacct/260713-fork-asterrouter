<script setup lang="ts">
import { Activity, BadgeDollarSign, BellRing, Boxes, FileClock, Gauge, KeyRound, Link2, Plug, Route, Send, Server, Settings, ShieldCheck, UsersRound } from '@lucide/vue'
import SurfaceShell from '@/components/SurfaceShell.vue'

const navGroups = [
  {
    label: 'nav.overview',
    items: [{ to: '/platform/overview', label: 'platform.overview', icon: Gauge }]
  },
  {
    label: 'nav.inference',
    items: [
      { to: '/platform/providers', label: 'admin.providers', icon: Server },
      { to: '/platform/gateway-models', label: 'admin.gatewayModels', icon: Boxes },
      { to: '/platform/model-routes', label: 'admin.modelRoutes', icon: Route },
      { to: '/platform/routing-groups', label: 'admin.routingGroups', icon: Route },
      { to: '/platform/provider-accounts', label: 'admin.providerAccounts', icon: Server },
      { to: '/platform/model-pricings', label: 'admin.modelPricings', icon: BadgeDollarSign }
    ]
  },
  {
    label: 'nav.governance',
    items: [
      { to: '/platform/tenants', label: 'platform.tenants', icon: UsersRound },
      { to: '/platform/policies', label: 'admin.policies', icon: ShieldCheck },
      { to: '/platform/api-keys', label: 'admin.apiKeys', icon: KeyRound }
    ]
  },
  {
    label: 'platform.integrationNav',
    items: [
      { to: '/platform/integrations', label: 'platform.integrations', icon: Link2 },
      { to: '/platform/usage-sinks', label: 'platform.usageSinks', icon: Send }
    ]
  },
  {
    label: 'nav.observability',
    items: [
      { to: '/platform/usage', label: 'admin.usage', icon: Activity },
      { to: '/platform/traces', label: 'admin.traces', icon: Activity },
      { to: '/platform/alerts', label: 'admin.alerts', icon: BellRing },
      { to: '/platform/audit', label: 'admin.audit', icon: FileClock }
    ]
  },
  {
    label: 'nav.extensions',
    items: [
      { to: '/platform/plugins', label: 'admin.plugins', icon: Plug },
      { to: '/platform/settings', label: 'admin.settings', icon: Settings }
    ]
  }
]
</script>

<template>
  <SurfaceShell
    home-to="/platform/overview"
    nav-label="nav.platformConsole"
    surface="platform"
    storage-key="asterrouter_platform_sidebar_collapsed"
    :nav-groups="navGroups"
  />
</template>
