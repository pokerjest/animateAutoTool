package store

import (
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

type LibraryIssueStore struct {
	db *gorm.DB
}

func NewLibraryIssueStore(db *gorm.DB) *LibraryIssueStore {
	return &LibraryIssueStore{db: db}
}

func (s *LibraryIssueStore) GetByKey(issueKey string) (*model.LibraryIssue, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var issue model.LibraryIssue
	if err := s.db.Where("issue_key = ?", issueKey).First(&issue).Error; err != nil {
		return nil, err
	}
	return &issue, nil
}

func (s *LibraryIssueStore) GetOpenByKey(issueKey, openStatus string) (*model.LibraryIssue, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var issue model.LibraryIssue
	if err := s.db.Where("issue_key = ? AND status = ?", issueKey, openStatus).First(&issue).Error; err != nil {
		return nil, err
	}
	return &issue, nil
}

func (s *LibraryIssueStore) Create(issue *model.LibraryIssue) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if issue == nil {
		return nil
	}
	return retrySQLiteBusy(func() error { return s.db.Create(issue).Error })
}

func (s *LibraryIssueStore) UpdateByID(id uint, updates map[string]interface{}) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if id == 0 || len(updates) == 0 {
		return nil
	}
	return retrySQLiteBusy(func() error {
		return s.db.Model(&model.LibraryIssue{}).Where("id = ?", id).Updates(updates).Error
	})
}

func (s *LibraryIssueStore) ListOpen(status string, limit int) ([]model.LibraryIssue, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	query := s.db.Where("status = ?", status).Order("last_seen_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	var issues []model.LibraryIssue
	if err := query.Find(&issues).Error; err != nil {
		return nil, err
	}
	return issues, nil
}

func (s *LibraryIssueStore) ListOpenByType(status, issueType string) ([]model.LibraryIssue, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var issues []model.LibraryIssue
	if err := s.db.
		Where("status = ? AND issue_type = ?", status, issueType).
		Order("local_anime_id ASC, id ASC").
		Find(&issues).Error; err != nil {
		return nil, err
	}
	return issues, nil
}
