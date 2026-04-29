package store

import (
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
	return s.db.Save(sub).Error
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
