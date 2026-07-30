package service

import (
	"encoding/json"
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

const (
	nfoOverwriteLocalOnly    = "local-only"
	nfoOverwriteFieldLayered = "field-layered"
)

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
	if !mediaWriteEnabled(model.ConfigKeyWriteNFOEnabled) {
		return nil
	}
	if anime.Metadata == nil {
		return fmt.Errorf("metadata is nil")
	}

	if !s.checkAndReportWritability(anime) {
		return fmt.Errorf("directory %s is read-only", anime.Path)
	}

	meta := anime.Metadata
	nfo := parser.TVShowNFO{
		Title:      meta.Title,
		Original:   firstNonEmpty(meta.OriginalTitle, meta.TitleJP),
		SortTitle:  firstNonEmpty(meta.SortTitle, meta.Title),
		Plot:       meta.Summary,
		Userrating: 0,
		Year:       "",
		Premiered:  meta.AirDate,
		UniqueIDs:  []parser.UniqueID{},
	}
	nfo.Genre = decodeList(meta.Genres)
	nfo.Studio = decodeList(meta.Studios)
	for _, actor := range decodeList(meta.Actors) {
		nfo.Actor = append(nfo.Actor, parser.Actor{Name: actor})
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

	// Save
	path := filepath.Join(anime.Path, "tvshow.nfo")
	if existing, err := parser.ParseTVShowNFO(path); err == nil {
		switch metadataOverwritePolicy() {
		case nfoOverwriteLocalOnly:
			return nil
		case nfoOverwriteFieldLayered:
			mergeLocalTVShowNFO(existing, &nfo)
		}
	}
	return s.saveXML(path, nfo)
}

// GenerateEpisodeNFO generates {filename}.nfo for an episode
func (s *NFOGeneratorService) GenerateEpisodeNFO(ep *model.LocalEpisode, anime *model.LocalAnime) error {
	if !mediaWriteEnabled(model.ConfigKeyWriteNFOEnabled) {
		return nil
	}
	if ep == nil || anime == nil {
		return fmt.Errorf("nil argument")
	}

	if !s.checkAndReportWritability(anime) {
		return fmt.Errorf("directory %s is read-only", anime.Path)
	}

	nfo := parser.EpisodeNFO{
		Title:      ep.Title,
		Original:   ep.ParsedTitle,
		Season:     ep.SeasonNum,
		Episode:    ep.EpisodeNum,
		EpisodeEnd: ep.EpisodeEndNum,
		Type:       ep.EpisodeType,
		Plot:       "",
		UniqueIDs:  []parser.UniqueID{},
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
	if existing, err := parser.ParseEpisodeNFO(nfoPath); err == nil {
		switch metadataOverwritePolicy() {
		case nfoOverwriteLocalOnly:
			return nil
		case nfoOverwriteFieldLayered:
			mergeLocalEpisodeNFO(existing, &nfo)
		}
	}

	return s.saveXML(nfoPath, nfo)
}

// SaveLocalImages saves cached images (poster/backdrop) to disk
func (s *NFOGeneratorService) SaveLocalImages(anime *model.LocalAnime) error {
	if !mediaWriteEnabled(model.ConfigKeyWriteImagesEnabled) {
		return nil
	}
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
		if err := saveBytesAtomic(path, data, 0644); err != nil { //nolint:gosec
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

	writeImg("backdrop.jpg", anime.Metadata.TMDBBackdropRaw)

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

	if err := saveBytesAtomic(path, output, 0644); err != nil { //nolint:gosec
		log.Printf("NFO: Failed to write %s: %v", path, err)
		return err
	}
	log.Printf("NFO: Generated %s", path)
	return nil
}

func saveBytesAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".animate-atomic-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	renameErr := os.Rename(tmpPath, path)
	if renameErr == nil {
		return nil
	}
	if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
		return renameErr
	}
	return os.Rename(tmpPath, path)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func decodeList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err == nil {
		return values
	}
	return strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '|' || r == '\n' })
}

func mediaWriteEnabled(key string) bool {
	value := strings.ToLower(strings.TrimSpace(configValue(key)))
	return value == "" || value == model.ConfigValueTrue
}

func metadataOverwritePolicy() string {
	switch value := strings.ToLower(strings.TrimSpace(configValue(model.ConfigKeyMetadataOverwritePolicy))); value {
	case nfoOverwriteLocalOnly, "network-first":
		return value
	default:
		return "field-layered"
	}
}

func mergeLocalTVShowNFO(local, generated *parser.TVShowNFO) {
	if local == nil || generated == nil {
		return
	}
	generated.Title = firstNonEmpty(local.Title, generated.Title)
	generated.Original = firstNonEmpty(local.Original, generated.Original)
	generated.SortTitle = firstNonEmpty(local.SortTitle, generated.SortTitle)
	generated.Plot = firstNonEmpty(local.Plot, generated.Plot)
	generated.Year = firstNonEmpty(local.Year, generated.Year)
	generated.Premiered = firstNonEmpty(local.Premiered, generated.Premiered)
	generated.Status = firstNonEmpty(local.Status, generated.Status)
	if local.Userrating > 0 {
		generated.Userrating = local.Userrating
	}
	if len(local.Studio) > 0 {
		generated.Studio = local.Studio
	}
	if len(local.Genre) > 0 {
		generated.Genre = local.Genre
	}
	if len(local.Actor) > 0 {
		generated.Actor = local.Actor
	}
	if len(local.UniqueIDs) > 0 {
		generated.UniqueIDs = mergeUniqueIDs(local.UniqueIDs, generated.UniqueIDs)
	}
	if local.BangumiID > 0 {
		generated.BangumiID = local.BangumiID
	}
	if local.TMDBID > 0 {
		generated.TMDBID = local.TMDBID
	}
}

func mergeLocalEpisodeNFO(local, generated *parser.EpisodeNFO) {
	if local == nil || generated == nil {
		return
	}
	generated.Title = firstNonEmpty(local.Title, generated.Title)
	generated.Original = firstNonEmpty(local.Original, generated.Original)
	generated.Plot = firstNonEmpty(local.Plot, generated.Plot)
	generated.Thumb = firstNonEmpty(local.Thumb, generated.Thumb)
	generated.Aired = firstNonEmpty(local.Aired, generated.Aired)
	if local.HasSeason() {
		generated.Season = local.Season
	}
	if local.Episode > 0 {
		generated.Episode = local.Episode
	}
	if local.EpisodeEnd > 0 {
		generated.EpisodeEnd = local.EpisodeEnd
	}
	generated.Type = firstNonEmpty(local.Type, generated.Type)
	if len(local.UniqueIDs) > 0 {
		generated.UniqueIDs = mergeUniqueIDs(local.UniqueIDs, generated.UniqueIDs)
	}
}

func mergeUniqueIDs(primary, fallback []parser.UniqueID) []parser.UniqueID {
	result := make([]parser.UniqueID, 0, len(primary)+len(fallback))
	seen := map[string]bool{}
	for _, group := range [][]parser.UniqueID{primary, fallback} {
		for _, item := range group {
			key := strings.ToLower(strings.TrimSpace(item.Type))
			if key == "" {
				key = strings.TrimSpace(item.Value)
			}
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, item)
		}
	}
	return result
}
