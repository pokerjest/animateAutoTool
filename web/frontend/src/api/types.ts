import type { components } from './schema'

export type SessionState = components['schemas']['SessionState']
export type TaskAccepted = components['schemas']['TaskAccepted']
export type TaskUpdate = components['schemas']['TaskUpdate']
export interface Metadata { ID: number; id?: number; UpdatedAt?: string; updated_at?: string; title: string; title_cn?: string; title_jp?: string; image: string; summary: string; air_date: string; bangumi_id: number; tmdb_id: number; anilist_id: number; data_source: string }
export type ResolutionFilter = '' | '2160p' | '1080p' | '720p'
export type SubtitleLanguage = '' | 'chs' | 'cht' | 'chs_cht'
export interface Subscription { ID: number; mikan_id?: string; title: string; rss_url: string; backup_rss_url?: string; image: string; subtitle_group: string; season: string; filter_rule: string; exclude_rule: string; resolution_filter?: ResolutionFilter; subtitle_language?: SubtitleLanguage; expected_episodes: number; downloaded_count: number; is_active: boolean; allow_multi_subgroup?: boolean; auto_disable_on_done?: boolean; stale_after_hours?: number; last_run_status: string; last_run_summary: string; last_error_display: string; has_repair_actions?: boolean; can_use_base_rss?: boolean; can_clear_filter?: boolean; can_reset_stale_logs?: boolean; can_retry_missing?: boolean; can_retry_stale?: boolean; can_retry_upgrade?: boolean; can_refresh_library?: boolean; library_stage?: string; library_tone?: string; library_hint?: string; local_anime_id?: number; library_episode_count?: number; playable?: boolean; metadata?: Metadata }
export type MikanDiscoveryItem = components['schemas']['MikanDiscoveryItem']
export type MikanDashboard = components['schemas']['MikanDashboard']
export type MikanSubgroup = components['schemas']['MikanSubgroup']
export type MikanEpisode = components['schemas']['MikanEpisode']
export type MikanEpisodePreview = components['schemas']['MikanEpisodePreview']
export type MetadataSearchResult = components['schemas']['MetadataSearchResult']
export interface MetadataSourceCandidate {
  id: number
  source: 'bangumi' | 'tmdb' | 'anilist'
  name: string
  name_cn: string
  image: string
  summary: string
  air_date: string
}
export interface MetadataMatchCandidate {
  title: string
  summary: string
  air_date: string
  image: string
  bangumi?: MetadataSourceCandidate
  tmdb?: MetadataSourceCandidate
  anilist?: MetadataSourceCandidate
  score: number
  evidence: string[]
}
export interface MetadataMatchSearchResult {
  query: string
  source: string
  source_id?: number
  source_status: Record<string, { configured: boolean; searched: boolean; error?: string; count: number }>
  candidates: MetadataMatchCandidate[]
}
export type RandomBackground = components['schemas']['RandomBackground']
export type JellyfinPlayInfo = components['schemas']['JellyfinPlayInfo']
export type PlaybackDiagnostic = components['schemas']['PlaybackDiagnostic']
export type PlaybackProgressInput = components['schemas']['PlaybackProgressInput']
export type ContinueWatchingItem = components['schemas']['ContinueWatchingItem']
export type ContinueWatchingResponse = components['schemas']['ContinueWatchingResponse']
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
  exclude_rule: string
  resolution_filter: ResolutionFilter
  subtitle_language: SubtitleLanguage
  allow_multi_subgroup: boolean
}
export interface LocalAnime { ID: number; title: string; image: string; path: string; file_count: number; total_size: number; season: number; summary: string; metadata?: Metadata; has_repair_actions: boolean }
export type LocalOrganizeSelection =
  | { mode: 'ids'; anime_ids: number[] }
  | { mode: 'query'; query: string; exclude_ids?: number[] }
export type LocalOrganizeChange = components['schemas']['LocalOrganizeChange']
export type LocalOrganizeAnimePreview = components['schemas']['LocalOrganizeAnimePreview']
export type LocalOrganizePreview = components['schemas']['LocalOrganizePreview']
export interface LibraryItem extends Metadata { is_subscribed: boolean; is_local: boolean; local_anime_id: number }
export interface TaskCard { title: string; status_label: string; status_tone: string; summary: string; detail?: string; progress_text?: string; display_error?: string }
export interface Dashboard { active_subscriptions: number; downloads: number; library_items: number; local_series: number; open_issues: number; services: Record<string, boolean>; tasks: TaskCard[]; recent_downloads: Array<{ ID: number; Title: string; Status: string; Episode: string }> }
export interface CalendarDay { weekday: { id: number; cn: string; en: string }; items: Array<{ id: number; name: string; name_cn: string; images?: { large?: string; common?: string }; air_date?: string; summary?: string }> }

export type MediaProvider = components['schemas']['MediaProvider']
export type MediaLibrary = components['schemas']['MediaLibrary']
export type MediaItem = components['schemas']['MediaItem']
export type MediaPlaybackInfo = components['schemas']['MediaPlaybackInfo']
export type MediaPage = components['schemas']['MediaPage']
export type UpdateRelease = components['schemas']['UpdateRelease']
export type UpdateReleaseCatalog = components['schemas']['UpdateReleaseCatalog']

export interface AIProposal {
  id: string
  type: string
  status: 'analyzing' | 'ready' | 'applied' | 'dismissed' | 'failed' | 'expired' | 'stale'
  target_type: string
  target_id: string
  summary: string
  confidence: number
  evidence: string[]
  warnings: string[]
  actionable: boolean
  payload: Record<string, unknown>
  provider: string
  model: string
  error?: string
  expires_at?: string
  created_at: string
  updated_at: string
}

export interface AIAnalysisAccepted {
  task_id: string
  proposal_id: string
  status: 'running' | 'queued'
}

export type AIToolRun = components['schemas']['AIToolRun']
