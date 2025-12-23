package rss

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"time"
)

type RSS struct {
	Channel Channel `xml:"channel"`
}

type Channel struct {
	Title string `xml:"title"`
	Items []Item `xml:"item"`
}

type Item struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"` // Magnet link for Mikan often in enclosure or link? Mikan puts torrent link in 'link' and enclosure.
	Description string    `xml:"description"`
	PubDate     string    `xml:"pubDate"`
	Enclosure   Enclosure `xml:"enclosure"`
}

type Enclosure struct {
	URL  string `xml:"url,attr"`
	Type string `xml:"type,attr"`
}

type ParsedItem struct {
	Title string
	Link  string // Magnet or Torrent URL
	Date  time.Time
}

// ParseMikan fetches and parses Mikan RSS
func ParseMikan(url string) ([]ParsedItem, error) {
	client := http.Client{
		Timeout: 10 * time.Second,
	}

	// Create client with UA
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}

	var rss RSS
	if err := xml.NewDecoder(resp.Body).Decode(&rss); err != nil {
		return nil, err
	}

	var result []ParsedItem
	for _, item := range rss.Channel.Items {
		// Mikan RSS:
		// <link> is the page url
		// <enclosure url="..."> is the .torrent file url
		// But qBittorrent can often take .torrent urls.
		// Also Mikan usually provides magnet links in description sometimes?
		// Actually Mikan RSS Enclosure URL is usually the download link (.torrent).
		// Let's use Enclosure URL as the primary link.

		link := item.Enclosure.URL
		if link == "" {
			link = item.Link
		}

		// Parse Time
		// RFC1123Z usually
		t, _ := time.Parse(time.RFC1123Z, item.PubDate)

		result = append(result, ParsedItem{
			Title: item.Title,
			Link:  link,
			Date:  t,
		})
	}

	return result, nil
}
