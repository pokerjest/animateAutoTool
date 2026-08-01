package store

import (
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

// ListForSubscriptionMatching returns only identity fields needed by the
// subscription list. Episode rows are aggregated separately so a large media
// library does not materialize every LocalEpisode model in memory.
func (s *LocalAnimeStore) ListForSubscriptionMatching() ([]model.LocalAnime, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}

	var list []model.LocalAnime
	query := s.db.Model(&model.LocalAnime{}).
		Select("local_animes.id", "local_animes.title", "local_animes.path", "local_animes.season", "local_animes.metadata_id", "local_animes.jellyfin_series_id").
		Where("EXISTS (?)", s.db.Model(&model.LocalEpisode{}).
			Select("1").
			Where("local_episodes.local_anime_id = local_animes.id"))
	if err := query.Preload("Metadata", func(tx *gorm.DB) *gorm.DB {
		return tx.Select(
			"id", "title", "title_cn", "title_en", "title_jp", "original_title",
			"bangumi_title", "tmdb_title", "ani_list_title", "bangumi_id", "tmdb_id", "ani_list_id",
		)
	}).Order("local_animes.id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}
