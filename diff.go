package main

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// heapAggregate sums in-use bytes across a debug=1 heap profile, keyed
// by the leaf (allocation-site) function symbol of each sample stack.
type heapAggregate struct {
	byLeaf map[string]int64
	total  int64
}

func newHeapAggregate() heapAggregate {
	return heapAggregate{byLeaf: map[string]int64{}}
}

// aggregateHeapText walks a debug=1 heap profile and sums the in-use
// bytes of every sample under the symbol of that sample's leaf frame
// (the first "#" line after each sample line). Anything after the
// "# runtime.MemStats" footer is ignored.
func aggregateHeapText(text []byte) (heapAggregate, error) {
	agg := newHeapAggregate()
	scanner := bufio.NewScanner(bytes.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var pendingBytes int64
	waitingLeaf := false
	inFooter := false

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "heap profile:") {
			continue
		}
		if strings.HasPrefix(line, "# runtime.MemStats") {
			inFooter = true
			waitingLeaf = false
			continue
		}
		if inFooter {
			continue
		}
		if isHeapSampleLine(line) {
			b, err := parseSampleInuseBytes(line)
			if err != nil {
				return heapAggregate{}, err
			}
			pendingBytes = b
			waitingLeaf = true
			continue
		}
		if !waitingLeaf {
			continue
		}
		leaf, ok := extractFrameLeaf(line)
		if !ok {
			continue
		}
		agg.byLeaf[leaf] += pendingBytes
		agg.total += pendingBytes
		waitingLeaf = false
	}
	if err := scanner.Err(); err != nil {
		return heapAggregate{}, err
	}
	return agg, nil
}

func isHeapSampleLine(line string) bool {
	if line == "" {
		return false
	}
	c := line[0]
	if c < '0' || c > '9' {
		return false
	}
	return strings.Contains(line, "] @")
}

func parseSampleInuseBytes(line string) (int64, error) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return 0, fmt.Errorf("unexpected sample line: %q", line)
	}
	raw := strings.TrimSuffix(fields[1], ":")
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse sample bytes %q: %w", fields[1], err)
	}
	return v, nil
}

// extractFrameLeaf reads a Go runtime pprof frame line of the shape
// "#\t0x<pc>\t<symbol>+0x<offset>\t<file>:<line>" and returns the
// symbol with the "+0x..." offset stripped. Lines that do not match
// that shape (blank lines, footer lines, sample lines) return ok=false.
func extractFrameLeaf(line string) (string, bool) {
	if !strings.HasPrefix(line, "#\t") {
		return "", false
	}
	parts := strings.Split(line, "\t")
	if len(parts) < 3 {
		return "", false
	}
	if !strings.HasPrefix(parts[1], "0x") {
		return "", false
	}
	sym := parts[2]
	if i := strings.LastIndex(sym, "+0x"); i >= 0 {
		sym = sym[:i]
	}
	sym = strings.TrimSpace(sym)
	if sym == "" {
		return "", false
	}
	return sym, true
}

// diffEntry is a per-leaf byte delta between two heap aggregates.
type diffEntry struct {
	leaf     string
	delta    int64
	baseline int64
	post     int64
}

// diffAggregates subtracts base from post and returns entries sorted by
// delta descending (largest growers first). Symbols present in only one
// side appear with the missing side reported as 0.
func diffAggregates(base, post heapAggregate) []diffEntry {
	seen := map[string]struct{}{}
	for k := range base.byLeaf {
		seen[k] = struct{}{}
	}
	for k := range post.byLeaf {
		seen[k] = struct{}{}
	}
	entries := make([]diffEntry, 0, len(seen))
	for leaf := range seen {
		b := base.byLeaf[leaf]
		p := post.byLeaf[leaf]
		entries = append(entries, diffEntry{
			leaf:     leaf,
			delta:    p - b,
			baseline: b,
			post:     p,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].delta != entries[j].delta {
			return entries[i].delta > entries[j].delta
		}
		return entries[i].leaf < entries[j].leaf
	})
	return entries
}

// topAllocatorsReport renders a plaintext "top N" table of diff entries
// in the shape an on-call SRE expects from `go tool pprof -top -base`.
// N is clamped to [1, len(entries)].
func topAllocatorsReport(entries []diffEntry, n int) string {
	if n < 1 {
		n = 1
	}
	if n > len(entries) {
		n = len(entries)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-4s  %14s  %14s  %14s  %s\n",
		"RANK", "DELTA_BYTES", "BASE_BYTES", "POST_BYTES", "LEAF")
	for i := 0; i < n; i++ {
		e := entries[i]
		fmt.Fprintf(&sb, "%-4d  %14d  %14d  %14d  %s\n",
			i+1, e.delta, e.baseline, e.post, e.leaf)
	}
	return sb.String()
}
