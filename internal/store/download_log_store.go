package store

import (
	"strings"
	"time"

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
		Order("created_at DESC, id DESC").
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
		Order("created_at DESC, id DESC").
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
		Order("created_at ASC, id ASC").
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

// ListBySubscription returns the most recent N download logs for a subscription.
func (s *DownloadLogStore) ListBySubscription(subscriptionID uint, limit int) ([]model.DownloadLog, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	q := s.db.Where("subscription_id = ?", subscriptionID).Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var logs []model.DownloadLog
	if err := q.Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

// ListBySubscriptionAndStatuses fetches all download logs for a subscription
// whose status appears in the supplied set.
func (s *DownloadLogStore) ListBySubscriptionAndStatuses(subscriptionID uint, statuses []string) ([]model.DownloadLog, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if len(statuses) == 0 {
		return nil, nil
	}
	var logs []model.DownloadLog
	if err := s.db.Where("subscription_id = ? AND status IN ?", subscriptionID, statuses).Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

// CountResettable counts download logs for a subscription that match given
// statuses and were created before `cutoff`. Used by the "reset stale logs"
// repair flow.
func (s *DownloadLogStore) CountResettable(subscriptionID uint, statuses []string, cutoff time.Time) (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	if len(statuses) == 0 {
		return 0, nil
	}
	var count int64
	if err := s.db.Model(&model.DownloadLog{}).
		Where("subscription_id = ? AND status IN ? AND created_at < ?", subscriptionID, statuses, cutoff).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// MarkResettableArchived flips matching download logs to the supplied
// archived status.
func (s *DownloadLogStore) MarkResettableArchived(subscriptionID uint, statuses []string, cutoff time.Time, archivedStatus string) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if len(statuses) == 0 {
		return nil
	}
	return s.db.Model(&model.DownloadLog{}).
		Where("subscription_id = ? AND status IN ? AND created_at < ?", subscriptionID, statuses, cutoff).
		Update("status", archivedStatus).Error
}

// UpdateTargetFileByOld replaces target_file on logs whose target_file matches
// the supplied old path. Used by rename flows.
func (s *DownloadLogStore) UpdateTargetFileByOld(oldPath, newPath string) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return s.db.Model(&model.DownloadLog{}).Where("target_file = ?", oldPath).Update("target_file", newPath).Error
}

// CountByStatus returns the number of download logs in the given status.
func (s *DownloadLogStore) CountByStatus(status string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var n int64
	if err := s.db.Model(&model.DownloadLog{}).Where("status = ?", status).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
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
