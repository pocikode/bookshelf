package ratelimit

import (
	"math"
	"sync"
	"time"
)

const window = 15 * time.Minute

type Clock interface{ Now() time.Time }
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type ipState struct {
	tokens                               float64
	lastRefill, lastFailure, lockedUntil time.Time
	lockLevel                            int
}

type State struct {
	IPFailures     int
	LockLevel      int
	GlobalFailures int
}

// Attempt holds the limiter lock across the password comparison callback. This
// prevents concurrent requests from all passing a separate check before any of
// their failures have been recorded.
func (l *Limiter) Attempt(ip string, evaluate func() bool) (success bool, retry time.Duration, limited bool, state State) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	l.prune(now)
	retry = l.retryLocked(ip, now)
	if retry > 0 {
		return false, retry, true, State{GlobalFailures: len(l.global)}
	}
	if evaluate() {
		return true, 0, false, State{GlobalFailures: len(l.global)}
	}
	state = l.recordFailureLocked(ip, now)
	return false, 0, false, state
}

type Limiter struct {
	mu     sync.Mutex
	clock  Clock
	ips    map[string]*ipState
	global []time.Time
}

func New(clock Clock) *Limiter {
	if clock == nil {
		clock = realClock{}
	}
	return &Limiter{clock: clock, ips: make(map[string]*ipState)}
}

func (l *Limiter) Check(ip string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	l.prune(now)
	retry := l.retryLocked(ip, now)
	return retry, retry > 0
}

func (l *Limiter) retryLocked(ip string, now time.Time) time.Duration {
	retry := time.Duration(0)
	if len(l.global) >= 20 {
		retry = max(retry, l.global[0].Add(window).Sub(now))
	}
	s := l.state(ip, now)
	l.refill(s, now)
	if now.Before(s.lockedUntil) {
		retry = max(retry, s.lockedUntil.Sub(now))
	}
	if s.tokens < 1 {
		retry = max(retry, time.Duration(math.Ceil((1-s.tokens)*float64(3*time.Minute))))
	}
	return retry
}

func (l *Limiter) RecordFailure(ip string) State {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	l.prune(now)
	return l.recordFailureLocked(ip, now)
}

func (l *Limiter) recordFailureLocked(ip string, now time.Time) State {
	s := l.state(ip, now)
	l.refill(s, now)
	if s.tokens >= 1 {
		s.tokens--
	}
	s.lastFailure = now
	l.global = append(l.global, now)
	if s.tokens < 1 {
		s.lockLevel++
		d := time.Minute * time.Duration(1<<min(s.lockLevel-1, 4))
		if d > 15*time.Minute {
			d = 15 * time.Minute
		}
		s.lockedUntil = now.Add(d)
	}
	return State{IPFailures: int(math.Ceil(5 - s.tokens)), LockLevel: s.lockLevel, GlobalFailures: len(l.global)}
}

func (l *Limiter) state(ip string, now time.Time) *ipState {
	s := l.ips[ip]
	if s == nil {
		s = &ipState{tokens: 5, lastRefill: now}
		l.ips[ip] = s
	}
	if !s.lastFailure.IsZero() && now.Sub(s.lastFailure) >= time.Hour {
		*s = ipState{tokens: 5, lastRefill: now}
	}
	return s
}
func (l *Limiter) refill(s *ipState, now time.Time) {
	if now.Before(s.lastRefill) {
		s.lastRefill = now
		return
	}
	elapsed := now.Sub(s.lastRefill)
	s.tokens = math.Min(5, s.tokens+float64(elapsed)/float64(3*time.Minute))
	s.lastRefill = now
}
func (l *Limiter) prune(now time.Time) {
	cut := now.Add(-window)
	i := 0
	for i < len(l.global) && !l.global[i].After(cut) {
		i++
	}
	if i > 0 {
		l.global = append([]time.Time(nil), l.global[i:]...)
	}
	for ip, s := range l.ips {
		if !s.lastFailure.IsZero() && now.Sub(s.lastFailure) > time.Hour && !now.Before(s.lockedUntil) {
			delete(l.ips, ip)
		}
	}
}
func RetryAfter(d time.Duration) int {
	n := int(math.Ceil(d.Seconds()))
	if n < 1 {
		return 1
	}
	return n
}
func max(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
