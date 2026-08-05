package authsession

import (
	"sync/atomic"
)

var (
	generation        atomic.Uint64
	requireGeneration atomic.Bool
)

func init() {
	generation.Store(1)
}

// Current returns the in-process authentication generation embedded in newly
// issued cookies.
func Current() uint64 {
	return generation.Load()
}

// Set restores a generation persisted by the database bootstrap path.
func Set(value uint64, required bool) {
	if value == 0 {
		value = 1
	}
	generation.Store(value)
	requireGeneration.Store(required)
}

// InvalidateAll immediately makes every existing session cookie unusable. The
// requirement flag also rejects legacy cookies that predate generation
// support, so a restore cannot accidentally map an old user_id to a different
// restored account.
func InvalidateAll() {
	generation.Add(1)
	requireGeneration.Store(true)
}

func Required() bool {
	return requireGeneration.Load()
}

func Valid(value any) bool {
	generationValue, ok := value.(uint64)
	if !ok {
		switch converted := value.(type) {
		case int:
			generationValue = uint64(converted)
			ok = true
		case int64:
			generationValue = uint64(converted)
			ok = true
		case float64:
			generationValue = uint64(converted)
			ok = true
		}
	}
	return ok && generationValue == Current()
}
