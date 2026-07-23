import type { MikanDiscoveryItem, MikanSubgroup, MikanSubscriptionSelection } from '../api/types'

export function escapeMikanFilter(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

export function mikanBaseRSS(mikanID: string) {
  return `https://mikanani.me/RSS/Bangumi?bangumiId=${encodeURIComponent(mikanID)}`
}

export function buildMikanSelection(
  anime: MikanDiscoveryItem & { season: string },
  group: MikanSubgroup,
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
    allow_multi_subgroup: isAll,
  }
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
