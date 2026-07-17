<script setup lang="ts">
import OperatorCrudPage, { type CrudColumn, type CrudField } from '@/components/operator/OperatorCrudPage.vue'
import { clearOperatorRiskBlock, getOperatorRiskBlocks } from '@/api/operator'
import type { GatewayRiskBlock } from '@/types'
import { RefreshCw, ShieldOff } from '@lucide/vue'
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const blocks = ref<GatewayRiskBlock[]>([])
const loadingBlocks = ref(false)
const blockError = ref('')
async function loadBlocks() {
	loadingBlocks.value = true
	blockError.value = ''
	try { blocks.value = await getOperatorRiskBlocks() }
	catch (error) { blockError.value = error instanceof Error ? error.message : t('common.failed') }
	finally { loadingBlocks.value = false }
}
async function clearBlock(block: GatewayRiskBlock) {
	if (!window.confirm(`确认解除 API Key ${block.api_key_id} 的临时封禁？`)) return
	try { await clearOperatorRiskBlock(block.api_key_id); await loadBlocks() }
	catch (error) { blockError.value = error instanceof Error ? error.message : t('common.failed') }
}
onMounted(loadBlocks)
const fields: CrudField[] = [
	{ key: 'name', label: t('operatorDomain.name'), required: true },
	{ key: 'rule_type', label: t('operatorDomain.ruleType'), type: 'select', required: true, default: 'rpm', options: [
		{ value: 'rpm', label: 'RPM' }, { value: 'tokens', label: 'Token' },
		{ value: 'spend', label: '成本（微美元）' }, { value: 'error_rate', label: '错误率（%）' }
	] },
	{ key: 'threshold', label: t('operatorDomain.threshold'), type: 'number', min: 0 },
	{ key: 'window_minutes', label: t('operatorDomain.windowMinutes'), type: 'number', min: 1, default: 60 },
	{ key: 'action', label: t('operatorDomain.action'), type: 'select', required: true, default: 'review', options: [
		{ value: 'review', label: '人工审查' }, { value: 'block', label: '临时封禁' }
	] },
	{ key: 'description', label: t('operatorDomain.description'), type: 'textarea' },
	{ key: 'status', label: t('providers.status'), type: 'select', options: [
		{ value: 'active', label: 'active' }, { value: 'disabled', label: 'disabled' }
	], default: 'active' }
]
const columns: CrudColumn[] = [
	{ key: 'name', label: t('operatorDomain.name') },
	{ key: 'rule_type', label: t('operatorDomain.ruleType') },
	{ key: 'threshold', label: t('operatorDomain.threshold') },
	{ key: 'window_minutes', label: t('operatorDomain.windowMinutes') },
	{ key: 'action', label: t('operatorDomain.action') },
	{ key: 'status', label: t('providers.status'), format: 'status' }
]
</script>

<template>
	<div>
		<OperatorCrudPage resource="risk-rules" :title="t('operatorDomain.risk')" :subtitle="t('operatorDomain.riskHelp')" :create-label="t('operatorDomain.newRisk')" :fields="fields" :columns="columns" />
		<section class="content crud-page risk-blocks-section">
			<div class="page-header"><div><h2>活动临时封禁</h2><p>查看由 block 规则触发且尚未到期的 API Key 封禁。</p></div><button class="button secondary" type="button" :disabled="loadingBlocks" @click="loadBlocks"><RefreshCw :size="17"/>{{ t('common.refresh') }}</button></div>
			<div v-if="blockError" class="notice">{{ blockError }}</div>
			<section class="panel table-panel"><div class="panel-body table-scroll"><table class="data-table crud-table"><thead><tr><th>API Key</th><th>规则</th><th>原因</th><th>创建时间</th><th>到期时间</th><th>{{ t('common.actions') }}</th></tr></thead><tbody><tr v-for="block in blocks" :key="block.api_key_id"><td><strong>{{ block.api_key_id }}</strong></td><td>{{ block.rule_id || '-' }}</td><td>{{ block.reason }}</td><td>{{ new Date(block.created_at).toLocaleString() }}</td><td>{{ new Date(block.expires_at).toLocaleString() }}</td><td><button class="button secondary" type="button" title="解除临时封禁" @click="clearBlock(block)"><ShieldOff :size="16"/>解除</button></td></tr><tr v-if="!blocks.length"><td colspan="6" class="empty-cell">暂无活动封禁</td></tr></tbody></table></div></section>
		</section>
	</div>
</template>
