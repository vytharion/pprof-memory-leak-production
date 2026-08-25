package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultProfileTimeout = 10 * time.Second

type profileClient struct {
	adminURL string
	client   *http.Client
}

func newProfileClient(adminURL string) *profileClient {
	return &profileClient{
		adminURL: strings.TrimRight(adminURL, "/"),
		client:   &http.Client{Timeout: defaultProfileTimeout},
	}
}

func (p *profileClient) captureHeap() ([]byte, error) {
	return p.fetch("/debug/pprof/heap?gc=1")
}

func (p *profileClient) captureHeapText() ([]byte, error) {
	return p.fetch("/debug/pprof/heap?debug=1&gc=1")
}

func (p *profileClient) captureGoroutineText() ([]byte, error) {
	return p.fetch("/debug/pprof/goroutine?debug=1")
}

func (p *profileClient) fetch(path string) ([]byte, error) {
	resp, err := p.client.Get(p.adminURL + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("profile fetch %s: status %d", path, resp.StatusCode)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, resp.Body); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func saveProfile(dir, name string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func isGzipPprof(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
}

// heapInuseBytesFromText reads the total in-use bytes from a debug=1 heap
// profile header. The header line looks like:
//
//	heap profile: <inuse_objects>: <inuse_bytes> [<alloc_objects>: <alloc_bytes>] @ heap/<rate>
func heapInuseBytesFromText(text []byte) (int64, error) {
	header, _, _ := bytes.Cut(text, []byte("\n"))
	fields := strings.Fields(string(header))
	if len(fields) < 4 {
		return 0, fmt.Errorf("unexpected heap header: %q", header)
	}
	raw := strings.TrimSuffix(fields[3], ":")
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse inuse bytes %q: %w", fields[3], err)
	}
	return v, nil
}
