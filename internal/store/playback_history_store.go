package store

import (
	"time"

	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PlaybackHistoryStore struct {
	db *gorm.DB
}

func NewPlaybackHistoryStore(db *gorm.DB) *PlaybackHistoryStore {
	return &PlaybackHistoryStore{db: db}
}

func (s *PlaybackHistoryStore) Upsert(history *model.PlaybackHistory) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if history.LastPlayedAt.IsZero() {
		history.LastPlayedAt = time.Now().UTC()
	}
	return retrySQLiteBusy(func() error {
		return s.db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "local_episode_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"local_anime_id", "position_ticks", "duration_ticks", "completed", "last_event", "last_played_at", "updated_at", "deleted_at",
			}),
		}).Create(history).Error
	})
}

func (s *PlaybackHistoryStore) Find(userID, episodeID uint) (*model.PlaybackHistory, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var history model.PlaybackHistory
	if err := s.db.Where("user_id = ? AND local_episode_id = ?", userID, episodeID).First(&history).Error; err != nil {
		return nil, err
	}
	return &history, nil
}

func (s *PlaybackHistoryStore) ListRecent(userID uint, limit int) ([]model.PlaybackHistory, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if limit <= 0 {
		limit = 100
	}
	var histories []model.PlaybackHistory
	if err := s.db.Where("user_id = ?", userID).Order("last_played_at DESC").Limit(limit).Find(&histories).Error; err != nil {
		return nil, err
	}
	return histories, nil
}

func (s *PlaybackHistoryStore) Count(userID uint) (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var count int64
	if err := s.db.Model(&model.PlaybackHistory{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
