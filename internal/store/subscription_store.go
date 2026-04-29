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
