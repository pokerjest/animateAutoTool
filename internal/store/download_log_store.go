package store

import (
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

type DownloadLogStore struct {
	db *gorm.DB
}

func NewDownloadLogStore(db *gorm.DB) *DownloadLogStore {
	return &DownloadLogStore{db: db}
}

// ListActiveOrIncompleteCompleted returns logs that are either still in flight
// (downloading/failed) or marked completed but missing a target file path.
func (s *DownloadLogStore) ListActiveOrIncompleteCompleted(downloading, failed, completed string) ([]model.DownloadLog, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var logs []model.DownloadLog
	if err := s.db.
		Where("status IN ?", []string{downloading, failed}).
		Or("status = ? AND (target_file = '' OR target_file IS NULL)", completed).
		Order("created_at DESC").
		Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

// ListByStatuses fetches logs whose status appears in the given set, newest first.
func (s *DownloadLogStore) ListByStatuses(statuses []string) ([]model.DownloadLog, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if len(statuses) == 0 {
		return nil, nil
	}
	var logs []model.DownloadLog
	if err := s.db.
		Where("status IN ?", statuses).
		Order("created_at DESC").
		Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

// ListByStatusesAsc fetches logs in the given statuses ordered oldest-first.
func (s *DownloadLogStore) ListByStatusesAsc(statuses []string) ([]model.DownloadLog, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if len(statuses) == 0 {
		return nil, nil
	}
	var logs []model.DownloadLog
	if err := s.db.
		Where("status IN ?", statuses).
		Order("created_at ASC").
		Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

// UpdateByID applies a partial update map to a single download log.
func (s *DownloadLogStore) UpdateByID(id uint, updates map[string]interface{}) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if len(updates) == 0 {
		return nil
	}
	return s.db.Model(&model.DownloadLog{}).Where("id = ?", id).Updates(updates).Error
}

// MarkArchived flips a log's status to the supplied archived value.
func (s *DownloadLogStore) MarkArchived(id uint, archivedStatus string) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return s.db.Model(&model.DownloadLog{}).Where("id = ?", id).Update("status", archivedStatus).Error
}

// HasCompletedSibling reports whether the same subscription already has a
// completed log entry, optionally filtered by episode number.
func (s *DownloadLogStore) HasCompletedSibling(subscriptionID uint, episode, completedStatus string) bool {
	if s == nil || s.db == nil {
		return false
	}
	query := s.db.Model(&model.DownloadLog{}).
		Where("subscription_id = ? AND status = ?", subscriptionID, completedStatus)
	if strings.TrimSpace(episode) != "" {
		query = query.Where("episode = ?", episode)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

// CountBySubscription returns the total log count tied to a subscription.
func (s *DownloadLogStore) CountBySubscription(subscriptionID uint) (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var count int64
	if err := s.db.Model(&model.DownloadLog{}).Where("subscription_id = ?", subscriptionID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
