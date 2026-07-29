import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PosterCard from './PosterCard.vue'

describe('PosterCard', () => {
  it('renders status badges inside the card body and opens from the whole card', async () => {
    const wrapper = mount(PosterCard, {
      props: {
        title: '测试番剧',
        image: '',
        badges: ['已订阅', '本地可用'],
        openable: true,
        openLabel: '查看详情',
      },
      slots: {
        default: '<button data-testid="inner-action">卡片操作</button>',
      },
    })

    const body = wrapper.get('article > div.p-3')
    expect(body.text()).toContain('已订阅')
    expect(body.text()).toContain('本地可用')
    expect(wrapper.find('.absolute .badge').exists()).toBe(false)
    expect(wrapper.text()).toContain('查看详情')

    await wrapper.get('h3').trigger('click')
    expect(wrapper.emitted('open')).toHaveLength(1)

    await wrapper.get('[data-testid="inner-action"]').trigger('click')
    expect(wrapper.emitted('open')).toHaveLength(1)
  })
})
