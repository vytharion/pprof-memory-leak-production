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

func TestRunLoad_ReproducesSessionLeakUnderConcurrentLoad(t *testing.T) {
	t.Cleanup(resetSessions)
	resetSessions()
	srv := httptest.NewServer(newAppMux())
	t.Cleanup(srv.Close)

	const requests = 64
	res := runLoad(loadConfig{
		targetURL:   srv.URL + "/work",
		concurrency: 8,
		requests:    requests,
		payload:     []byte("payload"),
	})

	if res.failed != 0 {
		t.Fatalf("failed = %d, want 0 (firstErr=%v)", res.failed, res.firstErr)
	}
	if got := activeSessions(); got != requests {
		t.Fatalf("activeSessions() = %d, want %d after sustained load", got, requests)
	}
}

func TestRunLoad_ParksOneGoroutinePerRequest(t *testing.T) {
	t.Cleanup(resetSessions)
	resetSessions()
	runtime.GC()
	srv := httptest.NewServer(newAppMux())
	t.Cleanup(srv.Close)

	before := runtime.NumGoroutine()

	const requests = 40
	runLoad(loadConfig{
		targetURL:   srv.URL + "/work",
		concurrency: 8,
		requests:    requests,
		payload:     []byte("x"),
	})

	waitForGoroutines(before+requests, 1*time.Second)
	after := runtime.NumGoroutine()
	if delta := after - before; delta < requests {
		t.Fatalf("goroutine delta = %d, want >= %d after load", delta, requests)
	}
}

func TestRunLoad_GrowsRetainedHeapAfterGC(t *testing.T) {
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

	runtime.GC()
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	if after.HeapAlloc <= before.HeapAlloc {
		t.Fatalf("HeapAlloc did not grow: before=%d after=%d", before.HeapAlloc, after.HeapAlloc)
	}
	growth := after.HeapAlloc - before.HeapAlloc
	minGrowth := uint64(requests*workDefaultPayloadBytes) / 2
	if growth < minGrowth {
		t.Fatalf("HeapAlloc growth = %d bytes, want >= %d bytes (leaked payloads should survive GC)", growth, minGrowth)
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
