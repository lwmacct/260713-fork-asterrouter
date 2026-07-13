<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { AlertTriangle, Check, Copy, Download, ExternalLink, Eye, EyeOff, KeyRound, RefreshCw, RotateCw, Terminal, WandSparkles } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { createPortalAPIKey, getPortalWorkspace, rotatePortalAPIKey } from '@/api/control'
import { useAppStore } from '@/stores/app'
import type { APIKeyRecord, PortalWorkspace } from '@/types'

type ConfigTarget = 'claude-code' | 'codex' | 'gemini-cli' | 'cursor' | 'opencode' | 'openclaw' | 'hermes' | 'curl' | 'python' | 'anthropic'
type CCSwitchTarget = 'codex' | 'claude-code' | 'gemini-cli' | 'opencode' | 'openclaw' | 'hermes'
type InstallMethod = string

interface InstallOption {
  id: string
  label: string
  note: string
  command: string
}

interface ResourceLink {
  label: string
  href: string
  primary?: boolean
}

interface ResourceGroup {
  title: string
  help: string
  links: ResourceLink[]
}

interface ToolInstallGuide {
  name: string
  version: string
  description: string
  configTarget: ConfigTarget
  methods: InstallOption[]
  resources: ResourceGroup[]
}

const configTargets: Array<{ id: ConfigTarget; label: string }> = [
  { id: 'claude-code', label: 'Claude Code' },
  { id: 'codex', label: 'Codex' },
  { id: 'gemini-cli', label: 'Gemini CLI' },
  { id: 'cursor', label: 'Cursor' },
  { id: 'opencode', label: 'OpenCode' },
  { id: 'openclaw', label: 'OpenClaw' },
  { id: 'hermes', label: 'Hermes' },
  { id: 'curl', label: 'cURL' },
  { id: 'python', label: 'Python SDK' },
  { id: 'anthropic', label: 'Anthropic SDK' }
]
const ccSwitchTargets: Array<{ id: CCSwitchTarget; label: string }> = [
  { id: 'codex', label: 'Codex' },
  { id: 'claude-code', label: 'Claude Code' },
  { id: 'gemini-cli', label: 'Gemini CLI' },
  { id: 'opencode', label: 'OpenCode' },
  { id: 'openclaw', label: 'OpenClaw' },
  { id: 'hermes', label: 'Hermes' }
]
const ccSwitchApp: Record<CCSwitchTarget, string> = {
  codex: 'codex',
  'claude-code': 'claude',
  'gemini-cli': 'gemini',
  opencode: 'opencode',
  openclaw: 'openclaw',
  hermes: 'hermes'
}

const { t } = useI18n()
const emit = defineEmits<{ 'workspace-changed': [] }>()
const app = useAppStore()
const workspace = ref<PortalWorkspace | null>(null)
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const copiedField = ref('')
const selectedKeyID = ref('')
const selectedModel = ref('')
const keySecret = ref('')
const secretKeyID = ref('')
const secretVisible = ref(false)
const configTarget = ref<ConfigTarget>('codex')
const ccSwitchTarget = ref<CCSwitchTarget>('codex')
const installMethod = ref<InstallMethod>('npm')

const activeKeys = computed(() => (workspace.value?.api_keys || []).filter((key) => key.status === 'active'))
const selectedKey = computed(() => activeKeys.value.find((key) => key.id === selectedKeyID.value) || null)
const canManageKeys = computed(() => Boolean(workspace.value?.can_manage_keys))
const siteName = computed(() => app.siteName || 'AsterRouter')
const providerSlug = computed(() => siteName.value.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_|_$/g, '') || 'asterrouter')

const installGuides = computed<Record<CCSwitchTarget, ToolInstallGuide>>(() => ({
  codex: {
    name: 'Codex CLI',
    version: 'v0.144.2',
    description: t('accountAccess.codexHelp'),
    configTarget: 'codex',
    methods: [
      { id: 'npm', label: 'npm', note: t('accountAccess.nodeNote'), command: 'npm install -g @openai/codex' },
      { id: 'homebrew', label: 'Homebrew', note: t('accountAccess.homebrewNote'), command: 'brew install --cask codex' },
      { id: 'script', label: t('accountAccess.installScript'), note: t('accountAccess.scriptNote'), command: 'curl -fsSL https://chatgpt.com/codex/install.sh | sh' }
    ],
    resources: [
      {
        title: t('accountAccess.installMethodB'),
        help: t('accountAccess.binaryHelp'),
        links: [
          { label: t('accountAccess.downloadMac'), href: 'https://github.com/openai/codex/releases/latest/download/codex-aarch64-apple-darwin.tar.gz', primary: true },
          { label: 'macOS Apple', href: 'https://github.com/openai/codex/releases/latest/download/codex-aarch64-apple-darwin.tar.gz' },
          { label: 'macOS Intel', href: 'https://github.com/openai/codex/releases/latest/download/codex-x86_64-apple-darwin.tar.gz' },
          { label: 'Windows x64', href: 'https://github.com/openai/codex/releases/latest/download/codex-x86_64-pc-windows-msvc.exe.zip' },
          { label: 'Linux x64', href: 'https://github.com/openai/codex/releases/latest/download/codex-x86_64-unknown-linux-musl.tar.gz' }
        ]
      },
      {
        title: t('accountAccess.installMethodC'),
        help: t('accountAccess.desktopHelp'),
        links: [
          { label: t('accountAccess.downloadDesktop'), href: 'https://chatgpt.com/codex', primary: true },
          { label: 'macOS', href: 'https://chatgpt.com/codex' },
          { label: 'Windows Store', href: 'https://apps.microsoft.com/detail/9PLM9XGG6VKS' },
          { label: 'Linux', href: 'https://chatgpt.com/codex' }
        ]
      }
    ]
  },
  'claude-code': {
    name: 'Claude Code',
    version: 'v2.1.207',
    description: t('accountAccess.claudeHelp'),
    configTarget: 'claude-code',
    methods: [
      { id: 'script', label: t('accountAccess.installScript'), note: 'macOS / Linux', command: 'curl -fsSL https://claude.ai/install.sh | bash' },
      { id: 'homebrew', label: 'Homebrew', note: t('accountAccess.homebrewNote'), command: 'brew install --cask claude-code' },
      { id: 'powershell', label: 'PowerShell', note: 'Windows', command: 'irm https://claude.ai/install.ps1 | iex' },
      { id: 'npm', label: 'npm', note: t('accountAccess.legacyNpmNote'), command: 'npm install -g @anthropic-ai/claude-code' }
    ],
    resources: [
      {
        title: t('accountAccess.officialResources'),
        help: t('accountAccess.claudeSetupHelp'),
        links: [
          { label: t('accountAccess.setupDocs'), href: 'https://code.claude.com/docs/en/setup', primary: true },
          { label: 'GitHub Releases', href: 'https://github.com/anthropics/claude-code/releases/latest' },
          { label: 'Windows winget', href: 'https://code.claude.com/docs/en/setup' }
        ]
      },
      {
        title: t('accountAccess.desktopApps'),
        help: t('accountAccess.claudeDesktopHelp'),
        links: [
          { label: t('accountAccess.downloadDesktop'), href: 'https://claude.ai/download', primary: true },
          { label: 'macOS', href: 'https://claude.ai/download' },
          { label: 'Windows', href: 'https://claude.ai/download' }
        ]
      }
    ]
  },
  'gemini-cli': {
    name: 'Gemini CLI',
    version: 'v0.50.0',
    description: t('accountAccess.geminiHelp'),
    configTarget: 'gemini-cli',
    methods: [
      { id: 'npx', label: 'npx', note: t('accountAccess.noInstallNote'), command: 'npx @google/gemini-cli' },
      { id: 'npm', label: 'npm', note: t('accountAccess.nodeNote'), command: 'npm install -g @google/gemini-cli' },
      { id: 'homebrew', label: 'Homebrew', note: t('accountAccess.homebrewNote'), command: 'brew install gemini-cli' }
    ],
    resources: [
      {
        title: t('accountAccess.officialResources'),
        help: t('accountAccess.geminiResourcesHelp'),
        links: [
          { label: t('accountAccess.setupDocs'), href: 'https://www.geminicli.com/docs/get-started/installation', primary: true },
          { label: 'GitHub Releases', href: 'https://github.com/google-gemini/gemini-cli/releases/latest' },
          { label: 'npm', href: 'https://www.npmjs.com/package/@google/gemini-cli' }
        ]
      },
      {
        title: t('accountAccess.releaseChannels'),
        help: t('accountAccess.releaseChannelsHelp'),
        links: [
          { label: 'Stable', href: 'https://github.com/google-gemini/gemini-cli/releases/latest', primary: true },
          { label: 'Preview', href: 'https://www.npmjs.com/package/@google/gemini-cli' },
          { label: 'Nightly', href: 'https://www.npmjs.com/package/@google/gemini-cli' }
        ]
      }
    ]
  },
  opencode: {
    name: 'OpenCode CLI',
    version: 'v1.17.18',
    description: t('accountAccess.openCodeHelp'),
    configTarget: 'opencode',
    methods: [
      { id: 'script', label: t('accountAccess.installScript'), note: 'macOS / Linux', command: 'curl -fsSL https://opencode.ai/install | bash' },
      { id: 'npm', label: 'npm', note: t('accountAccess.nodeNote'), command: 'npm install -g opencode-ai@latest' },
      { id: 'homebrew', label: 'Homebrew', note: t('accountAccess.homebrewNote'), command: 'brew install anomalyco/tap/opencode' }
    ],
    resources: [
      {
        title: t('accountAccess.officialResources'),
        help: t('accountAccess.openCodeResourcesHelp'),
        links: [
          { label: 'GitHub Releases', href: 'https://github.com/anomalyco/opencode/releases/latest', primary: true },
          { label: 'Install docs', href: 'https://opencode.ai/docs/' },
          { label: 'Windows', href: 'https://opencode.ai/docs/' }
        ]
      },
      {
        title: t('accountAccess.desktopApps'),
        help: t('accountAccess.openCodeDesktopHelp'),
        links: [
          { label: t('accountAccess.downloadDesktop'), href: 'https://opencode.ai/download', primary: true },
          { label: 'macOS', href: 'https://opencode.ai/download' },
          { label: 'Windows', href: 'https://opencode.ai/download' },
          { label: 'Linux', href: 'https://opencode.ai/download' }
        ]
      }
    ]
  },
  openclaw: {
    name: 'OpenClaw',
    version: 'v2026.6.11',
    description: t('accountAccess.openClawHelp'),
    configTarget: 'openclaw',
    methods: [
      { id: 'npm', label: 'npm', note: t('accountAccess.nodeNote'), command: 'npm install -g openclaw@latest' },
      { id: 'pnpm', label: 'pnpm', note: t('accountAccess.nodeNote'), command: 'pnpm add -g openclaw@latest' },
      { id: 'onboard', label: t('accountAccess.onboard'), note: t('accountAccess.afterInstallNote'), command: 'openclaw onboard --install-daemon' }
    ],
    resources: [
      {
        title: t('accountAccess.officialResources'),
        help: t('accountAccess.openClawResourcesHelp'),
        links: [
          { label: t('accountAccess.setupDocs'), href: 'https://docs.openclaw.ai/start/getting-started', primary: true },
          { label: 'GitHub Releases', href: 'https://github.com/openclaw/openclaw/releases/latest' },
          { label: 'npm', href: 'https://www.npmjs.com/package/openclaw' }
        ]
      },
      {
        title: t('accountAccess.runAndDeploy'),
        help: t('accountAccess.openClawDeployHelp'),
        links: [
          { label: 'Onboarding', href: 'https://docs.openclaw.ai/start/wizard', primary: true },
          { label: 'Docker', href: 'https://docs.openclaw.ai/install/docker' },
          { label: 'Website', href: 'https://openclaw.ai' }
        ]
      }
    ]
  },
  hermes: {
    name: 'Hermes Agent',
    version: 'v2026.7.7.2',
    description: t('accountAccess.hermesHelp'),
    configTarget: 'hermes',
    methods: [
      { id: 'script', label: t('accountAccess.installScript'), note: 'macOS / Linux', command: 'curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash' },
      { id: 'powershell', label: 'PowerShell', note: 'Windows', command: 'iex (irm https://hermes-agent.nousresearch.com/install.ps1)' }
    ],
    resources: [
      {
        title: t('accountAccess.officialResources'),
        help: t('accountAccess.hermesResourcesHelp'),
        links: [
          { label: t('accountAccess.setupDocs'), href: 'https://hermes-agent.nousresearch.com/docs/', primary: true },
          { label: 'GitHub Releases', href: 'https://github.com/NousResearch/hermes-agent/releases/latest' },
          { label: 'GitHub', href: 'https://github.com/NousResearch/hermes-agent' }
        ]
      },
      {
        title: t('accountAccess.platformGuides'),
        help: t('accountAccess.hermesPlatformHelp'),
        links: [
          { label: 'Windows', href: 'https://hermes-agent.nousresearch.com/docs/getting-started/installation', primary: true },
          { label: 'Android / Termux', href: 'https://hermes-agent.nousresearch.com/docs/getting-started/termux' },
          { label: 'macOS / Linux', href: 'https://hermes-agent.nousresearch.com/docs/getting-started/installation' }
        ]
      }
    ]
  }
}))

const activeInstallGuide = computed(() => installGuides.value[ccSwitchTarget.value])
const activeInstallOption = computed(() => activeInstallGuide.value.methods.find((method) => method.id === installMethod.value) || activeInstallGuide.value.methods[0])

const baseURL = computed(() => {
  const settings = app.publicSettings
  const base = (settings?.public_base_url || window.location.origin).replace(/\/$/, '')
  const path = workspace.value?.gateway_path || settings?.gateway_base_path || '/v1'
  if (/^https?:\/\//i.test(path)) return path.replace(/\/$/, '')
  return `${base}/${path.replace(/^\//, '')}`.replace(/\/$/, '')
})
const anthropicBaseURL = computed(() => baseURL.value.replace(/\/v1$/, ''))
const availableModels = computed(() => {
  const allModels = workspace.value?.models || []
  const allowed = selectedKey.value?.model_allowlist || []
  if (!allowed.length) return allModels
  const available = allowed.filter((model) => allModels.includes(model))
  return available.length ? available : allowed
})
const hasSecret = computed(() => keySecret.value.trim().length >= 8)
const canGenerate = computed(() => Boolean(selectedModel.value && hasSecret.value))
const displayKey = computed(() => keySecret.value.trim() || 'YOUR_ASTERROUTER_API_KEY')
const displayModel = computed(() => selectedModel.value || 'YOUR_MODEL')

const installCommand = computed(() => activeInstallOption.value.command)
const installNote = computed(() => activeInstallOption.value.note)

const generatedConfig = computed(() => {
  const key = displayKey.value
  const model = displayModel.value
  const slug = providerSlug.value
  if (configTarget.value === 'claude-code') {
    return JSON.stringify({ env: { ANTHROPIC_API_KEY: key, ANTHROPIC_BASE_URL: anthropicBaseURL.value, ANTHROPIC_MODEL: model } }, null, 2)
  }
  if (configTarget.value === 'codex') {
    return `model_provider = "${slug}"
model = "${model}"
model_reasoning_effort = "high"
disable_response_storage = true

[model_providers.${slug}]
name = "${siteName.value}"
base_url = "${baseURL.value}"
wire_api = "responses"
requires_openai_auth = true

# ${t('accountAccess.environmentVariable')}:
OPENAI_API_KEY=${key}`
  }
  if (configTarget.value === 'gemini-cli') {
    return `GEMINI_API_KEY=${key}
GOOGLE_GEMINI_BASE_URL=${anthropicBaseURL.value}
GEMINI_MODEL=${model}

# Start Gemini CLI after saving this file:
# gemini`
  }
  if (configTarget.value === 'cursor') {
    return `OPENAI_API_KEY=${key}
OPENAI_BASE_URL=${baseURL.value}
# Cursor Settings > Models > OpenAI API Key: ${key}
# Override OpenAI Base URL: ${baseURL.value}
# Model: ${model}`
  }
  if (configTarget.value === 'opencode') {
    return JSON.stringify({
      $schema: 'https://opencode.ai/config.json',
      model: `${slug}/${model}`,
      provider: {
        [slug]: {
          npm: '@ai-sdk/openai-compatible',
          name: siteName.value,
          options: { baseURL: baseURL.value, apiKey: key },
          models: { [model]: { name: model } }
        }
      }
    }, null, 2)
  }
  if (configTarget.value === 'openclaw') {
    return `OPENAI_API_KEY=${key}
OPENAI_BASE_URL=${baseURL.value}
OPENAI_MODEL=${model}

# Complete the local gateway setup after saving:
# openclaw onboard --install-daemon`
  }
  if (configTarget.value === 'hermes') {
    return `OPENAI_API_KEY=${key}
OPENAI_BASE_URL=${baseURL.value}
LLM_MODEL=${model}

# Start Hermes after saving:
# hermes`
  }
  if (configTarget.value === 'curl') {
    return `curl ${baseURL.value}/chat/completions \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '${JSON.stringify({ model, messages: [{ role: 'user', content: 'Hello from AsterRouter' }] })}'`
  }
  if (configTarget.value === 'python') {
    return `from openai import OpenAI

client = OpenAI(
    api_key="${key}",
    base_url="${baseURL.value}",
)

response = client.chat.completions.create(
    model="${model}",
    messages=[{"role": "user", "content": "Hello from AsterRouter"}],
)
print(response.choices[0].message.content)`
  }
  return `import anthropic

client = anthropic.Anthropic(
    api_key="${key}",
    base_url="${anthropicBaseURL.value}",
)

message = client.messages.create(
    model="${model}",
    max_tokens=1024,
    messages=[{"role": "user", "content": "Hello from AsterRouter"}],
)
print(message.content[0].text)`
})

const configFilename = computed(() => ({
  'claude-code': '~/.claude/settings.json',
  codex: '~/.codex/config.toml',
  'gemini-cli': '~/.gemini/.env',
  cursor: 'environment.env',
  opencode: '~/.opencode/config.json',
  openclaw: '~/.openclaw/.env',
  hermes: '~/.hermes/.env',
  curl: 'asterrouter-curl.sh',
  python: 'asterrouter-openai.py',
  anthropic: 'asterrouter-anthropic.py'
})[configTarget.value])

const protocolNotice = computed(() => {
  if (configTarget.value === 'codex') return t('accountAccess.responsesRequired')
  if (configTarget.value === 'claude-code' || configTarget.value === 'anthropic') return t('accountAccess.anthropicRequired')
  if (configTarget.value === 'gemini-cli') return t('accountAccess.geminiRequired')
  return ''
})

const ccSwitchConfig = computed(() => {
  const key = displayKey.value
  const model = displayModel.value
  if (ccSwitchTarget.value === 'claude-code') {
    return { env: { ANTHROPIC_API_KEY: key, ANTHROPIC_BASE_URL: anthropicBaseURL.value, ANTHROPIC_MODEL: model } }
  }
  if (ccSwitchTarget.value === 'gemini-cli') {
    return { env: { GEMINI_API_KEY: key, GOOGLE_GEMINI_BASE_URL: anthropicBaseURL.value, GEMINI_MODEL: model } }
  }
  return { env: { OPENAI_API_KEY: key, OPENAI_BASE_URL: baseURL.value, OPENAI_MODEL: model } }
})

const ccSwitchURL = computed(() => {
  const params = new URLSearchParams({
    resource: 'provider',
    app: ccSwitchApp[ccSwitchTarget.value],
    name: siteName.value,
    homepage: anthropicBaseURL.value,
    enabled: 'true',
    notes: `${siteName.value} - ${displayModel.value}`,
    configFormat: 'json',
    config: encodeBase64(JSON.stringify(ccSwitchConfig.value))
  })
  return `ccswitch://v1/import?${params.toString()}`
})
const maskedCCSwitchURL = computed(() => {
  const url = new URL(ccSwitchURL.value)
  url.searchParams.set('config', '[encoded-configuration]')
  return url.toString()
})

watch(selectedKeyID, (keyID) => {
  if (secretKeyID.value && secretKeyID.value !== keyID) {
    keySecret.value = ''
    secretKeyID.value = ''
  }
  ensureSelectedModel()
})
watch(availableModels, ensureSelectedModel)
watch(ccSwitchTarget, (target) => {
  const guide = installGuides.value[target]
  installMethod.value = guide.methods[0].id
  configTarget.value = guide.configTarget
})

function ensureSelectedModel() {
  if (!availableModels.value.includes(selectedModel.value)) selectedModel.value = availableModels.value[0] || ''
}
function encodeBase64(value: string) {
  const bytes = new TextEncoder().encode(value)
  let binary = ''
  bytes.forEach((byte) => { binary += String.fromCharCode(byte) })
  return window.btoa(binary)
}
function setGateError() {
  error.value = t('accountAccess.generateGate')
  document.querySelector('.selector-block')?.scrollIntoView({ behavior: 'smooth', block: 'center' })
}

async function loadWorkspace(preferredKeyID = selectedKeyID.value) {
  loading.value = true
  error.value = ''
  try {
    workspace.value = await getPortalWorkspace()
    const preferred = activeKeys.value.find((key) => key.id === preferredKeyID)
    selectedKeyID.value = preferred?.id || activeKeys.value[0]?.id || ''
    ensureSelectedModel()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}
async function createConfigKey() {
  if (!canManageKeys.value || !selectedModel.value) { setGateError(); return }
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await createPortalAPIKey({ name: `${siteName.value} ${t('accountAccess.configKeySuffix')}`, policy_id: '', model_allowlist: [selectedModel.value], qps_limit: 0, monthly_token_limit: 0, expires_at: '' })
    secretKeyID.value = result.record.id
    keySecret.value = result.key
    selectedKeyID.value = result.record.id
    notice.value = t('accountAccess.keyCreated')
    await loadWorkspace(result.record.id)
    emit('workspace-changed')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}
async function rotateSelectedKey() {
  const key = selectedKey.value
  if (!key || !canManageKeys.value) { setGateError(); return }
  if (!window.confirm(t('accountAccess.rotateConfirm', { name: key.name }))) return
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await rotatePortalAPIKey(key.id)
    secretKeyID.value = result.record.id
    keySecret.value = result.key
    selectedKeyID.value = result.record.id
    notice.value = t('accountAccess.keyRotated')
    await loadWorkspace(result.record.id)
    emit('workspace-changed')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}
async function copyText(value: string, field: string) {
  try {
    await navigator.clipboard.writeText(value)
  } catch {
    const textarea = document.createElement('textarea')
    textarea.value = value
    textarea.style.position = 'fixed'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    document.execCommand('copy')
    textarea.remove()
  }
  copiedField.value = field
  window.setTimeout(() => { if (copiedField.value === field) copiedField.value = '' }, 1600)
}
function downloadConfig() {
  const blob = new Blob([generatedConfig.value], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = configFilename.value.replace(/^.*\//, '')
  link.click()
  URL.revokeObjectURL(url)
}
function importToCCSwitch() {
  if (!canGenerate.value) { setGateError(); return }
  const link = document.createElement('a')
  link.href = ccSwitchURL.value
  link.click()
}
function keyLabel(key: APIKeyRecord) {
  return `${key.name} (${key.prefix || key.fingerprint})`
}

onMounted(loadWorkspace)
</script>

<template>
  <div class="account-access-workbench">
    <div v-if="error" class="notice">{{ error }}</div>
    <div v-if="notice" class="notice success">{{ notice }}</div>
    <div v-if="loading" class="panel access-loading">{{ t('common.loading') }}</div>

    <template v-else>
      <section class="panel selector-block">
        <div class="panel-header split-header">
          <div><h2>{{ t('accountAccess.setupTitle') }}</h2><p>{{ t('accountAccess.setupHelp') }}</p></div>
          <button class="icon-button" type="button" :title="t('common.refresh')" @click="loadWorkspace()"><RefreshCw :size="17" /></button>
        </div>
        <div class="panel-body selector-body">
          <label class="field"><span>{{ t('accountAccess.selectKey') }}</span><select v-model="selectedKeyID"><option v-if="!activeKeys.length" value="">{{ t('accountAccess.noKeys') }}</option><option v-for="key in activeKeys" :key="key.id" :value="key.id">{{ keyLabel(key) }}</option></select></label>
          <label class="field"><span>{{ t('accountAccess.selectModel') }}</span><select v-model="selectedModel"><option v-if="!availableModels.length" value="">{{ t('accountAccess.noModels') }}</option><option v-for="model in availableModels" :key="model" :value="model">{{ model }}</option></select></label>
          <label class="field secret-field"><span>{{ t('accountAccess.fullKey') }}</span><div class="secret-input-row"><input v-model="keySecret" :type="secretVisible ? 'text' : 'password'" autocomplete="off" spellcheck="false" :placeholder="t('accountAccess.fullKeyPlaceholder')" /><button class="icon-button" type="button" :title="secretVisible ? t('accountAccess.hideKey') : t('accountAccess.showKey')" @click="secretVisible = !secretVisible"><EyeOff v-if="secretVisible" :size="17" /><Eye v-else :size="17" /></button></div><small>{{ t('accountAccess.secretHelp') }}</small></label>
          <div class="account-access-actions"><button class="button" type="button" :disabled="saving || !canManageKeys" @click="createConfigKey"><WandSparkles :size="16" />{{ t('accountAccess.createConfigKey') }}</button><button class="button secondary" type="button" :disabled="saving || !canManageKeys" @click="rotateSelectedKey"><RotateCw :size="16" />{{ t('accountAccess.rotateKey') }}</button><span v-if="!canManageKeys" class="hint">{{ t('accountAccess.readOnly') }}</span></div>
          <div class="endpoint-band"><div class="endpoint-title"><strong>{{ t('accountAccess.endpoints') }}</strong><span>{{ t('accountAccess.dualEndpointHelp') }}</span></div><div class="endpoint-copy-grid"><div><span>{{ t('accountAccess.openAICompatible') }}</span><code>{{ baseURL }}</code><button class="icon-button" type="button" :title="t('common.copy')" @click="copyText(baseURL, 'base')"><Check v-if="copiedField === 'base'" :size="16" /><Copy v-else :size="16" /></button></div><div><span>{{ t('accountAccess.anthropicCompatible') }}</span><code>{{ anthropicBaseURL }}</code><button class="icon-button" type="button" :title="t('common.copy')" @click="copyText(anthropicBaseURL, 'anthropic-base')"><Check v-if="copiedField === 'anthropic-base'" :size="16" /><Copy v-else :size="16" /></button></div></div></div>
        </div>
      </section>

      <section class="panel method-panel cc-switch-panel">
        <div class="panel-header"><div><span class="access-kicker">{{ t('accountAccess.methodOne') }}</span><h2>{{ t('accountAccess.ccSwitchTitle') }} <span class="pill status-success">{{ t('accountAccess.recommended') }}</span></h2><p>{{ t('accountAccess.ccSwitchFullHelp') }}</p></div></div>
        <div class="panel-body method-body">
          <div class="install-step-box"><div class="setup-step"><div class="setup-step-copy"><strong>{{ t('accountAccess.installCCSwitchVersion') }}</strong><span>{{ t('accountAccess.installHelp') }}</span></div><div class="download-actions"><a class="button" href="https://github.com/farion1231/cc-switch/releases/latest" target="_blank" rel="noreferrer"><Download :size="16" />{{ t('accountAccess.downloadForPlatform') }}</a><details class="download-menu"><summary class="button secondary">{{ t('accountAccess.otherVersions') }}</summary><div><a href="https://github.com/farion1231/cc-switch/releases/latest" target="_blank" rel="noreferrer">macOS</a><a href="https://github.com/farion1231/cc-switch/releases/latest" target="_blank" rel="noreferrer">Windows</a><a href="https://github.com/farion1231/cc-switch/releases/latest" target="_blank" rel="noreferrer">Linux</a></div></details></div></div><span class="jump-note">{{ t('accountAccess.installedJump') }}</span><div class="compatibility-note"><AlertTriangle :size="16" /><span>{{ t('accountAccess.ccSwitchMacHelp') }}</span></div></div>
          <div class="import-step"><strong>{{ t('accountAccess.importStep') }}</strong><label>{{ t('accountAccess.importTarget') }}</label><div class="tool-tabs"><button v-for="target in ccSwitchTargets" :key="target.id" class="tool-tab" :class="{ active: ccSwitchTarget === target.id }" type="button" @click="ccSwitchTarget = target.id">{{ target.label }}</button></div></div>
          <div class="import-actions"><button class="button import-primary" type="button" @click="importToCCSwitch"><ExternalLink :size="16" />{{ t('accountAccess.importNow') }}</button><button class="button secondary" type="button" @click="copyText(ccSwitchURL, 'ccswitch')"><Check v-if="copiedField === 'ccswitch'" :size="16" /><Copy v-else :size="16" />{{ t('accountAccess.copyImportLink') }}</button></div>
          <p class="import-hint">{{ t('accountAccess.importActionHelp') }}</p>
          <div class="import-link-preview"><span>{{ t('accountAccess.importLink') }}</span><code>{{ maskedCCSwitchURL }}</code></div>
        </div>
      </section>

      <section class="panel method-panel tool-install-panel">
        <div class="panel-header"><div><span class="access-kicker command-kicker">{{ t('accountAccess.commandLine') }}</span><h2>{{ t('accountAccess.installTool', { tool: activeInstallGuide.name }) }} <span class="version-badge">{{ activeInstallGuide.version }}</span></h2><p>{{ activeInstallGuide.description }}</p></div></div>
        <div class="panel-body method-body">
          <div class="install-method"><strong>{{ t('accountAccess.installMethodA') }}</strong><div class="tool-tabs"><button v-for="method in activeInstallGuide.methods" :key="method.id" class="tool-tab" :class="{ active: installMethod === method.id }" type="button" @click="installMethod = method.id">{{ method.label }}</button></div><div class="command-box"><div><span>{{ installNote }}</span><button class="button secondary compact-button" type="button" @click="copyText(installCommand, 'install')"><Check v-if="copiedField === 'install'" :size="15" /><Copy v-else :size="15" />{{ t('common.copy') }}</button></div><code>{{ installCommand }}</code></div></div>
          <div v-for="group in activeInstallGuide.resources" :key="group.title" class="install-method"><strong>{{ group.title }}</strong><div class="download-button-grid"><a v-for="link in group.links" :key="link.label" class="button" :class="{ secondary: !link.primary }" :href="link.href" target="_blank" rel="noreferrer">{{ link.label }}</a></div><p>{{ group.help }}</p></div>
        </div>
      </section>

      <section class="panel method-panel generator-panel">
        <div class="panel-header"><div><span class="access-kicker generator-kicker">{{ t('accountAccess.methodTwo') }}</span><h2>{{ t('accountAccess.generateTitle') }}</h2><p>{{ t('accountAccess.generateHelp') }}</p></div></div>
        <div class="panel-body method-body"><div><label>{{ t('accountAccess.configTarget') }}</label><div class="tool-tabs config-tabs"><button v-for="target in configTargets" :key="target.id" class="tool-tab" :class="{ active: configTarget === target.id }" type="button" @click="configTarget = target.id">{{ target.label }}</button></div></div><div class="config-window"><div class="config-output-header"><div class="config-file-name"><span class="window-dots"><i></i><i></i><i></i></span><code>{{ configFilename }}</code></div><div><button class="button secondary compact-button" type="button" @click="copyText(generatedConfig, 'config')"><Check v-if="copiedField === 'config'" :size="15" /><Copy v-else :size="15" />{{ t('common.copy') }}</button><button class="button secondary compact-button" type="button" @click="downloadConfig"><Download :size="15" />{{ t('common.download') }}</button></div></div><pre class="generated-config">{{ generatedConfig }}</pre></div><p v-if="protocolNotice" class="protocol-warning"><AlertTriangle :size="15" />{{ protocolNotice }}</p><p v-if="!canGenerate" class="config-gate"><AlertTriangle :size="15" />{{ t('accountAccess.generateGate') }}</p></div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.account-access-workbench { display: grid; gap: 16px; }
.access-loading { min-height: 220px; display: grid; place-items: center; color: var(--text-muted); }
.panel { border-radius: 8px; }
.panel-header { min-height: 66px; }
.panel-header > div { display: grid; gap: 3px; }
.panel-header h2 { display: flex; align-items: center; flex-wrap: wrap; gap: 7px; font-size: 15px; }
.panel-header p { margin: 0; color: var(--text-muted); font-size: 12px; }
.selector-body, .method-body { gap: 16px; }
.secret-input-row { display: grid; grid-template-columns: minmax(0, 1fr) 38px; gap: 8px; }
.secret-input-row .icon-button { width: 38px; height: 38px; }
.secret-field small { margin-top: 3px; color: var(--text-muted); }
.account-access-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.endpoint-band { display: grid; gap: 10px; padding: 14px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface-subtle); }
.endpoint-title { display: grid; gap: 2px; }
.endpoint-title span { color: var(--text-muted); font-size: 11px; }
.endpoint-copy-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
.endpoint-copy-grid > div { display: grid; grid-template-columns: auto minmax(0, 1fr) 32px; align-items: center; gap: 8px; min-width: 0; padding: 8px 10px; border: 1px solid var(--border); border-radius: 7px; background: var(--surface); }
.endpoint-copy-grid span { color: var(--text-muted); font-size: 11px; }
.endpoint-copy-grid code { min-width: 0; overflow: hidden; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.endpoint-copy-grid .icon-button { width: 30px; height: 30px; }
.access-kicker { color: var(--info); font-size: 12px; font-weight: 750; }
.command-kicker { color: var(--success); }
.generator-kicker { color: var(--primary-700); }
.cc-switch-panel { border-color: color-mix(in srgb, var(--info) 32%, var(--border)); }
.tool-install-panel { border-color: color-mix(in srgb, var(--success) 28%, var(--border)); }
.install-step-box { display: grid; gap: 10px; padding: 14px; border: 1px solid color-mix(in srgb, var(--warning) 25%, var(--border)); border-radius: 8px; background: color-mix(in srgb, var(--warning-bg) 45%, var(--surface)); }
.setup-step { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.setup-step-copy { display: grid; gap: 3px; }
.setup-step-copy span, .jump-note, .install-method p { color: var(--text-muted); font-size: 12px; }
.download-actions, .download-button-grid { display: flex; flex-wrap: wrap; gap: 7px; }
.download-menu { position: relative; }
.download-menu summary { list-style: none; }
.download-menu summary::-webkit-details-marker { display: none; }
.download-menu > div { position: absolute; z-index: 4; top: calc(100% + 5px); right: 0; display: grid; min-width: 150px; padding: 6px; border: 1px solid var(--border); border-radius: 7px; background: var(--surface); box-shadow: var(--shadow-md); }
.download-menu a { padding: 8px; border-radius: 5px; color: var(--text-secondary); font-size: 12px; text-decoration: none; }
.download-menu a:hover { background: var(--surface-hover); }
.compatibility-note, .protocol-warning, .config-gate { display: flex; align-items: flex-start; gap: 7px; margin: 0; color: var(--warning); font-size: 12px; }
.compatibility-note { padding: 9px 10px; border: 1px solid color-mix(in srgb, var(--warning) 30%, var(--border)); border-radius: 7px; background: var(--warning-bg); }
.import-step, .install-method { display: grid; gap: 10px; }
.import-step label, .generator-panel label { color: var(--text-muted); font-size: 12px; }
.tool-tabs { display: flex; flex-wrap: wrap; gap: 7px; }
.tool-tab { min-height: 34px; padding: 6px 12px; border: 1px solid var(--border); border-radius: 7px; background: var(--surface); color: var(--text); cursor: pointer; font-size: 12px; font-weight: 650; }
.tool-tab:hover, .tool-tab.active { border-color: var(--primary-600); background: var(--primary-600); color: #fff; }
.import-actions { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; }
.import-primary { justify-content: center; }
.import-hint { margin: -6px 0 0; color: var(--warning); font-size: 11px; }
.import-link-preview { display: grid; gap: 5px; min-width: 0; padding: 12px; border-radius: 7px; background: var(--surface-subtle); }
.import-link-preview span { color: var(--text-muted); font-size: 11px; }
.import-link-preview code { overflow: hidden; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.version-badge { padding: 2px 6px; border-radius: 5px; background: var(--success-bg); color: var(--success); font-size: 10px; }
.command-box { overflow: hidden; border: 1px solid var(--border); border-radius: 8px; }
.command-box > div { display: flex; min-height: 42px; align-items: center; justify-content: space-between; gap: 10px; padding: 6px 12px; border-bottom: 1px solid var(--border); background: var(--surface-subtle); }
.command-box span { color: var(--text-muted); font-size: 11px; }
.command-box > code { display: block; padding: 14px; overflow-x: auto; color: var(--text); font-size: 12px; white-space: nowrap; }
.compact-button { min-height: 32px; padding: 0 10px; font-size: 11px; }
.install-method + .install-method { padding-top: 14px; border-top: 1px solid var(--border); }
.download-button-grid .button { min-height: 34px; padding: 0 11px; font-size: 11px; text-decoration: none; }
.config-tabs { margin-top: 7px; }
.config-window { overflow: hidden; border: 1px solid var(--border); border-radius: 8px; }
.config-output-header { display: flex; min-height: 48px; align-items: center; justify-content: space-between; gap: 12px; padding: 7px 10px; border-bottom: 1px solid var(--border); background: var(--surface-subtle); }
.config-output-header > div { display: flex; align-items: center; gap: 7px; }
.config-file-name { min-width: 0; }
.config-file-name code { overflow: hidden; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.window-dots { display: flex; gap: 4px; }
.window-dots i { width: 7px; height: 7px; border-radius: 50%; background: var(--border-strong); }
.window-dots i:first-child { background: #ef4444; }
.window-dots i:nth-child(2) { background: #f59e0b; }
.window-dots i:nth-child(3) { background: #22c55e; }
.generated-config { max-height: 440px; min-height: 260px; margin: 0; padding: 16px; background: #101827; color: #d9e6f7; overflow: auto; font: 12px/1.65 ui-monospace, SFMono-Regular, Menlo, monospace; white-space: pre-wrap; overflow-wrap: anywhere; }
.config-gate { color: var(--text-muted); }
@media (max-width: 780px) {
  .endpoint-copy-grid { grid-template-columns: 1fr; }
  .setup-step { align-items: stretch; flex-direction: column; }
  .download-actions { align-items: flex-start; }
  .download-menu > div { right: auto; left: 0; }
  .import-actions { grid-template-columns: 1fr; }
}
@media (max-width: 520px) {
  .endpoint-copy-grid > div { grid-template-columns: 1fr 32px; }
  .endpoint-copy-grid span { grid-column: 1 / -1; }
  .command-box > code { white-space: pre-wrap; overflow-wrap: anywhere; }
  .config-output-header { align-items: flex-start; flex-direction: column; }
  .config-output-header > div:last-child { width: 100%; flex-wrap: wrap; }
}
</style>
