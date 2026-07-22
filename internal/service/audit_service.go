package service

import (
	"encoding/json"
	"log"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
)

// Audit action identifiers. Keep these stable — log readers and any future
// alerting rules will key off of them.
const (
	AuditActionLoginSuccess         = "login.success"
	AuditActionLoginFailure         = "login.failure"
	AuditActionLogout               = "logout"
	AuditActionPasswordChange       = "password.change"
	AuditActionPasswordRecoveryLoc  = "password.recovery.local"
	AuditActionBootstrapComplete    = "bootstrap.complete"
	AuditActionSubscriptionDelete   = "subscription.delete"
	AuditActionLocalDirectoryDelete = "local_directory.delete"
	AuditActionBackupRestore        = "backup.restore"
	AuditActionR2BackupRestore      = "backup.r2.restore"
	AuditActionR2BackupDelete       = "backup.r2.delete"
	AuditActionAISettingsUpdate     = "settings.ai.update"
	AuditActionSettingsUpdate       = "settings.update"
)

const (
	AuditOutcomeSuccess = "success"
	AuditOutcomeFailure = "failure"
)

// AuditContext captures the request-side data we want every audit entry
// to carry. Handlers fill this in once and pass it to RecordAudit.
type AuditContext struct {
	UserID    uint
	Username  string
	IP        string
	UserAgent string
}

// AuditEntry is the input to RecordAudit. Details may be any JSON-marshalable
// value; if marshal fails the entry is still recorded with an empty details
// payload rather than dropped on the floor.
type AuditEntry struct {
	Action     string
	Outcome    string
	TargetType string
	TargetID   string
	Details    any
}

func auditLogStore() *store.AuditLogStore {
	if db.DB == nil {
		return nil
	}
	return store.NewAuditLogStore(db.DB)
}

// RecordAudit persists a single audit entry. It deliberately swallows
// store errors (after logging) — audit recording must never break a
// user-facing request flow.
func RecordAudit(ctx AuditContext, entry AuditEntry) {
	s := auditLogStore()
	if s == nil {
		return
	}

	if entry.Action == "" {
		return
	}
	if entry.Outcome == "" {
		entry.Outcome = AuditOutcomeSuccess
	}

	details := ""
	if entry.Details != nil {
		if encoded, err := json.Marshal(entry.Details); err == nil {
			details = string(encoded)
		} else {
			log.Printf("audit: failed to encode details for %s: %v", entry.Action, err)
		}
	}

	row := &model.AuditLog{
		UserID:     ctx.UserID,
		Username:   ctx.Username,
		Action:     entry.Action,
		Outcome:    entry.Outcome,
		TargetType: entry.TargetType,
		TargetID:   entry.TargetID,
		IP:         ctx.IP,
		UserAgent:  truncateUA(ctx.UserAgent),
		Details:    details,
	}
	if err := s.Create(row); err != nil {
		log.Printf("audit: failed to record %s for user=%q: %v", entry.Action, ctx.Username, err)
	}
}

// ListAuditLogs is a thin pass-through used by the API layer to expose
// recent entries on the audit page / endpoint.
func ListAuditLogs(q store.AuditLogQuery) ([]model.AuditLog, error) {
	s := auditLogStore()
	if s == nil {
		return nil, nil
	}
	return s.ListRecent(q)
}

// truncateUA keeps the user agent column from growing unbounded if a
// client sends a pathological header.
func truncateUA(ua string) string {
	const maxLen = 480
	if len(ua) <= maxLen {
		return ua
	}
	return ua[:maxLen] + "…"
}
