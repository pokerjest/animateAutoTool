import type {
  MikanDiscoveryItem,
  MikanEpisode,
  MikanSubgroup,
  MikanSubscriptionSelection,
  ResolutionFilter,
  SubtitleLanguage,
} from '../api/types'

export function escapeMikanFilter(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

export function mikanBaseRSS(mikanID: string) {
  return `https://mikanani.me/RSS/Bangumi?bangumiId=${encodeURIComponent(mikanID)}`
}

export function buildMikanSelection(
  anime: MikanDiscoveryItem & { season: string },
  group: MikanSubgroup,
  filters: { resolution_filter?: ResolutionFilter; subtitle_language?: SubtitleLanguage } = {},
): MikanSubscriptionSelection {
  const baseRSS = mikanBaseRSS(anime.mikan_id)
  const isAll = group.is_all || !group.id
  return {
    mikan_id: anime.mikan_id,
    title: anime.title,
    image: anime.image,
    season: anime.season,
    subgroup_id: isAll ? '' : group.id,
    subtitle_group: isAll ? '' : group.name,
    rss_url: isAll ? baseRSS : `${baseRSS}&subgroupid=${encodeURIComponent(group.id)}`,
    backup_rss_url: isAll ? '' : baseRSS,
    filter_rule: isAll ? '' : escapeMikanFilter(group.name),
    resolution_filter: filters.resolution_filter || '',
    subtitle_language: filters.subtitle_language || '',
    allow_multi_subgroup: isAll,
  }
}

function normalizedResolution(episode: MikanEpisode): ResolutionFilter {
  const source = `${episode.resolution || ''} ${episode.title}`.toLowerCase()
  if (/(^|[^a-z0-9])(2160p|4k|uhd|3840x2160)([^a-z0-9]|$)/i.test(source)) return '2160p'
  if (/(^|[^a-z0-9])(1080p|fhd|1920x1080)([^a-z0-9]|$)/i.test(source)) return '1080p'
  if (/(^|[^a-z0-9])(720p|1280x720)([^a-z0-9]|$)/i.test(source)) return '720p'
  return ''
}

function subtitleLanguages(title: string) {
  const combined = /简繁|簡繁|繁简|繁簡/.test(title)
  const simplified = combined || /简中|简体|簡中|簡體/.test(title)
    || /\[gb\]/i.test(title)
    || /(^|[^a-z0-9])(chs|sc|gbk|gb2312)([^a-z0-9]|$)/i.test(title)
  const traditional = combined || /繁中|繁体|繁體|正體/.test(title)
    || /(^|[^a-z0-9])(cht|tc|big5)([^a-z0-9]|$)/i.test(title)
  return { simplified, traditional }
}

export function mikanEpisodeMatchesFilters(
  episode: MikanEpisode,
  resolution: ResolutionFilter,
  language: SubtitleLanguage,
) {
  if (resolution && normalizedResolution(episode) !== resolution) return false
  if (!language) return true
  const detected = subtitleLanguages(episode.title)
  if (language === 'chs') return detected.simplified
  if (language === 'cht') return detected.traditional
  return detected.simplified && detected.traditional
}

export interface MikanAggregateDraft {
  mikan_id: string
  subtitle_group: string
  filter_rule: string
  rss_url: string
  backup_rss_url: string
  allow_multi_subgroup: boolean
}

export function switchToMikanAggregate(form: MikanAggregateDraft) {
  if (!form.allow_multi_subgroup || !form.mikan_id) return
  const generatedRule = escapeMikanFilter(form.subtitle_group)
  if (form.filter_rule === form.subtitle_group || form.filter_rule === generatedRule) form.filter_rule = ''
  form.rss_url = mikanBaseRSS(form.mikan_id)
  form.backup_rss_url = ''
  form.subtitle_group = ''
}
