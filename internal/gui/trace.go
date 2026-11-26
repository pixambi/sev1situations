package gui

import (
	"fmt"
	"os"
	"runtime/trace"
)

// startTrace begins a runtime/trace recording when S1S_TRACE is set, writing it
// to the file named by S1S_TRACE_FILE (default trace.out). The returned function
// stops the trace and must be called before the process exits.
// View the result with: go tool trace trace.out
func startTrace() (func(), error) {
	if os.Getenv("S1S_TRACE") == "" {
		return func() {}, nil
	}

	path := os.Getenv("S1S_TRACE_FILE")
	if path == "" {
		path = "trace.out"
	}

	traceFile, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create trace file: %w", err)
	}

	if err := trace.Start(traceFile); err != nil {
		_ = traceFile.Close()
		return nil, fmt.Errorf("start trace: %w", err)
	}

	return func() {
		trace.Stop()
		_ = traceFile.Close()
	}, nil
}
