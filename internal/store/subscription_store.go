package store

import (
	"errors"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

type SubscriptionStore struct {
	db *gorm.DB
}

func NewSubscriptionStore(db *gorm.DB) *SubscriptionStore {
	return &SubscriptionStore{db: db}
}

func (s *SubscriptionStore) ListActive() ([]model.Subscription, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var subs []model.Subscription
	if err := s.db.Where("is_active = ?", true).Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

func (s *SubscriptionStore) ListAll() ([]model.Subscription, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var subs []model.Subscription
	if err := s.db.Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

func (s *SubscriptionStore) ListWithMetadata() ([]model.Subscription, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var subs []model.Subscription
	if err := s.db.Preload("Metadata").Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

func (s *SubscriptionStore) ListWithMetadataPage(offset, limit int) ([]model.Subscription, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		return []model.Subscription{}, nil
	}
	var subs []model.Subscription
	if err := s.db.Preload("Metadata").Order("id ASC").Offset(offset).Limit(limit).Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

func (s *SubscriptionStore) GetByID(id any) (*model.Subscription, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var sub model.Subscription
	if err := s.db.First(&sub, id).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

func (s *SubscriptionStore) GetByIDWithMetadata(id any) (*model.Subscription, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var sub model.Subscription
	if err := s.db.Preload("Metadata").First(&sub, id).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

func (s *SubscriptionStore) Save(sub *model.Subscription) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return retrySQLiteBusy(func() error { return s.db.Save(sub).Error })
}

func (s *SubscriptionStore) Create(sub *model.Subscription) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return retrySQLiteBusy(func() error { return s.db.Create(sub).Error })
}

func (s *SubscriptionStore) UpdateLastEpisodeIfGreater(id uint, episode int) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if id == 0 || episode <= 0 {
		return nil
	}
	return retrySQLiteBusy(func() error {
		return s.db.Model(&model.Subscription{}).
			Where("id = ? AND last_ep < ?", id, episode).
			Update("last_ep", episode).Error
	})
}

func (s *SubscriptionStore) SetLastEpisode(id uint, episode int) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if id == 0 || episode < 0 {
		return nil
	}
	return retrySQLiteBusy(func() error {
		return s.db.Model(&model.Subscription{}).Where("id = ?", id).Update("last_ep", episode).Error
	})
}

// FindByRSSURLUnscoped looks up a subscription by RSS URL including
// soft-deleted rows. The bool reports whether a matching row exists.
func (s *SubscriptionStore) FindByRSSURLUnscoped(rssURL string) (*model.Subscription, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, gorm.ErrInvalidDB
	}
	var sub model.Subscription
	err := s.db.Unscoped().Where("rss_url = ?", rssURL).First(&sub).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &sub, true, nil
}

// DeleteCascade hard-deletes a subscription together with its download logs in
// a single transaction.
func (s *SubscriptionStore) DeleteCascade(id uint) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return retrySQLiteBusy(func() error {
		return s.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Unscoped().Where("subscription_id = ?", id).Delete(&model.DownloadLog{}).Error; err != nil {
				return err
			}
			if err := tx.Unscoped().Where("subscription_id = ?", id).Delete(&model.SubscriptionResource{}).Error; err != nil {
				return err
			}
			return tx.Unscoped().Delete(&model.Subscription{}, id).Error
		})
	})
}

func (s *SubscriptionStore) Count() (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var n int64
	if err := s.db.Model(&model.Subscription{}).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (s *SubscriptionStore) CountActive() (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var n int64
	if err := s.db.Model(&model.Subscription{}).Where("is_active = ?", true).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CountAutoDisabledOnDone reports subscriptions that auto-disabled themselves
// because they reached their expected episode count.
func (s *SubscriptionStore) CountAutoDisabledOnDone() (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var n int64
	if err := s.db.Model(&model.Subscription{}).
		Where("is_active = ? AND auto_disable_on_done = ? AND expected_episodes > 0 AND last_ep >= expected_episodes", false, true).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CountStaleSince counts active subscriptions whose stale_after_hours is set and
// whose last_success_at is older than `before`.
func (s *SubscriptionStore) CountStaleSince(before time.Time) (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var n int64
	if err := s.db.Model(&model.Subscription{}).
		Where("is_active = ? AND stale_after_hours > 0 AND last_success_at IS NOT NULL AND last_success_at < ?", true, before).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (s *SubscriptionStore) ListActiveByIDs(ids []uint) ([]model.Subscription, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var subs []model.Subscription
	if err := s.db.Where("id IN ? AND is_active = ?", ids, true).Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

func (s *SubscriptionStore) ListWithStaleStrategy() ([]model.Subscription, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var subs []model.Subscription
	if err := s.db.
		Where("is_active = ? AND stale_after_hours > 0 AND last_success_at IS NOT NULL", true).
		Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}
