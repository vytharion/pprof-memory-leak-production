package main

import (
	"log"
	"net/http"
)

func main() {
	addr := addrFromEnv()
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, newAppMux()); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
