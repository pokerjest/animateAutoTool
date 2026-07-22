import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import StateBlock from './StateBlock.vue'

describe('StateBlock', () => {
  it('offers a retry action for errors', async () => {
    const wrapper = mount(StateBlock, { props: { state: 'error', title: '加载失败' } })
    expect(wrapper.text()).toContain('加载失败')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })

  it('renders an accessible empty state without a retry button', () => {
    const wrapper = mount(StateBlock, { props: { state: 'empty', title: '暂无内容' } })
    expect(wrapper.text()).toContain('暂无内容')
    expect(wrapper.find('button').exists()).toBe(false)
  })
})
