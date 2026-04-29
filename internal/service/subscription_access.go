package service

import (
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"gorm.io/gorm"
)

func subscriptionStore() *store.SubscriptionStore {
	if db.DB == nil {
		return nil
	}
	return store.NewSubscriptionStore(db.DB)
}

func loadActiveSubscriptions() ([]model.Subscription, error) {
	s := subscriptionStore()
	if s == nil {
		return nil, gorm.ErrInvalidDB
	}
	return s.ListActive()
}

func loadActiveSubscriptionsByIDs(ids []uint) ([]model.Subscription, error) {
	s := subscriptionStore()
	if s == nil {
		return nil, gorm.ErrInvalidDB
	}
	return s.ListActiveByIDs(ids)
}

func loadStaleStrategySubscriptions() ([]model.Subscription, error) {
	s := subscriptionStore()
	if s == nil {
		return nil, gorm.ErrInvalidDB
	}
	return s.ListWithStaleStrategy()
}

func saveSubscription(sub *model.Subscription) error {
	s := subscriptionStore()
	if s == nil {
		return gorm.ErrInvalidDB
	}
	return s.Save(sub)
}
