package service

import (
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
)

func metadataStore() *store.AnimeMetadataStore {
	if db.DB == nil {
		return nil
	}
	return store.NewAnimeMetadataStore(db.DB)
}

func findMetadataByTitleVariants(title string) (*model.AnimeMetadata, error) {
	s := metadataStore()
	if s == nil {
		return nil, nil
	}
	return s.FindByAnyTitle(title)
}

func listAllMetadata() ([]model.AnimeMetadata, error) {
	s := metadataStore()
	if s == nil {
		return nil, nil
	}
	return s.ListAll()
}
