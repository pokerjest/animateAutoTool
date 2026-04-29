package store

import (
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
