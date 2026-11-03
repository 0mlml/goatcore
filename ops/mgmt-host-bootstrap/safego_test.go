// safego_test.go — coverage for the safeGo panic-recovering wrapper.
//
// Asserts both that the happy path runs fn and that a panic is captured
// (we override exitFn so the test binary stays alive).

package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSafeGo_HappyPath verifies fn is invoked in a separate goroutine.
func TestSafeGo_HappyPath(t *testing.T) {
	var ran atomic.Bool
	done := make(chan struct{})

	safeGo("happy", func() {
		ran.Store(true)
		close(done)
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("safeGo did not run fn within 2s")
	}
	if !ran.Load() {
		t.Fatal("ran flag was not set")
	}
}

// TestSafeGo_PanicCapturedAndExits verifies a panic in fn is recovered,
// the structured log fires, and exitFn(1) is called. Override exitFn so
// the test binary stays alive; the recovering goroutine returns naturally
// after exitFn returns, which keeps goleak.VerifyTestMain happy.
func TestSafeGo_PanicCapturedAndExits(t *testing.T) {
	var (
		mu       sync.Mutex
		exitCode int
		called   bool
	)
	done := make(chan struct{})

	prev := exitFn
	t.Cleanup(func() { exitFn = prev })
	exitFn = func(code int) {
		mu.Lock()
		exitCode = code
		called = true
		mu.Unlock()
		close(done)
	}

	safeGo("panic-test", func() {
		panic("intentional test panic")
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("safeGo did not invoke exitFn within 2s")
	}

	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Fatal("exitFn was not called")
	}
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1", exitCode)
	}
}
