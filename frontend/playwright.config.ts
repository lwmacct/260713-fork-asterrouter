import { defineConfig, devices } from '@playwright/test'

const frontendPort = process.env.ASTER_E2E_FRONTEND_PORT || '15173'
const backendPort = process.env.ASTER_E2E_BACKEND_PORT || '18080'
const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
const externalURL = process.env.ASTER_E2E_EXTERNAL_URL
const baseURL = externalURL || `http://127.0.0.1:${frontendPort}`
const artifactDir = process.env.ASTER_E2E_ARTIFACT_DIR
const chromiumChannel = process.env.ASTER_E2E_CHROMIUM_CHANNEL
const artifactPath = (relative: string) => artifactDir ? `${artifactDir}/${relative}` : `./${relative}`
const chromiumUse = chromiumChannel ? { channel: chromiumChannel } : {}
const videoMode = process.env.ASTER_E2E_VIDEO === 'off' ? 'off' as const : 'retain-on-failure' as const

export default defineConfig({
  testDir: './e2e',
  outputDir: artifactPath('test-results'),
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  // Setup requires a dedicated empty runtime. It is executed by
  // test-setup-browser-journey.sh, not against the reusable demo server.
  grepInvert: process.env.ASTER_E2E_INCLUDE_SETUP === '1' ? undefined : /@setup/,
  retries: Number(process.env.ASTER_E2E_RETRIES || '0'),
  // Several journeys temporarily update global registration settings while
  // creating isolated synthetic users. Keep the default deterministic; an
  // explicit worker count is required for a deliberate parallelism exercise.
  workers: Number(process.env.ASTER_E2E_WORKERS || '1'),
  reporter: process.env.CI
    ? [['line'], ['html', { outputFolder: artifactPath('playwright-report'), open: 'never' }], ['junit', { outputFile: artifactPath('test-results/junit.xml') }]]
    : [['list'], ['html', { outputFolder: artifactPath('playwright-report'), open: 'never' }]],
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: videoMode
  },
  expect: {
    timeout: 10_000
  },
  timeout: 30_000,
  projects: [
    {
      name: 'chromium-desktop',
      use: { ...devices['Desktop Chrome'], ...chromiumUse, viewport: { width: 1440, height: 900 } }
    },
    {
      name: 'chromium-compact',
      use: { ...devices['Desktop Chrome'], ...chromiumUse, viewport: { width: 1280, height: 800 } }
    },
    {
      name: 'chromium-mobile',
      use: { ...devices['Pixel 7'], ...chromiumUse, viewport: { width: 390, height: 844 } }
    },
    ...(process.env.ASTER_E2E_ALL_BROWSERS
      ? [
          { name: 'firefox-desktop', use: { ...devices['Desktop Firefox'], viewport: { width: 1440, height: 900 } } },
          { name: 'webkit-desktop', use: { ...devices['Desktop Safari'], viewport: { width: 1440, height: 900 } } }
        ]
      : [])
  ],
  webServer: externalURL
    ? undefined
    : {
        command: [
          'ASTER_DEV_KILL_OCCUPIED=0',
          `ASTER_DEV_BACKEND_PORT=${backendPort}`,
          `ASTER_DEV_FRONTEND_PORT=${frontendPort}`,
          `VITE_DEV_PROXY_TARGET=http://127.0.0.1:${backendPort}`,
          `ASTER_E2E_UPSTREAM_PORT=${upstreamPort}`,
          'ASTERROUTER_SERVER_BOOTSTRAP_DEMO_MODE=true',
          'ASTERROUTER_SERVER_SECURITY_SECRET_KEY=asterrouter-e2e-test-secret',
          'bash ../scripts/e2e.sh'
        ].join(' '),
        url: `http://127.0.0.1:${backendPort}/ready`,
        reuseExistingServer: false,
        timeout: 120_000,
        stdout: 'pipe',
        stderr: 'pipe'
      }
})
