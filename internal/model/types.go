package model

import (
	"gorm.io/gorm"
)

// Subscription 代表一个番剧订阅
type Subscription struct {
	gorm.Model
	MikanID         string `json:"mikan_id"`                                 // 蜜柑计划的 RSS ID 或 Group ID
	Title           string `json:"title" form:"Title"`                       // 番剧名称 (RSS 原始标题)
	RSSUrl          string `json:"rss_url" form:"RSSUrl" gorm:"uniqueIndex"` // 具体的 RSS 链接
	Image           string `json:"image" form:"Image"`                       // 番剧封面图片 (RSS 原始封面)
	SubtitleGroup   string `json:"subtitle_group" form:"SubtitleGroup"`      // 字幕组名称
	Season          string `json:"season" form:"season"`                     // 季度
	FilterRule      string `json:"filter_rule" form:"FilterRule"`            // 过滤规则
	ExcludeRule     string `json:"exclude_rule" form:"ExcludeRule"`          // 排除规则
	SavePath        string `json:"save_path"`                                // 保存路径
	RenameEnabled   bool   `json:"rename_enabled"`                           // 是否启用重命名
	Offset          int    `json:"offset"`                                   // 偏移
	LastEp          int    `json:"last_ep"`                                  // 最后集数
	IsActive        bool   `json:"is_active"`                                // 激活状态
	Summary         string `json:"summary"`                                  // 简介
	DownloadedCount int64  `json:"downloaded_count" gorm:"-"`                // 实际已下载的集数 (动态计算)

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

// AnimeMetadata 统一的番剧元数据表
type AnimeMetadata struct {
	gorm.Model
	// Primary display info (Selected by user)
	Title   string `json:"title"`
	Image   string `json:"image"`
	Summary string `json:"summary"`
	AirDate string `json:"air_date"`

	// Multi-language titles
	TitleCN string `json:"title_cn"`
	TitleEN string `json:"title_en"`
	TitleJP string `json:"title_jp"`

	// Sources IDs
	BangumiID int `json:"bangumi_id" gorm:"uniqueIndex"`
	TMDBID    int `json:"tmdb_id" gorm:"index"`
	AniListID int `json:"anilist_id" gorm:"index"`

	// Source Specific Data (Cache)
	BangumiTitle    string  `json:"bangumi_title"`
	BangumiImage    string  `json:"bangumi_image"`
	BangumiSummary  string  `json:"bangumi_summary"`
	BangumiRating   float64 `json:"bangumi_rating"`
	BangumiImageRaw []byte  `json:"-" gorm:"type:blob"`

	TMDBTitle    string  `json:"tmdb_title"`
	TMDBImage    string  `json:"tmdb_image"`
	TMDBSummary  string  `json:"tmdb_summary"`
	TMDBRating   float64 `json:"tmdb_rating"`
	TMDBImageRaw []byte  `json:"-" gorm:"type:blob"`

	AniListTitle    string  `json:"anilist_title"`
	AniListImage    string  `json:"anilist_image"`
	AniListSummary  string  `json:"anilist_summary"`
	AniListRating   float64 `json:"anilist_rating"`
	AniListImageRaw []byte  `json:"-" gorm:"type:blob"`

	// User Preference
	DataSource string `json:"data_source" gorm:"default:'jellyfin'"` // "bangumi", "tmdb", "anilist", "jellyfin"

	// Cached Progress
	BangumiWatchedEps int `json:"bangumi_watched_eps"`
	AniListWatchedEps int `json:"anilist_watched_eps"`
}

// DownloadLog 记录下载历史，避免重复下载
type DownloadLog struct {
	gorm.Model
	SubscriptionID uint   `gorm:"index"`
	Title          string // 种子标题
	Magnet         string // 磁力链
	Episode        string // 解析出的集数 (如 "01", "12.5")
	SeasonVal      string // 解析出的季度 (如 "S01")
	Status         string // "downloading", "completed", "failed", "renamed"
	InfoHash       string // 种子唯一标识 (由于RSS可能拿不到，不设唯一索引)
	TargetFile     string // 最终重命名后的文件路径
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
	ConfigKeyQBUrl               = "qb_url"
	ConfigKeyQBUsername          = "qb_username"
	ConfigKeyQBPassword          = "qb_password"
	ConfigKeyBaseDir             = "base_download_dir"
	ConfigKeyBangumiAppID        = "bangumi_app_id"
	ConfigKeyBangumiAppSecret    = "bangumi_app_secret" //nolint:gosec
	ConfigKeyBangumiAccessToken  = "bangumi_access_token"
	ConfigKeyBangumiRefreshToken = "bangumi_refresh_token"
	ConfigKeyTMDBToken           = "tmdb_token"
	ConfigKeyProxyURL            = "proxy_url"
	ConfigKeyProxyBangumi        = "proxy_bangumi_enabled"
	ConfigKeyProxyTMDB           = "proxy_tmdb_enabled"
	ConfigKeyAniListToken        = "anilist_token"
	ConfigKeyProxyAniList        = "proxy_anilist_enabled"
	ConfigKeyJellyfinUrl         = "jellyfin_url"
	ConfigKeyJellyfinUsername    = "jellyfin_username"
	ConfigKeyJellyfinPassword    = "jellyfin_password"
	ConfigKeyJellyfinApiKey      = "jellyfin_api_key" //nolint:gosec
	ConfigKeyProxyJellyfin       = "proxy_jellyfin_enabled"
	ConfigKeyAListUrl            = "alist_url"
	ConfigKeyAListToken          = "alist_token"
	ConfigKeyPikPakUsername      = "pikpak_username"
	ConfigKeyPikPakPassword      = "pikpak_password"
	ConfigKeyPikPakRefreshToken  = "pikpak_refresh_token" //nolint:gosec
	ConfigKeyPikPakCaptchaToken  = "pikpak_captcha_token"

	// Cloudflare R2
	// Cloudflare R2
	ConfigKeyR2Endpoint  = "r2_endpoint"
	ConfigKeyR2AccessKey = "r2_access_key"
	ConfigKeyR2SecretKey = "r2_secret_key" //nolint:gosec
	ConfigKeyR2Bucket    = "r2_bucket"
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
	DirectoryID uint   `json:"directory_id" gorm:"index"`  // 所属根目录ID
	Title       string `json:"title"`                      // 剧集标题 (通常是文件夹名)
	Image       string `json:"image"`                      // 封面图片链接
	Path        string `json:"path"`                       // 系列绝对路径
	FileCount   int    `json:"file_count"`                 // 视频文件数量 (mkv, mp4, etc.)
	TotalSize   int64  `json:"total_size"`                 // 总大小 (bytes)
	AirDate     string `json:"air_date" gorm:"default:''"` // 放送日期
	Summary     string `json:"summary"`                    // 当前显示的简介 (Deprecated: moved to Metadata)
	Season      int    `json:"season" gorm:"default:1"`    // 季度号 (默认 1)

	JellyfinSeriesID string `json:"jellyfin_series_id" gorm:"index"` // Cached Jellyfin Series ID

	// Refactored Metadata
	MetadataID *uint          `json:"metadata_id"`
	Metadata   *AnimeMetadata `json:"metadata" gorm:"foreignKey:MetadataID"`

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
	ParsedTitle  string `json:"parsed_title"`  // 从文件名解析出的原始系列标题
	ParsedSeason string `json:"parsed_season"` // 解析出的季度字符串
	Resolution   string `json:"resolution"`    // 解析出的分辨率
	SubGroup     string `json:"sub_group"`     // 解析出的字幕组
	VideoCodec   string `json:"video_codec"`   // 视频编码
	AudioCodec   string `json:"audio_codec"`   // 音频编码
	BitDepth     string `json:"bit_depth"`     // 位深
	Source       string `json:"source"`        // 来源
}

// Append AniList Config Key
// Note: This is a hacky way to append if I don't use multi_replace carefully, so I will use multi_replace instead.
