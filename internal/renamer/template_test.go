package renamer

import "testing"

func TestFormatTemplateUsesJellyfinPlexDefault(t *testing.T) {
	t.Parallel()

	got, err := FormatTemplate("", TemplateData{
		Title:    `My: Anime?`,
		Season:   "S2",
		Episode:  "3",
		Ext:      ".MKV",
		Original: "release",
	})
	if err != nil {
		t.Fatalf("FormatTemplate: %v", err)
	}
	want := "My Anime - S02E03.mkv"
	if got != want {
		t.Fatalf("FormatTemplate = %q, want %q", got, want)
	}
}

func TestFormatTemplateSupportsCustomTokensAndDecimalEpisode(t *testing.T) {
	t.Parallel()

	got, err := FormatTemplate("Specials/{title} ({year}) - S{season}E{episode}{ext}", TemplateData{
		Title:   "Example",
		Season:  "0",
		Episode: "12.5",
		Year:    "2026",
		Ext:     "mp4",
	})
	if err != nil {
		t.Fatalf("FormatTemplate: %v", err)
	}
	if got != "Specials/Example (2026) - S00E12.5.mp4" {
		t.Fatalf("unexpected custom path %q", got)
	}
}

func TestFormatTemplateRejectsUnsafeAndUnknownTemplates(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{"../{title}{ext}", "C:/{title}{ext}", "{title}/{unknown}{ext}"} {
		if _, err := FormatTemplate(pattern, TemplateData{Title: "Example", Season: "1", Episode: "1", Ext: ".mkv"}); err == nil {
			t.Fatalf("expected %q to be rejected", pattern)
		}
	}
}

func TestMediaTemplatesRequireRecognizableSingleSegmentNames(t *testing.T) {
	t.Parallel()

	if err := ValidateSeriesTemplate("{title} ({year})"); err != nil {
		t.Fatalf("valid series template: %v", err)
	}
	if err := ValidateSeriesTemplate("Season {season}/{title}"); err == nil {
		t.Fatal("expected nested series template to be rejected")
	}
	if err := ValidateEpisodeTemplate("{title} - S{season}E{episode}{ext}"); err != nil {
		t.Fatalf("valid episode template: %v", err)
	}
	if err := ValidateEpisodeTemplate("{title} - {episode}{ext}"); err == nil {
		t.Fatal("expected episode template without season token to be rejected")
	}
}

func TestPreserveVersionSuffixOnlyAddsMissingReleaseVersion(t *testing.T) {
	t.Parallel()

	if got := PreserveVersionSuffix("Example - S01E01.mp4", "V2"); got != "Example - S01E01 v2.mp4" {
		t.Fatalf("PreserveVersionSuffix = %q", got)
	}
	if got := PreserveVersionSuffix("Example - S01E01 [V2].mp4", "v2"); got != "Example - S01E01 [V2].mp4" {
		t.Fatalf("existing version was duplicated: %q", got)
	}
	if got := PreserveVersionSuffix("Example - S01E01.mp4", ""); got != "Example - S01E01.mp4" {
		t.Fatalf("empty version changed filename: %q", got)
	}
}
