package main

import (
	"os"
	"runtime"
	"testing"
)

// TestMain sets MemProfileRate=1 so every allocation is recorded in the heap
// profile. The default (512 KiB sampling) hides the small-payload leak that
// this step wants to observe end-to-end through the pprof endpoint.
func TestMain(m *testing.M) {
	runtime.MemProfileRate = 1
	os.Exit(m.Run())
}
