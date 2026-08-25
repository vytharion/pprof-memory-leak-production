package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestWorkHandler_RespondsWithQueuedID(t *testing.T) {
	t.Cleanup(resetSessions)

	req := httptest.NewRequest(http.MethodPost, "/work?id=abc", strings.NewReader("hi"))
	rec := httptest.NewRecorder()

	newAppMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(rec.Body)
	got := strings.TrimSpace(string(body))
	want := fmt.Sprintf("queued abc (%d bytes)", workDefaultPayloadBytes)
	if got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestWorkHandler_GeneratesIDWhenMissing(t *testing.T) {
	t.Cleanup(resetSessions)

	req := httptest.NewRequest(http.MethodPost, "/work", strings.NewReader(""))
	rec := httptest.NewRecorder()

	newAppMux().ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "queued sess-") {
		t.Fatalf("body = %q, want it to contain %q", string(body), "queued sess-")
	}
}

// TestWorkHandler_CapsActiveSessions is the guardrail for the map half
// of the Step 3 leak: no matter how many requests hit /work, the session
// cache must never grow past maxSessions.
func TestWorkHandler_CapsActiveSessions(t *testing.T) {
	t.Cleanup(resetSessions)
	resetSessions()

	const n = 200
	if n <= maxSessions {
		t.Fatalf("n=%d must exceed maxSessions=%d to exercise eviction", n, maxSessions)
	}
	mux := newAppMux()
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/work?id=b-%d", i), strings.NewReader("x"))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
	}

	if got := activeSessions(); got > maxSessions {
		t.Fatalf("activeSessions() = %d, want <= %d (cache is bounded)", got, maxSessions)
	}
	if got := activeSessions(); got != maxSessions {
		t.Fatalf("activeSessions() = %d, want == %d after %d inserts", got, maxSessions, n)
	}
}

// TestWorkHandler_ReleasesGoroutinesOnEviction is the guardrail for the
// goroutine half of the Step 3 leak: evicted sessions must signal their
// drainEvents goroutine to exit, so runtime.NumGoroutine stays bounded
// by the cache size regardless of request volume.
func TestWorkHandler_ReleasesGoroutinesOnEviction(t *testing.T) {
	t.Cleanup(resetSessions)
	resetSessions()
	runtime.GC()

	before := runtime.NumGoroutine()

	const n = 200
	mux := newAppMux()
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/work?id=g-%d", i), strings.NewReader("x"))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
	}

	ceiling := before + maxSessions*2
	waitForGoroutinesAtMost(ceiling, 2*time.Second)
	after := runtime.NumGoroutine()
	if delta := after - before; delta > maxSessions*2 {
		t.Fatalf("goroutine delta = %d, want <= %d (evictions must free workers)", delta, maxSessions*2)
	}
}

func TestPadPayload_ExpandsShortBodyToMinimum(t *testing.T) {
	got := padPayload([]byte("hi"), 32)
	if len(got) != 32 {
		t.Fatalf("len(padPayload) = %d, want 32", len(got))
	}
	if string(got[:2]) != "hi" {
		t.Fatalf("padPayload prefix = %q, want %q", string(got[:2]), "hi")
	}
}

func TestPadPayload_PreservesLongBody(t *testing.T) {
	in := strings.Repeat("a", 64)
	got := padPayload([]byte(in), 32)
	if len(got) != 64 {
		t.Fatalf("len(padPayload) = %d, want 64", len(got))
	}
}

func waitForGoroutinesAtMost(target int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= target {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}
