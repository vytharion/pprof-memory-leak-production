package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHelloHandler_DefaultsToWorld(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	rec := httptest.NewRecorder()

	newAppMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(rec.Body)
	if got := strings.TrimSpace(string(body)); got != "hello, world" {
		t.Fatalf("body = %q, want %q", got, "hello, world")
	}
}

func TestHelloHandler_EchoesNameQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/hello?name=vytharion", nil)
	rec := httptest.NewRecorder()

	newAppMux().ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Body)
	if got := strings.TrimSpace(string(body)); got != "hello, vytharion" {
		t.Fatalf("body = %q, want %q", got, "hello, vytharion")
	}
}

func TestHealthzHandler_ReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	newAppMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(rec.Body)
	if got := strings.TrimSpace(string(body)); got != "ok" {
		t.Fatalf("body = %q, want %q", got, "ok")
	}
}

func TestAddrFromEnv_UsesDefaultWhenUnset(t *testing.T) {
	t.Setenv("APP_ADDR", "")
	if got := addrFromEnv(); got != defaultAddr {
		t.Fatalf("addrFromEnv() = %q, want %q", got, defaultAddr)
	}
}

func TestAddrFromEnv_HonorsOverride(t *testing.T) {
	t.Setenv("APP_ADDR", ":9090")
	if got := addrFromEnv(); got != ":9090" {
		t.Fatalf("addrFromEnv() = %q, want %q", got, ":9090")
	}
}
