package store

import (
	"errors"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

type ConfigStore struct {
	db *gorm.DB
}

func NewConfigStore(db *gorm.DB) *ConfigStore {
	return &ConfigStore{db: db}
}

func (s *ConfigStore) ListMap() (map[string]string, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}

	var configs []model.GlobalConfig
	if err := s.db.Find(&configs).Error; err != nil {
		return nil, err
	}

	result := make(map[string]string, len(configs))
	for _, cfg := range configs {
		result[cfg.Key] = cfg.Value
	}
	return result, nil
}

func (s *ConfigStore) Get(key string) (string, error) {
	if s == nil || s.db == nil {
		return "", gorm.ErrInvalidDB
	}

	var cfg model.GlobalConfig
	result := s.db.Where("key = ?", key).Limit(1).Find(&cfg)
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 0 {
		return "", gorm.ErrRecordNotFound
	}
	return cfg.Value, nil
}

func (s *ConfigStore) GetDefault(key, fallback string) string {
	value, err := s.Get(key)
	if err != nil {
		return fallback
	}
	return value
}

func (s *ConfigStore) Set(key, value string) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("config key is required")
	}
	var conf model.GlobalConfig
	return s.db.Where(model.GlobalConfig{Key: key}).
		Assign(model.GlobalConfig{Value: value}).
		FirstOrCreate(&conf).Error
}

func (s *ConfigStore) SetMany(values map[string]string) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		txStore := NewConfigStore(tx)
		for key, value := range values {
			if err := txStore.Set(key, value); err != nil {
				return err
			}
		}
		return nil
	})
}
