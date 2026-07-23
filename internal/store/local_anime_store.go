package store

import (
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

type LocalAnimeStore struct {
	db *gorm.DB
}

func NewLocalAnimeStore(db *gorm.DB) *LocalAnimeStore {
	return &LocalAnimeStore{db: db}
}

func (s *LocalAnimeStore) ListDirectories() ([]model.LocalAnimeDirectory, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var dirs []model.LocalAnimeDirectory
	if err := s.db.Find(&dirs).Error; err != nil {
		return nil, err
	}
	return dirs, nil
}

// FindDirectoryByPath returns the directory matching path, optionally including
// soft-deleted rows. Returns ErrRecordNotFound when missing.
func (s *LocalAnimeStore) FindDirectoryByPath(path string, includeDeleted bool) (*model.LocalAnimeDirectory, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	q := s.db
	if includeDeleted {
		q = q.Unscoped()
	}
	var dir model.LocalAnimeDirectory
	if err := q.Where("path = ?", path).First(&dir).Error; err != nil {
		return nil, err
	}
	return &dir, nil
}

func (s *LocalAnimeStore) HardDeleteDirectory(dir *model.LocalAnimeDirectory) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return s.db.Unscoped().Delete(dir).Error
}

func (s *LocalAnimeStore) CreateDirectory(dir *model.LocalAnimeDirectory) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return s.db.Create(dir).Error
}

// RemoveDirectoryWithAnimes hard-deletes a directory together with its animes
// inside a single transaction.
func (s *LocalAnimeStore) RemoveDirectoryWithAnimes(id uint) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("directory_id = ?", id).Delete(&model.LocalAnime{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&model.LocalAnimeDirectory{}, id).Error
	})
}

func (s *LocalAnimeStore) CountAnimes() (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var n int64
	if err := s.db.Model(&model.LocalAnime{}).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (s *LocalAnimeStore) CountEpisodes() (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var n int64
	if err := s.db.Model(&model.LocalEpisode{}).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (s *LocalAnimeStore) CountAnimesWithJellyfin() (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var n int64
	if err := s.db.Model(&model.LocalAnime{}).Where("jellyfin_series_id <> ''").Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (s *LocalAnimeStore) CountEpisodesWithJellyfin() (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var n int64
	if err := s.db.Model(&model.LocalEpisode{}).Where("jellyfin_item_id <> ''").Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (s *LocalAnimeStore) ListAll() ([]model.LocalAnime, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var list []model.LocalAnime
	if err := s.db.Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (s *LocalAnimeStore) ListWithMetadataAndEpisodes() ([]model.LocalAnime, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var list []model.LocalAnime
	if err := s.db.Preload("Metadata").Preload("Episodes").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (s *LocalAnimeStore) GetWithMetadata(id any) (*model.LocalAnime, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var anime model.LocalAnime
	if err := s.db.Preload("Metadata").First(&anime, id).Error; err != nil {
		return nil, err
	}
	return &anime, nil
}

func (s *LocalAnimeStore) GetAnime(id any) (*model.LocalAnime, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var anime model.LocalAnime
	if err := s.db.First(&anime, id).Error; err != nil {
		return nil, err
	}
	return &anime, nil
}

func (s *LocalAnimeStore) GetDirectory(id any) (*model.LocalAnimeDirectory, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var dir model.LocalAnimeDirectory
	if err := s.db.First(&dir, id).Error; err != nil {
		return nil, err
	}
	return &dir, nil
}

func (s *LocalAnimeStore) ListAnimesByDirectory(directoryID uint) ([]model.LocalAnime, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var animes []model.LocalAnime
	if err := s.db.Where("directory_id = ?", directoryID).Find(&animes).Error; err != nil {
		return nil, err
	}
	return animes, nil
}

// ListEpisodesByAnimeIDOrdered returns episodes ordered by season then episode.
func (s *LocalAnimeStore) ListEpisodesByAnimeIDOrdered(animeID uint) ([]model.LocalEpisode, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var eps []model.LocalEpisode
	if err := s.db.Where("local_anime_id = ?", animeID).Order("season_num, episode_num").Find(&eps).Error; err != nil {
		return nil, err
	}
	return eps, nil
}

// UpdateEpisodePathByID sets the path on a single episode by primary key.
func (s *LocalAnimeStore) UpdateEpisodePathByID(id uint, newPath string) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return s.db.Model(&model.LocalEpisode{}).Where("id = ?", id).Update("path", newPath).Error
}

// UpdateEpisodePathByOldPath updates an episode's path by matching the old path.
func (s *LocalAnimeStore) UpdateEpisodePathByOldPath(oldPath, newPath string) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return s.db.Model(&model.LocalEpisode{}).Where("path = ?", oldPath).Update("path", newPath).Error
}

func (s *LocalAnimeStore) ListEpisodesByAnimeID(animeID uint) ([]model.LocalEpisode, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var eps []model.LocalEpisode
	if err := s.db.Where("local_anime_id = ?", animeID).Find(&eps).Error; err != nil {
		return nil, err
	}
	return eps, nil
}

func (s *LocalAnimeStore) FindAnimeByPath(path string) (*model.LocalAnime, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var anime model.LocalAnime
	if err := s.db.Where("path = ?", path).First(&anime).Error; err != nil {
		return nil, err
	}
	return &anime, nil
}

func (s *LocalAnimeStore) CreateAnime(anime *model.LocalAnime) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return retrySQLiteBusy(func() error { return s.db.Create(anime).Error })
}

func (s *LocalAnimeStore) SaveAnime(anime *model.LocalAnime) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return retrySQLiteBusy(func() error { return s.db.Save(anime).Error })
}

func (s *LocalAnimeStore) FindEpisodeByPath(path string) (*model.LocalEpisode, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var ep model.LocalEpisode
	if err := s.db.Where("path = ?", path).First(&ep).Error; err != nil {
		return nil, err
	}
	return &ep, nil
}

func (s *LocalAnimeStore) CreateEpisode(ep *model.LocalEpisode) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return retrySQLiteBusy(func() error { return s.db.Create(ep).Error })
}

func (s *LocalAnimeStore) SaveEpisode(ep *model.LocalEpisode) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return retrySQLiteBusy(func() error { return s.db.Save(ep).Error })
}

// DeleteEpisodesNotInPaths removes any episode rows under animeID whose path
// is not present in the keep set. When keep is empty, all episodes for the
// anime are removed.
func (s *LocalAnimeStore) DeleteEpisodesNotInPaths(animeID uint, keep []string) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if len(keep) == 0 {
		return s.db.Where("local_anime_id = ?", animeID).Delete(&model.LocalEpisode{}).Error
	}
	return s.db.Where("local_anime_id = ? AND path NOT IN ?", animeID, keep).Delete(&model.LocalEpisode{}).Error
}

// CleanupOrphans removes anime rows with no surviving episodes and any anime
// rows whose directory has been deleted.
func (s *LocalAnimeStore) CleanupOrphans() error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if err := s.db.Unscoped().
		Where("id NOT IN (?)", s.db.Model(&model.LocalEpisode{}).Select("DISTINCT local_anime_id")).
		Delete(&model.LocalAnime{}).Error; err != nil {
		return err
	}
	var dirIDs []uint
	if err := s.db.Model(&model.LocalAnimeDirectory{}).Pluck("id", &dirIDs).Error; err != nil {
		return err
	}
	if len(dirIDs) > 0 {
		return s.db.Unscoped().Where("directory_id NOT IN ?", dirIDs).Delete(&model.LocalAnime{}).Error
	}
	return s.db.Unscoped().Where("1 = 1").Delete(&model.LocalAnime{}).Error
}

// EpisodePathByMetadata is the join used by download log repair.
type EpisodePathRow struct {
	Path       string
	AnimeTitle string
}

func (s *LocalAnimeStore) EpisodePathsByMetadata(metadataID uint, episodeNum int) ([]EpisodePathRow, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var rows []EpisodePathRow
	if err := s.db.Table("local_episodes").
		Select("local_episodes.path, '' AS anime_title").
		Joins("JOIN local_animes ON local_animes.id = local_episodes.local_anime_id").
		Where("local_animes.metadata_id = ? AND local_episodes.episode_num = ?", metadataID, episodeNum).
		Order("local_episodes.updated_at DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *LocalAnimeStore) EpisodePathsByEpisodeNum(episodeNum int) ([]EpisodePathRow, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var rows []EpisodePathRow
	if err := s.db.Table("local_episodes").
		Select("local_episodes.path, local_animes.title AS anime_title").
		Joins("JOIN local_animes ON local_animes.id = local_episodes.local_anime_id").
		Where("local_episodes.episode_num = ?", episodeNum).
		Order("local_episodes.updated_at DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
