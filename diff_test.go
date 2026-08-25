package main

import (
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

const syntheticHeapText = "heap profile: 3: 12000 [3: 12000] @ heap/524288\n" +
	"1: 4096 [1: 4096] @ 0x1 0x2\n" +
	"#\t0x1\tmain.padPayload+0xa\t/tmp/leak.go:63\n" +
	"#\t0x2\tmain.newSession+0x1a\t/tmp/leak.go:48\n" +
	"1: 4096 [1: 4096] @ 0x3 0x4\n" +
	"#\t0x3\tmain.padPayload+0xa\t/tmp/leak.go:63\n" +
	"#\t0x4\tmain.newSession+0x1a\t/tmp/leak.go:48\n" +
	"1: 3808 [1: 3808] @ 0x5 0x6\n" +
	"#\t0x5\tmain.newSession+0x2a\t/tmp/leak.go:52\n" +
	"#\t0x6\tmain.workHandler+0x1e\t/tmp/leak.go:40\n" +
	"\n" +
	"# runtime.MemStats\n" +
	"# Alloc = 100\n" +
	"# HeapObjects = 5\n"

func TestAggregateHeapText_SumsBytesByLeafSymbol(t *testing.T) {
	agg, err := aggregateHeapText([]byte(syntheticHeapText))
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if got := agg.byLeaf["main.padPayload"]; got != 8192 {
		t.Fatalf("padPayload = %d, want 8192", got)
	}
	if got := agg.byLeaf["main.newSession"]; got != 3808 {
		t.Fatalf("newSession = %d, want 3808", got)
	}
	if _, hasWork := agg.byLeaf["main.workHandler"]; hasWork {
		t.Fatalf("workHandler should not appear (not a leaf), got=%d", agg.byLeaf["main.workHandler"])
	}
	if agg.total != 8192+3808 {
		t.Fatalf("total = %d, want %d", agg.total, 8192+3808)
	}
}

func TestAggregateHeapText_IgnoresMemStatsFooter(t *testing.T) {
	text := "heap profile: 0: 0 [0: 0] @ heap/1\n" +
		"# runtime.MemStats\n" +
		"# Alloc = 100\n" +
		"#\t0x1\tshould.not.count+0x0\t/tmp/x.go:1\n"
	agg, err := aggregateHeapText([]byte(text))
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if len(agg.byLeaf) != 0 || agg.total != 0 {
		t.Fatalf("footer leaked into aggregation: byLeaf=%v total=%d", agg.byLeaf, agg.total)
	}
}

func TestAggregateHeapText_EmptyBodyIsZeroAggregate(t *testing.T) {
	agg, err := aggregateHeapText([]byte("heap profile: 0: 0 [0: 0] @ heap/1\n"))
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if agg.total != 0 || len(agg.byLeaf) != 0 {
		t.Fatalf("empty profile produced non-zero aggregate: total=%d byLeaf=%v", agg.total, agg.byLeaf)
	}
}

func TestExtractFrameLeaf_StripsOffsetSuffix(t *testing.T) {
	sym, ok := extractFrameLeaf("#\t0x4b3a7f\tmain.padPayload+0x1f\t/tmp/leak.go:63")
	if !ok {
		t.Fatalf("extractFrameLeaf: ok=false")
	}
	if sym != "main.padPayload" {
		t.Fatalf("sym = %q, want main.padPayload", sym)
	}
}

func TestExtractFrameLeaf_RejectsNonFrameLines(t *testing.T) {
	cases := []string{
		"",
		"1: 4096 [1: 4096] @ 0x1",
		"# runtime.MemStats",
		"heap profile: 0: 0",
	}
	for _, line := range cases {
		if sym, ok := extractFrameLeaf(line); ok {
			t.Fatalf("expected reject for %q, got sym=%q", line, sym)
		}
	}
}

func TestDiffAggregates_ComputesSignedDeltas(t *testing.T) {
	base := heapAggregate{
		byLeaf: map[string]int64{"main.padPayload": 4096, "main.other": 2000},
		total:  6096,
	}
	post := heapAggregate{
		byLeaf: map[string]int64{
			"main.padPayload": 40960,
			"main.other":      500,
			"main.newSession": 1024,
		},
		total: 42484,
	}
	entries := diffAggregates(base, post)
	byLeaf := map[string]diffEntry{}
	for _, e := range entries {
		byLeaf[e.leaf] = e
	}
	if got := byLeaf["main.padPayload"].delta; got != 36864 {
		t.Fatalf("padPayload delta = %d, want 36864", got)
	}
	if got := byLeaf["main.other"].delta; got != -1500 {
		t.Fatalf("other delta = %d, want -1500", got)
	}
	if got := byLeaf["main.newSession"].delta; got != 1024 {
		t.Fatalf("newSession delta = %d, want 1024", got)
	}
	if got := byLeaf["main.newSession"].baseline; got != 0 {
		t.Fatalf("newSession baseline = %d, want 0", got)
	}
}

func TestDiffAggregates_SortsByDeltaDescending(t *testing.T) {
	base := heapAggregate{byLeaf: map[string]int64{}}
	post := heapAggregate{byLeaf: map[string]int64{
		"a": 100,
		"b": 500,
		"c": 300,
	}}
	entries := diffAggregates(base, post)
	want := []string{"b", "c", "a"}
	if len(entries) != len(want) {
		t.Fatalf("entries = %d, want %d", len(entries), len(want))
	}
	for i, e := range entries {
		if e.leaf != want[i] {
			t.Fatalf("entries[%d].leaf = %q, want %q", i, e.leaf, want[i])
		}
	}
}

func TestTopAllocatorsReport_ShowsHeaderAndTopN(t *testing.T) {
	entries := []diffEntry{
		{leaf: "main.padPayload", delta: 40000, baseline: 960, post: 40960},
		{leaf: "main.newSession", delta: 1024, baseline: 0, post: 1024},
		{leaf: "main.other", delta: -500, baseline: 1000, post: 500},
	}
	report := topAllocatorsReport(entries, 2)
	for _, col := range []string{"RANK", "DELTA_BYTES", "BASE_BYTES", "POST_BYTES", "LEAF"} {
		if !strings.Contains(report, col) {
			t.Fatalf("report missing header column %q:\n%s", col, report)
		}
	}
	if !strings.Contains(report, "main.padPayload") || !strings.Contains(report, "main.newSession") {
		t.Fatalf("report missing top-2 leaves:\n%s", report)
	}
	if strings.Contains(report, "main.other") {
		t.Fatalf("report should have clamped to 2 rows but includes main.other:\n%s", report)
	}
}

func TestTopAllocatorsReport_ClampsNToEntryCount(t *testing.T) {
	entries := []diffEntry{{leaf: "solo", delta: 10, post: 10}}
	report := topAllocatorsReport(entries, 50)
	lines := strings.Split(strings.TrimRight(report, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header + 1 row, got %d lines:\n%s", len(lines), report)
	}
}

func TestDiffProfiles_RanksLeakSymbolsInTopAllocatorsAfterLoad(t *testing.T) {
	t.Cleanup(resetSessions)
	resetSessions()

	app := httptest.NewServer(newAppMux())
	t.Cleanup(app.Close)
	admin := httptest.NewServer(newAdminMux())
	t.Cleanup(admin.Close)

	pc := newProfileClient(admin.URL)

	runtime.GC()
	runtime.GC()
	baselineText, err := pc.captureHeapText()
	if err != nil {
		t.Fatalf("baseline capture: %v", err)
	}
	baseAgg, err := aggregateHeapText(baselineText)
	if err != nil {
		t.Fatalf("aggregate baseline: %v", err)
	}

	const requests = 400
	res := runLoad(loadConfig{
		targetURL:   app.URL + "/work",
		concurrency: 8,
		requests:    requests,
		payload:     []byte("x"),
	})
	if res.failed != 0 {
		t.Fatalf("load failed = %d (firstErr=%v)", res.failed, res.firstErr)
	}

	runtime.GC()
	runtime.GC()
	postText, err := pc.captureHeapText()
	if err != nil {
		t.Fatalf("post-leak capture: %v", err)
	}
	postAgg, err := aggregateHeapText(postText)
	if err != nil {
		t.Fatalf("aggregate post: %v", err)
	}

	entries := diffAggregates(baseAgg, postAgg)
	if len(entries) == 0 {
		t.Fatalf("diff produced no entries")
	}

	topN := 10
	if topN > len(entries) {
		topN = len(entries)
	}
	head := entries[:topN]

	needles := []string{"padPayload", "newSession"}
	for _, needle := range needles {
		if !anyEntryContains(head, needle) {
			names := leafNames(head)
			t.Fatalf("top-%d diff does not contain %q; got %v", topN, needle, names)
		}
	}

	if entries[0].delta <= 0 {
		t.Fatalf("top entry delta = %d, want > 0 (leak should dominate)", entries[0].delta)
	}
	minTopDelta := int64(requests*workDefaultPayloadBytes) / 4
	if entries[0].delta < minTopDelta {
		t.Fatalf("top entry delta = %d, want >= %d (leaked payload volume)", entries[0].delta, minTopDelta)
	}

	report := topAllocatorsReport(entries, topN)
	if !strings.Contains(report, "padPayload") {
		t.Fatalf("report missing padPayload:\n%s", report)
	}
	t.Logf("top allocators after leak:\n%s", report)
}

func anyEntryContains(entries []diffEntry, needle string) bool {
	for _, e := range entries {
		if strings.Contains(e.leaf, needle) {
			return true
		}
	}
	return false
}

func leafNames(entries []diffEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.leaf)
	}
	return names
}
