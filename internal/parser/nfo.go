package parser

import (
	"encoding/xml"
	"os"
)

// UniqueID supports multiple IDs for Kodi/Jellyfin
type UniqueID struct {
	Type    string `xml:"type,attr"`
	Default string `xml:"default,attr,omitempty"`
	Value   string `xml:",chardata"`
}

// Actor represents cast members
type Actor struct {
	Name  string `xml:"name"`
	Role  string `xml:"role,omitempty"`
	Thumb string `xml:"thumb,omitempty"`
}

// TVShowNFO represents tvshow.nfo
type TVShowNFO struct {
	XMLName    xml.Name   `xml:"tvshow"`
	Title      string     `xml:"title"`
	Original   string     `xml:"originaltitle,omitempty"`
	SortTitle  string     `xml:"sorttitle,omitempty"`
	Plot       string     `xml:"plot,omitempty"`
	Userrating float64    `xml:"userrating,omitempty"`
	Year       string     `xml:"year,omitempty"`
	Premiered  string     `xml:"premiered,omitempty"` // YYYY-MM-DD
	Status     string     `xml:"status,omitempty"`    // Continuing / Ended
	Studio     []string   `xml:"studio,omitempty"`
	Genre      []string   `xml:"genre,omitempty"`
	Actor      []Actor    `xml:"actor,omitempty"`
	UniqueIDs  []UniqueID `xml:"uniqueid"`

	// Legacy simple IDs for compatibility if needed, though UniqueID is preferred
	BangumiID int `xml:"bangumiid,omitempty"`
	TMDBID    int `xml:"tmdbid,omitempty"`
}

// EpisodeNFO represents tiny nfo for episodes
type EpisodeNFO struct {
	XMLName   xml.Name   `xml:"episodedetails"`
	Title     string     `xml:"title"`
	Season    int        `xml:"season"`
	Episode   int        `xml:"episode"`
	Plot      string     `xml:"plot,omitempty"`
	Thumb     string     `xml:"thumb,omitempty"`
	Aired     string     `xml:"aired,omitempty"` // YYYY-MM-DD
	UniqueIDs []UniqueID `xml:"uniqueid"`
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
