package store

import (
	"strings"
	"time"
)

const (
	sqliteBusyMaxAttempts = 4
	sqliteBusyBaseDelay   = 50 * time.Millisecond
)

func retrySQLiteBusy(operation func() error) error {
	return retrySQLiteBusyWithBackoff(operation, sqliteBusyMaxAttempts, sqliteBusyBaseDelay)
}

func retrySQLiteBusyWithBackoff(operation func() error, maxAttempts int, baseDelay time.Duration) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = operation()
		if err == nil || !isSQLiteBusyError(err) {
			return err
		}
		if attempt+1 < maxAttempts && baseDelay > 0 {
			time.Sleep(baseDelay * time.Duration(1<<attempt))
		}
	}
	return err
}

func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "sqlite_busy") ||
		strings.Contains(message, "database is locked") ||
		strings.Contains(message, "database table is locked")
}
