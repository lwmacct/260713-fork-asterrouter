<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
	BadgeCheck,
	Building2,
	Camera,
	Check,
	Copy,
	Code2,
	KeyRound,
	Link,
	LockKeyhole,
	Mail,
	MonitorSmartphone,
	RefreshCw,
	Save,
	ShieldCheck,
	Trash2,
	Unlink,
	Upload,
	UserRound
} from '@lucide/vue'
import QRCode from 'qrcode'
import { useI18n } from 'vue-i18n'
import {
	beginTOTPSetup,
	beginAccountIdentityBinding,
	changeAccountPassword,
	confirmTOTP,
	disableTOTP,
	generateTOTPRecoveryCodes,
	getAccountProfile,
	revokeOtherAccountSessions,
	unbindAccountIdentity,
	updateAccountProfile
} from '@/api/account'
import { useAuthStore } from '@/stores/auth'
import type { AccountLoginMethod, AccountProfile, TOTPSetup } from '@/types'

const { t, locale } = useI18n()
const auth = useAuthStore()
const profile = ref<AccountProfile | null>(null)
const displayName = ref('')
const avatarDataURL = ref('')
const savedAvatarDataURL = ref('')
const loading = ref(true)
const saving = ref(false)
const avatarSaving = ref(false)
const passwordSaving = ref(false)
const totpSaving = ref(false)
const notice = ref('')
const error = ref('')
const fileInput = ref<HTMLInputElement | null>(null)
const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const totpSetup = ref<TOTPSetup | null>(null)
const totpQRCode = ref('')
const totpCode = ref('')
const disableCode = ref('')
const recoveryCodes = ref<string[]>([])
const copied = ref(false)
const unbindingProvider = ref('')
const bindingProvider = ref('')
const sessionSaving = ref(false)
type AccountTab = 'profile' | 'login' | 'security'
const accountTabOrder: AccountTab[] = ['profile', 'login', 'security']
const activeTab = ref<AccountTab>('profile')

const initials = computed(() => (displayName.value || profile.value?.email || profile.value?.id || 'AR').slice(0, 2).toUpperCase())
const passwordValid = computed(() => newPassword.value.length >= 10 && newPassword.value === confirmPassword.value)
const avatarDirty = computed(() => avatarDataURL.value !== savedAvatarDataURL.value)
const primaryLoginMethods = computed(() => profile.value?.login_methods.filter((method) => method.id === 'email' || method.id === 'local') || [])
const externalLoginMethods = computed(() => profile.value?.login_methods.filter((method) => !['email', 'local'].includes(method.id) && (method.available || method.bound)) || [])

function clearFeedback() {
	notice.value = ''
	error.value = ''
}

function selectAccountTab(tab: AccountTab) {
	activeTab.value = tab
}

function handleAccountTabKeydown(event: KeyboardEvent, tab: AccountTab) {
	const direction = event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? -1 : 0
	let nextIndex = accountTabOrder.indexOf(tab)
	if (event.key === 'Home') nextIndex = 0
	else if (event.key === 'End') nextIndex = accountTabOrder.length - 1
	else if (direction) nextIndex = (nextIndex + direction + accountTabOrder.length) % accountTabOrder.length
	else return
	event.preventDefault()
	const nextTab = accountTabOrder[nextIndex]
	selectAccountTab(nextTab)
	window.requestAnimationFrame(() => document.getElementById(`account-tab-${nextTab}`)?.focus())
}

function readableError(err: unknown) {
	return err instanceof Error ? err.message : t('account.genericError')
}

async function load() {
	loading.value = true
	clearFeedback()
	try {
		profile.value = await getAccountProfile()
		displayName.value = profile.value.display_name
		avatarDataURL.value = profile.value.avatar_data_url || ''
		savedAvatarDataURL.value = avatarDataURL.value
		auth.applyAccountProfile(profile.value)
	} catch (err) {
		error.value = readableError(err)
	} finally {
		loading.value = false
	}
}

async function saveProfile() {
	if (!profile.value || profile.value.managed_by_config) return
	saving.value = true
	clearFeedback()
	try {
		profile.value = await updateAccountProfile(displayName.value, avatarDataURL.value)
		displayName.value = profile.value.display_name
		avatarDataURL.value = profile.value.avatar_data_url || ''
		savedAvatarDataURL.value = avatarDataURL.value
		auth.applyAccountProfile(profile.value)
		notice.value = t('account.profileSaved')
	} catch (err) {
		error.value = readableError(err)
	} finally {
		saving.value = false
	}
}

async function unbindIdentity(method: AccountLoginMethod) {
	if (!profile.value || profile.value.managed_by_config || method.id === 'local' || !method.bound) return
	if (!window.confirm(t('account.unbindConfirm', { provider: method.label }))) return
	unbindingProvider.value = method.id
	clearFeedback()
	try {
		profile.value = await unbindAccountIdentity(method.id)
		auth.applyAccountProfile(profile.value)
		notice.value = t('account.unbound', { provider: method.label })
	} catch (err) {
		error.value = readableError(err)
	} finally {
		unbindingProvider.value = ''
	}
}

async function bindIdentity(method: AccountLoginMethod) {
	if (!profile.value || profile.value.managed_by_config || method.id === 'local' || method.bound || !method.available) return
	bindingProvider.value = method.id
	clearFeedback()
	try {
		const authorizationURL = await beginAccountIdentityBinding(method.id, window.location.pathname)
		window.location.assign(authorizationURL)
	} catch (err) {
		error.value = readableError(err)
		bindingProvider.value = ''
	}
}

async function saveAvatar() {
	if (!profile.value || !avatarDirty.value) return
	avatarSaving.value = true
	clearFeedback()
	try {
		profile.value = await updateAccountProfile(displayName.value, avatarDataURL.value)
		savedAvatarDataURL.value = profile.value.avatar_data_url || ''
		avatarDataURL.value = savedAvatarDataURL.value
		auth.applyAccountProfile(profile.value)
		notice.value = t('account.avatarSaved')
	} catch (err) {
		error.value = readableError(err)
	} finally {
		avatarSaving.value = false
	}
}

async function removeAvatar() {
	if (!profile.value || (!avatarDataURL.value && !savedAvatarDataURL.value)) return
	avatarSaving.value = true
	clearFeedback()
	try {
		profile.value = await updateAccountProfile(displayName.value, '')
		avatarDataURL.value = ''
		savedAvatarDataURL.value = ''
		auth.applyAccountProfile(profile.value)
		notice.value = t('account.avatarRemoved')
	} catch (err) {
		error.value = readableError(err)
	} finally {
		avatarSaving.value = false
	}
}

async function chooseAvatar(event: Event) {
	const file = (event.target as HTMLInputElement).files?.[0]
	if (!file) return
	clearFeedback()
	try {
		avatarDataURL.value = await compressAvatar(file)
	} catch (err) {
		error.value = readableError(err)
	} finally {
		if (fileInput.value) fileInput.value.value = ''
	}
}

async function compressAvatar(file: File): Promise<string> {
	if (!file.type.startsWith('image/')) throw new Error(t('account.avatarTypeError'))
	if (file.size > 8 * 1024 * 1024) throw new Error(t('account.avatarSizeError'))
	if (file.type === 'image/gif') {
		if (file.size > 20 * 1024) throw new Error(t('account.avatarGifSizeError'))
		return readFileAsDataURL(file)
	}
	const image = new Image()
	const objectURL = URL.createObjectURL(file)
	try {
		await new Promise<void>((resolve, reject) => {
			image.onload = () => resolve()
			image.onerror = () => reject(new Error(t('account.avatarReadError')))
			image.src = objectURL
		})
		let maxEdge = 320
		let quality = 0.88
		for (let attempt = 0; attempt < 12; attempt += 1) {
			const scale = Math.min(1, maxEdge / Math.max(image.naturalWidth, image.naturalHeight))
			const width = Math.max(1, Math.round(image.naturalWidth * scale))
			const height = Math.max(1, Math.round(image.naturalHeight * scale))
			const canvas = document.createElement('canvas')
			canvas.width = width
			canvas.height = height
			const context = canvas.getContext('2d')
			if (!context) throw new Error(t('account.avatarReadError'))
			context.drawImage(image, 0, 0, width, height)
			const result = canvas.toDataURL('image/webp', quality)
			if (dataURLBytes(result) <= 20 * 1024) return result
			quality = Math.max(0.58, quality - 0.08)
			maxEdge = Math.max(96, Math.round(maxEdge * 0.84))
		}
		throw new Error(t('account.avatarCompressedError'))
	} finally {
		URL.revokeObjectURL(objectURL)
	}
}

function readFileAsDataURL(file: File): Promise<string> {
	return new Promise((resolve, reject) => {
		const reader = new FileReader()
		reader.onload = () => resolve(typeof reader.result === 'string' ? reader.result : '')
		reader.onerror = () => reject(reader.error || new Error(t('account.avatarReadError')))
		reader.readAsDataURL(file)
	})
}

function dataURLBytes(value: string) {
	const payload = value.slice(value.indexOf(',') + 1)
	return Math.ceil((payload.length * 3) / 4)
}

async function savePassword() {
	if (!passwordValid.value) return
	passwordSaving.value = true
	clearFeedback()
	try {
		const result = await changeAccountPassword(currentPassword.value, newPassword.value)
		auth.replaceSessionToken(result.access_token)
		currentPassword.value = ''
		newPassword.value = ''
		confirmPassword.value = ''
		notice.value = t('account.passwordChanged')
		if (profile.value) profile.value.password_enabled = true
	} catch (err) {
		error.value = readableError(err)
	} finally {
		passwordSaving.value = false
	}
}

async function startTOTP() {
	totpSaving.value = true
	clearFeedback()
	recoveryCodes.value = []
	try {
		totpSetup.value = await beginTOTPSetup()
		totpQRCode.value = await QRCode.toDataURL(totpSetup.value.provisioning_uri, { width: 220, margin: 1, errorCorrectionLevel: 'M' })
	} catch (err) {
		error.value = readableError(err)
	} finally {
		totpSaving.value = false
	}
}

async function enableTOTP() {
	if (!totpSetup.value || totpCode.value.trim().length !== 6) return
	totpSaving.value = true
	clearFeedback()
	try {
		const result = await confirmTOTP(totpCode.value)
		auth.replaceSessionToken(result.access_token)
		recoveryCodes.value = result.codes || []
		totpSetup.value = null
		totpQRCode.value = ''
		totpCode.value = ''
		if (profile.value) profile.value.totp_enabled = true
		notice.value = t('account.totpEnabled')
	} catch (err) {
		error.value = readableError(err)
	} finally {
		totpSaving.value = false
	}
}

async function refreshRecoveryCodes() {
	totpSaving.value = true
	clearFeedback()
	try {
		const result = await generateTOTPRecoveryCodes()
		auth.replaceSessionToken(result.access_token)
		recoveryCodes.value = result.codes || []
	} catch (err) {
		error.value = readableError(err)
	} finally {
		totpSaving.value = false
	}
}

async function turnOffTOTP() {
	if (!disableCode.value.trim()) return
	totpSaving.value = true
	clearFeedback()
	try {
		const result = await disableTOTP(disableCode.value)
		auth.replaceSessionToken(result.access_token)
		disableCode.value = ''
		recoveryCodes.value = []
		if (profile.value) profile.value.totp_enabled = false
		notice.value = t('account.totpDisabled')
	} catch (err) {
		error.value = readableError(err)
	} finally {
		totpSaving.value = false
	}
}

async function revokeOtherSessions() {
	if (!profile.value || profile.value.managed_by_config || !window.confirm(t('account.revokeSessionsConfirm'))) return
	sessionSaving.value = true
	clearFeedback()
	try {
		const result = await revokeOtherAccountSessions()
		auth.replaceSessionToken(result.access_token)
		notice.value = t('account.sessionsRevoked')
	} catch (err) {
		error.value = readableError(err)
	} finally {
		sessionSaving.value = false
	}
}

async function copyRecoveryCodes() {
	await navigator.clipboard.writeText(recoveryCodes.value.join('\n'))
	copied.value = true
	window.setTimeout(() => (copied.value = false), 1600)
}

function money(micros: number) {
	return new Intl.NumberFormat(locale.value, { minimumFractionDigits: 2, maximumFractionDigits: 6 }).format(micros / 1_000_000)
}

function date(value: string) {
	if (!value) return t('account.notAvailable')
	const parsed = new Date(value)
	if (Number.isNaN(parsed.getTime()) || parsed.getUTCFullYear() < 1970) return t('account.notAvailable')
	return new Intl.DateTimeFormat(locale.value, { year: 'numeric', month: 'short', day: 'numeric' }).format(parsed)
}

function methodIcon(method: AccountLoginMethod) {
	if (method.id === 'github') return Code2
	if (method.id === 'email') return Mail
	if (method.id === 'local') return LockKeyhole
	return Building2
}

onMounted(async () => {
	await load()
	const params = new URLSearchParams(window.location.search)
	if (params.get('binding') === 'success') {
		notice.value = t('account.bindingSucceeded')
		activeTab.value = 'login'
	} else if (params.get('binding') === 'error') {
		error.value = params.get('message') || t('account.bindingFailed')
		activeTab.value = 'login'
	}
	if (params.has('binding')) {
		window.history.replaceState({}, '', window.location.pathname)
	}
})
</script>

<template>
	<main class="content account-page">
		<section class="page-header account-page-header">
			<div><h1>{{ t('account.title') }}</h1><p>{{ t('account.subtitle') }}</p></div>
			<button class="icon-button" type="button" :disabled="loading" :title="t('common.refresh')" @click="load"><RefreshCw :size="18" /></button>
		</section>

		<div v-if="notice" class="notice success">{{ notice }}</div>
		<div v-if="error" class="notice">{{ error }}</div>
		<div v-if="loading" class="account-loading">{{ t('common.loading') }}</div>

		<template v-else-if="profile">
			<section class="account-summary">
				<div class="account-identity">
					<span class="account-hero-avatar"><img v-if="avatarDataURL" :src="avatarDataURL" alt="" /><template v-else>{{ initials }}</template></span>
					<div><div class="account-name-line"><h2>{{ profile.display_name || profile.email || profile.id }}</h2><span class="pill status-success">{{ profile.role }}</span><span class="pill" :class="profile.status === 'active' ? 'status-success' : 'status-warning'">{{ profile.status }}</span></div><p>{{ profile.email || profile.id }}</p></div>
				</div>
				<div class="account-metrics">
					<div><span>{{ t('account.balance') }}</span><strong>{{ money(profile.balance_micros) }}</strong></div>
					<div><span>{{ t('account.concurrency') }}</span><strong>{{ profile.concurrency_limit }}</strong></div>
					<div><span>RPM</span><strong>{{ profile.rpm_limit || t('account.unlimited') }}</strong></div>
					<div><span>{{ t('account.registeredAt') }}</span><strong>{{ date(profile.created_at) }}</strong></div>
				</div>
			</section>

			<div v-if="profile.managed_by_config" class="notice info">{{ t('account.managedByConfig') }}</div>

			<nav class="account-tabs" role="tablist" :aria-label="t('account.tabsLabel')">
				<button id="account-tab-profile" type="button" role="tab" data-tab="profile" aria-controls="account-panel-profile" :tabindex="activeTab === 'profile' ? 0 : -1" :class="{ active: activeTab === 'profile' }" :aria-selected="activeTab === 'profile'" @click="selectAccountTab('profile')" @keydown="handleAccountTabKeydown($event, 'profile')"><UserRound :size="17" />{{ t('account.profileTab') }}</button>
				<button id="account-tab-login" type="button" role="tab" data-tab="login" aria-controls="account-panel-login" :tabindex="activeTab === 'login' ? 0 : -1" :class="{ active: activeTab === 'login' }" :aria-selected="activeTab === 'login'" @click="selectAccountTab('login')" @keydown="handleAccountTabKeydown($event, 'login')"><KeyRound :size="17" />{{ t('account.loginTab') }}</button>
				<button id="account-tab-security" type="button" role="tab" data-tab="security" aria-controls="account-panel-security" :tabindex="activeTab === 'security' ? 0 : -1" :class="{ active: activeTab === 'security' }" :aria-selected="activeTab === 'security'" @click="selectAccountTab('security')" @keydown="handleAccountTabKeydown($event, 'security')"><ShieldCheck :size="17" />{{ t('account.securityTab') }}</button>
			</nav>

			<section v-if="activeTab === 'profile'" id="account-panel-profile" class="panel account-section" role="tabpanel" aria-labelledby="account-tab-profile" data-section="account-profile">
				<div class="panel-header"><div><h2>{{ t('account.profileAndAvatar') }}</h2><p>{{ t('account.profileHelp') }}</p></div></div>
				<div class="panel-body profile-editor-grid">
					<div class="avatar-editor">
						<span class="avatar-preview"><img v-if="avatarDataURL" :src="avatarDataURL" alt="" /><UserRound v-else :size="32" /></span>
						<div><strong>{{ t('account.avatar') }}</strong><p>{{ t('account.avatarHelp') }}</p><div class="account-actions"><input ref="fileInput" class="sr-only" type="file" accept="image/png,image/jpeg,image/webp,image/gif" @change="chooseAvatar" /><button class="button secondary" type="button" :disabled="profile.managed_by_config || avatarSaving" @click="fileInput?.click()"><Upload :size="16" />{{ t('account.uploadAvatar') }}</button><button class="button" type="button" :disabled="profile.managed_by_config || avatarSaving || !avatarDirty" @click="saveAvatar"><Save :size="16" />{{ t('common.save') }}</button><button class="button secondary" type="button" :disabled="profile.managed_by_config || avatarSaving || (!avatarDataURL && !savedAvatarDataURL)" @click="removeAvatar"><Trash2 :size="16" />{{ t('account.removeAvatar') }}</button></div></div>
					</div>
					<form class="profile-fields" @submit.prevent="saveProfile">
						<div class="field"><label for="account-email">{{ t('account.email') }}</label><input id="account-email" :value="profile.email || t('account.notAvailable')" disabled /></div>
						<div class="field"><label for="account-display-name">{{ t('account.displayName') }}</label><input id="account-display-name" v-model="displayName" maxlength="80" required :disabled="profile.managed_by_config" /></div>
						<button class="button profile-save" type="submit" :disabled="saving || profile.managed_by_config || !displayName.trim()"><Save :size="16" />{{ saving ? t('common.saving') : t('account.saveProfile') }}</button>
					</form>
				</div>
			</section>

			<section v-else-if="activeTab === 'login'" id="account-panel-login" class="panel account-section" role="tabpanel" aria-labelledby="account-tab-login" data-section="account-login-methods">
				<div class="panel-header"><div><h2>{{ t('account.loginMethods') }}</h2><p>{{ t('account.loginMethodsHelp') }}</p></div></div>
				<div class="login-method-group">
					<div class="method-group-heading"><strong>{{ t('account.primaryCredential') }}</strong><span>{{ t('account.primaryCredentialHelp') }}</span></div>
					<div v-for="method in primaryLoginMethods" :key="method.id" class="login-method-row">
						<span class="method-icon"><component :is="methodIcon(method)" :size="19" /></span>
						<div><strong>{{ method.label }}</strong><span v-if="method.detail">{{ method.detail }}</span></div>
						<div class="method-actions"><span class="pill" :class="method.bound ? 'status-success' : 'status-warning'">{{ method.bound ? t('account.bound') : t('account.notSet') }}</span><button v-if="method.id === 'email' && !method.bound" class="button secondary" type="button" @click="activeTab = 'security'"><KeyRound :size="15" />{{ t('account.setPassword') }}</button></div>
					</div>
				</div>
				<div class="login-method-group external-methods">
					<div class="method-group-heading"><strong>{{ t('account.externalCredential') }}</strong><span>{{ t('account.externalCredentialHelp') }}</span></div>
					<div v-if="!externalLoginMethods.length" class="account-empty-state"><Building2 :size="22" /><div><strong>{{ t('account.noExternalMethods') }}</strong><p>{{ t('account.noExternalMethodsHelp') }}</p></div></div>
					<div v-for="method in externalLoginMethods" v-else :key="method.id" class="login-method-row">
						<span class="method-icon"><component :is="methodIcon(method)" :size="19" /></span>
						<div><strong>{{ method.label }}</strong><span v-if="method.detail">{{ method.detail }}</span></div>
						<div class="method-actions"><span class="pill" :class="method.bound ? 'status-success' : 'status-warning'">{{ method.bound ? t('account.bound') : t('account.available') }}</span><button v-if="method.bound" class="button secondary" type="button" :disabled="profile.managed_by_config || unbindingProvider === method.id" @click="unbindIdentity(method)"><Unlink :size="15" />{{ t('account.unbind') }}</button><button v-else class="button secondary" type="button" :disabled="profile.managed_by_config || bindingProvider === method.id" @click="bindIdentity(method)"><Link :size="15" />{{ t('account.bind') }}</button></div>
					</div>
				</div>
			</section>

			<div v-else id="account-panel-security" role="tabpanel" aria-labelledby="account-tab-security">
			<section class="panel account-section security-overview" data-section="account-security">
				<div class="panel-header"><div><h2>{{ t('account.securityOverview') }}</h2><p>{{ t('account.securityOverviewHelp') }}</p></div></div>
				<div class="security-overview-list">
					<div><span class="method-icon"><LockKeyhole :size="19" /></span><div><strong>{{ t('account.passwordSignIn') }}</strong><p>{{ profile.password_enabled ? t('account.passwordConfigured') : t('account.passwordNotConfigured') }}</p></div><span class="pill" :class="profile.password_enabled ? 'status-success' : 'status-warning'">{{ profile.password_enabled ? t('account.configured') : t('account.notSet') }}</span></div>
					<div><span class="method-icon"><ShieldCheck :size="19" /></span><div><strong>{{ t('account.twoFactor') }}</strong><p>{{ profile.totp_enabled ? t('account.totpStatusHelp') : profile.totp_available ? t('account.totpReadyHelp') : t('account.totpUnavailableHelp') }}</p></div><span class="pill" :class="profile.totp_enabled ? 'status-success' : 'status-warning'">{{ profile.totp_enabled ? t('account.enabled') : t('account.disabled') }}</span></div>
					<div><span class="method-icon"><MonitorSmartphone :size="19" /></span><div><strong>{{ t('account.activeSessions') }}</strong><p>{{ t('account.activeSessionsHelp') }}</p></div><span class="pill status-success">{{ t('account.currentSession') }}</span></div>
				</div>
			</section>

			<section class="panel account-section">
				<div class="panel-header"><div><h2>{{ profile.password_enabled ? t('account.changePassword') : t('account.setPassword') }}</h2><p>{{ t('account.passwordHelp') }}</p></div><LockKeyhole :size="20" /></div>
				<form class="panel-body password-form" data-form="account-password" @submit.prevent="savePassword">
					<div v-if="profile.password_enabled" class="field"><label for="account-current-password">{{ t('account.currentPassword') }}</label><input id="account-current-password" v-model="currentPassword" type="password" autocomplete="current-password" :disabled="profile.managed_by_config" required /></div>
					<div class="field"><label for="account-new-password">{{ t('account.newPassword') }}</label><input id="account-new-password" v-model="newPassword" type="password" minlength="10" autocomplete="new-password" :disabled="profile.managed_by_config" required /><small>{{ t('account.passwordRule') }}</small></div>
					<div class="field"><label for="account-confirm-password">{{ t('account.confirmPassword') }}</label><input id="account-confirm-password" v-model="confirmPassword" type="password" minlength="10" autocomplete="new-password" :disabled="profile.managed_by_config" required /></div>
					<div class="form-actions"><button class="button" type="submit" :disabled="passwordSaving || (profile.password_enabled && !currentPassword) || !passwordValid || profile.managed_by_config"><KeyRound :size="16" />{{ passwordSaving ? t('common.saving') : profile.password_enabled ? t('account.changePassword') : t('account.setPassword') }}</button></div>
				</form>
			</section>

			<section class="panel account-section">
				<div class="panel-header"><div><h2>{{ t('account.twoFactor') }}</h2><p>{{ t('account.twoFactorHelp') }}</p></div><ShieldCheck :size="21" /></div>
				<div class="panel-body totp-body">
					<div class="security-status"><span class="method-icon"><BadgeCheck v-if="profile.totp_enabled" :size="20" /><ShieldCheck v-else :size="20" /></span><div><strong>{{ profile.totp_enabled ? t('account.totpOn') : t('account.totpOff') }}</strong><p>{{ profile.totp_available || profile.totp_enabled ? t('account.totpStatusHelp') : t('account.totpUnavailable') }}</p></div><button v-if="!profile.totp_enabled && profile.totp_available && !totpSetup" class="button" type="button" :disabled="totpSaving" @click="startTOTP"><Camera :size="16" />{{ t('account.setupTOTP') }}</button></div>

						<div v-if="totpSetup" class="totp-setup">
						<img :src="totpQRCode" :alt="t('account.qrCode')" />
						<div class="totp-setup-copy"><h3>{{ t('account.scanCode') }}</h3><p>{{ t('account.scanCodeHelp') }}</p><code>{{ totpSetup.secret }}</code><details><summary>{{ t('account.manualURI') }}</summary><code class="uri-code">{{ totpSetup.provisioning_uri }}</code></details><div class="field"><label for="account-totp-code">{{ t('account.verificationCode') }}</label><input id="account-totp-code" v-model="totpCode" inputmode="numeric" maxlength="6" autocomplete="one-time-code" /></div><button class="button" type="button" :disabled="totpSaving || totpCode.trim().length !== 6" @click="enableTOTP"><Check :size="16" />{{ t('account.confirmEnable') }}</button></div>
					</div>

						<div v-if="profile.totp_enabled" class="totp-enabled-actions">
						<button class="button secondary" type="button" :disabled="totpSaving" @click="refreshRecoveryCodes"><RefreshCw :size="16" />{{ t('account.regenerateCodes') }}</button>
							<div class="disable-totp"><div class="field"><label for="account-disable-totp-code">{{ t('account.verificationCode') }}</label><input id="account-disable-totp-code" v-model="disableCode" inputmode="numeric" maxlength="6" autocomplete="one-time-code" /></div><button class="button danger" type="button" :disabled="totpSaving || !disableCode.trim()" @click="turnOffTOTP"><Trash2 :size="16" />{{ t('account.disableTOTP') }}</button></div>
					</div>

					<div v-if="recoveryCodes.length" class="recovery-panel"><div><h3>{{ t('account.recoveryCodes') }}</h3><p>{{ t('account.recoveryCodesHelp') }}</p></div><div class="recovery-grid"><code v-for="code in recoveryCodes" :key="code">{{ code }}</code></div><button class="button secondary" type="button" @click="copyRecoveryCodes"><Check v-if="copied" :size="16" /><Copy v-else :size="16" />{{ copied ? t('account.copied') : t('account.copyCodes') }}</button></div>
				</div>
			</section>

			<section class="panel account-section session-section">
				<div class="panel-header"><div><h2>{{ t('account.sessionManagement') }}</h2><p>{{ t('account.sessionManagementHelp') }}</p></div><MonitorSmartphone :size="20" /></div>
				<div class="panel-body session-action"><div><strong>{{ t('account.revokeOtherSessions') }}</strong><p>{{ t('account.revokeOtherSessionsHelp') }}</p></div><button class="button secondary" type="button" :disabled="profile.managed_by_config || sessionSaving" @click="revokeOtherSessions"><RefreshCw :size="16" />{{ sessionSaving ? t('common.saving') : t('account.revokeOtherSessions') }}</button></div>
			</section>
			</div>
		</template>
	</main>
</template>

<style scoped>
.account-page { width: min(1040px, 100%); margin-inline: auto; }
.account-page-header { margin-bottom: 18px; }
.account-loading { min-height: 280px; display: grid; place-items: center; color: var(--text-muted); }
.account-summary { display: grid; gap: 20px; padding: 24px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); box-shadow: var(--shadow-sm); }
.account-identity { display: flex; min-width: 0; align-items: center; gap: 16px; }
.account-identity h2 { margin: 0; font-size: 19px; letter-spacing: 0; overflow-wrap: anywhere; }
.account-identity p { margin: 5px 0 0; color: var(--text-muted); font-size: 13px; overflow-wrap: anywhere; }
.account-name-line { display: flex; align-items: center; flex-wrap: wrap; gap: 7px; }
.account-hero-avatar, .avatar-preview { display: grid; flex: 0 0 auto; place-items: center; overflow: hidden; background: var(--primary-600); color: white; font-weight: 700; }
.account-hero-avatar { width: 72px; height: 72px; border-radius: 8px; font-size: 22px; }
.account-hero-avatar img, .avatar-preview img { width: 100%; height: 100%; object-fit: cover; }
.account-metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; }
.account-metrics > div { min-width: 0; min-height: 68px; padding: 12px 14px; border-radius: 8px; background: var(--surface-subtle); }
.account-metrics span { display: block; margin-bottom: 5px; color: var(--text-muted); font-size: 12px; }
.account-metrics strong { display: block; font-size: 16px; overflow-wrap: anywhere; }
.account-tabs { display: flex; gap: 4px; margin-top: 18px; padding: 4px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface-subtle); }
.account-tabs button { display: inline-flex; min-height: 38px; align-items: center; justify-content: center; gap: 7px; padding: 0 14px; border: 0; border-radius: 6px; background: transparent; color: var(--text-muted); font: inherit; font-size: 13px; font-weight: 600; cursor: pointer; }
.account-tabs button:hover { background: var(--surface-hover); color: var(--text); }
.account-tabs button.active { background: var(--surface); color: var(--primary-700); box-shadow: var(--shadow-sm); }
.account-section { margin-top: 12px; border-radius: 8px; }
.account-section > .panel-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.panel-header p { margin: 4px 0 0; color: var(--text-muted); font-size: 13px; }
.profile-editor-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 24px; }
.avatar-editor { display: flex; align-items: center; gap: 18px; }
.avatar-editor strong { display: block; }
.avatar-editor p { max-width: 390px; margin: 5px 0 12px; color: var(--text-muted); font-size: 13px; }
.avatar-preview { width: 86px; height: 86px; border-radius: 8px; background: var(--surface-hover); color: var(--text-muted); }
.account-actions { display: flex; align-items: center; gap: 8px; }
.profile-fields { display: grid; align-content: start; gap: 14px; padding-left: 24px; border-left: 1px solid var(--border); }
.profile-save { justify-self: end; }
.login-method-group { padding: 4px 20px 12px; }
.login-method-group + .login-method-group { border-top: 1px solid var(--border); }
.method-group-heading { display: grid; gap: 3px; padding: 14px 0 8px; }
.method-group-heading strong { font-size: 13px; }
.method-group-heading span { color: var(--text-muted); font-size: 12px; }
.login-method-row { display: grid; grid-template-columns: 38px minmax(0, 1fr) auto; gap: 12px; align-items: center; min-height: 68px; border-top: 1px solid var(--border); }
.login-method-row > div { display: grid; gap: 3px; }
.login-method-row > div span { color: var(--text-muted); font-size: 12px; overflow-wrap: anywhere; }
.method-actions { display: flex !important; grid-auto-flow: column; align-items: center; gap: 8px !important; }
.method-icon { display: grid; width: 34px; height: 34px; place-items: center; border-radius: 8px; background: var(--primary-50); color: var(--primary-700); }
.account-empty-state { display: flex; align-items: flex-start; gap: 12px; margin: 8px 0 4px; padding: 18px 0; border-top: 1px solid var(--border); color: var(--text-muted); }
.account-empty-state strong { display: block; color: var(--text); font-size: 13px; }
.account-empty-state p { margin: 4px 0 0; font-size: 12px; }
.password-form { display: grid; gap: 16px; }
.field small { display: block; margin-top: 5px; color: var(--text-muted); }
.form-actions { display: flex; justify-content: flex-end; padding-top: 4px; }
.totp-body { display: grid; gap: 20px; }
.security-status { display: grid; grid-template-columns: 38px minmax(0, 1fr) auto; align-items: center; gap: 12px; }
.security-status p { margin: 4px 0 0; color: var(--text-muted); font-size: 13px; }
.security-overview-list { padding: 0 20px 8px; }
.security-overview-list > div { display: grid; grid-template-columns: 38px minmax(0, 1fr) auto; align-items: center; gap: 12px; min-height: 70px; border-top: 1px solid var(--border); }
.security-overview-list > div:first-child { border-top: 0; }
.security-overview-list p, .session-action p { margin: 4px 0 0; color: var(--text-muted); font-size: 13px; }
.totp-setup { display: grid; grid-template-columns: 220px minmax(0, 1fr); gap: 24px; padding-top: 20px; border-top: 1px solid var(--border); }
.totp-setup > img { width: 220px; height: 220px; border: 1px solid var(--border); border-radius: 8px; background: white; }
.totp-setup-copy { display: grid; align-content: start; gap: 12px; }
.totp-setup-copy h3, .recovery-panel h3 { margin: 0; font-size: 15px; }
.totp-setup-copy p, .recovery-panel p { margin: 0; color: var(--text-muted); font-size: 13px; }
.totp-setup-copy code { width: fit-content; padding: 7px 9px; border-radius: 6px; background: var(--surface-hover); overflow-wrap: anywhere; }
.totp-setup-copy details { max-width: 100%; }
.uri-code { display: block; width: 100% !important; margin-top: 8px; }
.totp-enabled-actions { display: flex; align-items: end; justify-content: space-between; gap: 16px; padding-top: 18px; border-top: 1px solid var(--border); }
.disable-totp { display: flex; align-items: end; gap: 8px; }
.recovery-panel { display: grid; gap: 14px; padding: 16px; border: 1px solid var(--warning); border-radius: 8px; background: var(--warning-bg); }
.recovery-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
.recovery-grid code { padding: 7px 9px; border-radius: 6px; background: var(--surface); }
.recovery-panel .button { justify-self: start; }
.session-action { display: flex; align-items: center; justify-content: space-between; gap: 24px; }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; }
.notice.info { border-color: var(--info); background: var(--info-bg); color: var(--text); }
.button.danger { background: var(--danger); color: white; }
@media (max-width: 980px) {
	.profile-editor-grid { grid-template-columns: 1fr; }
	.profile-fields { padding-top: 20px; padding-left: 0; border-top: 1px solid var(--border); border-left: 0; }
}
@media (max-width: 680px) {
	.account-summary { padding: 16px; }
	.account-page .button, .account-page .icon-button, .account-page input { min-height: 44px; }
	.account-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); row-gap: 16px; }
	.account-identity { align-items: flex-start; }
	.profile-editor-grid { padding: 16px; }
	.avatar-editor { align-items: flex-start; }
	.account-actions { flex-wrap: wrap; }
	.profile-save { justify-self: start; }
	.form-actions { justify-content: flex-start; }
	.account-tabs { overflow-x: auto; }
	.account-tabs button { flex: 1 0 auto; min-height: 44px; }
	.login-method-group { padding-inline: 16px; }
	.login-method-row { grid-template-columns: 38px minmax(0, 1fr); padding-block: 10px; }
	.method-actions { grid-column: 2; grid-auto-flow: row; justify-items: start; }
	.security-overview-list { padding-inline: 16px; }
	.security-overview-list > div { grid-template-columns: 38px minmax(0, 1fr); padding-block: 10px; }
	.security-overview-list > div > .pill { grid-column: 2; justify-self: start; }
	.security-status { grid-template-columns: 38px minmax(0, 1fr); }
	.security-status > .button { grid-column: 1 / -1; justify-self: start; }
	.totp-setup { grid-template-columns: 1fr; }
	.totp-setup > img { width: min(220px, 100%); height: auto; }
	.totp-enabled-actions, .disable-totp { align-items: stretch; flex-direction: column; }
	.recovery-grid { grid-template-columns: 1fr; }
	.session-action { align-items: stretch; flex-direction: column; }
}
</style>
