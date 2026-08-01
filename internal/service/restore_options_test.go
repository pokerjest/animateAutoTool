package service

import (
	"strings"
	"testing"
)

func TestValidateRestoreOptionsRejectsEmptySelection(t *testing.T) {
	err := validateRestoreOptions(BackupDescriptor{}, RestoreOptions{})
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("expected empty restore selection error, got %v", err)
	}
}
