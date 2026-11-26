package main

import (
	"log"
	"os"
	"runtime/trace"
)

// startTrace begins a runtime/trace recording when S1S_TRACE is set, writing it
// to the file named by S1S_TRACE_FILE (default trace.out). The returned function
// stops the trace and must be called before the process exits.
// View the result with: go tool trace trace.out
func startTrace() func() {
	if os.Getenv("S1S_TRACE") == "" {
		return func() {}
	}

	path := os.Getenv("S1S_TRACE_FILE")
	if path == "" {
		path = "trace.out"
	}

	traceFile, err := os.Create(path)
	if err != nil {
		log.Fatalf("%s could not be created: %v", path, err)
	}

	if err := trace.Start(traceFile); err != nil {
		_ = traceFile.Close()
		log.Fatalf("trace.Start could not start: %v", err)
	}

	return func() {
		trace.Stop()
		_ = traceFile.Close()
	}
}
