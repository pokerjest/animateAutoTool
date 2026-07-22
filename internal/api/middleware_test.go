package api

import "testing"

func TestSetupEnforcementExemptAllowsDirectoryPicker(t *testing.T) {
	paths := []string{
		"/api/system/pick-directory",
		"/api/v1/system/pick-directory",
		"/api/setup/bootstrap",
		"/api/v1/setup/bootstrap",
		"/api/v1/setup/readiness",
	}
	for _, path := range paths {
		if !setupEnforcementExempt(path) {
			t.Fatalf("expected %s to bypass setup enforcement", path)
		}
	}
}
