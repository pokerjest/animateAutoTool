import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AsyncButton from './AsyncButton.vue'

describe('AsyncButton', () => {
  it('shows progress semantics and prevents duplicate clicks while loading', async () => {
    const click = vi.fn()
    const wrapper = mount(AsyncButton, {
      props: { loading: true, loadingLabel: '刷新中…' },
      attrs: { onClick: click },
      slots: { default: '刷新' },
    })

    const button = wrapper.get('button')
    expect(button.attributes('aria-busy')).toBe('true')
    expect(button.attributes()).toHaveProperty('disabled')
    expect(button.text()).toContain('刷新中…')
    await button.trigger('click')
    expect(click).not.toHaveBeenCalled()
  })

  it('preserves the requested submit type when idle', () => {
    const wrapper = mount(AsyncButton, { props: { type: 'submit' }, slots: { default: '保存' } })
    expect(wrapper.get('button').attributes('type')).toBe('submit')
    expect(wrapper.text()).toContain('保存')
  })
})
