import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import StateBlock from './StateBlock.vue'
import { useUIStore } from '../stores/ui'

describe('StateBlock', () => {
  it('offers a retry action for errors', async () => {
    const wrapper = mount(StateBlock, { props: { state: 'error', title: '加载失败' }, global: { plugins: [createPinia()] } })
    expect(wrapper.text()).toContain('加载失败')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })

  it('renders an accessible empty state without a retry button', () => {
    const wrapper = mount(StateBlock, { props: { state: 'empty', title: '暂无内容' }, global: { plugins: [createPinia()] } })
    expect(wrapper.text()).toContain('暂无内容')
    expect(wrapper.find('button').exists()).toBe(false)
  })

  it('uses the semantic mascot scene for a local organizing state', () => {
    const pinia = createPinia()
    const ui = useUIStore(pinia)
    ui.setSkin('mascot')
    const wrapper = mount(StateBlock, {
      props: { state: 'empty', scene: 'organizing', title: '还没有扫描到本地番剧' },
      global: { plugins: [pinia] },
    })
    expect(wrapper.get('[data-mascot-scene="organizing"]').attributes('src')).toBe('/mascot/current/actions/organizing.png')
  })
})
