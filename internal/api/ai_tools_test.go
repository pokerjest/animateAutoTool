package api

import (
	"strings"
	"testing"
)

func TestGlobalAIRegistryExposesOnlyReadAndProposalTools(t *testing.T) {
	var names []string
	for _, definition := range GlobalAIRegistry.GetToolDefinitions() {
		names = append(names, definition.Function.Name)
	}
	joined := strings.Join(names, ",")
	if strings.Contains(joined, "scan_local_library") {
		t.Fatalf("legacy direct scan tool leaked into model definitions: %s", joined)
	}
	for _, expected := range []string{"get_health_report", "get_filename_context", "preview_filename_resolution", "propose_library_scan"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected model-visible tool %q in %s", expected, joined)
		}
	}
}
