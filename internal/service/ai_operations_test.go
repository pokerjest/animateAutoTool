package service

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

func setupAIOperationsTestDB(t *testing.T) {
	t.Helper()
	previous := db.DB
	target, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ai-operations.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	if err := target.AutoMigrate(&model.AIProposal{}, &model.AIToolRun{}); err != nil {
		t.Fatalf("migrate AI operations schema: %v", err)
	}
	db.DB = target
	t.Cleanup(func() {
		sqlDB, _ := target.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		db.DB = previous
	})
}

func readyProposalInput(userID uint) AIProposalInput {
	expiresAt := time.Now().Add(time.Hour)
	return AIProposalInput{
		UserID: userID, Type: AIProposalTypeMetadataMatch, Status: AIProposalStatusReady,
		TargetType: "local_anime", TargetID: "12", Summary: "match",
		Payload:          map[string]any{"local_anime_id": 12, "source_id": 42},
		InputFingerprint: "fingerprint-1", ApplyTool: "apply_metadata_match_proposal",
		Provider: "openai", Model: "gpt-test", ExpiresAt: &expiresAt,
	}
}

func TestAIProposalConfirmationIsUserScopedAndSingleUse(t *testing.T) {
	setupAIOperationsTestDB(t)
	row, err := CreateAIProposal(readyProposalInput(7))
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	token, err := ConfirmAIProposal(7, row.ID, time.Minute)
	if err != nil {
		t.Fatalf("confirm proposal: %v", err)
	}
	if _, err := ConsumeAIConfirmation(8, row.ID, token); !errors.Is(err, ErrAIProposalNotFound) {
		t.Fatalf("expected cross-user read to fail, got %v", err)
	}
	if _, err := ConsumeAIConfirmation(7, row.ID, token); err != nil {
		t.Fatalf("consume confirmation: %v", err)
	}
	if _, err := ConsumeAIConfirmation(7, row.ID, token); !errors.Is(err, ErrAIConfirmationInvalid) {
		t.Fatalf("expected reused token to fail, got %v", err)
	}
}

func TestAIProposalConfirmationBindsApplyToolAndFingerprint(t *testing.T) {
	setupAIOperationsTestDB(t)
	row, err := CreateAIProposal(readyProposalInput(3))
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	token, err := ConfirmAIProposal(3, row.ID, time.Minute)
	if err != nil {
		t.Fatalf("confirm proposal: %v", err)
	}
	if err := db.DB.Model(&model.AIProposal{}).Where("id = ?", row.ID).Update("apply_tool", "run_confirmed_library_scan").Error; err != nil {
		t.Fatalf("mutate proposal for binding check: %v", err)
	}
	if _, err := ConsumeAIConfirmation(3, row.ID, token); !errors.Is(err, ErrAIConfirmationInvalid) {
		t.Fatalf("expected changed apply tool to invalidate token, got %v", err)
	}
}

func TestAIProposalExpiryDismissAndView(t *testing.T) {
	setupAIOperationsTestDB(t)
	input := readyProposalInput(5)
	past := time.Now().Add(-time.Minute)
	input.ExpiresAt = &past
	row, err := CreateAIProposal(input)
	if err != nil {
		t.Fatalf("create expired proposal: %v", err)
	}
	loaded, err := GetAIProposal(5, row.ID)
	if err != nil {
		t.Fatalf("load proposal: %v", err)
	}
	if loaded.Status != AIProposalStatusExpired {
		t.Fatalf("expected expired status, got %q", loaded.Status)
	}
	if _, err := ConfirmAIProposal(5, row.ID, time.Minute); !errors.Is(err, ErrAIProposalExpired) {
		t.Fatalf("expected expired confirmation failure, got %v", err)
	}

	active, err := CreateAIProposal(readyProposalInput(5))
	if err != nil {
		t.Fatalf("create active proposal: %v", err)
	}
	if err := DismissAIProposal(5, active.ID); err != nil {
		t.Fatalf("dismiss proposal: %v", err)
	}
	dismissed, _ := GetAIProposal(5, active.ID)
	if dismissed.Status != AIProposalStatusDismissed {
		t.Fatalf("expected dismissed status, got %q", dismissed.Status)
	}
}

func TestSanitizeAITextRedactsCredentialsWithoutRemovingPaths(t *testing.T) {
	raw := `path=/media/Anime/01.mkv Authorization: Bearer secret-token api_key="abc" password=hunter2 cookie=session123 https://example.test?a=1&token=query-secret`
	sanitized := SanitizeAIText(raw)
	for _, secret := range []string{"secret-token", `"abc"`, "hunter2", "session123", "query-secret"} {
		if strings.Contains(sanitized, secret) {
			t.Fatalf("secret %q leaked in %q", secret, sanitized)
		}
	}
	if !strings.Contains(sanitized, "/media/Anime/01.mkv") {
		t.Fatalf("media path should remain available for diagnosis: %q", sanitized)
	}
}
