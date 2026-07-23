package service

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"gorm.io/gorm"
)

const (
	scanSpecialDirectoryName  = "special"
	scanSpecialsDirectoryName = "specials"
)

var (
	scanRunMu               sync.Mutex
	scanLeadingGroupPattern = regexp.MustCompile(`^(?:\[[^\]]+\]\s*)+`)
	scanBracketPattern      = regexp.MustCompile(`\[[^\]]*\]`)
	scanParenPattern        = regexp.MustCompile(`\([^)]*\)`)
	scanWhitespacePattern   = regexp.MustCompile(`\s+`)
	scanSeasonPatterns      = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:^|[\s._-])s(?:eason)?[\s._-]*0*(\d+)(?:$|[\s._-])`),
		regexp.MustCompile(`(?i)(?:^|[\s._-])(?:season|series)[\s._-]*0*(\d+)(?:$|[\s._-])`),
		regexp.MustCompile(`(?i)(?:^|[\s._-])(\d+)(?:st|nd|rd|th)[\s._-]+season(?:$|[\s._-])`),
		regexp.MustCompile(`第\s*(\d+)\s*[季期]`),
	}
	scanSeasonKeyPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:^|[\s._-])s(?:eason)?[\s._-]*0*\d+(?:$|[\s._-])`),
		regexp.MustCompile(`(?i)(?:^|[\s._-])(?:season|series)[\s._-]*0*\d+(?:$|[\s._-])`),
		regexp.MustCompile(`(?i)(?:^|[\s._-])\d+(?:st|nd|rd|th)[\s._-]+season(?:$|[\s._-])`),
		regexp.MustCompile(`(?i)(?:^|[\s._-])part[\s._-]*\d+(?:$|[\s._-])`),
		regexp.MustCompile(`第\s*\d+\s*[季期]`),
		regexp.MustCompile(`第[一二三四五六七八九十]+[季期]`),
	}
	scanTechnicalPattern  = regexp.MustCompile(`(?i)(?:^|[\s._-])(?:360p|480p|720p|1080p|2160p|4k|8k|fhd|uhd|hdtv|webrip|web-dl|webdl|bdrip|bluray|x264|x265|h264|h265|hevc|av1|aac|flac|10bit|8bit)(?:$|[\s._-])`)
	scanEpisodeDirPattern = regexp.MustCompile(`(?i)^(?:episode|episodes|ep|e)[\s._-]*0*\d+$`)
	scanSxEDirPattern     = regexp.MustCompile(`(?i)^s\d+[\s._-]*e\d+$`)
	scanQualityDirPattern = regexp.MustCompile(`(?i)^(?:360p|480p|720p|1080p|2160p|4k|8k|fhd|uhd)$`)
	scanYearPattern       = regexp.MustCompile(`(?:19|20)\d{2}`)
)

type ScannerService struct{}

func NewScannerService() *ScannerService {
	return &ScannerService{}
}

// ScanResult 代表扫描结果 stats
type ScanResult struct {
	DirectoryID uint
	Added       int
	Updated     int
	Deleted     int
}

type scannedMediaFile struct {
	Path         string
	Size         int64
	Parsed       parser.ParsedInfo
	Title        string
	Season       int
	Episode      int
	SeriesPath   string
	SeriesTitle  string
	SeriesKey    string
	Loose        bool
	ParsedSeason string
}

type scanCandidate struct {
	Key       string
	Title     string
	Path      string
	Files     []scannedMediaFile
	Seasons   map[int]struct{}
	AllLoose  bool
	FileCount int
	TotalSize int64
}

// ScanAll scans all configured directories. More specific nested roots run
// first and claim their physical files so overlapping library roots do not
// create duplicate series.
func (s *ScannerService) ScanAll() error {
	scanRunMu.Lock()
	defer scanRunMu.Unlock()

	st := localAnimeStore()
	if st == nil {
		return gorm.ErrInvalidDB
	}
	dirs, err := st.ListDirectories()
	if err != nil {
		return err
	}

	if len(dirs) == 0 {
		event.GlobalBus.Publish(event.EventScanRun, GlobalScanStatus.Skip("当前没有已配置目录可扫描"))
		return nil
	}

	sort.SliceStable(dirs, func(i, j int) bool {
		return len(canonicalComparisonPath(dirs[i].Path)) > len(canonicalComparisonPath(dirs[j].Path))
	})
	claimedFiles := make(map[string]struct{})
	event.GlobalBus.Publish(event.EventScanRun, GlobalScanStatus.Begin(len(dirs)))

	for i := range dirs {
		d := &dirs[i]
		res, scanErr := s.scanDirectory(d, claimedFiles)
		added := 0
		updated := 0
		if res != nil {
			added = res.Added
			updated = res.Updated
		}
		event.GlobalBus.Publish(event.EventScanRun, GlobalScanStatus.Advance(d.Path, added, updated, scanErr))
		if scanErr != nil {
			log.Printf("ScannerService: Failed to completely scan directory %s: %v", d.Path, scanErr)
		}
	}
	event.GlobalBus.Publish(event.EventScanRun, GlobalScanStatus.Finish())
	event.GlobalBus.Publish(event.EventScanComplete, map[string]interface{}{
		"scope":   "run",
		"summary": GlobalScanStatus.Snapshot().LastSummary,
	})
	return nil
}

// ScanDirectory scans one library root. It remains available for callers that
// explicitly rescan a single root and therefore does not share file claims.
func (s *ScannerService) ScanDirectory(dir *model.LocalAnimeDirectory) (*ScanResult, error) {
	scanRunMu.Lock()
	defer scanRunMu.Unlock()
	return s.scanDirectory(dir, nil)
}

func (s *ScannerService) scanDirectory(dir *model.LocalAnimeDirectory, claimedFiles map[string]struct{}) (*ScanResult, error) {
	if dir == nil || strings.TrimSpace(dir.Path) == "" {
		return nil, errors.New("scan directory path is empty")
	}

	root := filepath.Clean(dir.Path)
	issueKey := "scan:" + root
	log.Printf("ScannerService: Starting scan for %s", root)
	event.GlobalBus.Publish(event.EventScanProgress, map[string]interface{}{"type": "start", "dir": root})

	mediaFiles, walkErrors, fatalErr := discoverMediaFiles(root, claimedFiles)
	if fatalErr != nil {
		_ = reportScanIssue(issueKey, root, fatalErr)
		return nil, fatalErr
	}
	complete := len(walkErrors) == 0
	if complete {
		_ = ResolveLibraryIssue(issueKey)
	} else {
		_ = reportScanIssue(issueKey, root, errors.Join(walkErrors...))
	}

	candidates := buildScanCandidates(root, mediaFiles)
	st := localAnimeStore()
	if st == nil {
		return nil, gorm.ErrInvalidDB
	}
	existing, err := st.ListAnimesByDirectoryWithEpisodes(dir.ID)
	if err != nil {
		return nil, err
	}

	res := &ScanResult{DirectoryID: dir.ID}
	usedAnimeIDs := make(map[uint]struct{})
	for i := range candidates {
		candidate := &candidates[i]
		event.GlobalBus.Publish(event.EventScanProgress, map[string]interface{}{
			"type": "progress", "current": i + 1, "total": len(candidates), "dir": root,
		})
		anime := selectExistingAnime(candidate, existing, usedAnimeIDs)
		created := anime == nil
		animeRecordUpdated := false
		if created {
			anime = &model.LocalAnime{
				DirectoryID: dir.ID,
				Title:       candidate.Title,
				Path:        candidate.Path,
				Season:      candidateSeason(candidate),
			}
			if err := st.CreateAnime(anime); err != nil {
				log.Printf("Scanner: Create anime failed for %s: %v", candidate.Path, err)
				continue
			}
			res.Added++
		} else if updateAnimeFromCandidate(anime, dir.ID, candidate) {
			if err := st.SaveAnime(anime); err != nil {
				log.Printf("Scanner: Save anime failed for %s: %v", candidate.Path, err)
				continue
			}
			res.Updated++
			animeRecordUpdated = true
		}
		usedAnimeIDs[anime.ID] = struct{}{}

		episodeChanged := s.syncCandidateEpisodes(st, anime, candidate)
		if complete {
			paths := make([]string, 0, len(candidate.Files))
			for _, media := range candidate.Files {
				paths = append(paths, media.Path)
			}
			if err := st.DeleteEpisodesNotInPaths(anime.ID, paths); err != nil {
				log.Printf("Scanner: cleanup orphan episodes failed for %s: %v", candidate.Path, err)
			}
		}
		if !created && episodeChanged && !animeRecordUpdated {
			res.Updated++
		}

		if created {
			event.GlobalBus.Publish(event.EventMetadataUpdated, map[string]interface{}{
				"type": "new_anime", "id": anime.ID, "title": anime.Title,
			})
		}
	}

	if complete {
		for i := range existing {
			if _, used := usedAnimeIDs[existing[i].ID]; used {
				continue
			}
			if err := st.DeleteEpisodesNotInPaths(existing[i].ID, nil); err != nil {
				log.Printf("Scanner: cleanup stale anime %s failed: %v", existing[i].Path, err)
				continue
			}
			res.Deleted++
		}
		if err := st.CleanupOrphansByDirectory(dir.ID); err != nil {
			log.Printf("Scanner: cleanup empty anime rows failed for %s: %v", root, err)
		}
	}

	log.Printf("ScannerService: Scan complete. Added: %d, Updated: %d, Removed: %d", res.Added, res.Updated, res.Deleted)
	event.GlobalBus.Publish(event.EventScanComplete, map[string]interface{}{
		"scope": "directory", "directory_id": dir.ID, "directory": root,
		"added": res.Added, "updated": res.Updated, "deleted": res.Deleted,
	})

	if !complete {
		return res, errors.Join(walkErrors...)
	}
	return res, nil
}

func reportScanIssue(issueKey, root string, scanErr error) error {
	return ReportLibraryIssue(LibraryIssueInput{
		IssueKey:      issueKey,
		IssueType:     LibraryIssueTypeScan,
		Title:         filepath.Base(root),
		DirectoryPath: root,
		Message:       scanErr.Error(),
		Hint:          "检查目录是否存在，并确认应用对该目录及其子目录有读取权限。扫描不完整时不会删除已有记录。",
	})
}

func discoverMediaFiles(root string, claimed map[string]struct{}) ([]scannedMediaFile, []error, error) {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return nil, nil, err
	}
	if rootInfo.IsDir() {
		if _, err := os.ReadDir(root); err != nil {
			return nil, nil, err
		}
	}

	visitedDirectories := make(map[string]struct{})
	media := make([]scannedMediaFile, 0)
	walkErrors := make([]error, 0)
	var walk func(string)
	walk = func(path string) {
		info, statErr := os.Stat(path)
		if statErr != nil {
			walkErrors = append(walkErrors, fmt.Errorf("read %s: %w", path, statErr))
			return
		}
		if info.IsDir() {
			realDir := canonicalComparisonPath(path)
			if _, visited := visitedDirectories[realDir]; visited {
				return
			}
			visitedDirectories[realDir] = struct{}{}
			entries, readErr := os.ReadDir(path)
			if readErr != nil {
				walkErrors = append(walkErrors, fmt.Errorf("read directory %s: %w", path, readErr))
				return
			}
			for _, entry := range entries {
				if shouldSkipScanEntry(entry.Name(), entry.IsDir()) {
					continue
				}
				walk(filepath.Join(path, entry.Name()))
			}
			return
		}

		if !parser.IsVideoFile(path) || info.Size() == 0 {
			return
		}
		physicalPath := canonicalComparisonPath(path)
		if claimed != nil {
			if _, exists := claimed[physicalPath]; exists {
				return
			}
			claimed[physicalPath] = struct{}{}
		}
		media = append(media, inspectMediaFile(root, filepath.Clean(path), info.Size()))
	}
	walk(root)
	sort.Slice(media, func(i, j int) bool { return media[i].Path < media[j].Path })
	return media, walkErrors, nil
}

func shouldSkipScanEntry(name string, isDir bool) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || strings.HasPrefix(trimmed, ".") {
		return true
	}
	if !isDir {
		return false
	}
	switch strings.ToLower(trimmed) {
	case "@eadir", "$recycle.bin", "#recycle", "@recycle", "system volume information", "lost+found", "node_modules":
		return true
	default:
		return false
	}
}

func inspectMediaFile(root, path string, size int64) scannedMediaFile {
	parsed := parser.ParseFilename(path)
	seriesPath, loose := inferSeriesPath(root, path)
	season := parsed.Season
	if hint, ok := explicitSeasonFromAncestors(seriesPath, path); ok {
		season = hint
	}
	episode := parsed.Episode
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	nfoPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo"
	if nfo, err := parser.ParseEpisodeNFO(nfoPath); err == nil {
		if nfo.Episode > 0 {
			season = nfo.Season
			episode = nfo.Episode
		}
		if strings.TrimSpace(nfo.Title) != "" {
			title = strings.TrimSpace(nfo.Title)
		}
	} else if episode == 0 {
		parentName := filepath.Base(filepath.Dir(path))
		if scanEpisodeDirPattern.MatchString(parentName) || scanSxEDirPattern.MatchString(parentName) {
			parentParsed := parser.ParseFilename(parentName + filepath.Ext(path))
			if parentParsed.Episode > 0 {
				episode = parentParsed.Episode
				if parentParsed.Season > 0 {
					season = parentParsed.Season
				}
			}
		}
	}

	seriesTitle := inferSeriesTitle(root, seriesPath, path, parsed)
	if !loose {
		if nfo, err := parser.ParseTVShowNFO(filepath.Join(seriesPath, "tvshow.nfo")); err == nil && strings.TrimSpace(nfo.Title) != "" {
			seriesTitle = cleanSeriesDisplayTitle(nfo.Title)
			if year := strings.TrimSpace(nfo.Year); scanYearPattern.MatchString(year) && !scanYearPattern.MatchString(seriesTitle) {
				seriesTitle += " (" + scanYearPattern.FindString(year) + ")"
			}
		}
	}
	return scannedMediaFile{
		Path: path, Size: size, Parsed: parsed, Title: title, Season: season, Episode: episode,
		SeriesPath: seriesPath, SeriesTitle: seriesTitle, SeriesKey: canonicalSeriesKey(seriesTitle),
		Loose: loose, ParsedSeason: fmt.Sprintf("S%02d", season),
	}
}

func inferSeriesPath(root, mediaPath string) (string, bool) {
	parent := filepath.Dir(mediaPath)
	if sameComparisonPath(parent, root) {
		return mediaPath, true
	}
	seriesPath := parent
	for !sameComparisonPath(seriesPath, root) && isSeriesContainerDirectory(filepath.Base(seriesPath)) {
		seriesPath = filepath.Dir(seriesPath)
	}
	if !pathWithinRoot(root, seriesPath) {
		return mediaPath, true
	}
	return seriesPath, false
}

func isSeriesContainerDirectory(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if _, ok := explicitSeasonFromName(lower); ok && canonicalSeriesKey(lower) == "" {
		return true
	}
	switch lower {
	case scanSpecialDirectoryName, scanSpecialsDirectoryName, "ova", "oad", "extra", "extras", "特典", "特别篇", "番外", "bdmv", "stream", "streams", "video", "videos", "正片", "subtitle", "subtitles", "subs", "字幕":
		return true
	}
	return scanQualityDirPattern.MatchString(lower)
}

func explicitSeasonFromAncestors(seriesPath, mediaPath string) (int, bool) {
	for current := filepath.Dir(mediaPath); pathWithinRoot(seriesPath, current); current = filepath.Dir(current) {
		if season, ok := explicitSeasonFromName(filepath.Base(current)); ok {
			return season, true
		}
		lower := strings.ToLower(filepath.Base(current))
		switch lower {
		case scanSpecialDirectoryName, scanSpecialsDirectoryName, "ova", "oad", "extra", "extras", "特典", "特别篇", "番外":
			return 0, true
		}
		if sameComparisonPath(current, seriesPath) {
			break
		}
	}
	return 0, false
}

func explicitSeasonFromName(name string) (int, bool) {
	for _, pattern := range scanSeasonPatterns {
		match := pattern.FindStringSubmatch(name)
		if len(match) < 2 {
			continue
		}
		season, err := strconv.Atoi(match[1])
		if err == nil {
			return season, true
		}
	}
	cnSeasons := map[string]int{
		"第一季": 1, "第二季": 2, "第三季": 3, "第四季": 4, "第五季": 5,
		"第六季": 6, "第七季": 7, "第八季": 8, "第九季": 9, "第十季": 10,
		"第一期": 1, "第二期": 2, "第三期": 3, "第四期": 4, "第五期": 5,
	}
	for marker, season := range cnSeasons {
		if strings.Contains(name, marker) {
			return season, true
		}
	}
	return 0, false
}

func inferSeriesTitle(root, seriesPath, mediaPath string, parsed parser.ParsedInfo) string {
	folderTitle := filepath.Base(seriesPath)
	if sameComparisonPath(seriesPath, mediaPath) || sameComparisonPath(seriesPath, root) {
		folderTitle = filepath.Base(root)
	}
	folderTitle = cleanSeriesDisplayTitle(folderTitle)
	parsedTitle := cleanSeriesDisplayTitle(parsed.Title)
	if isUsableSeriesTitle(parsedTitle) {
		if year := scanYearPattern.FindString(folderTitle); year != "" && !scanYearPattern.MatchString(parsedTitle) {
			return parsedTitle + " (" + year + ")"
		}
		return parsedTitle
	}
	if isUsableSeriesTitle(folderTitle) {
		return folderTitle
	}
	return strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
}

func buildScanCandidates(root string, files []scannedMediaFile) []scanCandidate {
	byKey := make(map[string]*scanCandidate)
	for _, media := range files {
		key := media.SeriesKey
		if key == "" {
			key = "path:" + canonicalComparisonPath(media.SeriesPath)
		}
		candidate := byKey[key]
		if candidate == nil {
			candidate = &scanCandidate{
				Key: key, Title: media.SeriesTitle, Path: media.SeriesPath,
				Seasons: make(map[int]struct{}), AllLoose: true,
			}
			byKey[key] = candidate
		}
		if betterSeriesPath(media.SeriesPath, candidate.Path, media.Loose) {
			candidate.Path = media.SeriesPath
		}
		if betterSeriesTitle(media.SeriesTitle, candidate.Title) {
			candidate.Title = media.SeriesTitle
		}
		candidate.Files = append(candidate.Files, media)
		candidate.Seasons[media.Season] = struct{}{}
		candidate.AllLoose = candidate.AllLoose && media.Loose
		candidate.FileCount++
		candidate.TotalSize += media.Size
	}

	candidates := make([]scanCandidate, 0, len(byKey))
	for _, candidate := range byKey {
		if candidate.AllLoose && len(byKey) == 1 {
			candidate.Path = root
		}
		sort.Slice(candidate.Files, func(i, j int) bool { return candidate.Files[i].Path < candidate.Files[j].Path })
		candidates = append(candidates, *candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Title == candidates[j].Title {
			return candidates[i].Path < candidates[j].Path
		}
		return candidates[i].Title < candidates[j].Title
	})
	return candidates
}

func betterSeriesPath(next, current string, nextLoose bool) bool {
	if current == "" {
		return true
	}
	currentInfo, currentErr := os.Stat(current)
	currentLoose := currentErr != nil || !currentInfo.IsDir()
	if currentLoose != nextLoose {
		return !nextLoose
	}
	return next < current
}

func betterSeriesTitle(next, current string) bool {
	next = cleanSeriesDisplayTitle(next)
	current = cleanSeriesDisplayTitle(current)
	if current == "" {
		return next != ""
	}
	if next == "" {
		return false
	}
	return len([]rune(next)) < len([]rune(current))
}

func candidateSeason(candidate *scanCandidate) int {
	if len(candidate.Seasons) == 1 {
		for season := range candidate.Seasons {
			return season
		}
	}
	return 1
}

func selectExistingAnime(candidate *scanCandidate, existing []model.LocalAnime, used map[uint]struct{}) *model.LocalAnime {
	var selected *model.LocalAnime
	bestScore := -1
	candidatePaths := make(map[string]struct{}, len(candidate.Files))
	for _, media := range candidate.Files {
		candidatePaths[canonicalComparisonPath(media.Path)] = struct{}{}
	}
	for i := range existing {
		anime := &existing[i]
		if _, alreadyUsed := used[anime.ID]; alreadyUsed {
			continue
		}
		relationScore := 0
		if sameComparisonPath(anime.Path, candidate.Path) {
			relationScore = 4000
		}
		for _, episode := range anime.Episodes {
			if _, overlaps := candidatePaths[canonicalComparisonPath(episode.Path)]; overlaps && relationScore < 5000 {
				relationScore = 5000
				break
			}
		}
		animeKey := canonicalSeriesKey(anime.Title)
		pathKey := canonicalSeriesKey(filepath.Base(anime.Path))
		if (animeKey == candidate.Key || pathKey == candidate.Key) && relationScore < 2000 {
			relationScore = 2000
		}
		if relationScore == 0 {
			continue
		}
		score := relationScore + len(anime.Episodes)
		if anime.MetadataID != nil {
			score += 100000
		}
		if anime.JellyfinSeriesID != "" {
			score += 50000
		}
		if score > bestScore || (score == bestScore && (selected == nil || anime.ID < selected.ID)) {
			selected = anime
			bestScore = score
		}
	}
	return selected
}

func updateAnimeFromCandidate(anime *model.LocalAnime, directoryID uint, candidate *scanCandidate) bool {
	changed := false
	if anime.DirectoryID != directoryID {
		anime.DirectoryID = directoryID
		changed = true
	}
	if !sameComparisonPath(anime.Path, candidate.Path) {
		anime.Path = candidate.Path
		changed = true
	}
	if anime.MetadataID == nil && anime.Title != candidate.Title {
		anime.Title = candidate.Title
		changed = true
	}
	if anime.FileCount != candidate.FileCount {
		anime.FileCount = candidate.FileCount
		changed = true
	}
	if anime.TotalSize != candidate.TotalSize {
		anime.TotalSize = candidate.TotalSize
		changed = true
	}
	season := candidateSeason(candidate)
	if anime.Season != season {
		anime.Season = season
		changed = true
	}
	return changed
}

func (s *ScannerService) syncCandidateEpisodes(st interface {
	FindEpisodeByPathIncludingDeleted(string) (*model.LocalEpisode, error)
	CreateEpisode(*model.LocalEpisode) error
	SaveEpisodeIncludingDeleted(*model.LocalEpisode) error
}, anime *model.LocalAnime, candidate *scanCandidate) bool {
	changed := false
	for _, media := range candidate.Files {
		episode, err := st.FindEpisodeByPathIncludingDeleted(media.Path)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			episode = episodeFromMedia(anime.ID, media)
			if createErr := st.CreateEpisode(episode); createErr != nil {
				log.Printf("Scanner: Create episode failed for %s: %v", media.Path, createErr)
				continue
			}
			changed = true
			continue
		}
		if err != nil {
			log.Printf("Scanner: Find episode failed for %s: %v", media.Path, err)
			continue
		}
		if updateEpisodeFromMedia(episode, anime.ID, media) {
			if saveErr := st.SaveEpisodeIncludingDeleted(episode); saveErr != nil {
				log.Printf("Scanner: Save episode failed for %s: %v", media.Path, saveErr)
				continue
			}
			changed = true
		}
	}
	return changed
}

func episodeFromMedia(animeID uint, media scannedMediaFile) *model.LocalEpisode {
	return &model.LocalEpisode{
		LocalAnimeID: animeID, Title: media.Title, EpisodeNum: media.Episode, SeasonNum: media.Season,
		Path: media.Path, FileSize: media.Size, Container: media.Parsed.Extension,
		ParsedTitle: media.Parsed.Title, ParsedSeason: media.ParsedSeason,
		Resolution: media.Parsed.Resolution, SubGroup: media.Parsed.Group,
		VideoCodec: media.Parsed.VideoCodec, AudioCodec: media.Parsed.AudioCodec,
		BitDepth: media.Parsed.BitDepth, Source: media.Parsed.Source,
	}
}

func updateEpisodeFromMedia(episode *model.LocalEpisode, animeID uint, media scannedMediaFile) bool {
	changed := episode.DeletedAt.Valid || episode.LocalAnimeID != animeID || episode.Title != media.Title ||
		episode.EpisodeNum != media.Episode || episode.SeasonNum != media.Season || episode.FileSize != media.Size ||
		episode.Container != media.Parsed.Extension || episode.ParsedTitle != media.Parsed.Title ||
		episode.ParsedSeason != media.ParsedSeason || episode.Resolution != media.Parsed.Resolution ||
		episode.SubGroup != media.Parsed.Group || episode.VideoCodec != media.Parsed.VideoCodec ||
		episode.AudioCodec != media.Parsed.AudioCodec || episode.BitDepth != media.Parsed.BitDepth ||
		episode.Source != media.Parsed.Source
	if !changed {
		return false
	}
	episode.DeletedAt = gorm.DeletedAt{}
	episode.LocalAnimeID = animeID
	episode.Title = media.Title
	episode.EpisodeNum = media.Episode
	episode.SeasonNum = media.Season
	episode.FileSize = media.Size
	episode.Container = media.Parsed.Extension
	episode.ParsedTitle = media.Parsed.Title
	episode.ParsedSeason = media.ParsedSeason
	episode.Resolution = media.Parsed.Resolution
	episode.SubGroup = media.Parsed.Group
	episode.VideoCodec = media.Parsed.VideoCodec
	episode.AudioCodec = media.Parsed.AudioCodec
	episode.BitDepth = media.Parsed.BitDepth
	episode.Source = media.Parsed.Source
	return true
}

func cleanSeriesDisplayTitle(raw string) string {
	original := strings.TrimSpace(raw)
	s := original
	s = scanLeadingGroupPattern.ReplaceAllString(s, "")
	s = scanBracketPattern.ReplaceAllString(s, " ")
	s = scanParenPattern.ReplaceAllStringFunc(s, func(value string) string {
		inner := strings.TrimSpace(strings.Trim(value, "()"))
		if len(inner) == 4 {
			if year, err := strconv.Atoi(inner); err == nil && year >= 1900 && year <= 2100 {
				return " " + inner + " "
			}
		}
		return " "
	})
	for _, pattern := range scanSeasonKeyPatterns {
		s = pattern.ReplaceAllString(s, " ")
	}
	for scanTechnicalPattern.MatchString(s) {
		s = scanTechnicalPattern.ReplaceAllString(s, " ")
	}
	s = scanWhitespacePattern.ReplaceAllString(s, " ")
	s = strings.Trim(s, " ._-—–")
	if s == "" && strings.HasPrefix(original, "[") {
		if end := strings.Index(original, "]"); end > 1 {
			s = strings.TrimSpace(original[1:end])
		}
	}
	return s
}

func canonicalSeriesKey(raw string) string {
	s := strings.ToLower(cleanSeriesDisplayTitle(raw))
	var builder strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func isUsableSeriesTitle(title string) bool {
	key := canonicalSeriesKey(title)
	if key == "" {
		return false
	}
	hasLetter := false
	for _, r := range key {
		if unicode.IsLetter(r) {
			hasLetter = true
			break
		}
	}
	if !hasLetter {
		return false
	}
	switch key {
	case "episode", "episodes", "ep", "video", "videos", "anime", "season", scanSpecialDirectoryName, scanSpecialsDirectoryName, "unknown":
		return false
	default:
		return true
	}
}

func canonicalComparisonPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if absolute, err := filepath.Abs(cleaned); err == nil {
		cleaned = absolute
	}
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = resolved
	}
	cleaned = filepath.Clean(cleaned)
	if runtime.GOOS == "windows" {
		cleaned = strings.ToLower(cleaned)
	}
	return cleaned
}

func sameComparisonPath(left, right string) bool {
	return canonicalComparisonPath(left) == canonicalComparisonPath(right)
}

func pathWithinRoot(root, path string) bool {
	rootPath := canonicalComparisonPath(root)
	targetPath := canonicalComparisonPath(path)
	rel, err := filepath.Rel(rootPath, targetPath)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// CleanupGarbage removes anime rows that no longer own any live episodes.
func (s *ScannerService) CleanupGarbage() {
	st := localAnimeStore()
	if st == nil {
		return
	}
	if err := st.CleanupOrphans(); err != nil {
		log.Printf("Cleanup: CleanupOrphans failed: %v", err)
	}
}

// AddDirectory adds a canonical library root. Cleaning and resolving existing
// symlinks prevents the same physical directory from being registered under
// trailing-slash or alias variants.
func (s *ScannerService) AddDirectory(path string) error {
	path = canonicalComparisonPath(path)
	if strings.TrimSpace(path) == "" {
		return errors.New("library directory path is empty")
	}
	log.Printf("Adding directory: %s (Skipping strict existence check for cross-platform support)", path)

	st := localAnimeStore()
	if st == nil {
		return gorm.ErrInvalidDB
	}
	existing, err := st.FindDirectoryByPath(path, true)
	if err == nil && existing != nil {
		if existing.DeletedAt.Valid {
			log.Printf("Removing stale soft-deleted directory to allow fresh add: %s", path)
			if err := st.HardDeleteDirectory(existing); err != nil {
				return err
			}
		} else {
			return nil
		}
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	dir := &model.LocalAnimeDirectory{Path: path}
	return st.CreateDirectory(dir)
}

// RemoveDirectory 删除目录
func (s *ScannerService) RemoveDirectory(id uint) error {
	st := localAnimeStore()
	if st == nil {
		return gorm.ErrInvalidDB
	}
	return st.RemoveDirectoryWithAnimes(id)
}

func IsVideoFile(path string) bool {
	return parser.IsVideoFile(path)
}
