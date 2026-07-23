import type { components } from './schema'

export type SessionState = components['schemas']['SessionState']
export type TaskAccepted = components['schemas']['TaskAccepted']
export type TaskUpdate = components['schemas']['TaskUpdate']
export interface Metadata { ID: number; id?: number; UpdatedAt?: string; updated_at?: string; title: string; title_cn?: string; title_jp?: string; image: string; summary: string; air_date: string; bangumi_id: number; tmdb_id: number; anilist_id: number; data_source: string }
export interface Subscription { ID: number; mikan_id?: string; title: string; rss_url: string; backup_rss_url?: string; image: string; subtitle_group: string; season: string; filter_rule: string; exclude_rule: string; expected_episodes: number; downloaded_count: number; is_active: boolean; allow_multi_subgroup?: boolean; auto_disable_on_done?: boolean; stale_after_hours?: number; last_run_status: string; last_run_summary: string; last_error_display: string; has_repair_actions?: boolean; can_use_base_rss?: boolean; can_clear_filter?: boolean; can_reset_stale_logs?: boolean; can_retry_missing?: boolean; can_retry_stale?: boolean; can_retry_upgrade?: boolean; can_refresh_library?: boolean; metadata?: Metadata }
export type MikanDiscoveryItem = components['schemas']['MikanDiscoveryItem']
export type MikanDashboard = components['schemas']['MikanDashboard']
export type MikanSubgroup = components['schemas']['MikanSubgroup']
export type MikanEpisode = components['schemas']['MikanEpisode']
export type MikanEpisodePreview = components['schemas']['MikanEpisodePreview']
export interface MikanSubscriptionSelection {
  mikan_id: string
  title: string
  image: string
  season: string
  subgroup_id: string
  subtitle_group: string
  rss_url: string
  backup_rss_url: string
  filter_rule: string
  allow_multi_subgroup: boolean
}
export interface LocalAnime { ID: number; title: string; image: string; path: string; file_count: number; total_size: number; season: number; summary: string; metadata?: Metadata; has_repair_actions: boolean }
export interface LibraryItem extends Metadata { is_subscribed: boolean; is_local: boolean; local_anime_id: number }
export interface TaskCard { title: string; status_label: string; status_tone: string; summary: string; detail?: string; progress_text?: string; display_error?: string }
export interface Dashboard { active_subscriptions: number; downloads: number; library_items: number; local_series: number; open_issues: number; services: Record<string, boolean>; tasks: TaskCard[]; recent_downloads: Array<{ ID: number; Title: string; Status: string; Episode: string }> }
export interface CalendarDay { weekday: { id: number; cn: string; en: string }; items: Array<{ id: number; name: string; name_cn: string; images?: { large?: string; common?: string }; air_date?: string; summary?: string }> }
