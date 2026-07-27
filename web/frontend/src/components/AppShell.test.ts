import { afterEach, describe, expect, it } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import AppShell from './AppShell.vue'

const Page = defineComponent({ template: '<div>页面</div>' })

afterEach(() => {
  localStorage.clear()
  document.body.innerHTML = ''
})

describe('management workspace navigation', () => {
  it('keeps the existing assistant, health, backup, and settings entries', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: Page },
        { path: '/calendar', component: Page },
        { path: '/subscriptions', component: Page },
        { path: '/assistant', component: Page },
        { path: '/library', component: Page },
        { path: '/local-anime', component: Page },
        { path: '/health', component: Page },
        { path: '/backup', component: Page },
        { path: '/settings', component: Page },
      ],
    })
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppShell, {
      global: {
        plugins: [createPinia(), router],
        stubs: {
          AppBackground: true,
          PlaybackHost: true,
          TaskCenter: true,
        },
      },
    })

    const links = wrapper.findAll('a').map(link => ({
      href: link.attributes('href'),
      label: link.text(),
    }))

    expect(links).toEqual(expect.arrayContaining([
      expect.objectContaining({ href: '/assistant', label: 'AI 助手' }),
      expect.objectContaining({ href: '/health', label: '系统健康' }),
      expect.objectContaining({ href: '/backup', label: '备份恢复' }),
      expect.objectContaining({ href: '/settings', label: '系统设置' }),
    ]))

    const mobilePrimaryLinks = wrapper
      .get('nav[aria-label="移动端主导航"]')
      .findAll('a')
      .map(link => link.attributes('href'))
    expect(mobilePrimaryLinks).toEqual(['/', '/calendar', '/subscriptions', '/library'])

    wrapper.unmount()
  })
})
