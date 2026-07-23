import { describe, expect, it } from 'vitest'
import { buildMikanSelection, switchToMikanAggregate } from './mikanSubscription'

const anime = { mikan_id: '3141', title: '测试番剧', image: 'poster.jpg', season: '2026 夏季番组' }

describe('Mikan subscription strategy', () => {
  it('builds a subgroup feed with aggregate fallback and escaped filter', () => {
    const result = buildMikanSelection(anime, { id: '583', name: 'A+B [1080p]', is_all: false })
    expect(result).toMatchObject({
      rss_url: 'https://mikanani.me/RSS/Bangumi?bangumiId=3141&subgroupid=583',
      backup_rss_url: 'https://mikanani.me/RSS/Bangumi?bangumiId=3141',
      subtitle_group: 'A+B [1080p]',
      filter_rule: 'A\\+B \\[1080p\\]',
      allow_multi_subgroup: false,
    })
  })

  it('keeps the all-subgroups option free of accidental filters', () => {
    const result = buildMikanSelection(anime, { id: '', name: '全部字幕组', is_all: true })
    expect(result).toMatchObject({
      rss_url: 'https://mikanani.me/RSS/Bangumi?bangumiId=3141',
      backup_rss_url: '',
      subtitle_group: '',
      filter_rule: '',
      allow_multi_subgroup: true,
    })
  })

  it('switches to the aggregate feed without deleting a custom filter', () => {
    const draft = {
      mikan_id: '3141',
      subtitle_group: 'ANi',
      filter_rule: '1080p',
      rss_url: 'https://mikanani.me/RSS/Bangumi?bangumiId=3141&subgroupid=583',
      backup_rss_url: 'https://mikanani.me/RSS/Bangumi?bangumiId=3141',
      allow_multi_subgroup: true,
    }
    switchToMikanAggregate(draft)
    expect(draft.rss_url).toBe('https://mikanani.me/RSS/Bangumi?bangumiId=3141')
    expect(draft.backup_rss_url).toBe('')
    expect(draft.subtitle_group).toBe('')
    expect(draft.filter_rule).toBe('1080p')
  })

  it('clears the selector-generated filter in aggregate mode', () => {
    const draft = {
      mikan_id: '3141',
      subtitle_group: 'ANi',
      filter_rule: 'ANi',
      rss_url: 'subgroup-feed',
      backup_rss_url: 'base-feed',
      allow_multi_subgroup: true,
    }
    switchToMikanAggregate(draft)
    expect(draft.filter_rule).toBe('')
  })
})
