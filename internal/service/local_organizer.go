package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
	"github.com/pokerjest/animateAutoTool/internal/renamer"
	"gorm.io/gorm"
)

const LocalOrganizePlanLifetime = 15 * time.Minute

const (
	OrganizeSelectionIDs   = "ids"
	OrganizeSelectionQuery = "query"
	OrganizeStatusReady    = "ready"
	OrganizeStatusNoChange = "unchanged"
	OrganizeStatusConflict = "conflict"
	OrganizeStatusSkipped  = "skipped"
	organizeKindVideo      = "video"
)

var ErrOrganizePlanNotFound = errors.New("organize plan not found or expired")

// LocalOrganizeSelection supports explicit selection and server-side selection
// of all current search results without requiring every page in the browser.
type LocalOrganizeSelection struct {
	Mode       string `json:"mode"`
	AnimeIDs   []uint `json:"anime_ids,omitempty"`
	Query      string `json:"query,omitempty"`
	ExcludeIDs []uint `json:"exclude_ids,omitempty"`
}

type LocalOrganizePreviewRequest struct {
	Selection        LocalOrganizeSelection         `json:"selection"`
	SeriesTemplate   string                         `json:"series_template,omitempty"`
	EpisodeTemplate  string                         `json:"episode_template,omitempty"`
	EpisodeOverrides []LocalOrganizeEpisodeOverride `json:"episode_overrides,omitempty"`
}

type LocalOrganizeEpisodeOverride struct {
	Path            string `json:"path"`
	Season          int    `json:"season"`
	Episode         int    `json:"episode"`
	EpisodeEnd      int    `json:"episode_end,omitempty"`
	EpisodeType     string `json:"episode_type,omitempty"`
	AbsoluteEpisode int    `json:"absolute_episode,omitempty"`
}

type LocalOrganizeChange struct {
	Kind            string  `json:"kind"`
	Original        string  `json:"original"`
	Target          string  `json:"target"`
	Status          string  `json:"status"`
	Reason          string  `json:"reason,omitempty"`
	ManagedByQB     bool    `json:"managed_by_qb"`
	ParseSource     string  `json:"parse_source,omitempty"`
	ParseConfidence float64 `json:"parse_confidence,omitempty"`
	EpisodeType     string  `json:"episode_type,omitempty"`
	EpisodeEnd      int     `json:"episode_end,omitempty"`

	sourceSize    int64
	sourceModTime int64
	episodeID     uint
	groupKey      string
	qbHash        string
	qbOldRelative string
	qbNewRelative string
	qbOldDir      string
	qbTargetDir   string
	targetSeason  int
	targetEpisode int
	targetEnd     int
	targetType    string
	targetAbs     int
	targetVersion string
	targetLang    string
}

type LocalOrganizeAnimePreview struct {
	AnimeID         uint                  `json:"anime_id"`
	Title           string                `json:"title"`
	SourcePath      string                `json:"source_path"`
	TargetPath      string                `json:"target_path"`
	MetadataMatched bool                  `json:"metadata_matched"`
	Warnings        []string              `json:"warnings"`
	Changes         []LocalOrganizeChange `json:"changes"`
}

type LocalOrganizePreview struct {
	PlanID          string                      `json:"plan_id"`
	ExpiresAt       time.Time                   `json:"expires_at"`
	SeriesTemplate  string                      `json:"series_template"`
	EpisodeTemplate string                      `json:"episode_template"`
	SelectedCount   int                         `json:"selected_count"`
	ChangeCount     int                         `json:"change_count"`
	UnchangedCount  int                         `json:"unchanged_count"`
	ConflictCount   int                         `json:"conflict_count"`
	SkippedCount    int                         `json:"skipped_count"`
	Items           []LocalOrganizeAnimePreview `json:"items"`
	owner           string
}

type LocalOrganizeResult struct {
	AnimeCount int `json:"anime_count"`
	Moved      int `json:"moved"`
	Unchanged  int `json:"unchanged"`
	Skipped    int `json:"skipped"`
	Failed     int `json:"failed"`
}

type LocalOrganizeProgress func(message string, current, total int64)

type LocalOrganizerQB interface {
	ListTorrents() ([]downloader.TorrentInfo, error)
	RenameFile(hash, oldPath, newPath string) error
	SetLocation(hash, location string) error
}

type LocalOrganizer struct {
	db       *gorm.DB
	qb       LocalOrganizerQB
	now      func() time.Time
	torrents []downloader.TorrentInfo
}

func NewLocalOrganizer(database *gorm.DB, qb LocalOrganizerQB) *LocalOrganizer {
	return &LocalOrganizer{db: database, qb: qb, now: time.Now}
}

// NewConfiguredLocalOrganizer connects to qB before planning. When qB is
// configured but unavailable the preview fails rather than risking a direct
// move of files that may still be seeding.
func NewConfiguredLocalOrganizer() (*LocalOrganizer, error) {
	cfg := qbutil.LoadConfig()
	if strings.TrimSpace(cfg.URL) == "" {
		return NewLocalOrganizer(db.DB, nil), nil
	}
	client := downloader.NewQBittorrentClient(cfg.URL)
	if err := client.Login(cfg.Username, cfg.Password); err != nil {
		return nil, fmt.Errorf("连接 qBittorrent 失败，已停止整理以保护做种文件: %w", err)
	}
	return NewLocalOrganizer(db.DB, client), nil
}

func (o *LocalOrganizer) Preview(owner string, request LocalOrganizePreviewRequest) (*LocalOrganizePreview, error) {
	if o == nil || o.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	seriesTemplate := strings.TrimSpace(request.SeriesTemplate)
	if seriesTemplate == "" {
		seriesTemplate = strings.TrimSpace(configValue(model.ConfigKeyAutoRenameSeriesTemplate))
	}
	if seriesTemplate == "" {
		seriesTemplate = renamer.DefaultSeriesTemplate
	}
	episodeTemplate := strings.TrimSpace(request.EpisodeTemplate)
	if episodeTemplate == "" {
		episodeTemplate = strings.TrimSpace(configValue(model.ConfigKeyAutoRenameEpisodeTemplate))
	}
	if episodeTemplate == "" {
		episodeTemplate = renamer.DefaultEpisodeTemplate
	}
	if err := renamer.ValidateSeriesTemplate(seriesTemplate); err != nil {
		return nil, fmt.Errorf("系列文件夹模板无效: %w", err)
	}
	if err := renamer.ValidateEpisodeTemplate(episodeTemplate); err != nil {
		return nil, fmt.Errorf("剧集文件模板无效: %w", err)
	}

	items, err := o.selectAnime(request.Selection)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("没有选中可整理的本地番剧")
	}
	if o.qb != nil {
		o.torrents, err = o.qb.ListTorrents()
		if err != nil {
			return nil, fmt.Errorf("读取 qBittorrent 做种状态失败，已停止整理: %w", err)
		}
	}

	directories, err := o.directoryMap(items)
	if err != nil {
		return nil, err
	}
	preview := &LocalOrganizePreview{
		PlanID: randomPlanID(), ExpiresAt: o.now().Add(LocalOrganizePlanLifetime), owner: strings.TrimSpace(owner),
		SeriesTemplate: seriesTemplate, EpisodeTemplate: episodeTemplate, SelectedCount: len(items),
		Items: make([]LocalOrganizeAnimePreview, 0, len(items)),
	}
	overrides := make(map[string]LocalOrganizeEpisodeOverride, len(request.EpisodeOverrides))
	for _, override := range request.EpisodeOverrides {
		if strings.TrimSpace(override.Path) == "" || override.Episode <= 0 {
			continue
		}
		overrides[filepath.Clean(override.Path)] = override
	}
	for i := range items {
		directory, ok := directories[items[i].DirectoryID]
		if !ok {
			return nil, fmt.Errorf("番剧 %s 的扫描根目录不存在", items[i].Title)
		}
		item, itemErr := o.previewAnime(items[i], directory, seriesTemplate, episodeTemplate, overrides)
		if itemErr != nil {
			return nil, itemErr
		}
		preview.Items = append(preview.Items, item)
	}
	markDuplicateTargets(preview.Items)
	preview.recount()
	GlobalLocalOrganizePlans.Put(preview)
	return preview, nil
}

func (o *LocalOrganizer) selectAnime(selection LocalOrganizeSelection) ([]model.LocalAnime, error) {
	query := o.db.Model(&model.LocalAnime{}).Preload("Metadata").Order("local_animes.id ASC")
	switch strings.ToLower(strings.TrimSpace(selection.Mode)) {
	case OrganizeSelectionIDs, "":
		ids := uniqueUintIDs(selection.AnimeIDs)
		if len(ids) == 0 {
			return nil, errors.New("请选择至少一部番剧")
		}
		query = query.Where("local_animes.id IN ?", ids)
	case OrganizeSelectionQuery:
		if text := strings.TrimSpace(selection.Query); text != "" {
			like := "%" + escapeOrganizerLike(text) + "%"
			query = query.Joins("LEFT JOIN anime_metadata ON anime_metadata.id = local_animes.metadata_id AND anime_metadata.deleted_at IS NULL").
				Where(`local_animes.title LIKE ? ESCAPE '\' OR anime_metadata.title LIKE ? ESCAPE '\' OR anime_metadata.title_cn LIKE ? ESCAPE '\' OR anime_metadata.title_jp LIKE ? ESCAPE '\' OR anime_metadata.title_en LIKE ? ESCAPE '\'`, like, like, like, like, like)
		}
		if excluded := uniqueUintIDs(selection.ExcludeIDs); len(excluded) > 0 {
			query = query.Where("local_animes.id NOT IN ?", excluded)
		}
	default:
		return nil, errors.New("不支持的整理选择模式")
	}
	var items []model.LocalAnime
	if err := query.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (o *LocalOrganizer) directoryMap(items []model.LocalAnime) (map[uint]model.LocalAnimeDirectory, error) {
	ids := make([]uint, 0, len(items))
	seen := map[uint]struct{}{}
	for _, item := range items {
		if _, ok := seen[item.DirectoryID]; !ok {
			seen[item.DirectoryID] = struct{}{}
			ids = append(ids, item.DirectoryID)
		}
	}
	var directories []model.LocalAnimeDirectory
	if err := o.db.Where("id IN ?", ids).Find(&directories).Error; err != nil {
		return nil, err
	}
	result := make(map[uint]model.LocalAnimeDirectory, len(directories))
	for _, directory := range directories {
		result[directory.ID] = directory
	}
	return result, nil
}

//nolint:gocyclo // Preview generation evaluates parsing, overrides, conflicts, and seed protection together.
func (o *LocalOrganizer) previewAnime(anime model.LocalAnime, directory model.LocalAnimeDirectory, seriesTemplate, episodeTemplate string, overrides map[string]LocalOrganizeEpisodeOverride) (LocalOrganizeAnimePreview, error) {
	title, year, matched := organizerAnimeIdentity(anime)
	seriesName, err := renamer.FormatTemplate(seriesTemplate, renamer.TemplateData{Title: title, Year: year})
	if err != nil {
		return LocalOrganizeAnimePreview{}, err
	}
	targetSeries, err := organizerSafeJoin(directory.Path, seriesName)
	if err != nil {
		return LocalOrganizeAnimePreview{}, err
	}
	result := LocalOrganizeAnimePreview{
		AnimeID: anime.ID, Title: title, SourcePath: filepath.Clean(anime.Path), TargetPath: targetSeries,
		MetadataMatched: matched, Changes: []LocalOrganizeChange{}, Warnings: []string{},
	}
	if !matched {
		result.Warnings = append(result.Warnings, "未匹配规范元数据，将使用清理后的本地标题")
	}

	var episodes []model.LocalEpisode
	if err := o.db.Where("local_anime_id = ?", anime.ID).Find(&episodes).Error; err != nil {
		return result, err
	}
	episodesByPath := make(map[string]model.LocalEpisode, len(episodes))
	for _, episode := range episodes {
		episodesByPath[filepath.Clean(episode.Path)] = episode
	}
	videoPaths, walkErr := organizerVideoPaths(anime.Path)
	if walkErr != nil {
		return result, walkErr
	}
	seenSidecars := map[string]struct{}{}
	for _, source := range videoPaths {
		episode, found := episodesByPath[filepath.Clean(source)]
		parsed := parser.ParseFilename(source)
		season := parsed.Season
		episodeNumber := parsed.Episode
		if found {
			season = episode.SeasonNum
			episodeNumber = episode.EpisodeNum
			filenameSeason := season
			if parsed.HasExplicitSeason() && parsed.Season > 0 {
				filenameSeason = parsed.Season
			}
			if parsed.HasExplicitEpisode() && parsed.Episode > 0 &&
				(parsed.Episode != episodeNumber || filenameSeason != season) {
				result.Warnings = append(result.Warnings, fmt.Sprintf(
					"%s 的文件名编号 S%02dE%02d 与数据库 S%02dE%02d 不一致，本次按文件名整理",
					filepath.Base(source), filenameSeason, parsed.Episode, season, episodeNumber,
				))
				season = filenameSeason
				episodeNumber = parsed.Episode
			}
		}
		episodeEnd := parsed.EpisodeEnd
		episodeType := parsed.EpisodeType
		absoluteEpisode := parsed.AbsoluteEpisode
		versionTag := parsed.Version
		languageTag := parsed.Language
		if found {
			if episode.EpisodeEndNum > 0 {
				episodeEnd = episode.EpisodeEndNum
			}
			// Historical LocalEpisode rows predate the richer parser fields and
			// therefore contain empty/zero values. Only let persisted values
			// override a fresh filename parse when they are actually present;
			// otherwise old records would erase special types, versions,
			// languages, or absolute episode evidence discovered during preview.
			if strings.TrimSpace(episode.EpisodeType) != "" {
				episodeType = episode.EpisodeType
			}
			if episode.AbsoluteEpisodeNum > 0 {
				absoluteEpisode = episode.AbsoluteEpisodeNum
			}
			if strings.TrimSpace(episode.VersionTag) != "" {
				versionTag = episode.VersionTag
			}
			if strings.TrimSpace(episode.LanguageTag) != "" {
				languageTag = episode.LanguageTag
			}
		}
		if override, ok := overrides[filepath.Clean(source)]; ok {
			season = override.Season
			episodeNumber = override.Episode
			if override.EpisodeEnd > 0 {
				episodeEnd = override.EpisodeEnd
			}
			if strings.TrimSpace(override.EpisodeType) != "" {
				episodeType = strings.TrimSpace(strings.ToLower(override.EpisodeType))
			}
			if override.AbsoluteEpisode > 0 {
				absoluteEpisode = override.AbsoluteEpisode
			}
		}
		isSpecial := episodeType != "" && episodeType != "episode"
		if season < 0 || (season == 0 && !isSpecial) {
			season = max(1, anime.Season)
		}
		if episodeNumber <= 0 {
			change := o.newChange(organizeKindVideo, source, source, 0, source, OrganizeStatusSkipped, "无法识别剧集编号")
			change.ParseSource = parsed.ParseSource
			change.ParseConfidence = parsed.Confidence
			change.EpisodeType = episodeType
			change.EpisodeEnd = episodeEnd
			result.Changes = append(result.Changes, change)
			continue
		}
		ext := strings.ToLower(filepath.Ext(source))
		filename, formatErr := renamer.FormatTemplate(episodeTemplate, renamer.TemplateData{
			Title: title, Year: year, Season: strconv.Itoa(season), Episode: strconv.Itoa(episodeNumber),
			EpisodeEnd: strconv.Itoa(episodeEnd), EpisodeType: episodeType,
			AbsoluteEpisode: strconv.Itoa(absoluteEpisode), Group: parsed.Group,
			Resolution: parsed.Resolution, Version: versionTag, Language: languageTag,
			Ext: ext, Original: strings.TrimSuffix(filepath.Base(source), filepath.Ext(source)),
		})
		if formatErr != nil {
			return result, formatErr
		}
		if episodeEnd > episodeNumber && !strings.Contains(episodeTemplate, "{episode_end}") {
			filename = strings.TrimSuffix(filename, ext) + fmt.Sprintf("-E%02d", episodeEnd) + ext
		}
		seasonDirectory := fmt.Sprintf("Season %02d", season)
		if isSpecial {
			seasonDirectory = "Specials"
		}
		target, joinErr := organizerSafeJoin(directory.Path, filepath.Join(seriesName, seasonDirectory, filename))
		if joinErr != nil {
			return result, joinErr
		}
		groupKey := filepath.Clean(source)
		change := o.newChange(organizeKindVideo, source, target, episode.ID, groupKey, "", "")
		change.ParseSource = parsed.ParseSource
		change.ParseConfidence = parsed.Confidence
		change.EpisodeType = episodeType
		change.EpisodeEnd = episodeEnd
		change.targetSeason = season
		change.targetEpisode = episodeNumber
		change.targetEnd = episodeEnd
		change.targetType = episodeType
		change.targetAbs = absoluteEpisode
		change.targetVersion = versionTag
		change.targetLang = languageTag
		o.classifyQB(&change)
		result.Changes = append(result.Changes, change)
		for _, sidecar := range organizerSidecars(source) {
			if _, exists := seenSidecars[sidecar]; exists {
				continue
			}
			seenSidecars[sidecar] = struct{}{}
			sidecarName := filepath.Base(sidecar)
			videoStem := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
			suffix := sidecarName[len(videoStem):]
			targetSidecar := filepath.Join(filepath.Dir(target), strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))+suffix)
			result.Changes = append(result.Changes, o.newChange(sidecarKind(sidecar), sidecar, targetSidecar, 0, groupKey, "", ""))
		}
	}
	for _, asset := range organizerSeriesAssets(anime.Path) {
		if _, exists := seenSidecars[asset]; exists {
			continue
		}
		target := filepath.Join(targetSeries, filepath.Base(asset))
		result.Changes = append(result.Changes, o.newChange("series_asset", asset, target, 0, "", "", ""))
	}
	if len(result.Changes) == 0 {
		result.Warnings = append(result.Warnings, "没有找到可整理的视频或附属文件")
	}
	return result, nil
}

func (o *LocalOrganizer) newChange(kind, source, target string, episodeID uint, groupKey, status, reason string) LocalOrganizeChange {
	change := LocalOrganizeChange{Kind: kind, Original: filepath.Clean(source), Target: filepath.Clean(target), episodeID: episodeID, groupKey: groupKey, Status: status, Reason: reason}
	info, err := os.Lstat(change.Original)
	if err != nil {
		change.Status = OrganizeStatusSkipped
		change.Reason = "源文件不存在或不可访问"
		return change
	}
	change.sourceSize = info.Size()
	change.sourceModTime = info.ModTime().UnixNano()
	if info.Mode()&os.ModeSymlink != 0 {
		change.Status = OrganizeStatusSkipped
		change.Reason = "符号链接不会被整理"
		return change
	}
	if sameOrganizerPath(change.Original, change.Target) {
		change.Status = OrganizeStatusNoChange
		return change
	}
	if targetInfo, targetErr := os.Lstat(change.Target); targetErr == nil {
		if os.SameFile(info, targetInfo) {
			change.Status = OrganizeStatusReady // Case-only rename on a case-insensitive filesystem.
		} else {
			change.Status = OrganizeStatusConflict
			change.Reason = "目标文件已存在"
		}
		return change
	} else if !os.IsNotExist(targetErr) {
		change.Status = OrganizeStatusSkipped
		change.Reason = "无法检查目标路径"
		return change
	}
	if change.Status == "" {
		change.Status = OrganizeStatusReady
	}
	return change
}

func (o *LocalOrganizer) classifyQB(change *LocalOrganizeChange) {
	if change == nil || change.Kind != organizeKindVideo || change.Status != OrganizeStatusReady {
		return
	}
	for _, torrent := range o.torrents {
		contentPath := filepath.Clean(strings.TrimSpace(torrent.ContentPath))
		if contentPath == "." || contentPath == "" {
			continue
		}
		if sameOrganizerPath(contentPath, change.Original) {
			oldRelative := filepath.Base(change.Original)
			if rel := organizerRelativeTorrentPath(torrent.SavePath, change.Original); rel != "" {
				oldRelative = rel
			}
			change.ManagedByQB = true
			change.qbHash = torrent.Hash
			change.qbOldRelative = filepath.ToSlash(oldRelative)
			change.qbNewRelative = filepath.Base(change.Target)
			change.qbOldDir = filepath.Dir(change.Original)
			change.qbTargetDir = filepath.Dir(change.Target)
			return
		}
		if organizerPathWithin(contentPath, change.Original) {
			change.ManagedByQB = true
			change.Status = OrganizeStatusSkipped
			change.Reason = "属于多文件种子，无法可靠映射，已保护性跳过"
			return
		}
	}
}

func (o *LocalOrganizer) Execute(ctx context.Context, plan *LocalOrganizePreview, includedIDs []uint, progress LocalOrganizeProgress) (LocalOrganizeResult, error) {
	if o == nil || o.db == nil || plan == nil {
		return LocalOrganizeResult{}, errors.New("整理计划无效")
	}
	included := make(map[uint]struct{})
	for _, id := range uniqueUintIDs(includedIDs) {
		included[id] = struct{}{}
	}
	if len(included) == 0 {
		for _, item := range plan.Items {
			included[item.AnimeID] = struct{}{}
		}
	}
	total := int64(0)
	for _, item := range plan.Items {
		if _, ok := included[item.AnimeID]; !ok {
			continue
		}
		for _, change := range item.Changes {
			if change.Status == OrganizeStatusReady {
				total++
			}
		}
	}
	result := LocalOrganizeResult{}
	current := int64(0)
	for _, item := range plan.Items {
		if _, ok := included[item.AnimeID]; !ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.AnimeCount++
		failedGroups := map[string]struct{}{}
		moved := make([]LocalOrganizeChange, 0, len(item.Changes))
		for _, change := range item.Changes {
			switch change.Status {
			case OrganizeStatusNoChange:
				result.Unchanged++
				continue
			case OrganizeStatusConflict, OrganizeStatusSkipped:
				result.Skipped++
				if change.groupKey != "" && change.Kind == organizeKindVideo {
					failedGroups[change.groupKey] = struct{}{}
				}
				continue
			}
			if _, failed := failedGroups[change.groupKey]; failed && change.Kind != organizeKindVideo {
				result.Skipped++
				continue
			}
			current++
			if progress != nil {
				progress(fmt.Sprintf("正在整理 %s：%s", item.Title, filepath.Base(change.Original)), current, total)
			}
			if err := revalidateOrganizeChange(change); err != nil {
				result.Skipped++
				if change.groupKey != "" {
					failedGroups[change.groupKey] = struct{}{}
				}
				continue
			}
			if err := o.moveChange(change); err != nil {
				result.Failed++
				if change.groupKey != "" {
					failedGroups[change.groupKey] = struct{}{}
				}
				continue
			}
			moved = append(moved, change)
			result.Moved++
		}
		if len(moved) == 0 {
			continue
		}
		if err := o.persistMovedChanges(moved); err != nil {
			for index := len(moved) - 1; index >= 0; index-- {
				_ = o.rollbackChange(moved[index])
			}
			result.Moved -= len(moved)
			result.Failed += len(moved)
			continue
		}
		removeEmptyOrganizerDirectories(item.SourcePath)
	}
	return result, nil
}

func (o *LocalOrganizer) moveChange(change LocalOrganizeChange) error {
	if err := os.MkdirAll(filepath.Dir(change.Target), 0o755); err != nil {
		return err
	}
	if change.ManagedByQB && change.Kind == organizeKindVideo {
		if o.qb == nil {
			return errors.New("qBittorrent 不可用")
		}
		if change.qbOldRelative != change.qbNewRelative {
			if err := o.qb.RenameFile(change.qbHash, change.qbOldRelative, change.qbNewRelative); err != nil {
				return err
			}
		}
		if !sameOrganizerPath(change.qbOldDir, change.qbTargetDir) {
			if err := o.qb.SetLocation(change.qbHash, change.qbTargetDir); err != nil {
				if change.qbOldRelative != change.qbNewRelative {
					_ = o.qb.RenameFile(change.qbHash, change.qbNewRelative, change.qbOldRelative)
				}
				return err
			}
		}
		return nil
	}
	return renameOrganizerFile(change.Original, change.Target)
}

func (o *LocalOrganizer) rollbackChange(change LocalOrganizeChange) error {
	if change.ManagedByQB && change.Kind == organizeKindVideo {
		if o.qb == nil {
			return errors.New("qBittorrent 不可用")
		}
		if !sameOrganizerPath(change.qbOldDir, change.qbTargetDir) {
			if err := o.qb.SetLocation(change.qbHash, change.qbOldDir); err != nil {
				return err
			}
		}
		if change.qbOldRelative != change.qbNewRelative {
			return o.qb.RenameFile(change.qbHash, change.qbNewRelative, change.qbOldRelative)
		}
		return nil
	}
	if _, err := os.Lstat(change.Target); err != nil {
		return err
	}
	return renameOrganizerFile(change.Target, change.Original)
}

func (o *LocalOrganizer) persistMovedChanges(changes []LocalOrganizeChange) error {
	return o.db.Transaction(func(tx *gorm.DB) error {
		for _, change := range changes {
			if change.Kind == organizeKindVideo {
				updates := map[string]any{
					"path":                 change.Target,
					"season_num":           change.targetSeason,
					"episode_end_num":      change.targetEnd,
					"episode_type":         change.targetType,
					"absolute_episode_num": change.targetAbs,
					"version_tag":          change.targetVersion,
					"language_tag":         change.targetLang,
					"parse_source":         change.ParseSource,
					"parse_confidence":     change.ParseConfidence,
				}
				if change.targetEpisode > 0 {
					updates["episode_num"] = change.targetEpisode
				}
				if change.episodeID > 0 {
					if err := tx.Model(&model.LocalEpisode{}).Where("id = ?", change.episodeID).Updates(updates).Error; err != nil {
						return err
					}
				} else if err := tx.Model(&model.LocalEpisode{}).Where("path = ?", change.Original).Updates(updates).Error; err != nil {
					return err
				}
			}
			if err := tx.Model(&model.DownloadLog{}).Where("target_file = ?", change.Original).Updates(map[string]any{"target_file": change.Target, "status": "renamed"}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func revalidateOrganizeChange(change LocalOrganizeChange) error {
	info, err := os.Lstat(change.Original)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || info.Size() != change.sourceSize || info.ModTime().UnixNano() != change.sourceModTime {
		return errors.New("源文件在预览后发生变化")
	}
	if target, targetErr := os.Lstat(change.Target); targetErr == nil {
		if !os.SameFile(info, target) {
			return errors.New("目标文件已存在")
		}
	} else if !os.IsNotExist(targetErr) {
		return targetErr
	}
	return nil
}

func renameOrganizerFile(source, target string) error {
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if targetInfo, targetErr := os.Lstat(target); targetErr == nil {
		if !os.SameFile(sourceInfo, targetInfo) {
			return errors.New("目标文件已存在")
		}
		if source == target {
			return nil
		}
		temp, tempErr := os.CreateTemp(filepath.Dir(source), ".animate-organize-*")
		if tempErr != nil {
			return tempErr
		}
		tempPath := temp.Name()
		_ = temp.Close()
		_ = os.Remove(tempPath)
		if err := os.Rename(source, tempPath); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			_ = os.Rename(tempPath, source)
			return err
		}
		if err := os.Rename(tempPath, target); err != nil {
			_ = os.Rename(tempPath, source)
			return err
		}
		return nil
	} else if !os.IsNotExist(targetErr) {
		return targetErr
	}
	return os.Rename(source, target)
}

func organizerAnimeIdentity(anime model.LocalAnime) (title, year string, matched bool) {
	if anime.Metadata != nil {
		for _, value := range []string{anime.Metadata.TitleCN, anime.Metadata.Title, anime.Metadata.TitleJP, anime.Metadata.TitleEN} {
			if clean := parser.CleanTitle(strings.TrimSpace(value)); clean != "" {
				title = clean
				matched = true
				break
			}
		}
		if len(strings.TrimSpace(anime.Metadata.AirDate)) >= 4 {
			year = strings.TrimSpace(anime.Metadata.AirDate)[:4]
		}
	}
	if title == "" {
		title = parser.CleanTitle(strings.TrimSpace(anime.Title))
	}
	if title == "" {
		title = "未命名番剧"
	}
	return title, year, matched
}

func organizerVideoPaths(root string) ([]string, error) {
	result := []string{}
	err := filepath.WalkDir(filepath.Clean(root), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() && parser.IsVideoFile(path) {
			result = append(result, filepath.Clean(path))
		}
		return nil
	})
	sort.Strings(result)
	return result, err
}

var organizerSidecarExt = map[string]struct{}{
	".ass": {}, ".ssa": {}, ".srt": {}, ".vtt": {}, ".sub": {}, ".idx": {}, ".sup": {},
	".nfo": {}, ".jpg": {}, ".jpeg": {}, ".png": {}, ".webp": {},
}

func organizerSidecars(video string) []string {
	directory := filepath.Dir(video)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	stem := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	lowerStem := strings.ToLower(stem)
	result := []string{}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := entry.Name()
		lowerName := strings.ToLower(name)
		if !strings.HasPrefix(lowerName, lowerStem+".") {
			continue
		}
		if _, ok := organizerSidecarExt[strings.ToLower(filepath.Ext(name))]; ok {
			result = append(result, filepath.Join(directory, name))
		}
	}
	sort.Strings(result)
	return result
}

var organizerSeriesAssetNames = map[string]struct{}{
	"poster.jpg": {}, "poster.png": {}, "folder.jpg": {}, "fanart.jpg": {}, "fanart.png": {},
	"banner.jpg": {}, "banner.png": {}, "logo.png": {}, "clearlogo.png": {}, "tvshow.nfo": {},
}

func organizerSeriesAssets(root string) []string {
	entries, err := os.ReadDir(filepath.Clean(root))
	if err != nil {
		return nil
	}
	result := []string{}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if _, ok := organizerSeriesAssetNames[strings.ToLower(entry.Name())]; ok {
			result = append(result, filepath.Join(root, entry.Name()))
		}
	}
	sort.Strings(result)
	return result
}

func sidecarKind(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ass", ".ssa", ".srt", ".vtt", ".sub", ".idx", ".sup":
		return "subtitle"
	case ".nfo":
		return "nfo"
	default:
		return "image"
	}
}

func organizerSafeJoin(root, relative string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || root == "" || filepath.IsAbs(relative) {
		return "", errors.New("整理目标路径无效")
	}
	target := filepath.Clean(filepath.Join(root, relative))
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("整理目标超出扫描根目录")
	}
	return target, nil
}

func organizerRelativeTorrentPath(savePath, source string) string {
	if rel, err := filepath.Rel(filepath.Clean(savePath), filepath.Clean(source)); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return ""
}

func organizerPathWithin(root, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sameOrganizerPath(a, b string) bool {
	return filepath.Clean(strings.TrimSpace(a)) == filepath.Clean(strings.TrimSpace(b))
}

func markDuplicateTargets(items []LocalOrganizeAnimePreview) {
	targets := map[string][]*LocalOrganizeChange{}
	for itemIndex := range items {
		for changeIndex := range items[itemIndex].Changes {
			change := &items[itemIndex].Changes[changeIndex]
			if change.Status == OrganizeStatusReady {
				key := strings.ToLower(filepath.Clean(change.Target))
				targets[key] = append(targets[key], change)
			}
		}
	}
	for _, changes := range targets {
		if len(changes) < 2 {
			continue
		}
		for _, change := range changes {
			change.Status = OrganizeStatusConflict
			change.Reason = "多个源文件会写入同一目标"
		}
	}
}

func (p *LocalOrganizePreview) recount() {
	p.ChangeCount, p.UnchangedCount, p.ConflictCount, p.SkippedCount = 0, 0, 0, 0
	for _, item := range p.Items {
		for _, change := range item.Changes {
			switch change.Status {
			case OrganizeStatusReady:
				p.ChangeCount++
			case OrganizeStatusNoChange:
				p.UnchangedCount++
			case OrganizeStatusConflict:
				p.ConflictCount++
			default:
				p.SkippedCount++
			}
		}
	}
}

func removeEmptyOrganizerDirectories(root string) {
	root = filepath.Clean(root)
	directories := []string{}
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.IsDir() && path != root {
			directories = append(directories, path)
		}
		return nil
	})
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	for _, directory := range directories {
		_ = os.Remove(directory) // os.Remove only succeeds for empty directories.
	}
	_ = os.Remove(root)
}

func uniqueUintIDs(values []uint) []uint {
	seen := map[uint]struct{}{}
	result := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func escapeOrganizerLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func randomPlanID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err == nil {
		return hex.EncodeToString(buffer)
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

type LocalOrganizePlanStore struct {
	mu    sync.Mutex
	now   func() time.Time
	plans map[string]*LocalOrganizePreview
}

func NewLocalOrganizePlanStore() *LocalOrganizePlanStore {
	return &LocalOrganizePlanStore{now: time.Now, plans: map[string]*LocalOrganizePreview{}}
}

var GlobalLocalOrganizePlans = NewLocalOrganizePlanStore()

func (s *LocalOrganizePlanStore) Put(plan *LocalOrganizePreview) {
	if s == nil || plan == nil || strings.TrimSpace(plan.PlanID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	s.plans[plan.PlanID] = plan
}

func (s *LocalOrganizePlanStore) Take(planID, owner string) (*LocalOrganizePreview, error) {
	if s == nil {
		return nil, ErrOrganizePlanNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	plan, ok := s.plans[strings.TrimSpace(planID)]
	if !ok || plan.owner != strings.TrimSpace(owner) {
		return nil, ErrOrganizePlanNotFound
	}
	delete(s.plans, plan.PlanID)
	return plan, nil
}

func (s *LocalOrganizePlanStore) Reset() {
	s.mu.Lock()
	s.plans = map[string]*LocalOrganizePreview{}
	s.mu.Unlock()
}

func (s *LocalOrganizePlanStore) pruneLocked() {
	now := s.now()
	for id, plan := range s.plans {
		if plan == nil || !plan.ExpiresAt.After(now) {
			delete(s.plans, id)
		}
	}
}
