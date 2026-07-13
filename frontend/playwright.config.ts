import { defineConfig, devices } from '@playwright/test'

const frontendPort = process.env.ASTER_E2E_FRONTEND_PORT || '15173'
const backendPort = process.env.ASTER_E2E_BACKEND_PORT || '18080'
const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
const externalURL = process.env.ASTER_E2E_EXTERNAL_URL
const baseURL = externalURL || `http://127.0.0.1:${frontendPort}`

export default defineConfig({
  testDir: './e2e',
  outputDir: './test-results',
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: Number(process.env.ASTER_E2E_RETRIES || '0'),
  workers: process.env.CI ? 2 : undefined,
  reporter: process.env.CI
    ? [['line'], ['html', { outputFolder: 'playwright-report', open: 'never' }], ['junit', { outputFile: 'test-results/junit.xml' }]]
    : [['list'], ['html', { outputFolder: 'playwright-report', open: 'never' }]],
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure'
  },
  expect: {
    timeout: 10_000
  },
  timeout: 30_000,
  projects: [
    {
      name: 'chromium-desktop',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } }
    },
    {
      name: 'chromium-compact',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } }
    },
    {
      name: 'chromium-mobile',
      use: { ...devices['Pixel 7'], viewport: { width: 390, height: 844 } }
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
          'ASTER_DEMO_MODE=true',
          'ASTER_SECRET_KEY=asterrouter-e2e-test-secret',
          'bash ../scripts/e2e.sh'
        ].join(' '),
        url: `http://127.0.0.1:${backendPort}/ready`,
        reuseExistingServer: false,
        timeout: 120_000,
        stdout: 'pipe',
        stderr: 'pipe'
      }
})
