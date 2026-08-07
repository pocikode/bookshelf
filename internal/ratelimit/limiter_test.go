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
