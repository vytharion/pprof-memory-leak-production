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
	id       string
	payload  []byte
	events   chan string
	lastSeen time.Time
}

var (
	sessionsMu sync.Mutex
	sessions   = map[string]*session{}
	sessionSeq uint64
)

const workDefaultPayloadBytes = 4096

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
	sessions[s.id] = s
	sessionsMu.Unlock()
}

func drainEvents(s *session) {
	for range s.events {
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
	sessions = map[string]*session{}
}
