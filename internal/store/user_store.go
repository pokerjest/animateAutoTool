package store

import (
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

type UserStore struct {
	db *gorm.DB
}

func NewUserStore(db *gorm.DB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) Count() (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var count int64
	if err := s.db.Model(&model.User{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (s *UserStore) GetByID(id any) (*model.User, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var user model.User
	if err := s.db.First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *UserStore) GetByUsername(username string) (*model.User, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var user model.User
	if err := s.db.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *UserStore) Create(user *model.User) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return s.db.Create(user).Error
}

func (s *UserStore) Save(user *model.User) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return s.db.Save(user).Error
}

func (s *UserStore) DeleteByUsername(username string) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return s.db.Where("username = ?", username).Delete(&model.User{}).Error
}
