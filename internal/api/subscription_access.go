package api

import (
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
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
		return nil, nil
	}
	return s.GetByID(id)
}

func subscriptionWithMetadataByID(id any) (*model.Subscription, error) {
	s := subscriptionStore()
	if s == nil {
		return nil, nil
	}
	return s.GetByIDWithMetadata(id)
}

func saveSubscription(sub *model.Subscription) error {
	s := subscriptionStore()
	if s == nil {
		return nil
	}
	return s.Save(sub)
}

func listSubscriptionsWithMetadata() ([]model.Subscription, error) {
	s := subscriptionStore()
	if s == nil {
		return nil, nil
	}
	return s.ListWithMetadata()
}
