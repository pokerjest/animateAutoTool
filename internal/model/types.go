package model

import (
	"time"

	"gorm.io/gorm"
)

// Subscription 代表一个番剧订阅
type Subscription struct {
	gorm.Model
	MikanID               string     `json:"mikan_id"`                                  // 蜜柑计划的 RSS ID 或 Group ID
	Title                 string     `json:"title" form:"Title"`                        // 番剧名称 (RSS 原始标题)
	RSSUrl                string     `json:"rss_url" form:"RSSUrl" gorm:"uniqueIndex"`  // 具体的 RSS 链接
	Image                 string     `json:"image" form:"Image"`                        // 番剧封面图片 (RSS 原始封面)
	SubtitleGroup         string     `json:"subtitle_group" form:"SubtitleGroup"`       // 字幕组名称
	Season                string     `json:"season" form:"season"`                      // 季度
	FilterRule            string     `json:"filter_rule" form:"FilterRule"`             // 必须命中的资源标题正则
	ExcludeRule           string     `json:"exclude_rule" form:"ExcludeRule"`           // 命中后排除的资源标题正则
	ResolutionFilter      string     `json:"resolution_filter" form:"ResolutionFilter"` // 清晰度过滤 (2160p/1080p/720p)
	SubtitleLanguage      string     `json:"subtitle_language" form:"SubtitleLanguage"` // 字幕语言过滤 (chs/cht/chs_cht)
	BackupRSSUrl          string     `json:"backup_rss_url" form:"BackupRSSUrl"`        // 备用 RSS
	ExpectedEpisodes      int        `json:"expected_episodes" form:"ExpectedEpisodes"` // 预期总集数
	AutoDisableOnDone     bool       `json:"auto_disable_on_done" form:"AutoDisableOnDone"`
	AllowMultiSubgroup    bool       `json:"allow_multi_subgroup" form:"AllowMultiSubgroup"`
	StaleAfterHours       int        `json:"stale_after_hours" form:"StaleAfterHours"` // 超过多少小时无更新后提示
	SavePath              string     `json:"save_path"`                                // 保存路径
	RenameEnabled         bool       `json:"rename_enabled"`                           // 是否启用重命名
	Offset                int        `json:"offset"`                                   // 偏移
	LastEp                int        `json:"last_ep"`                                  // 最后集数
	IsActive              bool       `json:"is_active"`                                // 激活状态
	Summary               string     `json:"summary"`                                  // 简介
	DownloadedCount       int64      `json:"downloaded_count" gorm:"-"`                // 已加入下载且未归档的去重集数 (动态计算)
	RSSCount              int64      `json:"rss_count" gorm:"-"`
	CanonicalEpisodeCount int64      `json:"canonical_episode_count" gorm:"-"`
	ConfirmedCount        int64      `json:"confirmed_count" gorm:"-"`
	DownloadingCount      int64      `json:"downloading_count" gorm:"-"`
	CompletedCount        int64      `json:"completed_count" gorm:"-"`
	FailedCount           int64      `json:"failed_count" gorm:"-"`
	UnresolvedCount       int64      `json:"unresolved_count" gorm:"-"`
	NeedsAttention        bool       `json:"needs_attention" gorm:"-"`
	LastCheckAt           *time.Time `json:"last_check_at"`
	LastSuccessAt         *time.Time `json:"last_success_at"`
	LastRunStatus         string     `json:"last_run_status"`
	LastRunSummary        string     `json:"last_run_summary"`
	LastError             string     `json:"last_error"`
	LastErrorDisplay      string     `json:"last_error_display" gorm:"-"`
	LastNewDownloads      int        `json:"last_new_downloads"`
	LastDownloadedTitle   string     `json:"last_downloaded_title"`
	CanUseBaseRSS         bool       `json:"can_use_base_rss" gorm:"-"`
	BaseRSSURL            string     `json:"base_rss_url" gorm:"-"`
	CanClearFilter        bool       `json:"can_clear_filter" gorm:"-"`
	CanResetStaleLogs     bool       `json:"can_reset_stale_logs" gorm:"-"`
	CanRetryMissing       bool       `json:"can_retry_missing" gorm:"-"`
	CanRetryStale         bool       `json:"can_retry_stale" gorm:"-"`
	CanRetryUpgrade       bool       `json:"can_retry_upgrade" gorm:"-"`
	CanRefreshLibrary     bool       `json:"can_refresh_library" gorm:"-"`
	HasRepairActions      bool       `json:"has_repair_actions" gorm:"-"`
	StrategyHint          string     `json:"strategy_hint" gorm:"-"`
	LifecycleStage        string     `json:"lifecycle_stage" gorm:"-"`
	LifecycleTone         string     `json:"lifecycle_tone" gorm:"-"`
	LibraryStage          string     `json:"library_stage" gorm:"-"`
	LibraryTone           string     `json:"library_tone" gorm:"-"`
	LibraryHint           string     `json:"library_hint" gorm:"-"`
	LocalAnimeID          uint       `json:"local_anime_id" gorm:"-"`
	LibraryEpisodeCount   int64      `json:"library_episode_count" gorm:"-"`
	Playable              bool       `json:"playable" gorm:"-"`

	// Refactored Metadata
	MetadataID *uint          `json:"metadata_id"`
	Metadata   *AnimeMetadata `json:"metadata" gorm:"foreignKey:MetadataID"`
}

// User 用户表
type User struct {
	gorm.Model
	Username     string `json:"username" gorm:"uniqueIndex"`
	PasswordHash string `json:"-"`    // 存储 bcrypt 哈希
	Memo         string `json:"memo"` // 备注 (可存储明文恢复密码)
}

// PlaybackHistory stores the last known playback position for one user and
// local episode. Jellyfin remains an external synchronization target, while
// this local record makes continue-watching fast and resilient when Jellyfin
// is temporarily unavailable.
type PlaybackHistory struct {
	gorm.Model
	UserID         uint      `json:"user_id" gorm:"uniqueIndex:idx_playback_user_episode;index"`
	LocalAnimeID   uint      `json:"local_anime_id" gorm:"index"`
	LocalEpisodeID uint      `json:"local_episode_id" gorm:"uniqueIndex:idx_playback_user_episode;index"`
	PositionTicks  int64     `json:"position_ticks"`
	DurationTicks  int64     `json:"duration_ticks"`
	Completed      bool      `json:"completed" gorm:"index"`
	LastEvent      string    `json:"last_event" gorm:"size:24"`
	LastPlayedAt   time.Time `json:"last_played_at" gorm:"index"`
}

// AuditLog 记录登录、密码变更、删除、备份恢复等敏感操作,
// 用于多人部署场景下的事后追溯。Details 字段保存与操作相关的
// 结构化补充信息(JSON 字符串),便于在不增加列的情况下扩展上下文。
type AuditLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
	UserID     uint      `gorm:"index" json:"user_id"`
	Username   string    `gorm:"size:128;index" json:"username"`
	Action     string    `gorm:"size:64;index" json:"action"`
	TargetType string    `gorm:"size:64" json:"target_type"`
	TargetID   string    `gorm:"size:128" json:"target_id"`
	Outcome    string    `gorm:"size:16;index" json:"outcome"` // success / failure
	IP         string    `gorm:"size:64" json:"ip"`
	UserAgent  string    `gorm:"size:512" json:"user_agent"`
	Details    string    `gorm:"type:text" json:"details"`
}

// AIProposal stores a validated, user-scoped AI recommendation. The model is
// never allowed to apply Payload directly: ApplyTool is an internal allowlisted
// tool and confirmation fields are issued and consumed by the server.
type AIProposal struct {
	ID               string     `gorm:"primaryKey;size:36" json:"id"`
	CreatedAt        time.Time  `gorm:"index" json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	UserID           uint       `gorm:"index" json:"user_id"`
	Type             string     `gorm:"size:48;index" json:"type"`
	Status           string     `gorm:"size:24;index" json:"status"`
	TargetType       string     `gorm:"size:48;index" json:"target_type"`
	TargetID         string     `gorm:"size:160;index" json:"target_id"`
	Summary          string     `gorm:"type:text" json:"summary"`
	Confidence       float64    `json:"confidence"`
	Evidence         string     `gorm:"type:text" json:"-"`
	Warnings         string     `gorm:"type:text" json:"-"`
	Payload          string     `gorm:"type:text" json:"-"`
	InputFingerprint string     `gorm:"size:64;index" json:"input_fingerprint"`
	ApplyTool        string     `gorm:"size:96" json:"apply_tool"`
	Provider         string     `gorm:"size:32" json:"provider"`
	Model            string     `gorm:"size:160" json:"model"`
	Error            string     `gorm:"type:text" json:"error,omitempty"`
	ExpiresAt        *time.Time `gorm:"index" json:"expires_at,omitempty"`
	AppliedAt        *time.Time `json:"applied_at,omitempty"`
	ConfirmTokenHash string     `gorm:"size:64" json:"-"`
	ConfirmExpiresAt *time.Time `json:"-"`
	ConfirmUsedAt    *time.Time `json:"-"`
}

// AIToolRun is the append-only operational record for every AI tool call.
// Arguments and results contain only the bounded, redacted summaries produced
// by the tool observer.
type AIToolRun struct {
	ID                    string    `gorm:"primaryKey;size:36" json:"id"`
	CreatedAt             time.Time `gorm:"index" json:"created_at"`
	RequestID             string    `gorm:"size:64;index" json:"request_id"`
	TaskID                string    `gorm:"size:96;index" json:"task_id"`
	SessionID             string    `gorm:"size:160;index" json:"session_id"`
	ProposalID            string    `gorm:"size:36;index" json:"proposal_id"`
	UserID                uint      `gorm:"index" json:"user_id"`
	Username              string    `gorm:"size:128;index" json:"username"`
	ToolName              string    `gorm:"size:96;index" json:"tool_name"`
	Risk                  string    `gorm:"size:16;index" json:"risk"`
	ArgumentsSummary      string    `gorm:"type:text" json:"arguments_summary"`
	ArgumentsHash         string    `gorm:"size:64" json:"arguments_hash"`
	ResultSummary         string    `gorm:"type:text" json:"result_summary"`
	Outcome               string    `gorm:"size:16;index" json:"outcome"`
	ErrorType             string    `gorm:"size:96" json:"error_type"`
	DurationMilliseconds  int64     `json:"duration_ms"`
	Provider              string    `gorm:"size:32" json:"provider"`
	Model                 string    `gorm:"size:160" json:"model"`
	ConfirmationRequired  bool      `json:"confirmation_required"`
	ConfirmationValidated bool      `json:"confirmation_validated"`
}

// AnimeMetadata 统一的番剧元数据表
type AnimeMetadata struct {
	gorm.Model
	// Primary display info (Selected by user)
	Title   string `json:"title"`
	Image   string `json:"image"`
	Summary string `json:"summary"`
	AirDate string `json:"air_date"`

	// Multi-language titles
	TitleCN         string `json:"title_cn"`
	TitleEN         string `json:"title_en"`
	TitleJP         string `json:"title_jp"`
	SortTitle       string `json:"sort_title"`
	OriginalTitle   string `json:"original_title"`
	Genres          string `json:"genres"` // JSON array, kept as text for backward-compatible SQLite migrations.
	Studios         string `json:"studios"`
	Tags            string `json:"tags"`
	Actors          string `json:"actors"`
	Directors       string `json:"directors"`
	RuntimeMinutes  int    `json:"runtime_minutes"`
	ContentRating   string `json:"content_rating"`
	OriginalCountry string `json:"original_country"`

	// Sources IDs
	BangumiID int `json:"bangumi_id" gorm:"uniqueIndex:idx_anime_metadata_bangumi_id,where:bangumi_id != 0"`
	TMDBID    int `json:"tmdb_id" gorm:"index"`
	AniListID int `json:"anilist_id" gorm:"index"`

	// Source Specific Data (Cache)
	BangumiTitle    string  `json:"bangumi_title"`
	BangumiImage    string  `json:"bangumi_image"`
	BangumiSummary  string  `json:"bangumi_summary"`
	BangumiRating   float64 `json:"bangumi_rating"`
	BangumiImageRaw []byte  `json:"-" gorm:"type:blob"`

	TMDBTitle       string  `json:"tmdb_title"`
	TMDBImage       string  `json:"tmdb_image"`
	TMDBBackdrop    string  `json:"tmdb_backdrop"`
	TMDBSummary     string  `json:"tmdb_summary"`
	TMDBRating      float64 `json:"tmdb_rating"`
	TMDBImageRaw    []byte  `json:"-" gorm:"type:blob"`
	TMDBBackdropRaw []byte  `json:"-" gorm:"type:blob"`

	AniListTitle    string  `json:"anilist_title"`
	AniListImage    string  `json:"anilist_image"`
	AniListSummary  string  `json:"anilist_summary"`
	AniListRating   float64 `json:"anilist_rating"`
	AniListImageRaw []byte  `json:"-" gorm:"type:blob"`

	// User Preference
	DataSource   string `json:"data_source" gorm:"default:'jellyfin'"` // "bangumi", "tmdb", "anilist", "jellyfin"
	FieldSources string `json:"field_sources" gorm:"type:text"`        // JSON map of field -> provider/local source.

	// Cached Progress
	BangumiWatchedEps int `json:"bangumi_watched_eps"`
	AniListWatchedEps int `json:"anilist_watched_eps"`
}

// DownloadLog 记录下载历史，避免重复下载
type DownloadLog struct {
	gorm.Model
	SubscriptionID uint   `gorm:"index"`
	ResourceID     *uint  `gorm:"index"`
	Title          string // 种子标题
	Magnet         string // 磁力链
	Episode        string // 解析出的集数 (如 "01", "12.5")
	SeasonVal      string // 解析出的季度 (如 "S01")
	Status         string // "downloading", "completed", "failed", "renamed"
	InfoHash       string // 种子唯一标识 (由于RSS可能拿不到，不设唯一索引)
	TargetFile     string // 最终重命名后的文件路径

	// Live qBittorrent progress is populated only for API responses. These
	// fields are intentionally ignored by GORM and are never persisted.
	ProgressPercent float64 `json:"progress_percent" gorm:"-"`
	DownloadedBytes int64   `json:"downloaded_bytes" gorm:"-"`
	TotalBytes      int64   `json:"total_bytes" gorm:"-"`
	DownloadSpeed   int64   `json:"download_speed" gorm:"-"`
}

// SubscriptionResource is the durable reconciliation record for one RSS
// candidate. DownloadLog remains as a compatibility projection while this
// table becomes the source of truth for RSS, qBittorrent and local files.
type SubscriptionResource struct {
	gorm.Model
	SubscriptionID uint       `gorm:"index;not null;uniqueIndex:idx_subscription_resource_fingerprint" json:"subscription_id"`
	CanonicalKey   string     `gorm:"size:160;index" json:"canonical_key"`
	Fingerprint    string     `gorm:"size:64;uniqueIndex:idx_subscription_resource_fingerprint" json:"fingerprint"`
	Title          string     `gorm:"type:text" json:"title"`
	Episode        string     `gorm:"size:32;index" json:"episode"`
	SeasonVal      string     `gorm:"size:32;index" json:"season_val"`
	Subgroup       string     `gorm:"size:160" json:"subgroup"`
	VersionTag     string     `gorm:"size:24" json:"version_tag"`
	TorrentURL     string     `gorm:"type:text" json:"torrent_url"`
	RSSURL         string     `gorm:"type:text" json:"rss_url"`
	RSSGUID        string     `gorm:"size:255" json:"rss_guid"`
	InfoHash       string     `gorm:"size:128;index" json:"info_hash"`
	Source         string     `gorm:"size:16" json:"source"`
	State          string     `gorm:"size:24;index" json:"state"`
	StateReason    string     `gorm:"type:text" json:"state_reason"`
	LastError      string     `gorm:"type:text" json:"last_error"`
	TaskHash       string     `gorm:"size:128;index" json:"task_hash"`
	TargetFile     string     `gorm:"type:text;index" json:"target_file"`
	AttemptCount   int        `json:"attempt_count"`
	CandidateRank  int        `json:"candidate_rank"`
	Selected       bool       `gorm:"index" json:"selected"`
	Current        bool       `gorm:"index" json:"current"`
	LastSeenAt     *time.Time `gorm:"index" json:"last_seen_at,omitempty"`
	LastAttemptAt  *time.Time `json:"last_attempt_at,omitempty"`
	SubmittedAt    *time.Time `json:"submitted_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	RetryAfter     *time.Time `json:"retry_after,omitempty"`
}

// SubscriptionRunLog records each subscription check as a first-class run entry
// so operators can audit trends and diagnose failures over time.
type SubscriptionRunLog struct {
	gorm.Model
	SubscriptionID      uint      `gorm:"index"`
	CheckedAt           time.Time `gorm:"index"`
	TriggerSource       string
	Status              string
	Summary             string
	Error               string
	TotalEpisodes       int
	FilteredCount       int
	DuplicateCount      int
	NewDownloads        int
	FailedDownloads     int
	LastDownloadedTitle string
}

// GlobalConfig 存储全局配置 (虽是单用户，但也存在DB里方便迁移)
type GlobalConfig struct {
	Key   string `gorm:"primaryKey"`
	Value string
}

const (
	ConfigValueTrue = "true"
)

const (
	ConfigKeyQBUrl                     = "qb_url"
	ConfigKeyQBUsername                = "qb_username"
	ConfigKeyQBPassword                = "qb_password"
	ConfigKeyQBMode                    = "qb_mode"
	ConfigKeyBaseDir                   = "base_download_dir"
	ConfigKeyAutoRenameEnabled         = "auto_rename_enabled"
	ConfigKeyMediaNamingPreset         = "media_naming_preset"
	ConfigKeyAutoRenameSeriesTemplate  = "auto_rename_series_template"
	ConfigKeyAutoRenameEpisodeTemplate = "auto_rename_episode_template"
	ConfigKeyMetadataSourceOrder       = "metadata_source_order"
	ConfigKeyMetadataOverwritePolicy   = "metadata_overwrite_policy"
	ConfigKeyWriteNFOEnabled           = "write_nfo_enabled"
	ConfigKeyWriteImagesEnabled        = "write_images_enabled"
	ConfigKeyIncrementalScanEnabled    = "incremental_scan_enabled"
	ConfigKeyBangumiAppID              = "bangumi_app_id"
	ConfigKeyBangumiAppSecret          = "bangumi_app_secret" //nolint:gosec
	ConfigKeyBangumiAccessToken        = "bangumi_access_token"
	ConfigKeyBangumiRefreshToken       = "bangumi_refresh_token"
	ConfigKeyTMDBToken                 = "tmdb_token"
	ConfigKeyProxyURL                  = "proxy_url"
	ConfigKeyProxyBangumi              = "proxy_bangumi_enabled"
	ConfigKeyProxyMikan                = "proxy_mikan_enabled"
	ConfigKeyProxyTMDB                 = "proxy_tmdb_enabled"
	ConfigKeyAniListToken              = "anilist_token"
	ConfigKeyProxyAniList              = "proxy_anilist_enabled"
	ConfigKeyProxyAI                   = "proxy_ai_enabled"
	ConfigKeyProxyUpdater              = "proxy_updater_enabled"
	ConfigKeyAuthIPAllowlistEnabled    = "auth_ip_allowlist_enabled"
	ConfigKeyAuthIPAllowlist           = "auth_ip_allowlist"
	ConfigKeyRepoUpdateEnabled         = "repo_update_enabled"
	ConfigKeyRepoAutoPullEnabled       = "repo_auto_pull_enabled"
	ConfigKeyRepoUpdateIntervalMinutes = "repo_update_interval_minutes"
	ConfigKeyRepoUpdateOwner           = "repo_update_owner"
	ConfigKeyRepoUpdateName            = "repo_update_name"
	ConfigKeyRepoRequireChecksum       = "repo_update_require_checksum"
	ConfigKeyJellyfinUrl               = "jellyfin_url"
	ConfigKeyJellyfinDirectUrl         = "jellyfin_direct_url"
	ConfigKeyNetBirdProxyURL           = "netbird_proxy_url"
	ConfigKeyJellyfinLibraryIDs        = "jellyfin_library_ids"
	ConfigKeyJellyfinUsername          = "jellyfin_username"
	ConfigKeyJellyfinPassword          = "jellyfin_password"
	ConfigKeyJellyfinApiKey            = "jellyfin_api_key" //nolint:gosec
	ConfigKeyProxyJellyfin             = "proxy_jellyfin_enabled"
	ConfigKeyAListUrl                  = "alist_url"
	ConfigKeyAListToken                = "alist_token"
	ConfigKeyPikPakUsername            = "pikpak_username"
	ConfigKeyPikPakPassword            = "pikpak_password"
	ConfigKeyPikPakRefreshToken        = "pikpak_refresh_token" //nolint:gosec
	ConfigKeyPikPakCaptchaToken        = "pikpak_captcha_token" //nolint:gosec

	// Cloudflare R2
	// Cloudflare R2
	ConfigKeyR2Endpoint  = "r2_endpoint"
	ConfigKeyR2AccessKey = "r2_access_key"
	ConfigKeyR2SecretKey = "r2_secret_key" //nolint:gosec
	ConfigKeyR2Bucket    = "r2_bucket"

	// AI Assistant
	ConfigKeyAIProvider      = "ai_provider"
	ConfigKeyAIBaseURL       = "ai_base_url" // Legacy OpenAI-compatible key.
	ConfigKeyAIApiKey        = "ai_api_key"  //nolint:gosec // Legacy OpenAI-compatible key.
	ConfigKeyAIModel         = "ai_model"    // Legacy OpenAI-compatible key.
	ConfigKeyAIOpenAIBaseURL = "ai_openai_base_url"
	ConfigKeyAIOpenAIAPIKey  = "ai_openai_api_key" //nolint:gosec
	ConfigKeyAIOpenAIModel   = "ai_openai_model"
	ConfigKeyAIGeminiBaseURL = "ai_gemini_base_url"
	ConfigKeyAIGeminiAPIKey  = "ai_gemini_api_key" //nolint:gosec
	ConfigKeyAIGeminiModel   = "ai_gemini_model"
	ConfigKeyAIGeminiFormat  = "ai_gemini_api_format"
	ConfigKeyAIClaudeBaseURL = "ai_claude_base_url"
	ConfigKeyAIClaudeAPIKey  = "ai_claude_api_key" //nolint:gosec
	ConfigKeyAIClaudeModel   = "ai_claude_model"
	ConfigKeyAIClaudeFormat  = "ai_claude_api_format"
)

// LocalAnimeDirectory 用户配置的本地番剧目录根路径
type LocalAnimeDirectory struct {
	gorm.Model
	Path        string `json:"path" gorm:"uniqueIndex"` // 目录绝对路径
	Description string `json:"description"`             // 备注描述 (可选)
}

// LocalAnime 扫描出的本地番剧系列
type LocalAnime struct {
	gorm.Model
	DirectoryID uint   `json:"directory_id" gorm:"index"` // 所属根目录ID
	Title       string `json:"title"`                     // 剧集标题 (通常是文件夹名)
	Image       string `json:"image"`                     // 封面图片链接
	Path        string `json:"path"`                      // 系列绝对路径
	// ScanKey is populated for folder-based series. Loose files intentionally
	// leave it NULL because several unrelated series can share the library root.
	ScanKey   *string `json:"-" gorm:"column:scan_key"`
	FileCount int     `json:"file_count"`                 // 视频文件数量 (mkv, mp4, etc.)
	TotalSize int64   `json:"total_size"`                 // 总大小 (bytes)
	AirDate   string  `json:"air_date" gorm:"default:''"` // 放送日期
	Summary   string  `json:"summary"`                    // 当前显示的简介 (Deprecated: moved to Metadata)
	Season    int     `json:"season" gorm:"default:1"`    // 季度号 (默认 1)

	JellyfinSeriesID string `json:"jellyfin_series_id" gorm:"index"` // Cached Jellyfin Series ID

	// Refactored Metadata
	MetadataID       *uint          `json:"metadata_id"`
	Metadata         *AnimeMetadata `json:"metadata" gorm:"foreignKey:MetadataID"`
	HasRepairActions bool           `json:"has_repair_actions" gorm:"-"`
	CanRetryScrape   bool           `json:"can_retry_scrape" gorm:"-"`
	CanFixMatch      bool           `json:"can_fix_match" gorm:"-"`
	RepairHint       string         `json:"repair_hint" gorm:"-"`

	Episodes []LocalEpisode `json:"episodes" gorm:"foreignKey:LocalAnimeID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
}

// LocalEpisode 代表本地的一个视频文件（单集）
type LocalEpisode struct {
	gorm.Model
	LocalAnimeID uint   `json:"local_anime_id" gorm:"index"` // 关联的番剧系列
	Title        string `json:"title"`                       // 单集标题 (e.g. "Episode 1")
	EpisodeNum   int    `json:"episode_num"`                 // 核心集号 (绝对集数)
	SeasonNum    int    `json:"season_num"`                  // 季度号 (默认 1)
	Path         string `json:"path" gorm:"uniqueIndex"`     // 绝对路径
	Container    string `json:"container"`                   // 容器格式 (mkv, mp4)
	FileSize     int64  `json:"file_size"`                   // 文件大小
	Image        string `json:"image"`                       // 集数预览图 (TMDB Still Path)
	Summary      string `json:"summary"`                     // 集数简介

	JellyfinItemID string `json:"jellyfin_item_id" gorm:"index"` // Cached Jellyfin Episode ID

	// Offline Metadata / Raw Parsed Data
	ParsedTitle        string  `json:"parsed_title"`  // 从文件名解析出的原始系列标题
	ParsedSeason       string  `json:"parsed_season"` // 解析出的季度字符串
	EpisodeEndNum      int     `json:"episode_end_num"`
	EpisodeType        string  `json:"episode_type"`
	AbsoluteEpisodeNum int     `json:"absolute_episode_num"`
	VersionTag         string  `json:"version_tag"`
	LanguageTag        string  `json:"language_tag"`
	ParseSource        string  `json:"parse_source"`
	ParseConfidence    float64 `json:"parse_confidence"`
	ScanFingerprint    string  `json:"scan_fingerprint" gorm:"size:64;index"`
	Resolution         string  `json:"resolution"`  // 解析出的分辨率
	SubGroup           string  `json:"sub_group"`   // 解析出的字幕组
	VideoCodec         string  `json:"video_codec"` // 视频编码
	AudioCodec         string  `json:"audio_codec"` // 音频编码
	BitDepth           string  `json:"bit_depth"`   // 位深
	Source             string  `json:"source"`      // 来源
}

type LibraryIssue struct {
	gorm.Model
	IssueKey        string `gorm:"uniqueIndex"`
	IssueType       string `gorm:"index"` // scan, scrape
	Status          string `gorm:"index"` // open, resolved
	Title           string
	DirectoryPath   string
	LocalAnimeID    *uint `gorm:"index"`
	Message         string
	Hint            string
	OccurrenceCount int
	LastSeenAt      *time.Time
	ResolvedAt      *time.Time
}

// Append AniList Config Key
// Note: This is a hacky way to append if I don't use multi_replace carefully, so I will use multi_replace instead.
