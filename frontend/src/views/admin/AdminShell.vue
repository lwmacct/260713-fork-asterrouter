<script setup lang="ts">
import {
  Activity,
  Archive,
  BadgeDollarSign,
  Boxes,
  BellRing,
  Building2,
  FileClock,
  FileOutput,
  Gauge,
  FlaskConical,
  KeyRound,
  ListChecks,
  PieChart,
  Plug,
  RadioTower,
  Route,
  Settings,
  Server,
  ShieldCheck,
  UsersRound,
  WalletCards
} from '@lucide/vue'
import SurfaceShell from '@/components/SurfaceShell.vue'

const navGroups = [
  {
    label: 'nav.overview',
    items: [{ to: '/admin/dashboard', label: 'admin.overview', icon: Gauge }]
  },
  {
    label: 'nav.inference',
    items: [
      { to: '/admin/providers', label: 'admin.providers', icon: Server },
      { to: '/admin/gateway-models', label: 'admin.gatewayModels', icon: Boxes },
      { to: '/admin/model-routes', label: 'admin.modelRoutes', icon: Route },
      { to: '/admin/gateway-simulator', label: 'admin.gatewaySimulator', icon: FlaskConical },
      { to: '/admin/routing-groups', label: 'admin.routingGroups', icon: Route },
      { to: '/admin/provider-accounts', label: 'admin.providerAccounts', icon: RadioTower },
      { to: '/admin/pricing', label: 'pricingRules.adminTitle', icon: BadgeDollarSign },
      { to: '/admin/effective-pricing', label: 'admin.effectivePricing', icon: BadgeDollarSign }
    ]
  },
  {
    label: 'nav.organization',
    items: [
      { to: '/admin/users', label: 'admin.users', icon: UsersRound },
      { to: '/admin/departments', label: 'admin.departments', icon: Building2 },
			{ to: '/admin/organization-groups', label: 'organizationGroups.title', icon: UsersRound },
      { to: '/admin/policies', label: 'admin.policies', icon: ShieldCheck },
      { to: '/admin/api-keys', label: 'admin.apiKeys', icon: KeyRound }
    ]
  },
  {
    label: 'nav.analytics',
    items: [
      { to: '/admin/usage', label: 'admin.usage', icon: WalletCards },
      { to: '/admin/cost-allocation', label: 'admin.costAllocation', icon: PieChart },
      { to: '/admin/traces', label: 'admin.traces', icon: Activity },
      { to: '/admin/alerts', label: 'admin.alerts', icon: BellRing },
      { to: '/admin/artifacts', label: 'admin.artifacts', icon: Archive },
      { to: '/admin/ai-jobs', label: 'admin.aiJobs', icon: ListChecks }
    ]
  },
  {
    label: 'nav.auditData',
    items: [
      { to: '/admin/audit', label: 'admin.audit', icon: FileClock },
      { to: '/admin/exports', label: 'admin.exports', icon: FileOutput }
    ]
  },
  {
    label: 'nav.platform',
    items: [
      { to: '/admin/plugins', label: 'admin.plugins', icon: Plug },
      { to: '/admin/settings', label: 'admin.settings', icon: Settings }
    ]
  }
]
</script>

<template>
  <SurfaceShell home-to="/admin/dashboard" nav-label="nav.admin" surface="enterprise" :nav-groups="navGroups" />
</template>
