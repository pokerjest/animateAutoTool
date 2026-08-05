package updater

import (
	"strings"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
)

func compatibleStableManifest() ReleaseManifest {
	return ReleaseManifest{
		FormatVersion:            1,
		Version:                  "v1.0.0",
		Channel:                  string(ReleaseChannelStable),
		SchemaVersion:            "011",
		DatabaseFormat:           db.DatabaseFormat,
		SchemaFormat:             db.SchemaFormat,
		MinUpgradeFrom:           "0.9.0",
		MinReadableSchema:        "001",
		MaxReadableSchema:        "011",
		SwitchableFromPrerelease: true,
	}
}

func TestReleaseManifestAllowsCompatibleBetaToStableSwitch(t *testing.T) {
	manifest := compatibleStableManifest()
	allowed, reason := manifest.allows("v1.1.0-beta.2", "011_schema_migration_sequence", "v1.0.0")
	if !allowed {
		t.Fatalf("expected compatible beta to stable switch, got reason %q", reason)
	}
}

func TestReleaseManifestRejectsBetaToStableWithUnreadableSchema(t *testing.T) {
	manifest := compatibleStableManifest()
	manifest.MaxReadableSchema = "010"
	allowed, reason := manifest.allows("v1.1.0-beta.2", "011_schema_migration_sequence", "v1.0.0")
	if allowed || !strings.Contains(reason, "schema") {
		t.Fatalf("expected schema rejection, got allowed=%v reason=%q", allowed, reason)
	}
}

func TestReleaseManifestRejectsStableDowngrade(t *testing.T) {
	manifest := compatibleStableManifest()
	allowed, reason := manifest.allows("v1.1.0", "011_schema_migration_sequence", "v1.0.0")
	if allowed || !strings.Contains(reason, "稳定版降级") {
		t.Fatalf("expected stable downgrade rejection, got allowed=%v reason=%q", allowed, reason)
	}
}

func TestReleaseManifestEnforcesMinimumUpgradeVersion(t *testing.T) {
	manifest := compatibleStableManifest()
	allowed, reason := manifest.allows("v0.8.9", "010_ai_tool_operations", "v1.0.0")
	if allowed || !strings.Contains(reason, "0.9.0") {
		t.Fatalf("expected minimum upgrade rejection, got allowed=%v reason=%q", allowed, reason)
	}
}

func TestReleaseManifestRejectsPrereleaseAsRollbackTarget(t *testing.T) {
	manifest := compatibleStableManifest()
	manifest.Version = "v1.0.0-beta.1"
	manifest.Channel = string(ReleaseChannelBeta)
	allowed, reason := manifest.allows("v1.1.0-beta.2", "011", "v1.0.0-beta.1")
	if allowed || !strings.Contains(reason, "稳定版") {
		t.Fatalf("expected beta-to-beta downgrade rejection, got allowed=%v reason=%q", allowed, reason)
	}
}

func TestReleaseManifestValidationMatchesChannelAndSchemaRange(t *testing.T) {
	manifest := compatibleStableManifest()
	manifest.Channel = string(ReleaseChannelBeta)
	if err := manifest.validForRelease("v1.0.0"); err == nil {
		t.Fatal("expected stable tag with beta channel to be rejected")
	}

	manifest = compatibleStableManifest()
	manifest.MinReadableSchema = "012"
	manifest.MaxReadableSchema = "011"
	if err := manifest.validForRelease("v1.0.0"); err == nil {
		t.Fatal("expected reversed schema range to be rejected")
	}
}

func TestReleaseManifestRejectsUnsupportedDatabaseOrSchemaFormat(t *testing.T) {
	manifest := compatibleStableManifest()
	manifest.DatabaseFormat++
	if err := manifest.validForRelease("v1.0.0"); err == nil || !strings.Contains(err.Error(), "format") {
		t.Fatalf("expected unsupported database format rejection, got %v", err)
	}

	manifest = compatibleStableManifest()
	manifest.SchemaFormat++
	allowed, reason := manifest.allows("v0.9.9", "011", "v1.0.0")
	if allowed || !strings.Contains(reason, "format") {
		t.Fatalf("expected unsupported schema format rejection, got allowed=%v reason=%q", allowed, reason)
	}
}
