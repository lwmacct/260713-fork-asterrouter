<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Eye, EyeOff, Lock, LogIn, UserRound } from '@lucide/vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import TurnstileWidget from '@/components/TurnstileWidget.vue'
import { availableLocales, getLocale, setLocale, type LocaleCode } from '@/i18n'
import { forgotPassword, register, resetPassword, verifyEmail } from '@/api/auth'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const app = useAppStore()
const auth = useAuthStore()
const showPassword = ref(false)
const form = reactive({
  username: 'admin',
  password: ''
})
const mfaCode = ref('')
const mfaChallenge = computed(() => typeof route.query.mfa === 'string' ? route.query.mfa : '')
const authMode = ref<'login'|'register'|'forgot'|'reset'|'verify'>('login')
const accountForm = reactive({ email: '', displayName: '', password: '', confirmPassword: '', invitationCode: '' })
const actionMessage = ref('')
const agreementAccepted = ref(false)
const turnstileToken = ref('')
const turnstileResetKey = ref(0)

const redirectTo = computed(() => {
  const value = route.query.redirect
  if (typeof value === 'string' && value.startsWith('/')) return value
  return defaultEntry()
})
const demoMode = computed(() => Boolean(app.publicSettings?.demo_mode))

onMounted(async () => {
	if (typeof route.query.verify === 'string') { authMode.value = 'verify'; await verifyEmail(route.query.verify); actionMessage.value = t('auth.emailVerified'); return }
	if (typeof route.query.reset === 'string') { authMode.value = 'reset'; return }
	if (route.query.oidc !== 'success' && route.query.provider !== 'feishu') return
  await auth.completeOIDCLogin()
  await router.replace(defaultEntry())
})

function loginWithOIDC() {
  window.location.assign(`/api/v1/auth/oidc?agreement_accepted=${agreementAccepted.value}`)
}

function loginWithFeishu() { window.location.assign(`/api/v1/auth/feishu?agreement_accepted=${agreementAccepted.value}`) }
function loginWithDingTalk() { window.location.assign(`/api/v1/auth/dingtalk?agreement_accepted=${agreementAccepted.value}`) }
function loginWithSocial(provider: 'github' | 'google') { window.location.assign(`/api/v1/auth/oauth/${provider}?agreement_accepted=${agreementAccepted.value}`) }

async function submit() {
  try { await auth.login(form.username, form.password, agreementAccepted.value, turnstileToken.value); await router.push(redirectTo.value) }
  catch (err) { if (app.publicSettings?.turnstile_enabled) turnstileResetKey.value++; throw err }
}

async function enterDemo() {
  await auth.login('demo', 'demo')
  await router.push(redirectTo.value)
}

async function submitMFA() { await auth.completeMFA(mfaChallenge.value, mfaCode.value); await router.replace(defaultEntry()) }
async function submitAccountAction() {
	actionMessage.value = ''; auth.error = ''
	try {
		if (authMode.value === 'register') { if (accountForm.password !== accountForm.confirmPassword) throw new Error(t('auth.passwordMismatch')); await register(accountForm.email, accountForm.password, accountForm.displayName, accountForm.invitationCode, agreementAccepted.value); actionMessage.value = t('auth.registrationAccepted') }
		if (authMode.value === 'forgot') { await forgotPassword(accountForm.email); actionMessage.value = t('auth.resetEmailAccepted') }
		if (authMode.value === 'reset' && typeof route.query.reset === 'string') { if (accountForm.password !== accountForm.confirmPassword) throw new Error(t('auth.passwordMismatch')); await resetPassword(route.query.reset, accountForm.password); actionMessage.value = t('auth.passwordResetComplete'); authMode.value = 'login' }
	} catch (err) { auth.error = err instanceof Error ? err.message : t('common.failed') }
}

function defaultEntry(): string {
  const settings = app.publicSettings
  const profile = settings?.enabled_profiles.includes(settings.default_profile) ? settings.default_profile : settings?.enabled_profiles[0]
  if (profile === 'personal') return '/console/overview'
  if (profile === 'relay_operator') return ['super_admin', 'platform_admin', 'demo_admin'].includes(auth.user?.role || '') ? '/operator/overview' : '/customer/overview'
  return '/admin/dashboard'
}

function changeLocale(event: Event) {
  setLocale((event.target as HTMLSelectElement).value as LocaleCode)
}
</script>

<template>
  <main class="auth-page">
    <div class="auth-bg-grid" aria-hidden="true"></div>
    <label class="auth-locale locale-control">
      <select :value="getLocale()" :aria-label="t('nav.language')" @change="changeLocale">
        <option v-for="locale in availableLocales" :key="locale.code" :value="locale.code">
          {{ locale.label }}
        </option>
      </select>
    </label>

    <div class="auth-container">
      <div class="auth-brand">
		<img v-if="app.publicSettings?.site_logo" :src="app.publicSettings.site_logo" class="auth-brand-logo" alt=""/>
		<div v-else class="brand-mark large">AR</div>
        <h1>{{ app.siteName }}</h1>
        <p>{{ app.siteSubtitle }}</p>
      </div>

      <section class="auth-card">
        <div v-if="demoMode" class="notice demo-mode-notice">
          <strong>{{ t('auth.demoMode') }}</strong>
          <span>{{ t('auth.demoModeHelp') }}</span>
        </div>
        <div class="auth-title">
          <h2>{{ t('auth.welcomeBack') }}</h2>
          <p>{{ t('auth.signInToAccount') }}</p>
        </div>

        <div v-if="actionMessage" class="notice success">{{ actionMessage }}</div>
        <form v-if="authMode !== 'login' && !mfaChallenge" class="auth-form" @submit.prevent="submitAccountAction">
			<div v-if="authMode !== 'reset' && authMode !== 'verify'" class="field"><label>{{ t('auth.email') }}</label><input v-model="accountForm.email" type="email" autocomplete="email" required /></div>
			<div v-if="authMode === 'register'" class="field"><label>{{ t('auth.displayName') }}</label><input v-model="accountForm.displayName" autocomplete="name" /></div>
			<div v-if="authMode === 'register' && app.publicSettings?.invitation_required" class="field"><label>{{ t('auth.invitationCode') }}</label><input v-model="accountForm.invitationCode" required /></div>
			<div v-if="authMode === 'register' || authMode === 'reset'" class="field"><label>{{ t('auth.password') }}</label><input v-model="accountForm.password" type="password" minlength="10" autocomplete="new-password" required /></div>
			<div v-if="authMode === 'register' || authMode === 'reset'" class="field"><label>{{ t('auth.confirmPassword') }}</label><input v-model="accountForm.confirmPassword" type="password" minlength="10" autocomplete="new-password" required /></div>
			<label v-if="app.publicSettings?.login_agreement_enabled && authMode === 'register'" class="agreement-check"><input v-model="agreementAccepted" type="checkbox" required/><span>我已阅读并同意 <a v-for="document in app.publicSettings.legal_documents" :key="document.id" :href="`/legal/${document.slug}`" target="_blank">{{ document.name }}</a></span></label>
			<button v-if="authMode !== 'verify'" class="button auth-submit" type="submit">{{ authMode === 'register' ? t('auth.createAccount') : authMode === 'forgot' ? t('auth.sendResetEmail') : t('auth.resetPassword') }}</button>
			<button class="button secondary auth-submit" type="button" @click="authMode = 'login'">{{ t('auth.backToLogin') }}</button>
		</form>

        <form v-else-if="mfaChallenge" class="auth-form" @submit.prevent="submitMFA">
			<div class="field"><label for="mfa-code">{{ t('auth.totpCode') }}</label><div class="input-with-icon"><Lock :size="18"/><input id="mfa-code" v-model="mfaCode" inputmode="numeric" pattern="[0-9]{6}" maxlength="6" autocomplete="one-time-code" required /></div></div>
			<div v-if="auth.error" class="notice">{{ auth.error }}</div>
			<button class="button auth-submit" type="submit" :disabled="auth.loading"><LogIn :size="18"/>{{ t('auth.verifyAndSignIn') }}</button>
		</form>

        <form v-else class="auth-form" @submit.prevent="submit">
          <div class="field">
            <label for="username">{{ t('auth.username') }}</label>
            <div class="input-with-icon">
              <UserRound :size="18" aria-hidden="true" />
              <input
                id="username"
                v-model="form.username"
                autocomplete="username"
                autofocus
                required
                :placeholder="t('auth.usernamePlaceholder')"
              />
            </div>
          </div>

          <div class="field">
            <label for="password">{{ t('auth.password') }}</label>
            <div class="input-with-icon">
              <Lock :size="18" aria-hidden="true" />
              <input
                id="password"
                v-model="form.password"
                :type="showPassword ? 'text' : 'password'"
                autocomplete="current-password"
                required
                :placeholder="t('auth.passwordPlaceholder')"
              />
              <button
                type="button"
                class="icon-button"
                :aria-label="showPassword ? t('auth.hidePassword') : t('auth.showPassword')"
                :title="showPassword ? t('auth.hidePassword') : t('auth.showPassword')"
                @click="showPassword = !showPassword"
              >
                <EyeOff v-if="showPassword" :size="18" />
                <Eye v-else :size="18" />
              </button>
            </div>
          </div>

          <div v-if="auth.error" class="notice">{{ auth.error }}</div>
		  <TurnstileWidget v-if="app.publicSettings?.turnstile_enabled && app.publicSettings.turnstile_site_key" :site-key="app.publicSettings.turnstile_site_key" :reset-key="turnstileResetKey" @token="turnstileToken=$event"/>
		  <label v-if="app.publicSettings?.login_agreement_enabled" class="agreement-check"><input v-model="agreementAccepted" type="checkbox" required/><span>我已阅读并同意 <a v-for="document in app.publicSettings.legal_documents" :key="document.id" :href="`/legal/${document.slug}`" target="_blank">{{ document.name }}</a></span></label>

          <button class="button auth-submit" type="submit" :disabled="auth.loading || (app.publicSettings?.turnstile_enabled && !turnstileToken)">
            <LogIn :size="18" />
            {{ auth.loading ? t('auth.signingIn') : t('auth.signIn') }}
          </button>
          <button v-if="demoMode" class="button secondary auth-submit" type="button" :disabled="auth.loading" @click="enterDemo">
            <LogIn :size="18" />
            {{ auth.loading ? t('auth.signingIn') : t('auth.enterDemo') }}
          </button>
			<div class="auth-secondary-actions"><button type="button" @click="authMode = 'forgot'">{{ t('auth.forgotPassword') }}</button><button v-if="app.publicSettings?.registration_enabled" type="button" @click="authMode = 'register'">{{ t('auth.createAccount') }}</button></div>
			<button v-if="app.publicSettings?.oidc_enabled" class="button secondary auth-submit" type="button" :disabled="app.publicSettings?.login_agreement_enabled && !agreementAccepted" @click="loginWithOIDC">
				<LogIn :size="18" />
				{{ app.publicSettings?.oidc_provider_name || 'OIDC' }}
			</button>
			<button v-if="app.publicSettings?.feishu_enabled" class="button secondary auth-submit" type="button" :disabled="app.publicSettings?.login_agreement_enabled && !agreementAccepted" @click="loginWithFeishu">
				<LogIn :size="18" />
				{{ app.publicSettings?.feishu_region === 'global' ? 'Lark' : 'Feishu' }}
			</button>
			<button v-if="app.publicSettings?.github_oauth_enabled" class="button secondary auth-submit" type="button" :disabled="app.publicSettings?.login_agreement_enabled && !agreementAccepted" @click="loginWithSocial('github')"><LogIn :size="18"/>GitHub</button>
			<button v-if="app.publicSettings?.google_oauth_enabled" class="button secondary auth-submit" type="button" :disabled="app.publicSettings?.login_agreement_enabled && !agreementAccepted" @click="loginWithSocial('google')"><LogIn :size="18"/>Google</button>
			<button v-if="app.publicSettings?.dingtalk_enabled" class="button secondary auth-submit" type="button" :disabled="app.publicSettings?.login_agreement_enabled && !agreementAccepted" @click="loginWithDingTalk"><LogIn :size="18"/>钉钉</button>
        </form>
      </section>

      <p class="auth-footer">
        &copy; {{ new Date().getFullYear() }} {{ app.siteName }}. {{ t('auth.rightsReserved') }}
      </p>
    </div>
  </main>
</template>
