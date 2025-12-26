package parser

import (
	"encoding/xml"
	"os"
)

// TVShowNFO represents tvshow.nfo
type TVShowNFO struct {
	XMLName    xml.Name `xml:"tvshow"`
	Title      string   `xml:"title"`
	Original   string   `xml:"originaltitle"`
	Plot       string   `xml:"plot"`
	Userrating float64  `xml:"userrating"`
	Year       string   `xml:"year"`
	// IDs
	BangumiID int `xml:"bangumiid"` // Custom or from plugin
	TMDBID    int `xml:"tmdbid"`
	TVDBID    int `xml:"id"` // Often TVDB ID in <id>
}

// EpisodeNFO represents tiny nfo for episodes
type EpisodeNFO struct {
	XMLName xml.Name `xml:"episodedetails"`
	Title   string   `xml:"title"`
	Season  int      `xml:"season"`
	Episode int      `xml:"episode"`
	Plot    string   `xml:"plot"`
}

func ParseTVShowNFO(path string) (*TVShowNFO, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var nfo TVShowNFO
	if err := xml.Unmarshal(data, &nfo); err != nil {
		return nil, err
	}
	return &nfo, nil
}
