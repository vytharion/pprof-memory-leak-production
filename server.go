package main

import (
	"fmt"
	"net/http"
	nhpprof "net/http/pprof"
	"os"
)

const (
	defaultAddr      = ":8080"
	defaultAdminAddr = "127.0.0.1:6060"
)

func addrFromEnv() string {
	if v := os.Getenv("APP_ADDR"); v != "" {
		return v
	}
	return defaultAddr
}

func adminAddrFromEnv() string {
	if v := os.Getenv("APP_ADMIN_ADDR"); v != "" {
		return v
	}
	return defaultAdminAddr
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "world"
	}
	fmt.Fprintf(w, "hello, %s\n", name)
}

func newAppMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", helloHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	return mux
}

func newAdminMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", nhpprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", nhpprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", nhpprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", nhpprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", nhpprof.Trace)
	return mux
}
