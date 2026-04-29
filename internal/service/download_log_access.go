package service

import (
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"gorm.io/gorm"
)

func downloadLogStore() *store.DownloadLogStore {
	if db.DB == nil {
		return nil
	}
	return store.NewDownloadLogStore(db.DB)
}

func loadAllSubscriptions() ([]model.Subscription, error) {
	s := subscriptionStore()
	if s == nil {
		return nil, gorm.ErrInvalidDB
	}
	return s.ListAll()
}

func configValue(key string) string {
	if db.DB == nil {
		return ""
	}
	return store.NewConfigStore(db.DB).GetDefault(key, "")
}
