import { expect, test } from '@playwright/test'

test('packaged binary serves the embedded app and completes first-run setup', async ({ page }) => {
  const runtimeErrors: string[] = []
  page.on('console', message => {
    if (message.type() === 'error') runtimeErrors.push(`console: ${message.text()}`)
  })
  page.on('pageerror', error => runtimeErrors.push(`page: ${error.message}`))
  page.on('requestfailed', request => {
    const failure = request.failure()?.errorText || 'failed'
    if (request.url().endsWith('/api/v1/events') && failure.includes('ERR_ABORTED')) return
    if (request.url().includes('/api/v1/settings/updater/releases') && failure.includes('ERR_ABORTED')) return
    runtimeErrors.push(`request: ${request.url()} (${failure})`)
  })

  const initialResponse = await page.goto('/')
  expect(initialResponse?.status()).toBe(200)
  await expect(page).toHaveTitle(/登录 · AnimateTool/)
  await expect(page.getByRole('heading', { name: '创建你的管理员账户' })).toBeVisible()

  await page.getByRole('button', { name: /开始初始化/ }).click()
  await expect(page).toHaveURL(/\/setup$/)
  await expect(page.getByRole('heading', { name: '三步完成初始化' })).toBeVisible()

  await page.getByLabel(/^新密码/).fill('package-e2e-password-2026')
  await page.getByLabel('确认密码', { exact: true }).fill('package-e2e-password-2026')
  await page.getByRole('button', { name: '下一步' }).click()

  await page.getByRole('button', { name: /外部服务/ }).click()
  await page.getByLabel('Web UI 地址').fill('http://127.0.0.1:65534')
  await page.getByRole('button', { name: '下一步' }).click()
  await page.getByRole('button', { name: '完成初始化' }).click()

  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { name: '今天也在安心追番' })).toBeVisible()

  const sessionResponse = await page.request.get('/api/v1/session')
  expect(sessionResponse.ok()).toBeTruthy()
  const session = await sessionResponse.json()
  expect(session.data).toMatchObject({
    authenticated: true,
    setup_pending: false,
    version: process.env.PACKAGE_E2E_VERSION,
  })

  const deepLinkResponse = await page.goto('/subscriptions')
  expect(deepLinkResponse?.status()).toBe(200)
  await expect(
    page.locator('#main-content').getByRole('heading', { name: '订阅管理', level: 2 }),
  ).toBeVisible()
  await page.reload()
  await expect(
    page.locator('#main-content').getByRole('heading', { name: '订阅管理', level: 2 }),
  ).toBeVisible()
  await expect(page.getByLabel('搜索订阅')).toBeVisible()

  const playerResponse = await page.goto('/player')
  expect(playerResponse?.status()).toBe(200)
  await expect(page.getByText('还没有可继续观看的内容')).toBeVisible()
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('navigation', { name: '移动端主导航' })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBeTruthy()

  expect(runtimeErrors, runtimeErrors.join('\n')).toEqual([])
})
