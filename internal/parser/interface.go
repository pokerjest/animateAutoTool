package parser

import "time"

// Episode 代表从 RSS 解析出的单集信息
type Episode struct {
	Title         string    `json:"title"`          // 原始标题
	AnimeIdentify string    `json:"anime_identify"` // 用于识别番剧的标识(如番名)
	EpisodeNum    string    `json:"episode_num"`    // 集数字符串 "01", "12.5"
	Season        string    `json:"season"`         // 季度 S01, S02...
	Magnet        string    `json:"magnet"`         // 磁力链接
	TorrentURL    string    `json:"torrent_url"`    // 种子文件链接
	Size          string    `json:"size"`           // 文件大小 (格式化后)
	PubDate       time.Time `json:"pub_date"`       // 发布时间
	SubGroup      string    `json:"sub_group"`      // 字幕组
	Resolution    string    `json:"resolution"`     // 分辨率 1080p, 4k...
}

// SearchResult 代表搜索结果 (番剧维度)
type SearchResult struct {
	MikanID string // 蜜柑 ID (BangumiID)
	Title   string // 番剧标题
	Image   string // 封面图 URL
}

// Subgroup 代表字幕组信息
type Subgroup struct {
	ID   string
	Name string
}

// MikanDashboard 代表蜜柑主页展示的季节性番剧面板
type MikanDashboard struct {
	Season string
	Days   map[string][]SearchResult // 0-6: 星期日到星期六, 7: OVA, 8: 剧场版
}

// RSSParser 定义解析器接口
type RSSParser interface {
	Name() string
	Parse(url string) ([]Episode, error)
	Search(keyword string) ([]SearchResult, error)
	GetSubgroups(bangumiID string) ([]Subgroup, error)
	GetDashboard(year, season string) (*MikanDashboard, error)
}
