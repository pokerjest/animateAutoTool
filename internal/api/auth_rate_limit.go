package api

import (
	"math"
	"sync"
	"time"
)

const (
	loginBackoffThreshold = 3
	loginBackoffBase      = 2 * time.Second
	loginBackoffMax       = 5 * time.Minute
	loginThrottleTTL      = 24 * time.Hour
)

type loginThrottleState struct {
	Failures    int
	LockedUntil time.Time
	LastSeenAt  time.Time
}

type loginThrottleStore struct {
	mu    sync.Mutex
	byIP  map[string]loginThrottleState
	nowFn func() time.Time
}

var globalLoginThrottle = &loginThrottleStore{
	byIP:  make(map[string]loginThrottleState),
	nowFn: time.Now,
}

func resetLoginThrottleState() {
	globalLoginThrottle.mu.Lock()
	defer globalLoginThrottle.mu.Unlock()

	globalLoginThrottle.byIP = make(map[string]loginThrottleState)
	globalLoginThrottle.nowFn = time.Now
}

func setLoginThrottleTimeNow(fn func() time.Time) {
	globalLoginThrottle.mu.Lock()
	defer globalLoginThrottle.mu.Unlock()

	if fn == nil {
		globalLoginThrottle.nowFn = time.Now
		return
	}
	globalLoginThrottle.nowFn = fn
}

func currentLoginThrottleState(ip string) loginThrottleState {
	globalLoginThrottle.mu.Lock()
	defer globalLoginThrottle.mu.Unlock()

	return globalLoginThrottle.currentStateLocked(ip)
}

func checkLoginThrottle(ip string) (time.Duration, bool) {
	globalLoginThrottle.mu.Lock()
	defer globalLoginThrottle.mu.Unlock()

	now := globalLoginThrottle.nowFn()
	globalLoginThrottle.pruneLocked(now)
	state := globalLoginThrottle.currentStateLocked(ip)
	if state.LockedUntil.After(now) {
		return state.LockedUntil.Sub(now), true
	}

	return 0, false
}

func registerFailedLoginAttempt(ip string) time.Duration {
	globalLoginThrottle.mu.Lock()
	defer globalLoginThrottle.mu.Unlock()

	now := globalLoginThrottle.nowFn()
	globalLoginThrottle.pruneLocked(now)

	state := globalLoginThrottle.currentStateLocked(ip)
	state.Failures++
	state.LastSeenAt = now

	backoff := loginBackoffForFailures(state.Failures)
	if backoff > 0 {
		state.LockedUntil = now.Add(backoff)
	} else {
		state.LockedUntil = time.Time{}
	}

	globalLoginThrottle.byIP[ip] = state
	return backoff
}

func clearFailedLoginAttempts(ip string) {
	globalLoginThrottle.mu.Lock()
	defer globalLoginThrottle.mu.Unlock()

	delete(globalLoginThrottle.byIP, normalizeLoginThrottleIP(ip))
}

func loginBackoffForFailures(failures int) time.Duration {
	if failures < loginBackoffThreshold {
		return 0
	}

	exp := failures - loginBackoffThreshold
	multiplier := math.Pow(2, float64(exp))
	backoff := time.Duration(multiplier) * loginBackoffBase
	if backoff > loginBackoffMax {
		return loginBackoffMax
	}
	return backoff
}

func (s *loginThrottleStore) currentStateLocked(ip string) loginThrottleState {
	return s.byIP[normalizeLoginThrottleIP(ip)]
}

func (s *loginThrottleStore) pruneLocked(now time.Time) {
	for ip, state := range s.byIP {
		if now.Sub(state.LastSeenAt) > loginThrottleTTL && !state.LockedUntil.After(now) {
			delete(s.byIP, ip)
		}
	}
}

func normalizeLoginThrottleIP(ip string) string {
	if ip == "" {
		return "unknown"
	}
	return ip
}
