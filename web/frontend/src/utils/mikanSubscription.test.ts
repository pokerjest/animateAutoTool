import { describe, expect, it } from 'vitest'
import { buildMikanSelection, mikanEpisodeMatchesFilters, switchToMikanAggregate } from './mikanSubscription'

const anime = { mikan_id: '3141', bangumi_subject_id: '', title: '测试番剧', image: 'poster.jpg', is_subscribed: false, is_local: false, season: '2026 夏季番组' }

describe('Mikan subscription strategy', () => {
  it('builds a subgroup feed without a redundant subgroup filter', () => {
    const result = buildMikanSelection(anime, { id: '583', name: 'A+B [1080p]', is_all: false }, {
      filter_rule: '1080[Pp].*(CHS|简中)',
      exclude_rule: '(720[Pp]|合集)',
      resolution_filter: '1080p',
      subtitle_language: 'chs',
    })
    expect(result).toMatchObject({
      rss_url: 'https://mikanani.me/RSS/Bangumi?bangumiId=3141&subgroupid=583',
      backup_rss_url: '',
      subtitle_group: 'A+B [1080p]',
      filter_rule: '1080[Pp].*(CHS|简中)',
      exclude_rule: '(720[Pp]|合集)',
      resolution_filter: '1080p',
      subtitle_language: 'chs',
      allow_multi_subgroup: false,
    })
  })

  it('matches resolution and subtitle language without confusing file size for GB subtitles', () => {
    const episode = (title: string, resolution: string) => ({
      title,
      resolution,
      episode_num: '01',
      sub_group: 'Group',
      pub_date: '2026-07-23T00:00:00Z',
    })
    const simplified = episode('[ANi] Test - 01 [1080P][CHS]', '1080p')
    const bilingual = episode('[Group] Test - 01 [1080P][简繁内封字幕]', '')
    const fileSize = episode('[Group] Test - 01 [1080P][1.5GB]', '1080p')
    const spacedFileSize = episode('[Group] Test - 01 [1080P][1.5 GB]', '1080p')

    expect(mikanEpisodeMatchesFilters(simplified, '1080p', 'chs')).toBe(true)
    expect(mikanEpisodeMatchesFilters(simplified, '1080p', 'chs', '1080[Pp].*CHS')).toBe(true)
    expect(mikanEpisodeMatchesFilters(simplified, '1080p', 'chs', '', '(CHS|简中)')).toBe(false)
    expect(mikanEpisodeMatchesFilters(simplified, '720p', 'chs')).toBe(false)
    expect(mikanEpisodeMatchesFilters(bilingual, '1080p', 'chs_cht')).toBe(true)
    expect(mikanEpisodeMatchesFilters(fileSize, '1080p', 'chs')).toBe(false)
    expect(mikanEpisodeMatchesFilters(spacedFileSize, '1080p', 'chs')).toBe(false)
  })

  it('keeps the all-subgroups option free of accidental filters', () => {
    const result = buildMikanSelection(anime, { id: '', name: '全部字幕组', is_all: true })
    expect(result).toMatchObject({
      rss_url: 'https://mikanani.me/RSS/Bangumi?bangumiId=3141',
      backup_rss_url: '',
      subtitle_group: '',
      filter_rule: '',
      exclude_rule: '',
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
