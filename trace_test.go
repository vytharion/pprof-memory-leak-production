package main

import (
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

// syntheticStackedHeapText is a debug=1 heap profile with four
// samples. Two small samples land in main.padPayload at line 63
// (the append() branch), one large sample lands at line 61 (the
// make() branch), and all three share a padPayload -> newSession
// -> workHandler stack. A fourth sample lives outside padPayload
// so we can prove focus filtering actually filters.
const syntheticStackedHeapText = "heap profile: 4: 52000 [4: 52000] @ heap/524288\n" +
	"1: 4096 [1: 4096] @ 0x1 0x2 0x3\n" +
	"#\t0x1\tmain.padPayload+0xa\t/tmp/leak.go:63\n" +
	"#\t0x2\tmain.newSession+0x1a\t/tmp/leak.go:48\n" +
	"#\t0x3\tmain.workHandler+0x2b\t/tmp/leak.go:40\n" +
	"1: 4096 [1: 4096] @ 0x1 0x2 0x3\n" +
	"#\t0x1\tmain.padPayload+0xa\t/tmp/leak.go:63\n" +
	"#\t0x2\tmain.newSession+0x1a\t/tmp/leak.go:48\n" +
	"#\t0x3\tmain.workHandler+0x2b\t/tmp/leak.go:40\n" +
	"1: 40000 [1: 40000] @ 0x4 0x2 0x3\n" +
	"#\t0x4\tmain.padPayload+0x0\t/tmp/leak.go:61\n" +
	"#\t0x2\tmain.newSession+0x1a\t/tmp/leak.go:48\n" +
	"#\t0x3\tmain.workHandler+0x2b\t/tmp/leak.go:40\n" +
	"1: 3808 [1: 3808] @ 0x9 0xa\n" +
	"#\t0x9\tmain.helloHandler+0x4\t/tmp/server.go:31\n" +
	"#\t0xa\tnet/http.HandlerFunc.ServeHTTP+0x0\t/usr/local/go/src/net/http/server.go:2136\n" +
	"\n" +
	"# runtime.MemStats\n" +
	"# Alloc = 100\n"

func TestExtractFrame_ParsesSymbolFileAndLine(t *testing.T) {
	frame, ok := extractFrame("#\t0x4b3a7f\tmain.padPayload+0x1f\t/tmp/leak.go:63")
	if !ok {
		t.Fatalf("extractFrame ok=false")
	}
	if frame.symbol != "main.padPayload" {
		t.Fatalf("symbol = %q, want main.padPayload", frame.symbol)
	}
	if frame.file != "/tmp/leak.go" {
		t.Fatalf("file = %q, want /tmp/leak.go", frame.file)
	}
	if frame.line != 63 {
		t.Fatalf("line = %d, want 63", frame.line)
	}
}

func TestParseHeapSamples_KeepsFullStackInLeafFirstOrder(t *testing.T) {
	samples, err := parseHeapSamples([]byte(syntheticStackedHeapText))
	if err != nil {
		t.Fatalf("parseHeapSamples: %v", err)
	}
	if len(samples) != 4 {
		t.Fatalf("samples = %d, want 4", len(samples))
	}
	first := samples[0]
	if first.inuseBytes != 4096 {
		t.Fatalf("first.inuseBytes = %d, want 4096", first.inuseBytes)
	}
	if len(first.frames) != 3 {
		t.Fatalf("first.frames len = %d, want 3", len(first.frames))
	}
	if first.frames[0].symbol != "main.padPayload" {
		t.Fatalf("leaf symbol = %q, want main.padPayload", first.frames[0].symbol)
	}
	if first.frames[2].symbol != "main.workHandler" {
		t.Fatalf("root symbol = %q, want main.workHandler", first.frames[2].symbol)
	}
}

func TestListSourceAnnotations_GroupsBytesByLeafFileLine(t *testing.T) {
	lines, err := listSourceAnnotations([]byte(syntheticStackedHeapText), "padPayload")
	if err != nil {
		t.Fatalf("listSourceAnnotations: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2 (line 61 and line 63)", len(lines))
	}
	if lines[0].line != 61 || lines[0].bytes != 40000 {
		t.Fatalf("top line = %+v, want line=61 bytes=40000", lines[0])
	}
	if lines[1].line != 63 || lines[1].bytes != 8192 || lines[1].samples != 2 {
		t.Fatalf("second line = %+v, want line=63 bytes=8192 samples=2", lines[1])
	}
}

func TestListSourceAnnotations_IgnoresSymbolsOutsideTarget(t *testing.T) {
	lines, err := listSourceAnnotations([]byte(syntheticStackedHeapText), "helloHandler")
	if err != nil {
		t.Fatalf("listSourceAnnotations: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if lines[0].bytes != 3808 {
		t.Fatalf("bytes = %d, want 3808", lines[0].bytes)
	}
	if !strings.HasSuffix(lines[0].file, "server.go") {
		t.Fatalf("file = %q, want *server.go", lines[0].file)
	}
}

func TestBuildCallGraph_KeepsOnlySamplesTraversingTarget(t *testing.T) {
	g, err := buildCallGraph([]byte(syntheticStackedHeapText), "padPayload")
	if err != nil {
		t.Fatalf("buildCallGraph: %v", err)
	}
	if _, ok := g.nodes["main.helloHandler"]; ok {
		t.Fatalf("helloHandler leaked into focused graph: %v", g.nodes)
	}
	want := int64(4096 + 4096 + 40000)
	if got := g.nodes["main.padPayload"]; got != want {
		t.Fatalf("padPayload node = %d, want %d", got, want)
	}
	if got := g.nodes["main.newSession"]; got != want {
		t.Fatalf("newSession node = %d, want %d", got, want)
	}
	if got := g.nodes["main.workHandler"]; got != want {
		t.Fatalf("workHandler node = %d, want %d", got, want)
	}
}

func TestBuildCallGraph_AccumulatesEdgeWeightsCallerToCallee(t *testing.T) {
	g, err := buildCallGraph([]byte(syntheticStackedHeapText), "padPayload")
	if err != nil {
		t.Fatalf("buildCallGraph: %v", err)
	}
	want := int64(4096 + 4096 + 40000)
	edge := callEdge{parent: "main.newSession", child: "main.padPayload"}
	if got := g.edges[edge]; got != want {
		t.Fatalf("edge %+v = %d, want %d", edge, got, want)
	}
	edge = callEdge{parent: "main.workHandler", child: "main.newSession"}
	if got := g.edges[edge]; got != want {
		t.Fatalf("edge %+v = %d, want %d", edge, got, want)
	}
	badEdge := callEdge{parent: "main.padPayload", child: "main.newSession"}
	if got := g.edges[badEdge]; got != 0 {
		t.Fatalf("edge direction leaked (child->parent): %d", got)
	}
}

func TestRenderCallGraphDOT_ContainsTargetHighlightAndEdges(t *testing.T) {
	g, err := buildCallGraph([]byte(syntheticStackedHeapText), "padPayload")
	if err != nil {
		t.Fatalf("buildCallGraph: %v", err)
	}
	dot := renderCallGraphDOT(g)
	if !strings.HasPrefix(dot, "digraph pprof_focus {") {
		t.Fatalf("dot missing digraph header:\n%s", dot)
	}
	if !strings.Contains(dot, "\"main.padPayload\"") {
		t.Fatalf("dot missing padPayload node:\n%s", dot)
	}
	if !strings.Contains(dot, "#f4a3a3") {
		t.Fatalf("dot missing target highlight color:\n%s", dot)
	}
	if !strings.Contains(dot, "\"main.newSession\" -> \"main.padPayload\"") {
		t.Fatalf("dot missing newSession->padPayload edge:\n%s", dot)
	}
	if !strings.HasSuffix(strings.TrimSpace(dot), "}") {
		t.Fatalf("dot missing closing brace:\n%s", dot)
	}
}

func TestListReport_RanksAndClampsTopN(t *testing.T) {
	lines := []sourceLine{
		{file: "/tmp/leak.go", line: 61, bytes: 40000, samples: 5},
		{file: "/tmp/leak.go", line: 63, bytes: 8000, samples: 2},
		{file: "/tmp/leak.go", line: 47, bytes: 200, samples: 1},
	}
	report := listReport("main.padPayload", lines, 2)
	for _, col := range []string{"RANK", "BYTES", "SAMPLES", "SOURCE"} {
		if !strings.Contains(report, col) {
			t.Fatalf("report missing column %q:\n%s", col, report)
		}
	}
	if !strings.Contains(report, "leak.go:61") || !strings.Contains(report, "leak.go:63") {
		t.Fatalf("report missing expected source lines:\n%s", report)
	}
	if strings.Contains(report, "leak.go:47") {
		t.Fatalf("report should have clamped to top 2 but includes line 47:\n%s", report)
	}
}

func TestTraceFromLiveHeap_IsolatesLeakingSourceLineAndStack(t *testing.T) {
	t.Cleanup(resetSessions)
	resetSessions()

	app := httptest.NewServer(newAppMux())
	t.Cleanup(app.Close)
	admin := httptest.NewServer(newAdminMux())
	t.Cleanup(admin.Close)

	pc := newProfileClient(admin.URL)

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

	runtime.GC()
	runtime.GC()
	profile, err := pc.captureHeapText()
	if err != nil {
		t.Fatalf("captureHeapText: %v", err)
	}

	lines, err := listSourceAnnotations(profile, "padPayload")
	if err != nil {
		t.Fatalf("listSourceAnnotations: %v", err)
	}
	if len(lines) == 0 {
		t.Fatalf("no source-line attribution for padPayload")
	}
	if !strings.HasSuffix(lines[0].file, "leak.go") {
		t.Fatalf("top line file = %q, want */leak.go", lines[0].file)
	}
	if lines[0].line == 0 {
		t.Fatalf("top line number is zero; expected a real line")
	}
	minLeakBytes := int64(requests*workDefaultPayloadBytes) / 4
	if lines[0].bytes < minLeakBytes {
		t.Fatalf("top line bytes = %d, want >= %d", lines[0].bytes, minLeakBytes)
	}
	t.Logf("list padPayload:\n%s", listReport("padPayload", lines, 5))

	g, err := buildCallGraph(profile, "padPayload")
	if err != nil {
		t.Fatalf("buildCallGraph: %v", err)
	}
	if !hasNodeMatching(g, "padPayload") {
		t.Fatalf("call graph missing padPayload node: %v", nodeKeys(g))
	}
	if !hasNodeMatching(g, "newSession") {
		t.Fatalf("call graph missing newSession node: %v", nodeKeys(g))
	}
	if !hasNodeMatching(g, "workHandler") {
		t.Fatalf("call graph missing workHandler node: %v", nodeKeys(g))
	}
	if !hasEdgeMatching(g, "newSession", "padPayload") {
		t.Fatalf("call graph missing newSession -> padPayload edge: %v", edgeKeys(g))
	}
	if !hasEdgeMatching(g, "workHandler", "newSession") {
		t.Fatalf("call graph missing workHandler -> newSession edge: %v", edgeKeys(g))
	}

	dot := renderCallGraphDOT(g)
	if !strings.Contains(dot, "digraph pprof_focus") {
		t.Fatalf("dot output missing digraph header:\n%s", dot)
	}
	if !strings.Contains(dot, "#f4a3a3") {
		t.Fatalf("dot output missing target highlight color:\n%s", dot)
	}
}

func hasNodeMatching(g callGraph, suffix string) bool {
	for name := range g.nodes {
		if symbolMatches(name, suffix) {
			return true
		}
	}
	return false
}

func hasEdgeMatching(g callGraph, parentSuffix, childSuffix string) bool {
	for e := range g.edges {
		if symbolMatches(e.parent, parentSuffix) && symbolMatches(e.child, childSuffix) {
			return true
		}
	}
	return false
}

func nodeKeys(g callGraph) []string {
	out := make([]string, 0, len(g.nodes))
	for k := range g.nodes {
		out = append(out, k)
	}
	return out
}

func edgeKeys(g callGraph) []string {
	out := make([]string, 0, len(g.edges))
	for e := range g.edges {
		out = append(out, e.parent+"->"+e.child)
	}
	return out
}
