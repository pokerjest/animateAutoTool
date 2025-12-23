package parser

import "time"

// Episode 代表从 RSS 解析出的单集信息
type Episode struct {
	Title         string    // 原始标题
	AnimeIdentify string    // 用于识别番剧的标识(如番名)
	EpisodeNum    string    // 集数字符串 "01", "12.5"
	Season        string    // 季度 S01, S02...
	Magnet        string    // 磁力链接
	TorrentURL    string    // 种子文件链接
	PubDate       time.Time // 发布时间
	SubGroup      string    // 字幕组
	Resolution    string    // 分辨率 1080p, 4k...
}

// RSSParser 定义解析器接口
type RSSParser interface {
	Name() string
	Parse(url string) ([]Episode, error)
}
