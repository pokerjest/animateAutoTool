package store

import (
	"errors"
	"testing"
)

func TestRetrySQLiteBusyRetriesTransientLock(t *testing.T) {
	attempts := 0
	err := retrySQLiteBusyWithBackoff(func() error {
		attempts++
		if attempts < 3 {
			return errors.New("database is locked (5) (SQLITE_BUSY)")
		}
		return nil
	}, 4, 0)

	if err != nil {
		t.Fatalf("retrySQLiteBusyWithBackoff returned error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRetrySQLiteBusyDoesNotRetryOtherErrors(t *testing.T) {
	attempts := 0
	want := errors.New("constraint failed")
	err := retrySQLiteBusyWithBackoff(func() error {
		attempts++
		return want
	}, 4, 0)

	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}
