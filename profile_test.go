package main

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProfileClient_CaptureHeapReturnsGzippedProtobuf(t *testing.T) {
	admin := httptest.NewServer(newAdminMux())
	t.Cleanup(admin.Close)

	pc := newProfileClient(admin.URL)
	data, err := pc.captureHeap()
	if err != nil {
		t.Fatalf("captureHeap: %v", err)
	}
	if !isGzipPprof(data) {
		prefix := data
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		t.Fatalf("captureHeap returned %d bytes with prefix %x, want gzip magic 1f8b", len(data), prefix)
	}
}

func TestProfileClient_CaptureHeapTextHasHeaderPrefix(t *testing.T) {
	admin := httptest.NewServer(newAdminMux())
	t.Cleanup(admin.Close)

	pc := newProfileClient(admin.URL)
	data, err := pc.captureHeapText()
	if err != nil {
		t.Fatalf("captureHeapText: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("heap profile:")) {
		first, _, _ := bytes.Cut(data, []byte("\n"))
		t.Fatalf("first line = %q, want 'heap profile:' prefix", first)
	}
}

func TestHeapInuseBytesFromText_ParsesHeader(t *testing.T) {
	sample := []byte("heap profile: 42: 98765 [100: 200000] @ heap/524288\n")
	got, err := heapInuseBytesFromText(sample)
	if err != nil {
		t.Fatalf("heapInuseBytesFromText: %v", err)
	}
	if got != 98765 {
		t.Fatalf("inuse bytes = %d, want 98765", got)
	}
}

func TestHeapInuseBytesFromText_ErrorsOnBrokenHeader(t *testing.T) {
	_, err := heapInuseBytesFromText([]byte("garbage\n"))
	if err == nil {
		t.Fatalf("expected error for malformed header")
	}
}

func TestSaveProfile_WritesFileWithBytes(t *testing.T) {
	dir := t.TempDir()
	path, err := saveProfile(dir, "sample.pprof", []byte("payload"))
	if err != nil {
		t.Fatalf("saveProfile: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("path = %q, want dir %q", path, dir)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("contents = %q, want %q", got, "payload")
	}
}

func TestSaveProfile_CreatesMissingDirectory(t *testing.T) {
	base := t.TempDir()
	nested := filepath.Join(base, "profiles", "heap")
	path, err := saveProfile(nested, "baseline.pprof", []byte("x"))
	if err != nil {
		t.Fatalf("saveProfile: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %q to exist: %v", path, err)
	}
}

func TestProfileClient_PostLeakHeapGrowsOverBaseline(t *testing.T) {
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
		t.Fatalf("baseline capture: %v", err)
	}
	baselineBytes, err := heapInuseBytesFromText(baseline)
	if err != nil {
		t.Fatalf("baseline parse: %v", err)
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
	postLeak, err := pc.captureHeapText()
	if err != nil {
		t.Fatalf("post-leak capture: %v", err)
	}
	postBytes, err := heapInuseBytesFromText(postLeak)
	if err != nil {
		t.Fatalf("post-leak parse: %v", err)
	}

	if postBytes <= baselineBytes {
		t.Fatalf("in-use bytes did not grow: baseline=%d post=%d", baselineBytes, postBytes)
	}
	growth := postBytes - baselineBytes
	minGrowth := int64(requests*workDefaultPayloadBytes) / 2
	if growth < minGrowth {
		t.Fatalf("inuse growth = %d bytes, want >= %d (leak should dominate)", growth, minGrowth)
	}
}

func TestProfileClient_PostLeakHeapTextMentionsLeakStack(t *testing.T) {
	t.Cleanup(resetSessions)
	resetSessions()

	app := httptest.NewServer(newAppMux())
	t.Cleanup(app.Close)
	admin := httptest.NewServer(newAdminMux())
	t.Cleanup(admin.Close)

	const requests = 200
	res := runLoad(loadConfig{
		targetURL:   app.URL + "/work",
		concurrency: 8,
		requests:    requests,
		payload:     []byte("x"),
	})
	if res.failed != 0 {
		t.Fatalf("load failed = %d (firstErr=%v)", res.failed, res.firstErr)
	}

	pc := newProfileClient(admin.URL)
	runtime.GC()
	runtime.GC()
	text, err := pc.captureHeapText()
	if err != nil {
		t.Fatalf("captureHeapText: %v", err)
	}

	needles := []string{"padPayload", "newSession"}
	for _, needle := range needles {
		if !strings.Contains(string(text), needle) {
			t.Fatalf("post-leak profile does not mention %q — leak call stack not visible", needle)
		}
	}
}

func TestProfileClient_SavesBaselineAndPostLeakToDisk(t *testing.T) {
	t.Cleanup(resetSessions)
	resetSessions()

	app := httptest.NewServer(newAppMux())
	t.Cleanup(app.Close)
	admin := httptest.NewServer(newAdminMux())
	t.Cleanup(admin.Close)

	pc := newProfileClient(admin.URL)
	dir := t.TempDir()

	baseline, err := pc.captureHeap()
	if err != nil {
		t.Fatalf("baseline capture: %v", err)
	}
	basePath, err := saveProfile(dir, "baseline.pprof.gz", baseline)
	if err != nil {
		t.Fatalf("saveProfile baseline: %v", err)
	}

	res := runLoad(loadConfig{
		targetURL:   app.URL + "/work",
		concurrency: 4,
		requests:    64,
		payload:     []byte("x"),
	})
	if res.failed != 0 {
		t.Fatalf("load failed = %d (firstErr=%v)", res.failed, res.firstErr)
	}

	postLeak, err := pc.captureHeap()
	if err != nil {
		t.Fatalf("post-leak capture: %v", err)
	}
	postPath, err := saveProfile(dir, "post-leak.pprof.gz", postLeak)
	if err != nil {
		t.Fatalf("saveProfile post-leak: %v", err)
	}

	baseInfo, err := os.Stat(basePath)
	if err != nil || baseInfo.Size() == 0 {
		t.Fatalf("baseline profile not saved: err=%v size=%d", err, sizeOr(baseInfo))
	}
	postInfo, err := os.Stat(postPath)
	if err != nil || postInfo.Size() == 0 {
		t.Fatalf("post-leak profile not saved: err=%v size=%d", err, sizeOr(postInfo))
	}
	if !isGzipPprof(baseline) || !isGzipPprof(postLeak) {
		t.Fatalf("saved profiles are not gzipped pprof: base=%t post=%t", isGzipPprof(baseline), isGzipPprof(postLeak))
	}
}

func sizeOr(fi os.FileInfo) int64 {
	if fi == nil {
		return -1
	}
	return fi.Size()
}
