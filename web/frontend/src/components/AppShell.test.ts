import { afterEach, describe, expect, it } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import AppShell from './AppShell.vue'
import { useUIStore } from '../stores/ui'

const Page = defineComponent({ template: '<div>页面</div>' })

afterEach(() => {
  localStorage.clear()
  document.body.innerHTML = ''
})

describe('management workspace navigation', () => {
  it('keeps management tools while the floating pet replaces the assistant navigation entry', async () => {
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
          AssistantWidget: true,
          TaskCenter: true,
        },
      },
    })

    const links = wrapper.findAll('a').map(link => ({
      href: link.attributes('href'),
      label: link.text(),
    }))

    expect(links).toEqual(expect.arrayContaining([
      expect.objectContaining({ href: '/health', label: '系统健康' }),
      expect.objectContaining({ href: '/backup', label: '备份恢复' }),
      expect.objectContaining({ href: '/settings', label: '系统设置' }),
    ]))
    expect(links).not.toEqual(expect.arrayContaining([
      expect.objectContaining({ href: '/assistant' }),
    ]))
    expect(wrapper.find('assistant-widget-stub').exists()).toBe(false)
    expect(wrapper.find('.sidebar-footer .sidebar-account-row').exists()).toBe(true)
    expect(wrapper.find('.sidebar-footer .sidebar-version-row').text()).toContain('当前版本')

    const mobilePrimaryLinks = wrapper
      .get('nav[aria-label="移动端主导航"]')
      .findAll('a')
      .map(link => link.attributes('href'))
    expect(mobilePrimaryLinks).toEqual(['/', '/calendar', '/subscriptions', '/library'])
    expect(wrapper.findAll('header .app-header-mobile-menu')).toHaveLength(1)
    expect(wrapper.findAll('header .app-header-sidebar-toggle')).toHaveLength(1)

    await wrapper.get('header .app-header-mobile-menu').trigger('click')
    expect(wrapper.findAll('h2').some(heading => heading.text() === '所有功能')).toBe(true)

    wrapper.unmount()
  })

  it('lets desktop users collapse and restore the sidebar from the header', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: Page }],
    })
    await router.push('/')
    await router.isReady()

    const pinia = createPinia()
    const wrapper = mount(AppShell, {
      global: {
        plugins: [pinia, router],
        stubs: {
          AppBackground: true,
          PlaybackHost: true,
          AssistantWidget: true,
          TaskCenter: true,
        },
      },
    })

    const ui = pinia.state.value.ui as { desktopSidebarCollapsed: boolean }
    const toggle = wrapper.get('button[aria-label="收起侧边栏"]')
    await toggle.trigger('click')
    expect(ui.desktopSidebarCollapsed).toBe(true)
    expect(wrapper.find('button[aria-label="展开侧边栏"]').exists()).toBe(true)
    expect(localStorage.getItem('animate-desktop-sidebar-collapsed')).toBe('true')

    await wrapper.get('button[aria-label="展开侧边栏"]').trigger('click')
    expect(ui.desktopSidebarCollapsed).toBe(false)
    expect(localStorage.getItem('animate-desktop-sidebar-collapsed')).toBe('false')

    wrapper.unmount()
  })

  it('uses only the current mascot asset when the mascot skin is selected', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: Page }],
    })
    await router.push('/')
    await router.isReady()

    const pinia = createPinia()
    useUIStore(pinia).setSkin('mascot')
    const wrapper = mount(AppShell, {
      global: {
        plugins: [pinia, router],
        stubs: {
          AppBackground: true,
          PlaybackHost: true,
          AssistantWidget: true,
          TaskCenter: true,
        },
      },
    })

    expect(wrapper.get('.desktop-sidebar img').attributes('src')).toBe('/mascot/current/expressions/wink.png')
    wrapper.unmount()
  })
})
