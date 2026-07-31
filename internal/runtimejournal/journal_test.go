package runtimejournal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionJournalDetectsInterruptedOperation(t *testing.T) {
	dir := t.TempDir()
	originalDir := journalDir
	originalNow := nowUTC
	originalSession := currentSession
	originalRefs := operationRefs
	journalDir = func() string { return dir }
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	nowUTC = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	currentSession = nil
	operationRefs = make(map[string]int)
	t.Cleanup(func() {
		journalDir = originalDir
		nowUTC = originalNow
		currentSession = originalSession
		operationRefs = originalRefs
	})

	first, err := BeginSession("v1")
	if err != nil {
		t.Fatalf("begin first session: %v", err)
	}
	if first.Previous != nil {
		t.Fatalf("unexpected previous session: %+v", first.Previous)
	}
	if err := BeginOperation("local-library-scan"); err != nil {
		t.Fatalf("begin operation: %v", err)
	}

	// Simulate a process restart: the old marker remains because EndSession and
	// EndOperation were never called.
	currentSession = nil
	operationRefs = make(map[string]int)
	second, err := BeginSession("v2")
	if err != nil {
		t.Fatalf("begin second session: %v", err)
	}
	if second.Previous == nil || second.Previous.AppVersion != "v1" {
		t.Fatalf("missing previous session: %+v", second.Previous)
	}
	if len(second.Previous.Operations) != 1 || second.Previous.Operations[0].Name != "local-library-scan" {
		t.Fatalf("missing interrupted operation: %+v", second.Previous.Operations)
	}

	if err := EndSession(); err != nil {
		t.Fatalf("end second session: %v", err)
	}
	entries, err := os.ReadDir(filepath.Clean(dir))
	if err != nil {
		t.Fatalf("read journal dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected journal cleanup, got %v", entries)
	}
}

func TestSessionJournalKeepsConcurrentOperations(t *testing.T) {
	dir := t.TempDir()
	originalDir := journalDir
	originalSession := currentSession
	originalRefs := operationRefs
	journalDir = func() string { return dir }
	currentSession = nil
	operationRefs = make(map[string]int)
	t.Cleanup(func() {
		journalDir = originalDir
		currentSession = originalSession
		operationRefs = originalRefs
	})

	if _, err := BeginSession("v1"); err != nil {
		t.Fatalf("begin session: %v", err)
	}
	if err := BeginOperation(OperationLocalLibraryScan); err != nil {
		t.Fatalf("begin scan: %v", err)
	}
	if err := BeginOperation(OperationSubscriptionSync); err != nil {
		t.Fatalf("begin subscription sync: %v", err)
	}
	if err := EndOperation(OperationLocalLibraryScan); err != nil {
		t.Fatalf("end scan: %v", err)
	}

	currentSession = nil
	operationRefs = make(map[string]int)
	result, err := BeginSession("v2")
	if err != nil {
		t.Fatalf("restart session: %v", err)
	}
	if result.Previous == nil || len(result.Previous.Operations) != 1 {
		t.Fatalf("unexpected previous operations: %+v", result.Previous)
	}
	if result.Previous.Operations[0].Name != OperationSubscriptionSync {
		t.Fatalf("expected subscription operation to remain, got %+v", result.Previous.Operations)
	}
}

func TestRecoveryOperationPersistsInterruptedStages(t *testing.T) {
	dir := t.TempDir()
	originalDir := journalDir
	originalSession := currentSession
	originalRefs := operationRefs
	journalDir = func() string { return dir }
	currentSession = nil
	operationRefs = make(map[string]int)
	t.Cleanup(func() {
		journalDir = originalDir
		currentSession = originalSession
		operationRefs = originalRefs
	})

	if _, err := BeginSession("v1"); err != nil {
		t.Fatalf("begin session: %v", err)
	}
	stages := []string{OperationLocalLibraryScan, OperationSubscriptionSync}
	if err := BeginRecoveryOperation(stages); err != nil {
		t.Fatalf("begin recovery operation: %v", err)
	}

	currentSession = nil
	operationRefs = make(map[string]int)
	result, err := BeginSession("v2")
	if err != nil {
		t.Fatalf("restart session: %v", err)
	}
	if result.Previous == nil || len(result.Previous.Operations) != 1 {
		t.Fatalf("unexpected previous recovery operation: %+v", result.Previous)
	}
	operation := result.Previous.Operations[0]
	if operation.Name != OperationStartupRecovery {
		t.Fatalf("expected startup recovery marker, got %+v", operation)
	}
	if len(operation.RecoveryOf) != len(stages) {
		t.Fatalf("expected recovery stages %v, got %v", stages, operation.RecoveryOf)
	}
}
