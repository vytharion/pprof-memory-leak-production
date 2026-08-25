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

func TestWorkHandler_LeaksSessionEntriesUnbounded(t *testing.T) {
	t.Cleanup(resetSessions)
	resetSessions()

	const n = 25
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/work?id=leak-%d", i), strings.NewReader("x"))
		rec := httptest.NewRecorder()
		newAppMux().ServeHTTP(rec, req)
	}

	if got := activeSessions(); got != n {
		t.Fatalf("activeSessions() = %d, want %d (nothing should be evicted)", got, n)
	}
}

func TestWorkHandler_LeaksOneGoroutinePerRequest(t *testing.T) {
	t.Cleanup(resetSessions)
	resetSessions()
	runtime.GC()

	before := runtime.NumGoroutine()

	const n = 20
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/work?id=g-%d", i), strings.NewReader("x"))
		rec := httptest.NewRecorder()
		newAppMux().ServeHTTP(rec, req)
	}

	waitForGoroutines(before+n, 500*time.Millisecond)
	after := runtime.NumGoroutine()

	if delta := after - before; delta < n {
		t.Fatalf("goroutine delta = %d, want at least %d (each request should park one worker)", delta, n)
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

func waitForGoroutines(target int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() >= target {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}
