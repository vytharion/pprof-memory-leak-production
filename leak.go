package main

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type session struct {
	id        string
	payload   []byte
	events    chan string
	done      chan struct{}
	closeOnce sync.Once
	lastSeen  time.Time
}

const (
	workDefaultPayloadBytes = 4096
	defaultMaxSessions      = 32
)

var (
	sessionsMu  sync.Mutex
	sessions    = map[string]*session{}
	sessionSeq  uint64
	maxSessions = defaultMaxSessions
)

func workHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		id = nextSessionID()
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	s := newSession(id, body)
	registerSession(s)
	go drainEvents(s)

	fmt.Fprintf(w, "queued %s (%d bytes)\n", s.id, len(s.payload))
}

func newSession(id string, body []byte) *session {
	payload := padPayload(body, workDefaultPayloadBytes)
	return &session{
		id:       id,
		payload:  payload,
		events:   make(chan string),
		done:     make(chan struct{}),
		lastSeen: time.Now(),
	}
}

func padPayload(body []byte, minBytes int) []byte {
	if len(body) >= minBytes {
		return append([]byte(nil), body...)
	}
	buf := make([]byte, minBytes)
	copy(buf, body)
	return buf
}

func registerSession(s *session) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if prev, ok := sessions[s.id]; ok {
		prev.close()
	}
	sessions[s.id] = s
	for len(sessions) > maxSessions {
		evictOldestLocked()
	}
}

// evictOldestLocked drops the least-recently-seen session so the cache
// stays under maxSessions. The caller MUST hold sessionsMu.
func evictOldestLocked() {
	var (
		oldestID string
		oldest   time.Time
	)
	for id, s := range sessions {
		if oldestID == "" || s.lastSeen.Before(oldest) {
			oldestID = id
			oldest = s.lastSeen
		}
	}
	if oldestID == "" {
		return
	}
	s := sessions[oldestID]
	delete(sessions, oldestID)
	s.close()
}

// close signals the drain goroutine to exit. sync.Once makes it safe
// to call from an eviction, an explicit reset, or both.
func (s *session) close() {
	s.closeOnce.Do(func() { close(s.done) })
}

func drainEvents(s *session) {
	for {
		select {
		case <-s.done:
			return
		case _, ok := <-s.events:
			if !ok {
				return
			}
		}
	}
}

func nextSessionID() string {
	n := atomic.AddUint64(&sessionSeq, 1)
	return "sess-" + strconv.FormatUint(n, 10)
}

func activeSessions() int {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	return len(sessions)
}

func resetSessions() {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	for id, s := range sessions {
		s.close()
		delete(sessions, id)
	}
}
