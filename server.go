package main

import (
	"fmt"
	"net/http"
	"os"
)

const defaultAddr = ":8080"

func addrFromEnv() string {
	if v := os.Getenv("APP_ADDR"); v != "" {
		return v
	}
	return defaultAddr
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
