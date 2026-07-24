import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { Subscription } from '../../api/types'
import SubscriptionCard from './SubscriptionCard.vue'
import SubscriptionOverview from './SubscriptionOverview.vue'

function subscription(overrides: Partial<Subscription> = {}): Subscription {
  return {
    ID: 7,
    title: '测试番剧',
    rss_url: 'https://example.test/rss',
    image: '',
    subtitle_group: '测试字幕组',
    season: '',
    filter_rule: '',
    exclude_rule: '',
    expected_episodes: 12,
    downloaded_count: 3,
    is_active: true,
    last_run_status: '',
    last_run_summary: '',
    last_error_display: '',
    ...overrides,
  }
}

describe('subscription presentational components', () => {
  it('emits filter and search updates from the overview', async () => {
    const wrapper = mount(SubscriptionOverview, {
      props: {
        trend: {
          checked_count: 5,
          success_count: 3,
          warning_count: 1,
          error_count: 1,
          active_issue_count: 2,
        },
        search: '',
        filter: 'all',
      },
    })

    await wrapper.get('input[aria-label="搜索订阅"]').setValue('字幕组')
    await wrapper.findAll('button').find(button => button.text().includes('有异常'))!.trigger('click')

    expect(wrapper.emitted('update:search')).toEqual([['字幕组']])
    expect(wrapper.emitted('update:filter')).toEqual([['issues']])
    expect(wrapper.text()).toContain('最近检查')
  })

  it('keeps card loading state and repair actions behind a small event contract', async () => {
    const isBusy = vi.fn((...keys: string[]) => keys.includes('run-7'))
    const wrapper = mount(SubscriptionCard, {
      props: {
        item: subscription({
          has_repair_actions: true,
          can_clear_filter: true,
        }),
        isBusy,
      },
    })

    expect(wrapper.text()).toContain('检查中…')
    await wrapper.findAll('button').find(button => button.text().includes('清空过滤'))!.trigger('click')
    await wrapper.get('button[aria-label="编辑订阅"]').trigger('click')

    expect(wrapper.emitted('repair')).toEqual([['clear-filter']])
    expect(wrapper.emitted('edit')).toHaveLength(1)
    expect(isBusy).toHaveBeenCalledWith('run-7', 'subscription-7')
  })
})
