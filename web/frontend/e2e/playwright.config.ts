import { defineConfig } from '@playwright/test'

const baseURL = process.env.PACKAGE_E2E_BASE_URL
if (!baseURL) throw new Error('PACKAGE_E2E_BASE_URL is required')

export default defineConfig({
  testDir: '.',
  testMatch: 'package.spec.ts',
  timeout: 60_000,
  use: {
    baseURL,
    headless: true,
    actionTimeout: 10_000,
    navigationTimeout: 15_000,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    video: 'retain-on-failure',
  },
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  outputDir: '../test-results/package-e2e',
  reporter: [
    ['list'],
    ['html', { open: 'never', outputFolder: '../playwright-report/package-e2e' }],
  ],
})
