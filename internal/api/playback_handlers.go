package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"gorm.io/gorm"
)

const (
	playbackTicksPerSecond int64 = 10_000_000
	playbackEventEnded           = "ended"
	playbackEventRestart         = "restart"
)

var playbackResumeImportAttempts sync.Map

type PlaybackProgressInput struct {
	EpisodeID     uint   `json:"episode_id" binding:"required"`
	Event         string `json:"event" binding:"required"`
	Ticks         int64  `json:"ticks"`
	DurationTicks int64  `json:"duration_ticks"`
}

type ContinueWatchingItem struct {
	AnimeID          uint      `json:"anime_id"`
	EpisodeID        uint      `json:"episode_id"`
	Title            string    `json:"title"`
	EpisodeTitle     string    `json:"episode_title"`
	Image            string    `json:"image"`
	Season           int       `json:"season"`
	Episode          int       `json:"episode"`
	PositionTicks    int64     `json:"position_ticks"`
	DurationTicks    int64     `json:"duration_ticks"`
	ProgressPercent  float64   `json:"progress_percent"`
	RemainingSeconds int64     `json:"remaining_seconds"`
	UpdatedAt        time.Time `json:"updated_at"`
	StreamURL        string    `json:"stream_url"`
}

func validPlaybackEvent(event string) bool {
	switch event {
	case "playing", "timeupdate", "seeked", "pause", playbackEventEnded, "stop", "destroy", playbackEventRestart:
		return true
	default:
		return false
	}
}

func persistPlaybackProgress(userID uint, input PlaybackProgressInput) (*model.PlaybackHistory, *model.LocalEpisode, *model.LocalAnime, error) {
	if db.DB == nil {
		return nil, nil, nil, gorm.ErrInvalidDB
	}
	var episode model.LocalEpisode
	if err := db.DB.First(&episode, input.EpisodeID).Error; err != nil {
		return nil, nil, nil, err
	}
	var anime model.LocalAnime
	if err := db.DB.Preload("Metadata").First(&anime, episode.LocalAnimeID).Error; err != nil {
		return nil, nil, nil, err
	}

	historyStore := store.NewPlaybackHistoryStore(db.DB)
	previous, previousErr := historyStore.Find(userID, input.EpisodeID)
	duration := max(input.DurationTicks, int64(0))
	if duration == 0 && previousErr == nil {
		duration = previous.DurationTicks
	}
	position := max(input.Ticks, int64(0))
	if duration > 0 && position > duration {
		position = duration
	}
	completed := input.Event == playbackEventEnded
	if completed && duration > 0 {
		position = duration
	}
	if input.Event == playbackEventRestart {
		position = 0
		completed = false
	}
	history := &model.PlaybackHistory{
		UserID: userID, LocalAnimeID: episode.LocalAnimeID, LocalEpisodeID: episode.ID,
		PositionTicks: position, DurationTicks: duration, Completed: completed,
		LastEvent: input.Event, LastPlayedAt: time.Now().UTC(),
	}
	if err := historyStore.Upsert(history); err != nil {
		return nil, nil, nil, err
	}
	return history, &episode, &anime, nil
}

func syncPlaybackProgressToJellyfin(input PlaybackProgressInput, episode *model.LocalEpisode, anime *model.LocalAnime) error {
	client, err := resolveJellyfinPlaybackClient()
	if err != nil {
		return err
	}
	itemID, err := ensureJellyfinEpisodeID(client, anime, episode)
	if err != nil {
		return err
	}

	switch input.Event {
	case playbackEventEnded:
		if err := client.MarkPlayed(itemID); err != nil {
			return err
		}
		if anime.Metadata != nil && anime.Metadata.BangumiID != 0 {
			bangumiID := anime.Metadata.BangumiID
			episodeNumber := episode.EpisodeNum
			GoBackground(func(context.Context) {
				syncEndedEpisodeToBangumi(bangumiID, episodeNumber)
			})
		}
	case playbackEventRestart:
		if err := client.UnmarkPlayed(itemID); err != nil {
			return err
		}
		return client.UpdateProgress(itemID, 0)
	default:
		return client.UpdateProgress(itemID, max(input.Ticks, int64(0)))
	}
	return nil
}

func syncEndedEpisodeToBangumi(bangumiID, episodeNumber int) {
	token := configValue(model.ConfigKeyBangumiAccessToken)
	if token == "" {
		return
	}
	client := bangumi.NewClient("", "", "")
	applyProxyToBangumiClient(client)
	if err := client.UpdateWatchedEpisodes(token, bangumiID, episodeNumber); err != nil {
		log.Printf("Failed to sync progress to Bangumi: %v", err)
	}
}

// ReportProgressHandler stores progress locally before best-effort Jellyfin
// synchronization. It serves both the new playback endpoint and the legacy
// Jellyfin progress endpoint.
func ReportProgressHandler(c *gin.Context) {
	var input PlaybackProgressInput
	if err := c.ShouldBindJSON(&input); err != nil || !validPlaybackEvent(input.Event) || input.Ticks < 0 || input.DurationTicks < 0 {
		v1Error(c, http.StatusBadRequest, "invalid_playback_progress", "播放进度请求格式不正确")
		return
	}
	userID, err := currentSessionUserID(c)
	if err != nil {
		v1Error(c, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	history, episode, anime, err := persistPlaybackProgress(userID, input)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			v1Error(c, http.StatusNotFound, "episode_not_found", "未找到对应的剧集")
			return
		}
		v1Error(c, http.StatusInternalServerError, "playback_progress_failed", "保存播放进度失败")
		return
	}
	jellyfinSynced := true
	if err := syncPlaybackProgressToJellyfin(input, episode, anime); err != nil {
		jellyfinSynced = false
		log.Printf("playback progress saved locally but Jellyfin sync failed: %v", err)
	}
	v1Data(c, http.StatusOK, gin.H{
		"position_ticks": history.PositionTicks, "duration_ticks": history.DurationTicks,
		"completed": history.Completed, "updated_at": history.LastPlayedAt, "jellyfin_synced": jellyfinSynced,
	})
}

func importJellyfinResumeItems(userID uint) {
	if _, loaded := playbackResumeImportAttempts.LoadOrStore(userID, struct{}{}); loaded {
		return
	}
	historyStore := store.NewPlaybackHistoryStore(db.DB)
	count, err := historyStore.Count(userID)
	if err != nil || count > 0 {
		return
	}
	client, err := resolveJellyfinPlaybackClient()
	if err != nil {
		playbackResumeImportAttempts.Delete(userID)
		return
	}
	items, err := client.GetResumeItems(50)
	if err != nil {
		playbackResumeImportAttempts.Delete(userID)
		log.Printf("import Jellyfin resume items failed: %v", err)
		return
	}
	now := time.Now().UTC()
	for index, item := range items {
		if item.ID == "" || item.UserData.PlaybackPositionTicks <= 0 || item.UserData.Played {
			continue
		}
		var episode model.LocalEpisode
		if err := db.DB.Where("jellyfin_item_id = ?", item.ID).First(&episode).Error; err != nil {
			continue
		}
		lastPlayedAt := now.Add(-time.Duration(index) * time.Second)
		if item.UserData.LastPlayedDate != "" {
			if parsed, parseErr := time.Parse(time.RFC3339, item.UserData.LastPlayedDate); parseErr == nil {
				lastPlayedAt = parsed.UTC()
			}
		}
		_ = historyStore.Upsert(&model.PlaybackHistory{
			UserID: userID, LocalAnimeID: episode.LocalAnimeID, LocalEpisodeID: episode.ID,
			PositionTicks: item.UserData.PlaybackPositionTicks, DurationTicks: item.RunTimeTicks,
			Completed: false, LastEvent: "import", LastPlayedAt: lastPlayedAt,
		})
	}
}

func nextLocalEpisode(current model.LocalEpisode) (*model.LocalEpisode, error) {
	var next model.LocalEpisode
	err := db.DB.Where(
		"local_anime_id = ? AND (season_num > ? OR (season_num = ? AND episode_num > ?))",
		current.LocalAnimeID, current.SeasonNum, current.SeasonNum, current.EpisodeNum,
	).Order("season_num ASC, episode_num ASC").First(&next).Error
	if err != nil {
		return nil, err
	}
	return &next, nil
}

func continueWatchingItem(userID uint, history model.PlaybackHistory) (*ContinueWatchingItem, error) {
	var episode model.LocalEpisode
	if err := db.DB.First(&episode, history.LocalEpisodeID).Error; err != nil {
		return nil, err
	}
	if history.Completed {
		next, err := nextLocalEpisode(episode)
		if err != nil {
			return nil, err
		}
		episode = *next
		if nextHistory, findErr := store.NewPlaybackHistoryStore(db.DB).Find(userID, episode.ID); findErr == nil && !nextHistory.Completed {
			history = *nextHistory
		} else {
			history.PositionTicks = 0
			history.DurationTicks = 0
			history.Completed = false
		}
	}
	var anime model.LocalAnime
	if err := db.DB.Preload("Metadata").First(&anime, episode.LocalAnimeID).Error; err != nil {
		return nil, err
	}
	title := anime.Title
	image := anime.Image
	if anime.Metadata != nil {
		if anime.Metadata.TitleCN != "" {
			title = anime.Metadata.TitleCN
		} else if anime.Metadata.Title != "" {
			title = anime.Metadata.Title
		}
		if anime.Metadata.Image != "" {
			image = anime.Metadata.Image
		}
	}
	progress := 0.0
	remaining := int64(0)
	if history.DurationTicks > 0 {
		progress = min(100, float64(history.PositionTicks)/float64(history.DurationTicks)*100)
		remaining = max(0, (history.DurationTicks-history.PositionTicks)/playbackTicksPerSecond)
	}
	return &ContinueWatchingItem{
		AnimeID: anime.ID, EpisodeID: episode.ID, Title: title, EpisodeTitle: episode.Title,
		Image: image, Season: episode.SeasonNum, Episode: episode.EpisodeNum,
		PositionTicks: history.PositionTicks, DurationTicks: history.DurationTicks,
		ProgressPercent: progress, RemainingSeconds: remaining, UpdatedAt: history.LastPlayedAt,
		StreamURL: fmt.Sprintf("/api/v1/jellyfin/stream/%d", episode.ID),
	}, nil
}

func ContinueWatchingHandler(c *gin.Context) {
	userID, err := currentSessionUserID(c)
	if err != nil {
		v1Error(c, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit < 1 {
		limit = 10
	}
	limit = min(limit, 10)
	importJellyfinResumeItems(userID)
	histories, err := store.NewPlaybackHistoryStore(db.DB).ListRecent(userID, 250)
	if err != nil {
		v1Error(c, http.StatusInternalServerError, "continue_watching_failed", "读取继续观看记录失败")
		return
	}
	items := make([]ContinueWatchingItem, 0, limit)
	seenAnime := make(map[uint]struct{}, limit)
	for _, history := range histories {
		if _, exists := seenAnime[history.LocalAnimeID]; exists {
			continue
		}
		seenAnime[history.LocalAnimeID] = struct{}{}
		item, itemErr := continueWatchingItem(userID, history)
		if itemErr != nil {
			continue
		}
		items = append(items, *item)
		if len(items) == limit {
			break
		}
	}
	v1Data(c, http.StatusOK, gin.H{"items": items})
}
