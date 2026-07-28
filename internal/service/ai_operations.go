package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pokerjest/animateAutoTool/internal/ai"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

const (
	AIProposalStatusAnalyzing = "analyzing"
	AIProposalStatusReady     = "ready"
	AIProposalStatusApplied   = "applied"
	AIProposalStatusDismissed = "dismissed"
	AIProposalStatusFailed    = "failed"
	AIProposalStatusExpired   = "expired"
	AIProposalStatusStale     = "stale"

	AIProposalTypeFilenameResolution = "filename_resolution"
	AIProposalTypeMetadataMatch      = "metadata_match"
	AIProposalTypeHealthDiagnosis    = "health_diagnosis"
	AIProposalTypeSubscriptionRule   = "subscription_rule"
	AIProposalTypeLibraryScan        = "library_scan"
)

var (
	ErrAIProposalNotFound      = errors.New("AI proposal not found")
	ErrAIProposalNotReady      = errors.New("AI proposal is not ready")
	ErrAIProposalExpired       = errors.New("AI proposal expired")
	ErrAIProposalNotActionable = errors.New("AI proposal is not actionable")
	ErrAIConfirmationInvalid   = errors.New("AI confirmation token is invalid or already used")
	aiAuthorizationPattern     = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(?:bearer\s+)?[^\s,;"']+`)
	aiSecretAssignmentPattern  = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|private[_-]?key|token|password|passwd|secret|cookie)\b(\s*[:=]\s*)("[^"]*"|'[^']*'|[^,\s;&]+)`)
	aiSecretJSONPattern        = regexp.MustCompile(`(?i)("(?:api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|private[_-]?key|token|password|passwd|secret|authorization|cookie)"\s*:\s*)"[^"]*"`)
	aiSecretQueryPattern       = regexp.MustCompile(`(?i)([?&](?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|secret)=)[^&#\s]+`)
)

type AIProposalInput struct {
	UserID           uint
	Type             string
	TargetType       string
	TargetID         string
	Summary          string
	Confidence       float64
	Evidence         []string
	Warnings         []string
	Payload          any
	InputFingerprint string
	ApplyTool        string
	Provider         string
	Model            string
	ExpiresAt        *time.Time
	Status           string
	Error            string
}

type AIProposalView struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Status     string         `json:"status"`
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	Summary    string         `json:"summary"`
	Confidence float64        `json:"confidence"`
	Evidence   []string       `json:"evidence"`
	Warnings   []string       `json:"warnings"`
	Actionable bool           `json:"actionable"`
	Payload    map[string]any `json:"payload"`
	Provider   string         `json:"provider"`
	Model      string         `json:"model"`
	Error      string         `json:"error,omitempty"`
	ExpiresAt  *time.Time     `json:"expires_at,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

func CreateAIProposal(input AIProposalInput) (*model.AIProposal, error) {
	if db.DB == nil {
		return nil, gorm.ErrInvalidDB
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = AIProposalStatusAnalyzing
	}
	row := &model.AIProposal{
		ID:               uuid.NewString(),
		UserID:           input.UserID,
		Type:             strings.TrimSpace(input.Type),
		Status:           status,
		TargetType:       strings.TrimSpace(input.TargetType),
		TargetID:         strings.TrimSpace(input.TargetID),
		Summary:          strings.TrimSpace(input.Summary),
		Confidence:       clampAIConfidence(input.Confidence),
		Evidence:         marshalAIJSON(input.Evidence),
		Warnings:         marshalAIJSON(input.Warnings),
		Payload:          marshalAIJSON(input.Payload),
		InputFingerprint: strings.TrimSpace(input.InputFingerprint),
		ApplyTool:        strings.TrimSpace(input.ApplyTool),
		Provider:         strings.TrimSpace(input.Provider),
		Model:            strings.TrimSpace(input.Model),
		Error:            SanitizeAIText(input.Error),
		ExpiresAt:        input.ExpiresAt,
	}
	if err := db.DB.Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func CompleteAIProposal(id string, input AIProposalInput) error {
	if db.DB == nil {
		return gorm.ErrInvalidDB
	}
	updates := map[string]any{
		"status":            AIProposalStatusReady,
		"summary":           strings.TrimSpace(input.Summary),
		"confidence":        clampAIConfidence(input.Confidence),
		"evidence":          marshalAIJSON(input.Evidence),
		"warnings":          marshalAIJSON(input.Warnings),
		"payload":           marshalAIJSON(input.Payload),
		"input_fingerprint": strings.TrimSpace(input.InputFingerprint),
		"apply_tool":        strings.TrimSpace(input.ApplyTool),
		"provider":          strings.TrimSpace(input.Provider),
		"model":             strings.TrimSpace(input.Model),
		"error":             "",
		"expires_at":        input.ExpiresAt,
	}
	return db.DB.Model(&model.AIProposal{}).Where("id = ?", strings.TrimSpace(id)).Updates(updates).Error
}

func FailAIProposal(id string, err error) {
	if db.DB == nil || strings.TrimSpace(id) == "" {
		return
	}
	message := "AI 分析失败"
	if err != nil {
		message = SanitizeAIText(err.Error())
	}
	_ = db.DB.Model(&model.AIProposal{}).Where("id = ?", id).Updates(map[string]any{
		"status": AIProposalStatusFailed,
		"error":  truncateAIValue(message, 4000),
	}).Error
}

func GetAIProposal(userID uint, id string) (*model.AIProposal, error) {
	if db.DB == nil {
		return nil, gorm.ErrInvalidDB
	}
	var row model.AIProposal
	err := db.DB.Where("id = ? AND user_id = ?", strings.TrimSpace(id), userID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAIProposalNotFound
	}
	if err != nil {
		return nil, err
	}
	if row.ExpiresAt != nil && row.ExpiresAt.Before(time.Now()) &&
		row.Status != AIProposalStatusApplied && row.Status != AIProposalStatusDismissed && row.Status != AIProposalStatusExpired {
		row.Status = AIProposalStatusExpired
		_ = db.DB.Model(&model.AIProposal{}).Where("id = ?", row.ID).Update("status", row.Status).Error
	}
	return &row, nil
}

func AIProposalToView(row *model.AIProposal) AIProposalView {
	if row == nil {
		return AIProposalView{Evidence: []string{}, Warnings: []string{}, Payload: map[string]any{}}
	}
	view := AIProposalView{
		ID:         row.ID,
		Type:       row.Type,
		Status:     row.Status,
		TargetType: row.TargetType,
		TargetID:   row.TargetID,
		Summary:    row.Summary,
		Confidence: row.Confidence,
		Evidence:   []string{},
		Warnings:   []string{},
		Actionable: strings.TrimSpace(row.ApplyTool) != "",
		Payload:    map[string]any{},
		Provider:   row.Provider,
		Model:      row.Model,
		Error:      row.Error,
		ExpiresAt:  row.ExpiresAt,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
	_ = json.Unmarshal([]byte(row.Evidence), &view.Evidence)
	_ = json.Unmarshal([]byte(row.Warnings), &view.Warnings)
	_ = json.Unmarshal([]byte(row.Payload), &view.Payload)
	return view
}

func ConfirmAIProposal(userID uint, id string, ttl time.Duration) (string, error) {
	row, err := GetAIProposal(userID, id)
	if err != nil {
		return "", err
	}
	if row.Status == AIProposalStatusExpired {
		return "", ErrAIProposalExpired
	}
	if row.Status != AIProposalStatusReady {
		return "", ErrAIProposalNotReady
	}
	if strings.TrimSpace(row.ApplyTool) == "" {
		return "", ErrAIProposalNotActionable
	}
	if ttl <= 0 || ttl > 10*time.Minute {
		ttl = 5 * time.Minute
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tokenBytes)
	hash := hashAIConfirmationToken(token, row)
	expiresAt := time.Now().Add(ttl)
	if err := db.DB.Model(&model.AIProposal{}).
		Where("id = ? AND user_id = ? AND status = ?", row.ID, userID, AIProposalStatusReady).
		Updates(map[string]any{
			"confirm_token_hash": hash,
			"confirm_expires_at": &expiresAt,
			"confirm_used_at":    nil,
		}).Error; err != nil {
		return "", err
	}
	return token, nil
}

func ConsumeAIConfirmation(userID uint, id, token string) (*model.AIProposal, error) {
	row, err := GetAIProposal(userID, id)
	if err != nil {
		return nil, err
	}
	if row.Status == AIProposalStatusExpired {
		return nil, ErrAIProposalExpired
	}
	if row.Status != AIProposalStatusReady || strings.TrimSpace(row.ApplyTool) == "" {
		return nil, ErrAIProposalNotReady
	}
	hashText := hashAIConfirmationToken(strings.TrimSpace(token), row)
	now := time.Now()
	result := db.DB.Model(&model.AIProposal{}).
		Where("id = ? AND user_id = ? AND status = ? AND confirm_token_hash = ? AND confirm_used_at IS NULL AND confirm_expires_at > ?",
			row.ID, userID, AIProposalStatusReady, hashText, now).
		Update("confirm_used_at", &now)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrAIConfirmationInvalid
	}
	row.ConfirmUsedAt = &now
	return row, nil
}

func hashAIConfirmationToken(token string, row *model.AIProposal) string {
	if row == nil {
		return ""
	}
	binding := strings.Join([]string{
		strconv.FormatUint(uint64(row.UserID), 10),
		row.ID,
		row.TargetType,
		row.TargetID,
		row.ApplyTool,
		row.InputFingerprint,
	}, "\x00")
	hash := sha256.Sum256([]byte(strings.TrimSpace(token) + "\x00" + binding))
	return hex.EncodeToString(hash[:])
}

func MarkAIProposalApplied(id string) error {
	now := time.Now()
	return db.DB.Model(&model.AIProposal{}).Where("id = ?", id).Updates(map[string]any{
		"status":     AIProposalStatusApplied,
		"applied_at": &now,
		"error":      "",
	}).Error
}

func MarkAIProposalStale(id, message string) error {
	return db.DB.Model(&model.AIProposal{}).Where("id = ?", id).Updates(map[string]any{
		"status": AIProposalStatusStale,
		"error":  truncateAIValue(SanitizeAIText(message), 4000),
	}).Error
}

func DismissAIProposal(userID uint, id string) error {
	result := db.DB.Model(&model.AIProposal{}).
		Where("id = ? AND user_id = ? AND status IN ?", strings.TrimSpace(id), userID,
			[]string{AIProposalStatusAnalyzing, AIProposalStatusReady, AIProposalStatusFailed}).
		Update("status", AIProposalStatusDismissed)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrAIProposalNotFound
	}
	return nil
}

func ListAIToolRuns(userID uint, limit int) ([]model.AIToolRun, error) {
	if db.DB == nil {
		return nil, gorm.ErrInvalidDB
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var rows []model.AIToolRun
	err := db.DB.Where("user_id = ?", userID).Order("created_at DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func RecordAIToolRun(event ai.ToolRunEvent) {
	if db.DB == nil || strings.TrimSpace(event.Name) == "" {
		return
	}
	arguments := truncateAIValue(SanitizeAIText(event.Arguments), 4000)
	result := truncateAIValue(SanitizeAIText(event.Result), 4000)
	argumentHash := sha256.Sum256([]byte(arguments))
	outcome := AuditOutcomeSuccess
	errorType := ""
	if strings.TrimSpace(event.Error) != "" {
		outcome = AuditOutcomeFailure
		errorType = "tool_error"
		if result == "" {
			result = truncateAIValue(SanitizeAIText(event.Error), 4000)
		}
	}
	row := model.AIToolRun{
		ID:                    uuid.NewString(),
		CreatedAt:             time.Now().UTC(),
		RequestID:             truncateAIValue(event.Meta.RequestID, 64),
		TaskID:                truncateAIValue(event.Meta.TaskID, 96),
		SessionID:             truncateAIValue(event.Meta.SessionID, 160),
		ProposalID:            truncateAIValue(event.Meta.ProposalID, 36),
		UserID:                event.Meta.UserID,
		Username:              truncateAIValue(event.Meta.Username, 128),
		ToolName:              truncateAIValue(event.Name, 96),
		Risk:                  truncateAIValue(string(event.Risk), 16),
		ArgumentsSummary:      arguments,
		ArgumentsHash:         hex.EncodeToString(argumentHash[:]),
		ResultSummary:         result,
		Outcome:               outcome,
		ErrorType:             errorType,
		DurationMilliseconds:  event.Duration.Milliseconds(),
		Provider:              truncateAIValue(event.Meta.Provider, 32),
		Model:                 truncateAIValue(event.Meta.Model, 160),
		ConfirmationRequired:  event.RequiresConfirmation,
		ConfirmationValidated: event.ConfirmationValidated,
	}
	if err := db.DB.Create(&row).Error; err != nil {
		fmt.Printf("AI tool log: failed to record %s: %v\n", event.Name, err)
	}
}

func FingerprintAIInput(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func SanitizeAIText(value string) string {
	value = aiAuthorizationPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = aiSecretAssignmentPattern.ReplaceAllString(value, `${1}${2}[REDACTED]`)
	value = aiSecretJSONPattern.ReplaceAllString(value, `${1}"[REDACTED]"`)
	return aiSecretQueryPattern.ReplaceAllString(value, `${1}[REDACTED]`)
}

func marshalAIJSON(value any) string {
	if value == nil {
		return "{}"
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return SanitizeAIText(string(encoded))
}

func truncateAIValue(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func clampAIConfidence(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
