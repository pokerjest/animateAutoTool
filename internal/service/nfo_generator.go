package service

import (
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
)

type NFOGeneratorService struct{}

func NewNFOGeneratorService() *NFOGeneratorService {
	return &NFOGeneratorService{}
}

func (s *NFOGeneratorService) checkAndReportWritability(anime *model.LocalAnime) bool {
	if anime == nil {
		return false
	}
	writable := isDirWritable(anime.Path)
	issueKey := "readonly:" + strconv.FormatUint(uint64(anime.ID), 10)
	if !writable {
		log.Printf("NFO: Directory %s is not writable. Reporting LibraryIssue...", anime.Path)
		_ = ReportLibraryIssue(LibraryIssueInput{
			IssueKey:      issueKey,
			IssueType:     LibraryIssueTypeScrape,
			Title:         "媒体目录只读 (Directory Read-Only)",
			DirectoryPath: anime.Path,
			LocalAnimeID:  &anime.ID,
			Message:       fmt.Sprintf("动漫《%s》的本地存放路径没有写权限，无法生成 NFO 元数据或本地海报缓存。", anime.Title),
			Hint:          "请检查您的系统权限、挂载点属性（如只读 SMB/NFS 或 Docker 卷映射只读），并确保给该路径授予读写权限。",
		})
		return false
	}

	// Writable, resolve any previous read-only issue
	_ = ResolveLibraryIssue(issueKey)
	return true
}

func isDirWritable(dir string) bool {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return false
	}
	testFile := filepath.Join(dir, ".animate_write_test")
	err := os.WriteFile(testFile, []byte{1}, 0o600)
	if err != nil {
		return false
	}
	_ = os.Remove(testFile)
	return true
}

// GenerateTVShowNFO generates tvshow.nfo for the series
func (s *NFOGeneratorService) GenerateTVShowNFO(anime *model.LocalAnime) error {
	if anime.Metadata == nil {
		return fmt.Errorf("metadata is nil")
	}

	if !s.checkAndReportWritability(anime) {
		return fmt.Errorf("directory %s is read-only", anime.Path)
	}

	meta := anime.Metadata
	nfo := parser.TVShowNFO{
		Title:      meta.Title,
		Original:   meta.TitleJP,
		SortTitle:  meta.Title,
		Plot:       meta.Summary,
		Userrating: 0,
		Year:       "",
		Premiered:  meta.AirDate,
		UniqueIDs:  []parser.UniqueID{},
	}

	// Ratings
	if meta.BangumiRating > 0 {
		nfo.Userrating = meta.BangumiRating
	} else if meta.TMDBRating > 0 {
		nfo.Userrating = meta.TMDBRating
	}

	// Year
	if len(meta.AirDate) >= 4 {
		nfo.Year = meta.AirDate[:4]
	}

	// IDs
	if meta.BangumiID != 0 {
		nfo.BangumiID = meta.BangumiID
		nfo.UniqueIDs = append(nfo.UniqueIDs, parser.UniqueID{
			Type:    "bangumi",
			Default: "true",
			Value:   strconv.Itoa(meta.BangumiID),
		})
	}
	if meta.TMDBID != 0 {
		nfo.TMDBID = meta.TMDBID
		nfo.UniqueIDs = append(nfo.UniqueIDs, parser.UniqueID{
			Type:  "tmdb",
			Value: strconv.Itoa(meta.TMDBID),
		})
	}
	if meta.AniListID != 0 {
		nfo.UniqueIDs = append(nfo.UniqueIDs, parser.UniqueID{
			Type:  "anilist",
			Value: strconv.Itoa(meta.AniListID),
		})
	}

	// TODO: Actors/Genres/Studios (Currently not in AnimeMetadata model, but supported in NFO struct)

	// Save
	path := filepath.Join(anime.Path, "tvshow.nfo")
	return s.saveXML(path, nfo)
}

// GenerateEpisodeNFO generates {filename}.nfo for an episode
func (s *NFOGeneratorService) GenerateEpisodeNFO(ep *model.LocalEpisode, anime *model.LocalAnime) error {
	if ep == nil || anime == nil {
		return fmt.Errorf("nil argument")
	}

	if !s.checkAndReportWritability(anime) {
		return fmt.Errorf("directory %s is read-only", anime.Path)
	}

	nfo := parser.EpisodeNFO{
		Title:     ep.Title,
		Season:    ep.SeasonNum,
		Episode:   ep.EpisodeNum,
		Plot:      "",
		UniqueIDs: []parser.UniqueID{},
	}

	// Inherit some data from series metadata if available?
	// Usually episode metadata needs specific scraping which we might not have yet.
	// For now, minimal NFO ensures Jellyfin recognizes S/E structure correctly.

	if ep.ParsedTitle != "" && nfo.Title == "" {
		nfo.Title = fmt.Sprintf("Episode %d", ep.EpisodeNum)
	}

	// Determine Path
	ext := filepath.Ext(ep.Path)
	nfoPath := strings.TrimSuffix(ep.Path, ext) + ".nfo"

	return s.saveXML(nfoPath, nfo)
}

// SaveLocalImages saves cached images (poster/backdrop) to disk
func (s *NFOGeneratorService) SaveLocalImages(anime *model.LocalAnime) error {
	if anime.Metadata == nil {
		return nil
	}

	if !s.checkAndReportWritability(anime) {
		return fmt.Errorf("directory %s is read-only", anime.Path)
	}

	// Helper to write blob
	writeImg := func(name string, data []byte) {
		if len(data) == 0 {
			return
		}
		path := filepath.Join(anime.Path, name)
		if _, err := os.Stat(path); err == nil {
			return // Exists
		}
		if err := os.WriteFile(path, data, 0644); err != nil { //nolint:gosec
			log.Printf("NFO: Failed to write image %s: %v", path, err)
		} else {
			log.Printf("NFO: Saved local image %s", path)
		}
	}

	// 1. Poster
	// Priority: TMDB > Bangumi > AniList (TMDB usually higher res)
	var poster []byte
	if len(anime.Metadata.TMDBImageRaw) > 0 {
		poster = anime.Metadata.TMDBImageRaw
	} else if len(anime.Metadata.BangumiImageRaw) > 0 {
		poster = anime.Metadata.BangumiImageRaw
	} else if len(anime.Metadata.AniListImageRaw) > 0 {
		poster = anime.Metadata.AniListImageRaw
	}
	writeImg("poster.jpg", poster)

	// 2. Fanart/Backdrop (Currently we only cache Poster in *ImageRaw fields)
	// If we want fanart, we need to extend Metadata model to cache BackdropRaw.
	// For now, skipping.

	return nil
}

func (s *NFOGeneratorService) saveXML(path string, v interface{}) error {
	output, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	// Add XML header
	header := []byte(xml.Header)
	output = append(header, output...)

	if err := os.WriteFile(path, output, 0644); err != nil { //nolint:gosec
		log.Printf("NFO: Failed to write %s: %v", path, err)
		return err
	}
	log.Printf("NFO: Generated %s", path)
	return nil
}
