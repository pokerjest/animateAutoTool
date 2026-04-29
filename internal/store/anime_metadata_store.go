package store

import (
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

type AnimeMetadataStore struct {
	db *gorm.DB
}

func NewAnimeMetadataStore(db *gorm.DB) *AnimeMetadataStore {
	return &AnimeMetadataStore{db: db}
}

func (s *AnimeMetadataStore) GetByID(id any) (*model.AnimeMetadata, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var m model.AnimeMetadata
	if err := s.db.First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *AnimeMetadataStore) FindByBangumiID(id int) (*model.AnimeMetadata, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var m model.AnimeMetadata
	if err := s.db.Where("bangumi_id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *AnimeMetadataStore) FindByTMDBID(id int) (*model.AnimeMetadata, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var m model.AnimeMetadata
	if err := s.db.Where("tmdb_id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// FindByAnyTitle returns the first metadata row matching the title against
// title / title_cn / title_jp / title_en columns.
func (s *AnimeMetadataStore) FindByAnyTitle(title string) (*model.AnimeMetadata, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var m model.AnimeMetadata
	if err := s.db.Where("title = ? OR title_cn = ? OR title_jp = ? OR title_en = ?",
		title, title, title, title).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *AnimeMetadataStore) FindByAniListID(id int) (*model.AnimeMetadata, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var m model.AnimeMetadata
	if err := s.db.Where("ani_list_id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *AnimeMetadataStore) Create(m *model.AnimeMetadata) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return s.db.Create(m).Error
}

func (s *AnimeMetadataStore) Save(m *model.AnimeMetadata) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return s.db.Save(m).Error
}

func (s *AnimeMetadataStore) ListAll() ([]model.AnimeMetadata, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var list []model.AnimeMetadata
	if err := s.db.Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ListWithImageRawMissing fetches metadata rows whose external image URL
// is set but the cached raw bytes are still empty.
func (s *AnimeMetadataStore) ListWithImageRawMissing() ([]model.AnimeMetadata, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var list []model.AnimeMetadata
	if err := s.db.Where(
		"(bangumi_image != '' AND (bangumi_image_raw IS NULL OR bangumi_image_raw = '')) OR " +
			"(tmdb_image != '' AND (tmdb_image_raw IS NULL OR tmdb_image_raw = '')) OR " +
			"(ani_list_image != '' AND (ani_list_image_raw IS NULL OR ani_list_image_raw = ''))",
	).Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// PropagateToSubscriptions updates fields on all subscriptions linked to the
// given metadata id.
func (s *AnimeMetadataStore) PropagateToSubscriptions(metadataID uint, updates map[string]interface{}) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if metadataID == 0 || len(updates) == 0 {
		return nil
	}
	return s.db.Model(&model.Subscription{}).Where("metadata_id = ?", metadataID).Updates(updates).Error
}

// PropagateToLocalAnimes updates fields on all LocalAnime rows linked to the
// given metadata id.
func (s *AnimeMetadataStore) PropagateToLocalAnimes(metadataID uint, updates map[string]interface{}) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if metadataID == 0 || len(updates) == 0 {
		return nil
	}
	return s.db.Model(&model.LocalAnime{}).Where("metadata_id = ?", metadataID).Updates(updates).Error
}
