package runtimejournal

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/pokerjest/animateAutoTool/internal/config"
)

const (
	sessionFilePrefix   = "session-"
	operationFilePrefix = "operation-"
	journalFileSuffix   = ".json"

	OperationLocalLibraryScan = "local-library-scan"
	OperationMetadataEnrich   = "metadata-enrichment"
	OperationSubscriptionSync = "subscription-reconciliation"
	OperationStartupRecovery  = "startup-crash-recovery"
)

var ErrRecoveryBlocked = errors.New("数据库完整性检查失败，数据写入已停用")
var ErrRecoveryInProgress = errors.New("异常退出恢复正在运行，请等待恢复完成")

type Operation struct {
	Name       string    `json:"name"`
	StartedAt  time.Time `json:"started_at"`
	RecoveryOf []string  `json:"recovery_of,omitempty"`
}

type Session struct {
	ID         string      `json:"id"`
	PID        int         `json:"pid"`
	AppVersion string      `json:"app_version"`
	StartedAt  time.Time   `json:"started_at"`
	Operations []Operation `json:"operations,omitempty"`
}

type StartResult struct {
	Previous      *Session
	PreviousError string
}

var (
	journalMu       sync.Mutex
	currentSession  *Session
	operationRefs   = make(map[string]int)
	recoveryActive  atomic.Bool
	recoveryBlocked atomic.Bool
	nowUTC          = func() time.Time { return time.Now().UTC() }
	journalDir      = func() string {
		if strings.TrimSpace(config.AppPaths.DataDir) == "" {
			return ""
		}
		return config.DataPath("runtime")
	}
)

func SetRecoveryInProgress(active bool) {
	recoveryActive.Store(active)
	if active {
		recoveryBlocked.Store(false)
	}
}

func RecoveryInProgress() bool {
	return recoveryActive.Load()
}

func SetRecoveryBlocked(blocked bool) {
	recoveryBlocked.Store(blocked)
	if blocked {
		recoveryActive.Store(false)
	}
}

func RecoveryBlocked() bool {
	return recoveryBlocked.Load()
}

func BeginSession(appVersion string) (StartResult, error) {
	journalMu.Lock()
	defer journalMu.Unlock()

	dir := journalDir()
	if dir == "" {
		return StartResult{}, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return StartResult{}, err
	}

	result := loadPreviousSessionLocked(dir)
	startedAt := nowUTC()
	session := &Session{
		ID:         startedAt.Format("20060102T150405.000000000Z") + "-" + uuid.NewString(),
		PID:        os.Getpid(),
		AppVersion: strings.TrimSpace(appVersion),
		StartedAt:  startedAt,
	}
	if err := writeJSONFile(sessionPath(dir, session.ID), session); err != nil {
		return result, err
	}
	currentSession = session
	operationRefs = make(map[string]int)
	cleanupOldJournalsLocked(dir, session.ID)
	return result, nil
}

func EndSession() error {
	journalMu.Lock()
	defer journalMu.Unlock()

	if currentSession == nil {
		return nil
	}
	dir := journalDir()
	if dir == "" {
		currentSession = nil
		operationRefs = make(map[string]int)
		return nil
	}
	removeOperationMarkersLocked(dir, currentSession.ID)
	err := os.Remove(sessionPath(dir, currentSession.ID))
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	currentSession = nil
	operationRefs = make(map[string]int)
	syncDirectory(dir)
	return err
}

func BeginOperation(name string) error {
	return beginOperation(name, nil)
}

func BeginRecoveryOperation(operations []string) error {
	return beginOperation(OperationStartupRecovery, operations)
}

func beginOperation(name string, recoveryOf []string) error {
	journalMu.Lock()
	defer journalMu.Unlock()

	name = strings.TrimSpace(name)
	if currentSession == nil || name == "" {
		return nil
	}
	operationRefs[name]++
	if operationRefs[name] > 1 {
		return nil
	}
	dir := journalDir()
	if dir == "" {
		return nil
	}
	operation := &Operation{
		Name:       name,
		StartedAt:  nowUTC(),
		RecoveryOf: append([]string(nil), recoveryOf...),
	}
	if err := writeJSONFile(operationPath(dir, currentSession.ID, name), operation); err != nil {
		delete(operationRefs, name)
		return err
	}
	currentSession.Operations = append(currentSession.Operations, *operation)
	return nil
}

func EndOperation(name string) error {
	journalMu.Lock()
	defer journalMu.Unlock()

	if currentSession == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if operationRefs[name] > 1 {
		operationRefs[name]--
		return nil
	}
	delete(operationRefs, name)
	dir := journalDir()
	if dir == "" {
		removeCurrentOperationLocked(name)
		return nil
	}
	err := os.Remove(operationPath(dir, currentSession.ID, name))
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	removeCurrentOperationLocked(name)
	syncDirectory(dir)
	return err
}

func loadPreviousSessionLocked(dir string) StartResult {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return StartResult{PreviousError: err.Error()}
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), sessionFilePrefix) ||
			!strings.HasSuffix(entry.Name(), journalFileSuffix) {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	if len(names) == 0 {
		return StartResult{}
	}

	data, err := os.ReadFile(filepath.Join(dir, names[0])) //nolint:gosec // names are filtered session markers from the journal directory.
	if err != nil {
		return StartResult{PreviousError: err.Error()}
	}
	var previous Session
	if err := json.Unmarshal(data, &previous); err != nil {
		return StartResult{PreviousError: fmt.Sprintf("decode %s: %v", names[0], err)}
	}
	if strings.TrimSpace(previous.ID) == "" {
		return StartResult{PreviousError: fmt.Sprintf("decode %s: session id is empty", names[0])}
	}
	operationNames := operationMarkerNamesLocked(dir, previous.ID)
	var operationErrors []string
	for _, name := range operationNames {
		operationData, readErr := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // name is filtered to an operation marker in the journal directory.
		if readErr != nil {
			operationErrors = append(operationErrors, fmt.Sprintf("read %s: %v", name, readErr))
			continue
		}
		var operation Operation
		if decodeErr := json.Unmarshal(operationData, &operation); decodeErr != nil {
			operationErrors = append(operationErrors, fmt.Sprintf("decode %s: %v", name, decodeErr))
			continue
		}
		if strings.TrimSpace(operation.Name) == "" {
			operationErrors = append(operationErrors, fmt.Sprintf("decode %s: operation name is empty", name))
			continue
		}
		previous.Operations = append(previous.Operations, operation)
	}
	sort.Slice(previous.Operations, func(i, j int) bool {
		return previous.Operations[i].StartedAt.Before(previous.Operations[j].StartedAt)
	})
	if len(operationErrors) > 0 {
		return StartResult{Previous: &previous, PreviousError: strings.Join(operationErrors, "; ")}
	}
	return StartResult{Previous: &previous}
}

func cleanupOldJournalsLocked(dir, keepSessionID string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == filepath.Base(sessionPath(dir, keepSessionID)) ||
			name == operationFilePrefix+filepath.Base(keepSessionID)+journalFileSuffix ||
			strings.HasPrefix(name, operationFilePrefix+filepath.Base(keepSessionID)+"-") {
			continue
		}
		if (strings.HasPrefix(name, sessionFilePrefix) || strings.HasPrefix(name, operationFilePrefix)) &&
			strings.HasSuffix(name, journalFileSuffix) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
	syncDirectory(dir)
}

func sessionPath(dir, id string) string {
	return filepath.Join(dir, sessionFilePrefix+filepath.Base(id)+journalFileSuffix)
}

func operationPath(dir, id, name string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(name)))
	return filepath.Join(
		dir,
		fmt.Sprintf("%s%s-%x%s", operationFilePrefix, filepath.Base(id), sum[:8], journalFileSuffix),
	)
}

func writeJSONFile(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	file, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	syncDirectory(filepath.Dir(path))
	return nil
}

func removeCurrentOperationLocked(name string) {
	operations := currentSession.Operations[:0]
	for _, operation := range currentSession.Operations {
		if operation.Name != name {
			operations = append(operations, operation)
		}
	}
	currentSession.Operations = operations
}

func operationMarkerNamesLocked(dir, sessionID string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	legacyName := operationFilePrefix + filepath.Base(sessionID) + journalFileSuffix
	prefix := operationFilePrefix + filepath.Base(sessionID) + "-"
	names := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == legacyName || (strings.HasPrefix(name, prefix) && strings.HasSuffix(name, journalFileSuffix)) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func removeOperationMarkersLocked(dir, sessionID string) {
	for _, name := range operationMarkerNamesLocked(dir, sessionID) {
		_ = os.Remove(filepath.Join(dir, name))
	}
}

func syncDirectory(dir string) {
	file, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return
	}
	_ = file.Sync()
	_ = file.Close()
}
