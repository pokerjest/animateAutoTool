package service

import (
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/store"
)

func localAnimeStore() *store.LocalAnimeStore {
	if db.DB == nil {
		return nil
	}
	return store.NewLocalAnimeStore(db.DB)
}
