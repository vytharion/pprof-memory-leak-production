package main

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// heapFrame is one entry in a heap-profile sample's call stack. Frames
// arrive leaf-first (index 0 is where the allocation happened, the last
// index is the outermost caller).
type heapFrame struct {
	symbol string
	file   string
	line   int
}

// heapSample is one sample block from a debug=1 heap profile: how many
// in-use bytes are attributed to this stack and the stack itself.
type heapSample struct {
	inuseBytes int64
	frames     []heapFrame
}

// parseHeapSamples walks the debug=1 heap profile text and returns one
// heapSample per sample block. It stops feeding samples once the
// "# runtime.MemStats" footer starts.
func parseHeapSamples(text []byte) ([]heapSample, error) {
	scanner := bufio.NewScanner(bytes.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		samples []heapSample
		current *heapSample
		footer  bool
	)

	flush := func() {
		if current != nil && len(current.frames) > 0 {
			samples = append(samples, *current)
		}
		current = nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "# runtime.MemStats") {
			footer = true
			flush()
			continue
		}
		if footer {
			continue
		}
		if strings.HasPrefix(line, "heap profile:") {
			continue
		}
		if isHeapSampleLine(line) {
			flush()
			b, err := parseSampleInuseBytes(line)
			if err != nil {
				return nil, err
			}
			current = &heapSample{inuseBytes: b}
			continue
		}
		if current == nil {
			continue
		}
		frame, ok := extractFrame(line)
		if !ok {
			continue
		}
		current.frames = append(current.frames, frame)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()
	return samples, nil
}

// extractFrame parses a debug=1 profile frame line into a heapFrame.
// The line shape is "#\t0x<pc>\t<symbol>+0x<offset>\t<file>:<line>".
func extractFrame(line string) (heapFrame, bool) {
	if !strings.HasPrefix(line, "#\t") {
		return heapFrame{}, false
	}
	parts := strings.Split(line, "\t")
	if len(parts) < 4 || !strings.HasPrefix(parts[1], "0x") {
		return heapFrame{}, false
	}
	sym := parts[2]
	if i := strings.LastIndex(sym, "+0x"); i >= 0 {
		sym = sym[:i]
	}
	sym = strings.TrimSpace(sym)
	if sym == "" {
		return heapFrame{}, false
	}
	file, lineNum := splitFileLine(parts[3])
	return heapFrame{symbol: sym, file: file, line: lineNum}, true
}

func splitFileLine(raw string) (string, int) {
	raw = strings.TrimSpace(raw)
	i := strings.LastIndex(raw, ":")
	if i < 0 {
		return raw, 0
	}
	n, err := strconv.Atoi(raw[i+1:])
	if err != nil {
		return raw, 0
	}
	return raw[:i], n
}

// symbolMatches lets callers use either the fully-qualified symbol or
// the tail (e.g. "padPayload" as shorthand for
// "github.com/vytharion/pprof-memory-leak-production.padPayload"). Exact
// match wins; otherwise a "."+target suffix is accepted.
func symbolMatches(symbol, target string) bool {
	if symbol == target {
		return true
	}
	return strings.HasSuffix(symbol, "."+target)
}

// sourceLine is per-source-location allocation attribution used by the
// list view. Bytes are the sum of in-use bytes across every sample
// whose leaf frame lives at this file:line.
type sourceLine struct {
	file    string
	line    int
	bytes   int64
	samples int
}

// listSourceAnnotations mirrors "go tool pprof -list <target>" over the
// debug=1 text profile: for every sample whose leaf frame belongs to
// the target symbol, bump the byte total at that leaf's file:line.
func listSourceAnnotations(text []byte, targetSymbol string) ([]sourceLine, error) {
	samples, err := parseHeapSamples(text)
	if err != nil {
		return nil, err
	}
	type key struct {
		file string
		line int
	}
	byLoc := map[key]*sourceLine{}
	for _, s := range samples {
		if len(s.frames) == 0 {
			continue
		}
		leaf := s.frames[0]
		if !symbolMatches(leaf.symbol, targetSymbol) {
			continue
		}
		k := key{file: leaf.file, line: leaf.line}
		entry, ok := byLoc[k]
		if !ok {
			entry = &sourceLine{file: leaf.file, line: leaf.line}
			byLoc[k] = entry
		}
		entry.bytes += s.inuseBytes
		entry.samples++
	}
	out := make([]sourceLine, 0, len(byLoc))
	for _, v := range byLoc {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].bytes != out[j].bytes {
			return out[i].bytes > out[j].bytes
		}
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}
		return out[i].line < out[j].line
	})
	return out, nil
}

// listReport pretty-prints listSourceAnnotations output as a fixed-width
// table so it drops cleanly into an incident write-up.
func listReport(target string, lines []sourceLine, n int) string {
	if n < 1 {
		n = 1
	}
	if n > len(lines) {
		n = len(lines)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "list %s (top %d source lines by in-use bytes)\n", target, n)
	fmt.Fprintf(&sb, "%-4s  %14s  %8s  %s\n", "RANK", "BYTES", "SAMPLES", "SOURCE")
	for i := 0; i < n; i++ {
		l := lines[i]
		loc := fmt.Sprintf("%s:%d", shortenFile(l.file), l.line)
		fmt.Fprintf(&sb, "%-4d  %14d  %8d  %s\n", i+1, l.bytes, l.samples, loc)
	}
	return sb.String()
}

// shortenFile keeps the trailing two path segments so long GOPATH-style
// paths stay readable in the console (e.g. "/root/go/src/foo/bar/a.go"
// becomes "bar/a.go"). Files without a slash are returned as-is.
func shortenFile(path string) string {
	if path == "" {
		return "?"
	}
	parts := strings.Split(path, "/")
	if len(parts) <= 2 {
		return path
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

// callEdge is a parent→child pair aggregating in-use bytes across every
// sample whose stack traverses that edge.
type callEdge struct {
	parent string
	child  string
}

// callGraph is the focused view around a target symbol. Every sample
// whose stack contains target contributes its in-use bytes to each
// visited node (in `nodes`) and to each parent→child edge along the
// stack (in `edges`).
type callGraph struct {
	target string
	nodes  map[string]int64
	edges  map[callEdge]int64
}

func newCallGraph(target string) callGraph {
	return callGraph{
		target: target,
		nodes:  map[string]int64{},
		edges:  map[callEdge]int64{},
	}
}

// buildCallGraph mirrors "go tool pprof -focus=<target> -web": it keeps
// only samples that traverse the target symbol, then accumulates node
// and edge weights so a caller can render a Graphviz DOT graph.
func buildCallGraph(text []byte, targetSymbol string) (callGraph, error) {
	samples, err := parseHeapSamples(text)
	if err != nil {
		return callGraph{}, err
	}
	g := newCallGraph(targetSymbol)
	for _, s := range samples {
		if !sampleContainsSymbol(s, targetSymbol) {
			continue
		}
		accumulateSample(&g, s)
	}
	return g, nil
}

func sampleContainsSymbol(s heapSample, target string) bool {
	for _, f := range s.frames {
		if symbolMatches(f.symbol, target) {
			return true
		}
	}
	return false
}

func accumulateSample(g *callGraph, s heapSample) {
	for _, f := range s.frames {
		g.nodes[f.symbol] += s.inuseBytes
	}
	for i := 0; i < len(s.frames)-1; i++ {
		edge := callEdge{parent: s.frames[i+1].symbol, child: s.frames[i].symbol}
		g.edges[edge] += s.inuseBytes
	}
}

// renderCallGraphDOT emits a Graphviz DOT digraph representing the
// focused call graph. The target node is filled red so an on-call
// reader spots the leak source without hunting through the graph.
func renderCallGraphDOT(g callGraph) string {
	var sb strings.Builder
	sb.WriteString("digraph pprof_focus {\n")
	fmt.Fprintf(&sb, "  label=%q;\n", "focus="+g.target)
	sb.WriteString("  node [shape=box, style=filled, fillcolor=\"#e8e8e8\"];\n")

	names := sortedNodeNames(g.nodes)
	for _, name := range names {
		attrs := nodeAttrs(name, g)
		fmt.Fprintf(&sb, "  %q [%s];\n", name, attrs)
	}

	edges := sortedEdges(g.edges)
	for _, e := range edges {
		fmt.Fprintf(&sb, "  %q -> %q [label=\"%d B\"];\n", e.parent, e.child, g.edges[e])
	}
	sb.WriteString("}\n")
	return sb.String()
}

func nodeAttrs(name string, g callGraph) string {
	label := fmt.Sprintf("%s\\n%d B", name, g.nodes[name])
	if symbolMatches(name, g.target) {
		return fmt.Sprintf("label=%q, fillcolor=\"#f4a3a3\"", label)
	}
	return fmt.Sprintf("label=%q", label)
}

func sortedNodeNames(nodes map[string]int64) []string {
	out := make([]string, 0, len(nodes))
	for k := range nodes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedEdges(edges map[callEdge]int64) []callEdge {
	out := make([]callEdge, 0, len(edges))
	for k := range edges {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].parent != out[j].parent {
			return out[i].parent < out[j].parent
		}
		return out[i].child < out[j].child
	})
	return out
}
