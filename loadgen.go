package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultLoadTimeout = 5 * time.Second
)

type loadConfig struct {
	targetURL   string
	concurrency int
	requests    int
	payload     []byte
	client      *http.Client
}

type loadResult struct {
	sent     int
	ok       int
	failed   int
	totalDur time.Duration
	firstErr error
}

func runLoad(cfg loadConfig) loadResult {
	cfg = withLoadDefaults(cfg)

	var (
		wg       sync.WaitGroup
		ok       int64
		failed   int64
		errMu    sync.Mutex
		firstErr error
	)

	jobs := make(chan struct{}, cfg.requests)
	for i := 0; i < cfg.requests; i++ {
		jobs <- struct{}{}
	}
	close(jobs)

	start := time.Now()
	for w := 0; w < cfg.concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				if err := fireOne(cfg); err != nil {
					atomic.AddInt64(&failed, 1)
					recordFirstErr(&errMu, &firstErr, err)
					continue
				}
				atomic.AddInt64(&ok, 1)
			}
		}()
	}
	wg.Wait()

	return loadResult{
		sent:     cfg.requests,
		ok:       int(ok),
		failed:   int(failed),
		totalDur: time.Since(start),
		firstErr: firstErr,
	}
}

func withLoadDefaults(cfg loadConfig) loadConfig {
	if cfg.client == nil {
		cfg.client = &http.Client{Timeout: defaultLoadTimeout}
	}
	if cfg.concurrency < 1 {
		cfg.concurrency = 1
	}
	if cfg.requests < 1 {
		cfg.requests = 1
	}
	return cfg
}

func fireOne(cfg loadConfig) error {
	req, err := http.NewRequest(http.MethodPost, cfg.targetURL, bytes.NewReader(cfg.payload))
	if err != nil {
		return err
	}
	resp, err := cfg.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func recordFirstErr(mu *sync.Mutex, dst *error, err error) {
	mu.Lock()
	defer mu.Unlock()
	if *dst == nil {
		*dst = err
	}
}
