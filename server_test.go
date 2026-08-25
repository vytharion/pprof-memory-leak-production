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

func TestAdminMux_ExposesPprofIndex(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()

	newAdminMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin /debug/pprof/ = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAdminMux_ExposesHeapProfile(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/heap", nil)
	rec := httptest.NewRecorder()

	newAdminMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin /debug/pprof/heap = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAdminMux_ExposesGoroutineProfile(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/goroutine", nil)
	rec := httptest.NewRecorder()

	newAdminMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin /debug/pprof/goroutine = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAppMux_DoesNotLeakPprof(t *testing.T) {
	paths := []string{
		"/debug/pprof/",
		"/debug/pprof/heap",
		"/debug/pprof/goroutine",
		"/debug/pprof/cmdline",
	}
	for _, p := range paths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()

		newAppMux().ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("public %s = %d, want %d (pprof must not leak on the app port)", p, rec.Code, http.StatusNotFound)
		}
	}
}

func TestAdminAddrFromEnv_DefaultsToLoopback(t *testing.T) {
	t.Setenv("APP_ADMIN_ADDR", "")
	got := adminAddrFromEnv()
	if got != defaultAdminAddr {
		t.Fatalf("adminAddrFromEnv() = %q, want %q", got, defaultAdminAddr)
	}
	if !strings.HasPrefix(got, "127.0.0.1") {
		t.Fatalf("admin default = %q, want loopback bind (127.0.0.1:...)", got)
	}
}

func TestAdminAddrFromEnv_HonorsOverride(t *testing.T) {
	t.Setenv("APP_ADMIN_ADDR", "127.0.0.1:7070")
	if got := adminAddrFromEnv(); got != "127.0.0.1:7070" {
		t.Fatalf("adminAddrFromEnv() = %q, want %q", got, "127.0.0.1:7070")
	}
}
