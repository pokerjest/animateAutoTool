package parser

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"time"

	"github.com/go-resty/resty/v2"
)

type MikanParser struct {
	client *resty.Client
}

func NewMikanParser() *MikanParser {
	return &MikanParser{
		client: resty.New().SetTimeout(10 * time.Second),
	}
}

func (p *MikanParser) Name() string {
	return "Mikan Project"
}

// RSS/Atom XML 结构定义
type MikanRSS struct {
	Channel struct {
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			Enclosure   struct {
				URL string `xml:"url,attr"`
			} `xml:"enclosure"`
			Torrent struct {
				Link          string `xml:"link"`
				ContentLength int64  `xml:"contentLength"`
				PubDate       string `xml:"pubDate"`
			} `xml:"torrent"` // Mikan 实际上是标准 RSS 2.0，部分字段可能不同，这里先按标准试
			PubDate string `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
}

func (p *MikanParser) Parse(url string) ([]Episode, error) {
	resp, err := p.client.R().Get(url)
	if err != nil {
		return nil, err
	}

	var rss MikanRSS
	if err := xml.Unmarshal(resp.Body(), &rss); err != nil {
		return nil, fmt.Errorf("xml unmarshal error: %v", err)
	}

	var episodes []Episode
	for _, item := range rss.Channel.Items {
		// Mikan 的磁力链通常在 link 中，或者通过 torrent 标签
		// 实际上 Mikan RSS 的 item.link 是详情页，enclosure.url 是种子/磁力
		// 或者是 <torrent:link> (如果有命名空间)

		// 修正：根据经验，Mikan RSS 的 item -> enclosure url 属性通常是种子下载链接
		// Link 标签通常指向 Mikan 网站详情页
		// 磁力链接有时不在 RSS 直接提供，或者在此处需要进一步处理。
		// 但大部分用户需要磁力，Mikan RSS 直接给的是 .torrent 下载链接。
		// 部分客户端(qBit)支持直接把 .torrent URL 传进去下载。

		// 这里简单解析一下 Title
		ep := parseTitle(item.Title)
		ep.TorrentURL = item.Enclosure.URL

		// 处理时间 RFC1123Z ?
		// Mikan Example: Mon, 23 Dec 2024 10:30:00 +0800
		t, _ := time.Parse(time.RFC1123Z, item.PubDate)
		ep.PubDate = t

		episodes = append(episodes, ep)
	}

	return episodes, nil
}

// 简单的正则解析器 (初步实现，后续需增强)
// 示例标题: [Moozzi2] Fate/stay night [Unlimited Blade Works] - 25 (BD 1920x1080 x264 Flac) TV-rip
// [LoliHouse] 葬送的芙莉莲 / Sousou no Frieren - 28 [WebRip 1080p HEVC-10bit AAC][简繁内封字幕]
func parseTitle(title string) Episode {
	var ep Episode
	ep.Title = title

	// 1. 尝试提取字幕组 (通常在开头的 [])
	groupRegex := regexp.MustCompile(`^\[(.*?)\]`)
	if match := groupRegex.FindStringSubmatch(title); len(match) > 1 {
		ep.SubGroup = match[1]
	}

	// 2. 尝试提取集数
	// 策略 A: 匹配 " - 28 " 或 " - 28" (结尾)
	epRegex1 := regexp.MustCompile(`\s-\s(\d+(\.\d+)?)(\s|$)`)
	// 策略 B: 匹配 " [28] " 或 "[28]" 且后面不是分辨率等
	// 这是一个简化的假设，为了提高准确性可能需要更严格的排除
	epRegex2 := regexp.MustCompile(`\[(\d+(\.\d+)?)\]`)

	if match := epRegex1.FindStringSubmatch(title); len(match) > 1 {
		ep.EpisodeNum = match[1]
	} else if match := epRegex2.FindStringSubmatch(title); len(match) > 1 {
		// 稍微过滤一下，排除 [1080p] 这种
		val := match[1]
		if len(val) < 4 { // 简单的启发式：集数通常小于 4 位 (排除 1080, 720, 264 等)
			ep.EpisodeNum = val
		}
	}

	return ep
}
