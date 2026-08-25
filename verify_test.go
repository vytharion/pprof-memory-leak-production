package main

import (
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

// TestFixedLeak_HeapDeltaBoundedAfterLoad is the "third profile" check.
// Step 5 captured a baseline and a post-leak profile; Step 6/7 diffed
// them and pinned padPayload as the leaker. Now we capture baseline
// AGAIN, drive the same load pattern, capture a post-fix profile, and
// diff the two. The padPayload delta MUST fit inside the bounded-cache
// budget (maxSessions * workDefaultPayloadBytes plus generous slack for
// scheduler + allocator overhead).
func TestFixedLeak_HeapDeltaBoundedAfterLoad(t *testing.T) {
	t.Cleanup(resetSessions)
	resetSessions()

	app := httptest.NewServer(newAppMux())
	t.Cleanup(app.Close)
	admin := httptest.NewServer(newAdminMux())
	t.Cleanup(admin.Close)

	pc := newProfileClient(admin.URL)

	runtime.GC()
	runtime.GC()
	baseline, err := pc.captureHeapText()
	if err != nil {
		t.Fatalf("baseline captureHeapText: %v", err)
	}

	const requests = 400
	res := runLoad(loadConfig{
		targetURL:   app.URL + "/work",
		concurrency: 8,
		requests:    requests,
		payload:     []byte("x"),
	})
	if res.failed != 0 {
		t.Fatalf("load failed=%d (firstErr=%v)", res.failed, res.firstErr)
	}

	waitForGoroutinesAtMost(runtime.NumGoroutine(), 500*time.Millisecond)
	runtime.GC()
	runtime.GC()
	post, err := pc.captureHeapText()
	if err != nil {
		t.Fatalf("post captureHeapText: %v", err)
	}

	baseAgg, err := aggregateHeapText(baseline)
	if err != nil {
		t.Fatalf("aggregate baseline: %v", err)
	}
	postAgg, err := aggregateHeapText(post)
	if err != nil {
		t.Fatalf("aggregate post: %v", err)
	}
	entries := diffAggregates(baseAgg, postAgg)

	var padDelta int64
	found := false
	for _, e := range entries {
		if symbolMatches(e.leaf, "padPayload") {
			padDelta = e.delta
			found = true
			break
		}
	}
	if !found {
		t.Logf("padPayload not present in diff (delta effectively zero)")
	}

	ceiling := int64(maxSessions*workDefaultPayloadBytes) * 4
	if padDelta > ceiling {
		t.Fatalf("padPayload delta = %d bytes after %d requests, want <= %d (bounded cache)",
			padDelta, requests, ceiling)
	}
	if got := activeSessions(); got > maxSessions {
		t.Fatalf("activeSessions() = %d after load, want <= %d", got, maxSessions)
	}
	t.Logf("padPayload delta after fix: %d bytes (ceiling=%d, active=%d)",
		padDelta, ceiling, activeSessions())
	t.Logf("top allocators diff (post-fix):\n%s", topAllocatorsReport(entries, 5))
}

// TestFixedLeak_GoroutineCountBoundedAfterLoad is the runtime guardrail
// paired with the heap-diff check above: after 400 requests through a
// real HTTP server, the goroutine population must return to within a
// small multiple of the session-cache size, proving evicted sessions
// actually unpark their drainEvents worker.
func TestFixedLeak_GoroutineCountBoundedAfterLoad(t *testing.T) {
	t.Cleanup(resetSessions)
	resetSessions()

	app := httptest.NewServer(newAppMux())
	t.Cleanup(app.Close)

	runtime.GC()
	baseline := runtime.NumGoroutine()

	const requests = 400
	res := runLoad(loadConfig{
		targetURL:   app.URL + "/work",
		concurrency: 8,
		requests:    requests,
		payload:     []byte("x"),
	})
	if res.failed != 0 {
		t.Fatalf("load failed=%d (firstErr=%v)", res.failed, res.firstErr)
	}

	ceiling := baseline + maxSessions*3
	waitForGoroutinesAtMost(ceiling, 3*time.Second)
	after := runtime.NumGoroutine()
	if delta := after - baseline; delta > maxSessions*3 {
		t.Fatalf("goroutine delta = %d after %d requests, want <= %d (bounded by cache size)",
			delta, requests, maxSessions*3)
	}
	t.Logf("goroutines: baseline=%d, after=%d, delta=%d (ceiling=%d)",
		baseline, after, after-baseline, maxSessions*3)
}
