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
