package main

import (
	"log"
	"net/http"
)

func main() {
	go serveAdmin()
	appAddr := addrFromEnv()
	log.Printf("app listening on %s", appAddr)
	if err := http.ListenAndServe(appAddr, newAppMux()); err != nil {
		log.Fatalf("app server exited: %v", err)
	}
}

func serveAdmin() {
	addr := adminAddrFromEnv()
	log.Printf("admin (pprof) listening on %s", addr)
	if err := http.ListenAndServe(addr, newAdminMux()); err != nil {
		log.Fatalf("admin server exited: %v", err)
	}
}
