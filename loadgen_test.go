package main

import (
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func TestRunLoad_CompletesEveryRequest(t *testing.T) {
	t.Cleanup(resetSessions)
	srv := httptest.NewServer(newAppMux())
	t.Cleanup(srv.Close)

	res := runLoad(loadConfig{
		targetURL:   srv.URL + "/work",
		concurrency: 4,
		requests:    32,
		payload:     []byte("x"),
	})

	if res.sent != 32 {
		t.Fatalf("sent = %d, want 32", res.sent)
	}
	if res.ok != 32 {
		t.Fatalf("ok = %d, want 32 (firstErr=%v)", res.ok, res.firstErr)
	}
	if res.failed != 0 {
		t.Fatalf("failed = %d, want 0 (firstErr=%v)", res.failed, res.firstErr)
	}
	if res.totalDur <= 0 {
		t.Fatalf("totalDur = %v, want > 0", res.totalDur)
	}
}

func TestRunLoad_AppliesConcurrencyDefaults(t *testing.T) {
	t.Cleanup(resetSessions)
	srv := httptest.NewServer(newAppMux())
	t.Cleanup(srv.Close)

	res := runLoad(loadConfig{
		targetURL:   srv.URL + "/work",
		concurrency: 0,
		requests:    0,
		payload:     []byte("x"),
	})

	if res.sent != 1 {
		t.Fatalf("sent = %d, want 1 (defaults should clamp requests to 1)", res.sent)
	}
	if res.ok != 1 {
		t.Fatalf("ok = %d, want 1 (firstErr=%v)", res.ok, res.firstErr)
	}
}

func TestRunLoad_KeepsSessionsBoundedUnderConcurrentLoad(t *testing.T) {
	t.Cleanup(resetSessions)
	resetSessions()
	srv := httptest.NewServer(newAppMux())
	t.Cleanup(srv.Close)

	const requests = 200
	if requests <= maxSessions {
		t.Fatalf("requests=%d must exceed maxSessions=%d to exercise eviction", requests, maxSessions)
	}
	res := runLoad(loadConfig{
		targetURL:   srv.URL + "/work",
		concurrency: 8,
		requests:    requests,
		payload:     []byte("payload"),
	})

	if res.failed != 0 {
		t.Fatalf("failed = %d, want 0 (firstErr=%v)", res.failed, res.firstErr)
	}
	if got := activeSessions(); got > maxSessions {
		t.Fatalf("activeSessions() = %d, want <= %d after sustained load", got, maxSessions)
	}
}

func TestRunLoad_KeepsGoroutinesBoundedUnderConcurrentLoad(t *testing.T) {
	t.Cleanup(resetSessions)
	resetSessions()
	runtime.GC()
	srv := httptest.NewServer(newAppMux())
	t.Cleanup(srv.Close)

	before := runtime.NumGoroutine()

	const requests = 200
	runLoad(loadConfig{
		targetURL:   srv.URL + "/work",
		concurrency: 8,
		requests:    requests,
		payload:     []byte("x"),
	})

	ceiling := before + maxSessions*3
	waitForGoroutinesAtMost(ceiling, 2*time.Second)
	after := runtime.NumGoroutine()
	if delta := after - before; delta > maxSessions*3 {
		t.Fatalf("goroutine delta = %d, want <= %d after load (evictions must free workers)", delta, maxSessions*3)
	}
}

func TestRunLoad_RetainedHeapStaysBoundedAfterGC(t *testing.T) {
	t.Cleanup(resetSessions)
	resetSessions()
	srv := httptest.NewServer(newAppMux())
	t.Cleanup(srv.Close)

	warmLoadPools(srv.URL + "/work")
	resetSessions()

	runtime.GC()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	const requests = 200
	res := runLoad(loadConfig{
		targetURL:   srv.URL + "/work",
		concurrency: 8,
		requests:    requests,
		payload:     []byte("x"),
	})
	if res.failed != 0 {
		t.Fatalf("failed = %d, want 0 (firstErr=%v)", res.failed, res.firstErr)
	}

	waitForGoroutinesAtMost(runtime.NumGoroutine(), 500*time.Millisecond)
	runtime.GC()
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	ceiling := int64(maxSessions*workDefaultPayloadBytes) * 6
	if growth > ceiling {
		t.Fatalf("HeapAlloc growth = %d bytes after %d requests, want <= %d bytes (bounded by cache size)",
			growth, requests, ceiling)
	}
}

func warmLoadPools(targetURL string) {
	runLoad(loadConfig{
		targetURL:   targetURL,
		concurrency: 4,
		requests:    8,
		payload:     []byte("warmup"),
	})
}
