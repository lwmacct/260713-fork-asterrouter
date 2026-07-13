<script setup lang="ts">
import { Activity, BadgeDollarSign, BellRing, Boxes, FlaskConical, Gauge, KeyRound, Plug, RadioTower, Route, Server, Settings, ShieldAlert, Tags, UsersRound, WalletCards } from '@lucide/vue'
import SurfaceShell from '@/components/SurfaceShell.vue'

const navGroups = [
  {
    label: 'nav.overview',
    items: [{ to: '/operator/overview', label: 'operator.overview', icon: Gauge }]
  },
  {
    label: 'operatorDomain.resourceGovernance',
    items: [
      { to: '/operator/customers', label: 'operatorDomain.customers', icon: UsersRound },
      { to: '/operator/customer-keys', label: 'operatorDomain.keyList', icon: KeyRound },
      { to: '/operator/customer-groups', label: 'operatorDomain.groups', icon: Tags },
      { to: '/operator/plans', label: 'operatorDomain.plans', icon: BadgeDollarSign },
      { to: '/operator/balances', label: 'operatorDomain.balances', icon: WalletCards },
      { to: '/operator/pricing', label: 'operatorDomain.pricing', icon: BadgeDollarSign }
    ]
  },
  {
    label: 'nav.inference',
    items: [
      { to: '/operator/providers', label: 'admin.providers', icon: Server },
      { to: '/operator/models', label: 'admin.gatewayModels', icon: Boxes },
      { to: '/operator/model-routes', label: 'admin.modelRoutes', icon: Route },
      { to: '/operator/gateway-simulator', label: 'admin.gatewaySimulator', icon: FlaskConical },
      { to: '/operator/routing-groups', label: 'operator.groupList', icon: Route },
      { to: '/operator/resources', label: 'operator.resourceList', icon: RadioTower }
    ]
  },
  {
    label: 'nav.operations',
    items: [
      { to: '/operator/usage', label: 'operator.traffic', icon: Activity },
      { to: '/operator/risk', label: 'operatorDomain.risk', icon: ShieldAlert },
      { to: '/operator/notices', label: 'operatorDomain.notices', icon: BellRing }
    ]
  },
  {
    label: 'nav.platform',
    items: [
      { to: '/operator/plugins', label: 'admin.plugins', icon: Plug },
      { to: '/operator/settings', label: 'admin.settings', icon: Settings }
    ]
  }
]
</script>

<template>
  <SurfaceShell home-to="/operator/overview" nav-label="nav.operator" surface="relay_operator" :nav-groups="navGroups" />
</template>
