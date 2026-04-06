package api

import "testing"

func TestSetupEnforcementExemptAllowsDirectoryPicker(t *testing.T) {
	if !setupEnforcementExempt("/api/system/pick-directory") {
		t.Fatal("expected /api/system/pick-directory to bypass setup enforcement")
	}
}
