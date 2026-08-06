package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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
	"github.com/pokerjest/animateAutoTool/internal/runtimejournal"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"gorm.io/gorm"
)

const (
	scanSpecialDirectoryName  = "special"
	scanSpecialsDirectoryName = "specials"
	scanEpisodeTypeEpisode    = "episode"
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

func IncrementalScanEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(configValue(model.ConfigKeyIncrementalScanEnabled)))
	return value == "" || value == model.ConfigValueTrue
}

// ScanResult 代表扫描结果 stats
type ScanResult struct {
	DirectoryID     uint
	DiscoveredFiles int
	CandidateSeries int
	WalkErrors      int
	Added           int
	Updated         int
	Deleted         int
	ParseFailures   int
	ParseConflicts  int
}

// TargetScanResult describes an incremental scan limited to the series
// directories inferred from completed download targets.
type TargetScanResult struct {
	DirectoryID      uint
	ScannedScopes    []string
	AffectedAnimeIDs []uint
	ScanResult       ScanResult
}

var errTargetScanNeedsFullRoot = errors.New("target scan overlaps media outside the inferred series scope")

// ScanProgress describes the current phase of a full local-library scan.
// Current and Total are monotonic work units: readable folders plus one
// finalization unit for each configured library root.
type ScanProgress struct {
	Phase        string
	Message      string
	Current      int64
	Total        int64
	FoldersFound int64
}

type ScanProgressFunc func(ScanProgress)

type scannedMediaFile struct {
	Path          string
	Size          int64
	Fingerprint   string
	Parsed        parser.ParsedInfo
	Title         string
	Season        int
	Episode       int
	SeriesPath    string
	SeriesTitle   string
	SeriesKey     string
	Loose         bool
	ParsedSeason  string
	ParseConflict string
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
	return s.ScanAllWithProgressContext(context.Background(), nil)
}

// ScanAllWithProgress first estimates the number of readable folders, then
// reports monotonic progress while the real scan processes those folders.
func (s *ScannerService) ScanAllWithProgress(report ScanProgressFunc) error {
	return s.ScanAllWithProgressContext(context.Background(), report)
}

func (s *ScannerService) ScanAllWithProgressContext(ctx context.Context, report ScanProgressFunc) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if runtimejournal.RecoveryBlocked() {
		return runtimejournal.ErrRecoveryBlocked
	}
	scanRunMu.Lock()
	defer scanRunMu.Unlock()
	if err := runtimejournal.BeginOperation(runtimejournal.OperationLocalLibraryScan); err != nil {
		log.Printf("WARN: failed to persist local scan operation marker: %v", err)
	}
	defer func() {
		if err := runtimejournal.EndOperation(runtimejournal.OperationLocalLibraryScan); err != nil {
			log.Printf("WARN: failed to clear local scan operation marker: %v", err)
		}
	}()

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
		emitScanProgress(report, ScanProgress{Phase: "complete", Message: "当前没有已配置目录可扫描"})
		return nil
	}

	sort.SliceStable(dirs, func(i, j int) bool {
		return len(canonicalComparisonPath(dirs[i].Path)) > len(canonicalComparisonPath(dirs[j].Path))
	})

	var folderEstimate int64
	for i := range dirs {
		baseCount := folderEstimate
		count, countErr := countScanDirectories(ctx, dirs[i].Path, func(current int64, _ string) {
			emitScanProgress(report, ScanProgress{
				Phase:        "planning",
				Message:      fmt.Sprintf("正在统计扫描范围，已发现约 %d 个文件夹", baseCount+current),
				FoldersFound: baseCount + current,
			})
		})
		folderEstimate += count
		if countErr != nil {
			log.Printf("ScannerService: Failed to estimate directory %s: %v", dirs[i].Path, countErr)
		}
	}
	totalWork := folderEstimate + int64(len(dirs))
	emitScanProgress(report, ScanProgress{
		Phase:        "scanning",
		Message:      fmt.Sprintf("扫描范围统计完成：约 %d 个文件夹，开始读取媒体文件", folderEstimate),
		Total:        totalWork,
		FoldersFound: folderEstimate,
	})

	claimedFiles := make(map[string]struct{})
	event.GlobalBus.Publish(event.EventScanRun, GlobalScanStatus.Begin(len(dirs)))
	var scannedFolders int64
	var lastReportedFolders int64
	reportStride := max(int64(1), folderEstimate/100)

	for i := range dirs {
		if err := ctx.Err(); err != nil {
			return err
		}
		d := &dirs[i]
		root := filepath.Clean(d.Path)
		res, scanErr := s.scanDirectoryContext(ctx, d, claimedFiles, func(path string) {
			scannedFolders++
			if scannedFolders != 1 && scannedFolders != folderEstimate && scannedFolders-lastReportedFolders < reportStride {
				return
			}
			lastReportedFolders = scannedFolders
			if scannedFolders > folderEstimate {
				folderEstimate = scannedFolders
				totalWork = folderEstimate + int64(len(dirs))
			}
			emitScanProgress(report, ScanProgress{
				Phase:        "scanning",
				Message:      fmt.Sprintf("正在扫描文件夹 %d/%d：%s", scannedFolders, folderEstimate, relativeScanPath(root, path)),
				Current:      scannedFolders + int64(i),
				Total:        totalWork,
				FoldersFound: folderEstimate,
			})
		})
		event.GlobalBus.Publish(event.EventScanRun, GlobalScanStatus.Advance(d.Path, res, scanErr))
		if scanErr != nil {
			log.Printf("ScannerService: Failed to completely scan directory %s: %v", d.Path, scanErr)
		}
		message := fmt.Sprintf("已完成 %d/%d 个扫描根目录", i+1, len(dirs))
		if res != nil {
			message = fmt.Sprintf("已完成 %d/%d 个扫描根目录，发现 %d 个媒体文件、%d 部番剧", i+1, len(dirs), res.DiscoveredFiles, res.CandidateSeries)
		}
		emitScanProgress(report, ScanProgress{
			Phase:        "finalizing",
			Message:      message,
			Current:      scannedFolders + int64(i+1),
			Total:        totalWork,
			FoldersFound: folderEstimate,
		})
	}
	event.GlobalBus.Publish(event.EventScanRun, GlobalScanStatus.Finish())
	event.GlobalBus.Publish(event.EventScanComplete, map[string]interface{}{
		"scope":   "run",
		"summary": GlobalScanStatus.Snapshot().LastSummary,
	})
	emitScanProgress(report, ScanProgress{
		Phase:        "complete",
		Message:      "文件扫描完成，正在准备元数据整理",
		Current:      totalWork,
		Total:        totalWork,
		FoldersFound: folderEstimate,
	})
	return nil
}

// ScanDirectory scans one library root. It remains available for callers that
// explicitly rescan a single root and therefore does not share file claims.
func (s *ScannerService) ScanDirectory(dir *model.LocalAnimeDirectory) (*ScanResult, error) {
	scanRunMu.Lock()
	defer scanRunMu.Unlock()
	return s.scanDirectoryContext(context.Background(), dir, nil, nil)
}

func (s *ScannerService) scanDirectoryContext(ctx context.Context, dir *model.LocalAnimeDirectory, claimedFiles map[string]struct{}, onDirectory func(string)) (*ScanResult, error) {
	if dir == nil {
		return nil, errors.New("scan directory path is empty")
	}
	result, _, err := s.scanScopeContext(ctx, dir, filepath.Clean(dir.Path), claimedFiles, onDirectory, true)
	return result, err
}

// ScanTargets incrementally scans only the series directories affected by the
// supplied download targets. It falls back to a full root scan when an
// existing database row spans files outside an inferred scope, because
// shrinking such a row would otherwise discard valid episodes.
func (s *ScannerService) ScanTargets(dir *model.LocalAnimeDirectory, targets []string) (*TargetScanResult, error) {
	scanRunMu.Lock()
	defer scanRunMu.Unlock()

	if dir == nil || strings.TrimSpace(dir.Path) == "" {
		return nil, errors.New("scan directory path is empty")
	}

	root := filepath.Clean(dir.Path)
	scopes, err := inferTargetScanScopes(root, targets)
	if err != nil {
		return nil, err
	}
	result := &TargetScanResult{
		DirectoryID:   dir.ID,
		ScannedScopes: make([]string, 0, len(scopes)),
		ScanResult:    ScanResult{DirectoryID: dir.ID},
	}
	affected := make(map[uint]struct{})
	for _, scope := range scopes {
		if _, statErr := os.Stat(scope); statErr != nil {
			if os.IsNotExist(statErr) {
				// qBittorrent can report completion just before a moved file is
				// visible. The delayed pass will retry this scope.
				continue
			}
			return result, statErr
		}
		scopeResult, scopeIDs, scanErr := s.scanScope(dir, scope, nil, nil, false)
		if errors.Is(scanErr, errTargetScanNeedsFullRoot) {
			fullResult, _, fullErr := s.scanScope(dir, root, nil, nil, true)
			if fullResult != nil {
				result.ScanResult = *fullResult
			}
			result.ScannedScopes = []string{root}
			result.AffectedAnimeIDs = s.affectedAnimeIDsForTargets(dir.ID, root, targets, scopes)
			return result, fullErr
		}
		if scopeResult != nil {
			mergeScanResult(&result.ScanResult, scopeResult)
		}
		result.ScannedScopes = append(result.ScannedScopes, scope)
		idsForScope := scopeIDs
		if sameComparisonPath(scope, root) {
			// A loose file in the library root necessarily scans the whole
			// root, but metadata work should still be limited to the completed
			// target rather than every series discovered during that scan.
			idsForScope = s.affectedAnimeIDsForTargets(dir.ID, root, targets, scopes)
		}
		for _, id := range idsForScope {
			affected[id] = struct{}{}
		}
		if scanErr != nil {
			result.AffectedAnimeIDs = sortedUintKeys(affected)
			return result, scanErr
		}
	}
	result.AffectedAnimeIDs = sortedUintKeys(affected)
	return result, nil
}

func (s *ScannerService) scanScope(
	dir *model.LocalAnimeDirectory,
	scanRoot string,
	claimedFiles map[string]struct{},
	onDirectory func(string),
	cleanupWholeDirectory bool,
) (*ScanResult, []uint, error) {
	return s.scanScopeContext(context.Background(), dir, scanRoot, claimedFiles, onDirectory, cleanupWholeDirectory)
}

//nolint:gocyclo // Scan scope preserves the existing safe scan/update/cleanup phases.
func (s *ScannerService) scanScopeContext(
	ctx context.Context,
	dir *model.LocalAnimeDirectory,
	scanRoot string,
	claimedFiles map[string]struct{},
	onDirectory func(string),
	cleanupWholeDirectory bool,
) (*ScanResult, []uint, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if dir == nil || strings.TrimSpace(dir.Path) == "" {
		return nil, nil, errors.New("scan directory path is empty")
	}

	root := filepath.Clean(scanRoot)
	libraryRoot := filepath.Clean(dir.Path)
	if !pathWithinRoot(libraryRoot, root) {
		return nil, nil, fmt.Errorf("target scan scope %s is outside library root %s", root, libraryRoot)
	}
	issueKey := "scan:" + root
	log.Printf("ScannerService: Starting scan for %s", root)
	event.GlobalBus.Publish(event.EventScanProgress, map[string]interface{}{"type": "start", "dir": root})

	mediaFiles, walkErrors, fatalErr := discoverMediaFiles(ctx, root, claimedFiles, onDirectory)
	if fatalErr != nil {
		_ = reportScanIssue(issueKey, root, fatalErr)
		return nil, nil, fatalErr
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
		return nil, nil, gorm.ErrInvalidDB
	}
	existing, err := st.ListAnimesByDirectoryWithEpisodes(dir.ID)
	if err != nil {
		return nil, nil, err
	}

	res := &ScanResult{
		DirectoryID:     dir.ID,
		DiscoveredFiles: len(mediaFiles),
		CandidateSeries: len(candidates),
		WalkErrors:      len(walkErrors),
	}
	for _, media := range mediaFiles {
		if media.Episode <= 0 {
			res.ParseFailures++
		}
		if media.ParseConflict != "" {
			res.ParseConflicts++
		}
	}
	usedAnimeIDs := make(map[uint]struct{})
	selectedAnimes := make([]*model.LocalAnime, len(candidates))
	for i := range candidates {
		selected := selectExistingAnime(&candidates[i], existing, usedAnimeIDs)
		if selected != nil {
			if !cleanupWholeDirectory && animeHasEpisodeOutsideScope(selected, root) {
				return res, nil, errTargetScanNeedsFullRoot
			}
			usedAnimeIDs[selected.ID] = struct{}{}
		}
		selectedAnimes[i] = selected
	}
	usedAnimeIDs = make(map[uint]struct{})
	affectedAnimeIDs := make([]uint, 0, len(candidates))
	for i := range candidates {
		if err := ctx.Err(); err != nil {
			return res, affectedAnimeIDs, err
		}
		candidate := &candidates[i]
		event.GlobalBus.Publish(event.EventScanProgress, map[string]interface{}{
			"type": "progress", "current": i + 1, "total": len(candidates), "dir": root,
		})
		anime := selectedAnimes[i]
		created := anime == nil
		animeRecordUpdated := false
		if created {
			anime = &model.LocalAnime{
				DirectoryID: dir.ID,
				Title:       candidate.Title,
				Path:        candidate.Path,
				ScanKey:     localAnimeScanKey(candidate.Path, candidate.AllLoose),
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
		affectedAnimeIDs = append(affectedAnimeIDs, anime.ID)

		episodeChanged := s.syncCandidateEpisodes(st, anime, candidate)
		s.syncCandidateParseIssues(anime, candidate)
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

	if err := ctx.Err(); err != nil {
		return res, affectedAnimeIDs, err
	}
	if complete && cleanupWholeDirectory {
		for i := range existing {
			if err := ctx.Err(); err != nil {
				return res, affectedAnimeIDs, err
			}
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
	if removed, cleanupErr := cleanupEmptyDuplicateAnimes(st, dir.ID, root); cleanupErr != nil {
		log.Printf("Scanner: cleanup empty duplicate anime rows failed for %s: %v", root, cleanupErr)
	} else if removed > 0 {
		res.Deleted += int(removed)
		log.Printf("Scanner: removed %d empty duplicate anime rows for %s", removed, root)
	}

	log.Printf(
		"ScannerService: Scan complete for %s. Media files: %d, candidate series: %d, added: %d, updated: %d, removed: %d, walk errors: %d",
		root, res.DiscoveredFiles, res.CandidateSeries, res.Added, res.Updated, res.Deleted, res.WalkErrors,
	)
	eventScope := "target"
	if cleanupWholeDirectory {
		eventScope = "directory"
	}
	event.GlobalBus.Publish(event.EventScanComplete, map[string]interface{}{
		"scope": eventScope, "directory_id": dir.ID, "directory": root,
		"discovered_files": res.DiscoveredFiles, "candidate_series": res.CandidateSeries, "walk_errors": res.WalkErrors,
		"added": res.Added, "updated": res.Updated, "deleted": res.Deleted,
	})

	if !complete {
		return res, affectedAnimeIDs, errors.Join(walkErrors...)
	}
	return res, affectedAnimeIDs, nil
}

// cleanupEmptyDuplicateAnimes removes ghost rows left by an earlier
// incremental scan. A path can legitimately be shared by multiple loose-file
// series, so only an empty row is removed when another row at that exact path
// already owns live episodes.
func cleanupEmptyDuplicateAnimes(st *store.LocalAnimeStore, directoryID uint, scope string) (int64, error) {
	if st == nil {
		return 0, gorm.ErrInvalidDB
	}
	animes, err := st.ListAnimesByDirectoryWithEpisodes(directoryID)
	if err != nil {
		return 0, err
	}

	groups := make(map[string][]model.LocalAnime)
	for _, anime := range animes {
		if !pathWithinRoot(scope, anime.Path) {
			continue
		}
		key := canonicalComparisonPath(anime.Path)
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], anime)
	}

	duplicateIDs := make([]uint, 0)
	for _, group := range groups {
		hasPopulatedRow := false
		for _, anime := range group {
			if len(anime.Episodes) > 0 {
				hasPopulatedRow = true
				break
			}
		}
		if !hasPopulatedRow {
			continue
		}
		for _, anime := range group {
			if len(anime.Episodes) == 0 {
				duplicateIDs = append(duplicateIDs, anime.ID)
			}
		}
	}
	return st.DeleteAnimesByIDs(duplicateIDs)
}

func inferTargetScanScopes(root string, targets []string) ([]string, error) {
	root = filepath.Clean(root)
	scopes := make([]string, 0, len(targets))
	for _, rawTarget := range targets {
		target := filepath.Clean(strings.TrimSpace(rawTarget))
		if strings.TrimSpace(rawTarget) == "" {
			continue
		}
		if !pathWithinRoot(root, target) {
			return nil, fmt.Errorf("download target %s is outside library root %s", target, root)
		}

		scope := target
		info, statErr := os.Stat(target)
		switch {
		case statErr == nil && info.IsDir():
			for !sameComparisonPath(scope, root) && isSeriesContainerDirectory(filepath.Base(scope)) {
				scope = filepath.Dir(scope)
			}
		case statErr == nil:
			seriesPath, loose := inferSeriesPath(root, target)
			if loose {
				scope = root
			} else {
				scope = seriesPath
			}
		case os.IsNotExist(statErr) && parser.IsVideoFile(target):
			seriesPath, loose := inferSeriesPath(root, target)
			if loose {
				scope = root
			} else {
				scope = seriesPath
			}
		case os.IsNotExist(statErr):
			for !sameComparisonPath(scope, root) && isSeriesContainerDirectory(filepath.Base(scope)) {
				scope = filepath.Dir(scope)
			}
		default:
			return nil, statErr
		}
		scope = filepath.Clean(scope)

		covered := false
		for i := 0; i < len(scopes); {
			switch {
			case pathWithinRoot(scopes[i], scope):
				covered = true
				i = len(scopes)
			case pathWithinRoot(scope, scopes[i]):
				scopes = append(scopes[:i], scopes[i+1:]...)
			default:
				i++
			}
		}
		if !covered {
			scopes = append(scopes, scope)
		}
	}
	sort.Strings(scopes)
	return scopes, nil
}

func animeHasEpisodeOutsideScope(anime *model.LocalAnime, scope string) bool {
	if anime == nil {
		return false
	}
	for _, episode := range anime.Episodes {
		if !pathWithinRoot(scope, episode.Path) {
			return true
		}
	}
	return false
}

func mergeScanResult(dst *ScanResult, src *ScanResult) {
	if dst == nil || src == nil {
		return
	}
	if dst.DirectoryID == 0 {
		dst.DirectoryID = src.DirectoryID
	}
	dst.DiscoveredFiles += src.DiscoveredFiles
	dst.CandidateSeries += src.CandidateSeries
	dst.WalkErrors += src.WalkErrors
	dst.Added += src.Added
	dst.Updated += src.Updated
	dst.Deleted += src.Deleted
	dst.ParseFailures += src.ParseFailures
	dst.ParseConflicts += src.ParseConflicts
}

func sortedUintKeys(values map[uint]struct{}) []uint {
	result := make([]uint, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (s *ScannerService) affectedAnimeIDsForTargets(directoryID uint, root string, targets, scopes []string) []uint {
	st := localAnimeStore()
	if st == nil {
		return nil
	}
	animes, err := st.ListAnimesByDirectoryWithEpisodes(directoryID)
	if err != nil {
		return nil
	}

	affected := make(map[uint]struct{})
	for i := range animes {
		anime := &animes[i]
		for _, episode := range anime.Episodes {
			for _, rawTarget := range targets {
				target := filepath.Clean(strings.TrimSpace(rawTarget))
				if strings.TrimSpace(rawTarget) == "" {
					continue
				}
				if sameComparisonPath(target, episode.Path) {
					affected[anime.ID] = struct{}{}
					break
				}
				if info, statErr := os.Stat(target); statErr == nil && info.IsDir() && pathWithinRoot(target, episode.Path) {
					affected[anime.ID] = struct{}{}
					break
				}
			}
			if _, ok := affected[anime.ID]; ok {
				break
			}
		}
		if _, ok := affected[anime.ID]; ok {
			continue
		}
		for _, scope := range scopes {
			if sameComparisonPath(scope, root) {
				continue
			}
			if sameComparisonPath(anime.Path, scope) || pathWithinRoot(scope, anime.Path) {
				affected[anime.ID] = struct{}{}
				break
			}
		}
	}
	return sortedUintKeys(affected)
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

func discoverMediaFiles(ctx context.Context, root string, claimed map[string]struct{}, onDirectory func(string)) ([]scannedMediaFile, []error, error) {
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
		if ctx.Err() != nil {
			return
		}
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
			if onDirectory != nil {
				onDirectory(path)
			}
			for _, entry := range entries {
				if ctx.Err() != nil {
					return
				}
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
	if err := ctx.Err(); err != nil {
		return nil, walkErrors, err
	}
	sort.Slice(media, func(i, j int) bool { return media[i].Path < media[j].Path })
	return media, walkErrors, nil
}

func emitScanProgress(report ScanProgressFunc, progress ScanProgress) {
	if report != nil {
		report(progress)
	}
}

// countScanDirectories mirrors the real walk without parsing media files. It
// deliberately returns an estimate: directories may change between the two
// passes, and the real scan adjusts its total upward if it discovers more.
func countScanDirectories(ctx context.Context, root string, onCount func(int64, string)) (int64, error) {
	root = filepath.Clean(root)
	rootInfo, err := os.Stat(root)
	if err != nil {
		return 0, err
	}
	if !rootInfo.IsDir() {
		return 0, nil
	}
	if _, err := os.ReadDir(root); err != nil {
		return 0, err
	}

	visited := make(map[string]struct{})
	var count int64
	var walk func(string)
	walk = func(path string) {
		if ctx.Err() != nil {
			return
		}
		info, statErr := os.Stat(path)
		if statErr != nil || !info.IsDir() {
			return
		}
		realDir := canonicalComparisonPath(path)
		if _, ok := visited[realDir]; ok {
			return
		}
		visited[realDir] = struct{}{}
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			return
		}
		count++
		if onCount != nil && (count == 1 || count%25 == 0) {
			onCount(count, path)
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return
			}
			isDirectory := entry.IsDir()
			if !isDirectory && entry.Type()&os.ModeSymlink != 0 {
				targetInfo, statErr := os.Stat(filepath.Join(path, entry.Name()))
				isDirectory = statErr == nil && targetInfo.IsDir()
			}
			if !isDirectory || shouldSkipScanEntry(entry.Name(), true) {
				continue
			}
			walk(filepath.Join(path, entry.Name()))
		}
	}
	walk(root)
	if err := ctx.Err(); err != nil {
		return count, err
	}
	if onCount != nil && count > 1 && count%25 != 0 {
		onCount(count, root)
	}
	return count, nil
}

func relativeScanPath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
		return filepath.Base(path)
	}
	return filepath.Join(filepath.Base(root), relative)
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
	fingerprint := fingerprintFile(path, size)
	seriesPath, loose := inferSeriesPath(root, path)
	season := parsed.Season
	if hint, ok := explicitSeasonFromAncestors(seriesPath, path); ok {
		season = hint
		// A file in a Specials/OVA directory is a special even when its
		// filename only contains a normal numeric marker (for example
		// "Show - 01.mkv"). Preserve that directory-level evidence so the
		// organizer keeps it under Specials instead of promoting it to S01.
		if hint == 0 && parsed.EpisodeType == scanEpisodeTypeEpisode {
			parsed.EpisodeType = scanSpecialDirectoryName
			parsed.ParseSource = "directory:special"
			if parsed.Confidence < 0.9 {
				parsed.Confidence = 0.9
			}
		}
	}
	episode := parsed.Episode
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	parseConflict := ""

	nfoPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo"
	if nfo, err := parser.ParseEpisodeNFO(nfoPath); err == nil {
		if nfo.Episode > 0 {
			// A clear filename marker is safer than stale or mismatched sidecar
			// numbering. NFO numbering remains authoritative when the filename
			// only yielded an ambiguous standalone-number fallback.
			if !parsed.HasExplicitEpisode() {
				season = nfo.Season
				episode = nfo.Episode
				parsed.ParseSource = "episode-nfo"
				parsed.Confidence = 0.94
			}
			if parsed.HasExplicitEpisode() && (parsed.Episode != nfo.Episode || (parsed.HasExplicitSeason() && parsed.Season != nfo.Season)) {
				parseConflict = fmt.Sprintf(
					"文件名编号 S%02dE%02d 与 NFO 编号 S%02dE%02d 不一致；当前按明确文件名编号处理",
					parsed.Season, parsed.Episode, nfo.Season, nfo.Episode,
				)
			}
			if nfo.EpisodeEnd > 0 {
				parsed.EpisodeEnd = nfo.EpisodeEnd
			}
		}
		if strings.TrimSpace(nfo.Type) != "" {
			parsed.EpisodeType = strings.TrimSpace(strings.ToLower(nfo.Type))
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
	if parsed.EpisodeEnd == 0 {
		parsed.EpisodeEnd = episode
	}
	return scannedMediaFile{
		Path: path, Size: size, Fingerprint: fingerprint, Parsed: parsed, Title: title, Season: season, Episode: episode,
		SeriesPath: seriesPath, SeriesTitle: seriesTitle, SeriesKey: canonicalSeriesKey(seriesTitle),
		Loose: loose, ParsedSeason: fmt.Sprintf("S%02d", season), ParseConflict: parseConflict,
	}
}

func (s *ScannerService) syncCandidateParseIssues(anime *model.LocalAnime, candidate *scanCandidate) {
	if anime == nil || candidate == nil {
		return
	}
	for _, media := range candidate.Files {
		sum := sha256.Sum256([]byte(canonicalComparisonPath(media.Path)))
		issueKey := "parse:" + hex.EncodeToString(sum[:])
		switch {
		case media.Episode <= 0:
			_ = ReportLibraryIssue(LibraryIssueInput{
				IssueKey: issueKey, IssueType: LibraryIssueTypeParse, Title: filepath.Base(media.Path),
				DirectoryPath: media.Path, LocalAnimeID: &anime.ID,
				Message: "无法从目录、NFO 或文件名中稳定识别剧集编号。",
				Hint:    "在整理预览中使用“AI 协助识别”，或补充与视频同名的 episode NFO；确认前不会移动文件。",
			})
		case media.ParseConflict != "":
			_ = ReportLibraryIssue(LibraryIssueInput{
				IssueKey: issueKey, IssueType: LibraryIssueTypeParse, Title: filepath.Base(media.Path),
				DirectoryPath: media.Path, LocalAnimeID: &anime.ID,
				Message: media.ParseConflict,
				Hint:    "请在整理预览中核对解析证据；修改文件名或 NFO 后重新扫描即可消除提示。",
			})
		default:
			_ = ResolveLibraryIssue(issueKey)
		}
	}
}

// fingerprintFile hashes the beginning and end of a file together with its
// size. This catches the common "same path, replaced release" case without
// reading large media files in full during every library scan.
func fingerprintFile(path string, size int64) string {
	file, err := os.Open(path) //nolint:gosec // path is discovered below a configured local library root.
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	_, _ = io.WriteString(hash, fmt.Sprintf("%d:", size))
	const sampleSize int64 = 64 * 1024
	head := make([]byte, sampleSize)
	if n, readErr := io.ReadFull(file, head); readErr == nil || readErr == io.ErrUnexpectedEOF {
		_, _ = hash.Write(head[:n])
	}
	if size > sampleSize {
		if _, seekErr := file.Seek(-sampleSize, io.SeekEnd); seekErr == nil {
			tail := make([]byte, sampleSize)
			if n, readErr := io.ReadFull(file, tail); readErr == nil || readErr == io.ErrUnexpectedEOF {
				_, _ = hash.Write(tail[:n])
			}
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
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
		if candidate.AllLoose {
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
		// Multiple loose series can legitimately share the same library root.
		// Reusing a row from that shared path alone can attach another show's
		// metadata after an interrupted or partial scan.
		if !candidate.AllLoose && sameComparisonPath(anime.Path, candidate.Path) {
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
	nextScanKey := localAnimeScanKey(candidate.Path, candidate.AllLoose)
	if !sameOptionalString(anime.ScanKey, nextScanKey) {
		anime.ScanKey = nextScanKey
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

func localAnimeScanKey(path string, loose bool) *string {
	if loose || strings.TrimSpace(path) == "" {
		return nil
	}
	pathKey := canonicalComparisonPath(path)
	if pathKey == "" {
		return nil
	}
	key := "folder:" + pathKey
	return &key
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
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
		EpisodeEndNum: media.Parsed.EpisodeEnd, EpisodeType: media.Parsed.EpisodeType,
		AbsoluteEpisodeNum: media.Parsed.AbsoluteEpisode, VersionTag: media.Parsed.Version,
		LanguageTag: media.Parsed.Language, ParseSource: media.Parsed.ParseSource,
		ParseConfidence: media.Parsed.Confidence, ScanFingerprint: media.Fingerprint,
		Resolution: media.Parsed.Resolution, SubGroup: media.Parsed.Group,
		VideoCodec: media.Parsed.VideoCodec, AudioCodec: media.Parsed.AudioCodec,
		BitDepth: media.Parsed.BitDepth, Source: media.Parsed.Source,
	}
}

func updateEpisodeFromMedia(episode *model.LocalEpisode, animeID uint, media scannedMediaFile) bool {
	changed := episode.DeletedAt.Valid || episode.LocalAnimeID != animeID || episode.Title != media.Title ||
		episode.EpisodeNum != media.Episode || episode.SeasonNum != media.Season || episode.FileSize != media.Size ||
		episode.EpisodeEndNum != media.Parsed.EpisodeEnd || episode.EpisodeType != media.Parsed.EpisodeType ||
		episode.AbsoluteEpisodeNum != media.Parsed.AbsoluteEpisode || episode.VersionTag != media.Parsed.Version ||
		episode.LanguageTag != media.Parsed.Language || episode.ParseSource != media.Parsed.ParseSource ||
		episode.ParseConfidence != media.Parsed.Confidence || episode.ScanFingerprint != media.Fingerprint ||
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
	episode.EpisodeEndNum = media.Parsed.EpisodeEnd
	episode.EpisodeType = media.Parsed.EpisodeType
	episode.AbsoluteEpisodeNum = media.Parsed.AbsoluteEpisode
	episode.VersionTag = media.Parsed.Version
	episode.LanguageTag = media.Parsed.Language
	episode.ParseSource = media.Parsed.ParseSource
	episode.ParseConfidence = media.Parsed.Confidence
	episode.ScanFingerprint = media.Fingerprint
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
