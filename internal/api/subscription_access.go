package api

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

func subscriptionByID(id any) (*model.Subscription, error) {
	s := subscriptionStore()
	if s == nil {
		return nil, gorm.ErrInvalidDB
	}
	return s.GetByID(id)
}

func subscriptionWithMetadataByID(id any) (*model.Subscription, error) {
	s := subscriptionStore()
	if s == nil {
		return nil, gorm.ErrInvalidDB
	}
	return s.GetByIDWithMetadata(id)
}

func saveSubscription(sub *model.Subscription) error {
	s := subscriptionStore()
	if s == nil {
		return gorm.ErrInvalidDB
	}
	return s.Save(sub)
}

func listSubscriptionsWithMetadata() ([]model.Subscription, error) {
	s := subscriptionStore()
	if s == nil {
		return nil, gorm.ErrInvalidDB
	}
	return s.ListWithMetadata()
}

func downloadLogStore() *store.DownloadLogStore {
	if db.DB == nil {
		return nil
	}
	return store.NewDownloadLogStore(db.DB)
}

func localAnimeStore() *store.LocalAnimeStore {
	if db.DB == nil {
		return nil
	}
	return store.NewLocalAnimeStore(db.DB)
}
