package store

import (
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

// SubscriptionResourceStore contains durable RSS/download reconciliation
// operations. Candidate selection remains in the service layer.
type SubscriptionResourceStore struct {
	db *gorm.DB
}

func NewSubscriptionResourceStore(db *gorm.DB) *SubscriptionResourceStore {
	return &SubscriptionResourceStore{db: db}
}

func (s *SubscriptionResourceStore) ListBySubscription(subscriptionID uint) ([]model.SubscriptionResource, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var resources []model.SubscriptionResource
	err := s.db.Where("subscription_id = ?", subscriptionID).
		Order("season_val ASC, episode ASC, selected DESC, candidate_rank ASC, id ASC").
		Find(&resources).Error
	return resources, err
}

func (s *SubscriptionResourceStore) ListByCanonicalKey(subscriptionID uint, canonicalKey string) ([]model.SubscriptionResource, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var resources []model.SubscriptionResource
	err := s.db.Where("subscription_id = ? AND canonical_key = ?", subscriptionID, strings.TrimSpace(canonicalKey)).
		Order("selected DESC, candidate_rank ASC, id ASC").
		Find(&resources).Error
	return resources, err
}

func (s *SubscriptionResourceStore) FindByFingerprint(subscriptionID uint, fingerprint string) (*model.SubscriptionResource, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var resource model.SubscriptionResource
	err := s.db.Where("subscription_id = ? AND fingerprint = ?", subscriptionID, strings.TrimSpace(fingerprint)).
		Limit(1).
		Find(&resource).Error
	if err != nil {
		return nil, err
	}
	if resource.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &resource, nil
}

func (s *SubscriptionResourceStore) Upsert(resource *model.SubscriptionResource) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if resource == nil {
		return nil
	}
	if resource.ID != 0 {
		return retrySQLiteBusy(func() error {
			return s.db.Save(resource).Error
		})
	}

	var existing model.SubscriptionResource
	err := s.db.Where("subscription_id = ? AND fingerprint = ?", resource.SubscriptionID, resource.Fingerprint).
		Limit(1).
		Find(&existing).Error
	switch {
	case err != nil:
		return err
	case existing.ID != 0:
		resource.ID = existing.ID
		resource.CreatedAt = existing.CreatedAt
		return s.Upsert(resource)
	default:
		return retrySQLiteBusy(func() error { return s.db.Create(resource).Error })
	}
}

func (s *SubscriptionResourceStore) UpdateByID(id uint, updates map[string]any) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if id == 0 || len(updates) == 0 {
		return nil
	}
	return retrySQLiteBusy(func() error {
		return s.db.Model(&model.SubscriptionResource{}).Where("id = ?", id).Updates(updates).Error
	})
}

func (s *SubscriptionResourceStore) MarkAllNotCurrent(subscriptionID uint) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if subscriptionID == 0 {
		return nil
	}
	return retrySQLiteBusy(func() error {
		return s.db.Model(&model.SubscriptionResource{}).
			Where("subscription_id = ? AND current = ?", subscriptionID, true).
			Update("current", false).Error
	})
}

func (s *SubscriptionResourceStore) CountByState(subscriptionID uint, states []string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	if len(states) == 0 {
		return 0, nil
	}
	var count int64
	err := s.db.Model(&model.SubscriptionResource{}).
		Where("subscription_id = ? AND state IN ?", subscriptionID, states).
		Count(&count).Error
	return count, err
}
