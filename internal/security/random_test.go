package security

import (
	"strings"
	"testing"
)

func TestRandomPasswordUsesAlphabetWithoutBiasFallbackIssues(t *testing.T) {
	password, err := RandomPassword(64)
	if err != nil {
		t.Fatalf("RandomPassword returned error: %v", err)
	}
	if len(password) != 64 {
		t.Fatalf("expected password length 64, got %d", len(password))
	}
	for _, ch := range password {
		if !strings.ContainsRune(passwordAlphabet, ch) {
			t.Fatalf("password contains unsupported rune %q", ch)
		}
	}
}
