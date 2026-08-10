package ratelimit

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }
func TestBoundaries(t *testing.T) {
	c := &fakeClock{time.Unix(0, 0)}
	l := New(c)
	for i := 1; i <= 5; i++ {
		if _, limited := l.Check("1.2.3.4"); limited {
			t.Fatalf("attempt %d limited", i)
		}
		l.RecordFailure("1.2.3.4")
	}
	if retry, limited := l.Check("1.2.3.4"); !limited || RetryAfter(retry) < 1 {
		t.Fatal("attempt 6 not limited")
	}
}

func TestConcurrentAttemptsEvaluateOnlyFive(t *testing.T) {
	c := &fakeClock{time.Unix(0, 0)}
	l := New(c)
	var evaluated atomic.Int32
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.Attempt("198.51.100.9", func() bool { evaluated.Add(1); return false })
		}()
	}
	wg.Wait()
	if got := evaluated.Load(); got != 5 {
		t.Fatalf("evaluated %d passwords, want 5", got)
	}
}
func TestGlobalBoundary(t *testing.T) {
	c := &fakeClock{time.Unix(0, 0)}
	l := New(c)
	for i := 0; i < 20; i++ {
		ip := fmt.Sprintf("10.0.0.%d", i)
		if _, limited := l.Check(ip); limited {
			t.Fatalf("attempt %d limited", i+1)
		}
		l.RecordFailure(ip)
	}
	if _, limited := l.Check("192.0.2.1"); !limited {
		t.Fatal("attempt 21 not limited")
	}
	c.now = c.now.Add(window + time.Second)
	if _, limited := l.Check("192.0.2.1"); limited {
		t.Fatal("global window did not clear")
	}
}

func TestDefaultClockAndSuccessfulAttempt(t *testing.T) {
	l := New(nil)
	if l.clock.Now().IsZero() {
		t.Fatal("default clock returned zero time")
	}
	if success, retry, limited, state := l.Attempt("203.0.113.5", func() bool { return true }); !success || retry != 0 || limited || state.GlobalFailures != 0 {
		t.Fatalf("successful attempt: success=%v retry=%v limited=%v state=%+v", success, retry, limited, state)
	}
}

func TestLimiterMaintenanceBranches(t *testing.T) {
	now := time.Unix(100, 0)
	l := New(&fakeClock{now: now})

	backward := &ipState{tokens: 2, lastRefill: now.Add(time.Hour)}
	l.refill(backward, now)
	if backward.lastRefill != now || backward.tokens != 2 {
		t.Fatalf("backward refill: %+v", backward)
	}

	l.ips["reset"] = &ipState{tokens: 0, lastFailure: now.Add(-time.Hour)}
	reset := l.state("reset", now)
	if reset.tokens != 5 || !reset.lastFailure.IsZero() {
		t.Fatalf("stale state was not reset: %+v", reset)
	}
	l.ips["stale"] = &ipState{lastFailure: now.Add(-2 * time.Hour), lockedUntil: now.Add(-time.Minute)}
	l.prune(now)
	if _, ok := l.ips["stale"]; ok {
		t.Fatal("stale state was not pruned")
	}

	if RetryAfter(0) != 1 || max(time.Second, 2*time.Second) != 2*time.Second || max(2*time.Second, time.Second) != 2*time.Second || min(1, 2) != 1 {
		t.Fatal("helper boundary")
	}
}

func TestLockDurationCaps(t *testing.T) {
	c := &fakeClock{now: time.Unix(0, 0)}
	l := New(c)
	for i := 0; i < 10; i++ {
		l.RecordFailure("198.51.100.20")
	}
	if l.ips["198.51.100.20"].lockedUntil.Sub(c.now) != 15*time.Minute {
		t.Fatalf("lock duration: %v", l.ips["198.51.100.20"].lockedUntil.Sub(c.now))
	}
}
