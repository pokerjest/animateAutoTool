package store

import (
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

type AuditLogStore struct {
	db *gorm.DB
}

func NewAuditLogStore(db *gorm.DB) *AuditLogStore {
	return &AuditLogStore{db: db}
}

// Create persists a single audit entry. The caller has already populated
// CreatedAt via gorm callbacks, so this is intentionally a thin wrapper.
func (s *AuditLogStore) Create(entry *model.AuditLog) error {
	if s == nil || s.db == nil {
		return gorm.ErrInvalidDB
	}
	if entry == nil {
		return nil
	}
	return s.db.Create(entry).Error
}

// AuditLogQuery scopes a ListRecent call. All fields are optional;
// the zero value lists the most recent entries across every action.
type AuditLogQuery struct {
	Limit    int
	Offset   int
	Action   string
	Username string
	Outcome  string
}

// ListRecent returns audit entries newest-first, honoring the query
// scope. Limit defaults to 100 and is capped at 500 to prevent
// runaway queries.
func (s *AuditLogStore) ListRecent(q AuditLogQuery) ([]model.AuditLog, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	tx := s.db.Model(&model.AuditLog{})
	if q.Action != "" {
		tx = tx.Where("action = ?", q.Action)
	}
	if q.Username != "" {
		tx = tx.Where("username = ?", q.Username)
	}
	if q.Outcome != "" {
		tx = tx.Where("outcome = ?", q.Outcome)
	}

	var rows []model.AuditLog
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	if err := tx.Order("id desc").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Count returns total entries matching the same scope as ListRecent.
func (s *AuditLogStore) Count(q AuditLogQuery) (int64, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}

	tx := s.db.Model(&model.AuditLog{})
	if q.Action != "" {
		tx = tx.Where("action = ?", q.Action)
	}
	if q.Username != "" {
		tx = tx.Where("username = ?", q.Username)
	}
	if q.Outcome != "" {
		tx = tx.Where("outcome = ?", q.Outcome)
	}

	var n int64
	if err := tx.Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}
