package parser

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/pokerjest/animateAutoTool/internal/httpx"
)

type MikanParser struct {
	client *resty.Client
}

const (
	maxTorrentFileSize     = 10 << 20
	mikanMappingCacheTTL   = 12 * time.Hour
	mikanMappingConcurrent = 4
)

type cachedMikanSubject struct {
	subjectID string
	expiresAt time.Time
}

type cachedMikanResults struct {
	items     []SearchResult
	expiresAt time.Time
}

var mikanBangumiMappings = struct {
	sync.RWMutex
	byMikan   map[string]cachedMikanSubject
	bySubject map[string]cachedMikanResults
}{
	byMikan:   make(map[string]cachedMikanSubject),
	bySubject: make(map[string]cachedMikanResults),
}

var mikanBangumiSubjectLinkRegex = regexp.MustCompile(`(?i)https?://(?:bgm\.tv|bangumi\.tv|chii\.in)/subject/(\d+)`)

func NewMikanParser() *MikanParser {
	client := httpx.NewRestyClient(10*time.Second, "", nil).
		SetRetryCount(2).
		SetRetryWaitTime(250 * time.Millisecond).
		SetRetryMaxWaitTime(time.Second).
		AddRetryCondition(func(response *resty.Response, err error) bool {
			if err != nil {
				return true
			}
			if response == nil {
				return false
			}
			return response.StatusCode() == http.StatusTooManyRequests || response.StatusCode() >= http.StatusInternalServerError
		})

	return &MikanParser{
		client: client,
	}
}

// MikanIDFromRSSURL extracts the Mikan bangumi identifier from an official
// Bangumi RSS URL. Mikan calls this query parameter "bangumiId", but it is
// unrelated to the subject identifier used by bgm.tv.
func MikanIDFromRSSURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "mikanani.me" && host != "www.mikanani.me" {
		return "", false
	}
	if !strings.EqualFold(strings.TrimSuffix(parsed.Path, "/"), "/RSS/Bangumi") {
		return "", false
	}

	id := strings.TrimSpace(parsed.Query().Get("bangumiId"))
	if id == "" {
		return "", false
	}
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return "", false
	}
	return id, true
}

// SetProxy sets the proxy for the Mikan parser client
func (p *MikanParser) SetProxy(proxyURL string) error {
	normalized, err := httpx.NormalizeProxyURL(proxyURL)
	if err != nil {
		return err
	}
	if normalized == "" {
		p.client.RemoveProxy()
		return nil
	}
	p.client.SetProxy(normalized)
	return nil
}

func (p *MikanParser) Name() string {
	return "Mikan Project"
}

// FetchTorrentContext downloads a Mikan torrent using the same client as RSS
// discovery, so a configured Mikan proxy is honored. qBittorrent can then
// receive the file directly even when its own host cannot reach Mikan.
func (p *MikanParser) FetchTorrentContext(ctx context.Context, rawURL string) (string, []byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", nil, fmt.Errorf("torrent URL is empty")
	}

	resp, err := httpx.NewRequest(ctx, p.client).
		SetDoNotParseResponse(true).
		Get(rawURL)
	if err != nil {
		return "", nil, fmt.Errorf("download torrent: %w", err)
	}
	if resp.RawBody() == nil {
		return "", nil, fmt.Errorf("download torrent: empty response body")
	}
	defer func() {
		if closeErr := resp.RawBody().Close(); closeErr != nil {
			log.Printf("Mikan torrent response close failed: %v", closeErr)
		}
	}()

	if resp.StatusCode() != http.StatusOK {
		return "", nil, fmt.Errorf("download torrent: unexpected status %s", resp.Status())
	}
	if resp.RawResponse != nil && resp.RawResponse.ContentLength > maxTorrentFileSize {
		return "", nil, fmt.Errorf("download torrent: file exceeds %d MiB limit", maxTorrentFileSize>>20)
	}

	data, err := io.ReadAll(io.LimitReader(resp.RawBody(), maxTorrentFileSize+1))
	if err != nil {
		return "", nil, fmt.Errorf("download torrent body: %w", err)
	}
	if len(data) > maxTorrentFileSize {
		return "", nil, fmt.Errorf("download torrent: file exceeds %d MiB limit", maxTorrentFileSize>>20)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "", nil, fmt.Errorf("download torrent: response is empty")
	}
	if trimmed[0] != 'd' {
		return "", nil, fmt.Errorf("download torrent: response is not a bencoded torrent file")
	}

	return torrentResponseFilename(rawURL, resp.Header()), data, nil
}

func torrentResponseFilename(rawURL string, header http.Header) string {
	filename := ""
	if disposition := header.Get("Content-Disposition"); disposition != "" {
		if _, params, err := mime.ParseMediaType(disposition); err == nil {
			filename = params["filename"]
		}
	}
	if filename == "" {
		if parsed, err := url.Parse(rawURL); err == nil {
			filename = path.Base(parsed.Path)
		}
	}
	filename = path.Base(strings.ReplaceAll(strings.TrimSpace(filename), `\`, "/"))
	if filename == "" || filename == "." || filename == "/" {
		filename = "mikan.torrent"
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".torrent") {
		filename += ".torrent"
	}
	return filename
}

// RSS/Atom XML 结构定义
// Mikan actually uses a custom namespace for the torrent element
// xmlns="https://mikanani.me/0.1/"
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
			} `xml:"https://mikanani.me/0.1/ torrent"`
			PubDate string `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
}

func (p *MikanParser) Parse(url string) ([]Episode, error) {
	return p.ParseContext(context.Background(), url)
}

func (p *MikanParser) ParseContext(ctx context.Context, url string) ([]Episode, error) {
	resp, err := httpx.NewRequest(ctx, p.client).Get(url)
	if err != nil {
		return nil, err
	}

	var rss MikanRSS
	if err := xml.Unmarshal(resp.Body(), &rss); err != nil {
		return nil, fmt.Errorf("xml unmarshal error: %v", err)
	}

	var episodes []Episode
	for _, item := range rss.Channel.Items {
		// 这里简单解析一下 Title
		ep := ParseTitle(item.Title)
		ep.TorrentURL = item.Enclosure.URL

		// Fix: 如果没有 Magnet，使用 TorrentURL 作为替代下载链接
		// 很多 RSS 只提供 .torrent 下载地址，PikPak 等工具支持直接传入
		if ep.Magnet == "" && ep.TorrentURL != "" {
			ep.Magnet = ep.TorrentURL
		}

		// Calculate Size
		if item.Torrent.ContentLength > 0 {
			// Simple formatting
			sizeMB := float64(item.Torrent.ContentLength) / 1024 / 1024
			if sizeMB > 1024 {
				ep.Size = fmt.Sprintf("%.2f GB", sizeMB/1024)
			} else {
				ep.Size = fmt.Sprintf("%.2f MB", sizeMB)
			}
		}

		// 处理时间
		// Mikan usually puts PubDate inside the torrent element now?
		// Or if item.PubDate is empty, try torrent.PubDate
		rawDate := item.PubDate
		if rawDate == "" {
			rawDate = item.Torrent.PubDate
		}

		// Try parsing multiple formats
		formats := []string{
			time.RFC1123Z,
			"2006-01-02T15:04:05.999999", // From XML: 2025-10-28T20:40:03.684339
			"2006-01-02T15:04:05.999",
			time.RFC3339,
		}

		var t time.Time
		for _, f := range formats {
			if parsed, err := time.Parse(f, rawDate); err == nil {
				t = parsed
				break
			}
		}
		ep.PubDate = t

		episodes = append(episodes, ep)
	}

	return episodes, nil
}

// 简单的正则解析器 (初步实现，后续需增强)
// 示例标题: [Moozzi2] Fate/stay night [Unlimited Blade Works] - 25 (BD 1920x1080 x264 Flac) TV-rip
// [LoliHouse] 葬送的芙莉莲 / Sousou no Frieren - 28 [WebRip 1080p HEVC-10bit AAC][简繁内封字幕]
func ParseTitle(title string) Episode {
	var ep Episode
	ep.Title = title

	// 1. 尝试提取字幕组 (通常在开头的 [])
	groupRegex := regexp.MustCompile(`^\[(.*?)\]`)
	if match := groupRegex.FindStringSubmatch(title); len(match) > 1 {
		ep.SubGroup = match[1]
	}

	// 2. 提取清晰度。Mikan RSS 没有独立的清晰度字段，只能从发布标题中识别。
	resolutionRegex := regexp.MustCompile(`(?i)(^|[^a-z0-9])(2160p|4k|uhd|3840x2160|1080p|fhd|1920x1080|720p|1280x720)([^a-z0-9]|$)`)
	if match := resolutionRegex.FindStringSubmatch(title); len(match) > 2 {
		switch strings.ToLower(match[2]) {
		case "4k", "uhd", "3840x2160":
			ep.Resolution = "2160p"
		case "fhd", "1920x1080":
			ep.Resolution = "1080p"
		case "1280x720":
			ep.Resolution = "720p"
		default:
			ep.Resolution = strings.ToLower(match[2])
		}
	}

	// 3. 尝试提取集数
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

func (p *MikanParser) Search(keyword string) ([]SearchResult, error) {
	return p.SearchContext(context.Background(), keyword)
}

func (p *MikanParser) SearchContext(ctx context.Context, keyword string) ([]SearchResult, error) {
	// Search URL: https://mikanani.me/Home/Search?searchstr={keyword}
	encodedKeyword := url.QueryEscape(keyword)
	url := fmt.Sprintf("https://mikanani.me/Home/Search?searchstr=%s", encodedKeyword)

	resp, err := httpx.NewRequest(ctx, p.client).Get(url)
	if err != nil {
		log.Printf("Search Request Failed: %v", err)
		return nil, err
	}

	htmlContent := string(resp.Body())
	log.Printf("DEBUG: Mikan Search HTML Len: %d", len(htmlContent))

	// Regex to extract anime entries
	// Structure: <a href="/Home/Bangumi/3141" ...> ... <span data-src="..."> ... <div class="an-text" title="...">
	// We use `(?s)` to allow . to match newlines
	// Relaxed Regex: Look for the Bangumi ID link, then image, then title
	// We handle potential variation in attribute order for the title div by just looking for title="..." inside the block
	re := regexp.MustCompile(`(?s)href="/Home/Bangumi/(\d+)".*?data-src="([^"]+)".*?class="an-text".*?title="([^"]+)"`)

	matches := re.FindAllStringSubmatch(htmlContent, -1)
	log.Printf("DEBUG: Mikan Search Matches Found: %d", len(matches))

	var results []SearchResult
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}

		img := match[2]
		if len(img) > 0 && img[0] == '/' {
			img = "https://mikanani.me" + img
		}

		results = append(results, SearchResult{
			MikanID: match[1],
			Image:   img,
			Title:   htmlUnescape(match[3]),
		})
	}

	return results, nil
}

// ResolveBangumiSubjectContext searches Mikan by title, then verifies each
// candidate against the bgm.tv subject link published on its Mikan detail
// page. Mikan IDs and bgm.tv subject IDs are deliberately kept separate.
func (p *MikanParser) ResolveBangumiSubjectContext(ctx context.Context, subjectID, keyword string) ([]SearchResult, error) {
	subjectID = strings.TrimSpace(subjectID)
	keyword = strings.TrimSpace(keyword)
	if _, err := strconv.ParseUint(subjectID, 10, 64); err != nil {
		return nil, fmt.Errorf("invalid Bangumi subject ID")
	}
	if keyword == "" {
		return nil, fmt.Errorf("mikan search keyword is empty")
	}
	if cached, ok := cachedMikanResultsForSubject(subjectID); ok {
		return cached, nil
	}

	candidates, err := p.SearchContext(ctx, keyword)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return []SearchResult{}, nil
	}

	matched := make([]bool, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	semaphore := make(chan struct{}, mikanMappingConcurrent)
	var wg sync.WaitGroup
	var resultMu sync.Mutex
	var firstErr error

	for index := range candidates {
		mikanID := strings.TrimSpace(candidates[index].MikanID)
		if _, duplicate := seen[mikanID]; mikanID == "" || duplicate {
			continue
		}
		seen[mikanID] = struct{}{}
		wg.Add(1)
		go func(candidateIndex int, candidateMikanID string) {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				resultMu.Lock()
				if firstErr == nil {
					firstErr = ctx.Err()
				}
				resultMu.Unlock()
				return
			}

			resolvedSubjectID, resolveErr := p.bangumiSubjectIDContext(ctx, candidateMikanID)
			resultMu.Lock()
			defer resultMu.Unlock()
			if resolveErr != nil {
				if firstErr == nil {
					firstErr = resolveErr
				}
				return
			}
			if resolvedSubjectID == subjectID {
				matched[candidateIndex] = true
			}
		}(index, mikanID)
	}
	wg.Wait()

	results := make([]SearchResult, 0, len(candidates))
	for index, isMatch := range matched {
		if !isMatch {
			continue
		}
		item := candidates[index]
		item.BangumiSubjectID = subjectID
		results = append(results, item)
	}
	if len(results) > 0 {
		cacheMikanResultsForSubject(subjectID, results)
		return results, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return []SearchResult{}, nil
}

func (p *MikanParser) bangumiSubjectIDContext(ctx context.Context, mikanID string) (string, error) {
	if subjectID, ok := cachedSubjectForMikan(mikanID); ok {
		return subjectID, nil
	}

	detailURL := fmt.Sprintf("https://mikanani.me/Home/Bangumi/%s", url.PathEscape(mikanID))
	resp, err := httpx.NewRequest(ctx, p.client).Get(detailURL)
	if err != nil {
		return "", err
	}
	if resp.IsError() {
		return "", fmt.Errorf("mikan detail request returned HTTP %d", resp.StatusCode())
	}

	match := mikanBangumiSubjectLinkRegex.FindStringSubmatch(html.UnescapeString(string(resp.Body())))
	subjectID := ""
	if len(match) > 1 {
		subjectID = match[1]
	}
	cacheSubjectForMikan(mikanID, subjectID)
	return subjectID, nil
}

func cachedSubjectForMikan(mikanID string) (string, bool) {
	now := time.Now()
	mikanBangumiMappings.RLock()
	cached, ok := mikanBangumiMappings.byMikan[mikanID]
	mikanBangumiMappings.RUnlock()
	if !ok || !now.Before(cached.expiresAt) {
		return "", false
	}
	return cached.subjectID, true
}

func cacheSubjectForMikan(mikanID, subjectID string) {
	mikanBangumiMappings.Lock()
	mikanBangumiMappings.byMikan[mikanID] = cachedMikanSubject{subjectID: subjectID, expiresAt: time.Now().Add(mikanMappingCacheTTL)}
	mikanBangumiMappings.Unlock()
}

func cachedMikanResultsForSubject(subjectID string) ([]SearchResult, bool) {
	now := time.Now()
	mikanBangumiMappings.RLock()
	cached, ok := mikanBangumiMappings.bySubject[subjectID]
	mikanBangumiMappings.RUnlock()
	if !ok || !now.Before(cached.expiresAt) {
		return nil, false
	}
	return append([]SearchResult(nil), cached.items...), true
}

func cacheMikanResultsForSubject(subjectID string, items []SearchResult) {
	mikanBangumiMappings.Lock()
	mikanBangumiMappings.bySubject[subjectID] = cachedMikanResults{
		items:     append([]SearchResult(nil), items...),
		expiresAt: time.Now().Add(mikanMappingCacheTTL),
	}
	mikanBangumiMappings.Unlock()
}

func (p *MikanParser) GetSubgroups(bangumiID string) ([]Subgroup, error) {
	return p.GetSubgroupsContext(context.Background(), bangumiID)
}

func (p *MikanParser) GetSubgroupsContext(ctx context.Context, bangumiID string) ([]Subgroup, error) {
	url := fmt.Sprintf("https://mikanani.me/Home/Bangumi/%s", bangumiID)

	resp, err := httpx.NewRequest(ctx, p.client).Get(url)
	if err != nil {
		return nil, err
	}

	htmlContent := string(resp.Body())

	// Regex to find subgroups
	// Structure: <div class="subgroup-text" id="(\d+)"> followed by the subgroup name link
	// We look for the link that doesn't have the mikan-rss class and contains the name
	re := regexp.MustCompile(`(?s)<div class="subgroup-text" id="(\d+)">.*?<a[^>]*style="color:[^>]*>(.*?)</a>`)
	matches := re.FindAllStringSubmatch(htmlContent, -1)

	var subgroups []Subgroup
	// Always add "全部" as the first option
	subgroups = append(subgroups, Subgroup{ID: "", Name: "全部 (All)"})

	seen := make(map[string]bool)
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		id := match[1]
		name := htmlUnescape(match[2])
		if !seen[id] {
			subgroups = append(subgroups, Subgroup{ID: id, Name: name})
			seen[id] = true
		}
	}

	return subgroups, nil
}

func (p *MikanParser) GetDashboard(year, season string) (*MikanDashboard, error) {
	return p.GetDashboardContext(context.Background(), year, season)
}

func (p *MikanParser) GetDashboardContext(ctx context.Context, year, season string) (*MikanDashboard, error) {
	baseUrl := "https://mikanani.me/"
	if year != "" && season != "" {
		baseUrl = fmt.Sprintf("https://mikanani.me/Home/BangumiCoverFlowByDayOfWeek?year=%s&seasonStr=%s", year, url.QueryEscape(season))
	}

	resp, err := httpx.NewRequest(ctx, p.client).Get(baseUrl)
	if err != nil {
		return nil, err
	}

	htmlContent := string(resp.Body())

	dashboard := &MikanDashboard{
		Days: make(map[string][]SearchResult),
	}

	// 1. Extract Season
	// Example: <div class="sk-col date-text"> 2025 &#x79CB;&#x5B63;&#x756B;&#x7EC4; <span class="caret"></span> </div>
	// If it's the AJAX endpoint, it might not have the full container, but we can still try
	seasonRegex := regexp.MustCompile(`(?s)<div class="sk-col date-text">\s*(.*?)\s*<span class="caret">`)
	if match := seasonRegex.FindStringSubmatch(htmlContent); len(match) > 1 {
		dashboard.Season = htmlUnescape(match[1])
	} else if year != "" && season != "" {
		// Fallback for AJAX response which might just be the grid
		dashboard.Season = fmt.Sprintf("%s %s季番组", year, season)
	}

	// 2. Extract Days and Anime
	// Mikan uses <div class="sk-bangumi" data-dayofweek="X">
	// Use a more inclusive regex for days that captures everything until the next sk-bangumi or end of content

	// Actually, a simpler way to split by day might be better
	daysSplit := regexp.MustCompile(`(?s)<div class="sk-bangumi" data-dayofweek="(\d+)">`).FindAllStringSubmatchIndex(htmlContent, -1)
	for i, matchIdx := range daysSplit {
		dayID := htmlContent[matchIdx[2]:matchIdx[3]]
		start := matchIdx[1]
		end := len(htmlContent)
		if i+1 < len(daysSplit) {
			end = daysSplit[i+1][0]
		}
		dayContent := htmlContent[start:end]

		// Extract anime items in this day
		// Selector: span.js-expand_bangumi for ID/Image, a.an-text for Title
		// We use a more flexible regex that allows other tags between the attributes
		animeRegex := regexp.MustCompile(`(?s)data-src="([^"]+)"[^{}]*?data-bangumiid="(\d+)"[^{}]*?class="an-text"[^{}]*?title="([^"]+)"`)
		animeMatches := animeRegex.FindAllStringSubmatch(dayContent, -1)

		for _, animeMatch := range animeMatches {
			img := animeMatch[1]
			// Handle relative URLs
			if len(img) > 0 && img[0] == '/' {
				img = "https://mikanani.me" + img
			}
			dashboard.Days[dayID] = append(dashboard.Days[dayID], SearchResult{
				MikanID: animeMatch[2],
				Image:   img,
				Title:   htmlUnescape(animeMatch[3]),
			})
		}
	}

	return dashboard, nil
}

// Simple wrapper for html.UnescapeString if we don't want to import "html" everywhere,
func htmlUnescape(s string) string {
	return html.UnescapeString(s)
}
