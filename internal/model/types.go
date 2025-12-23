package model

import (
	"gorm.io/gorm"
)

// Subscription 代表一个番剧订阅
type Subscription struct {
	gorm.Model
	MikanID       string `json:"mikan_id"`                                 // 蜜柑计划的 RSS ID 或 Group ID
	Title         string `json:"title" form:"Title"`                       // 番剧名称
	RSSUrl        string `json:"rss_url" form:"RSSUrl" gorm:"uniqueIndex"` // 具体的 RSS 链接
	Season        string `json:"season"`                                   // 季度 (如 "2024年10月")
	FilterRule    string `json:"filter_rule" form:"FilterRule"`            // 过滤规则 (正则或关键词，以逗号分隔)
	ExcludeRule   string `json:"exclude_rule" form:"ExcludeRule"`          // 排除规则
	SavePath      string `json:"save_path"`                                // 保存路径 (相对或绝对)
	RenameEnabled bool   `json:"rename_enabled"`                           // 是否启用重命名
	Offset        int    `json:"offset"`                                   // 第几集开始偏移 (可选)
	LastEp        int    `json:"last_ep"`                                  // 无论下到哪一集了
	IsActive      bool   `json:"is_active"`                                // 激活状态
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
	ConfigKeyQBUrl      = "qb_url"
	ConfigKeyQBUsername = "qb_username"
	ConfigKeyQBPassword = "qb_password"
	ConfigKeyBaseDir    = "base_download_dir"
)
