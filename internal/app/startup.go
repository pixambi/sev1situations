package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/pixambi/sev1situations/internal/runtime"
)

// StartupReport says what start-up found and did.
type StartupReport struct {
	// RemovedRuns lists the leftover runs that were cleaned up completely.
	RemovedRuns []string
	// Failures lists leftover runs that could not be cleaned up completely.
	Failures []RunFailure
}

// RunFailure is a leftover run that could not be fully removed.
type RunFailure struct {
	// RunID is the run that was left behind.
	RunID string
	// Err says what failed.
	Err error
}

// Startup checks that the container runtime is reachable and removes what a
// previous launch left behind (F-10, F-11, R-STATE-3). A crash or kill -9 skips
// shutdown, so anything that carries this application's labels and is not part
// of the current launch is removed.
//
// It returns an error only if the runtime cannot be reached, in which case the
// caller should show it and offer to retry. It never tries to start the Podman
// machine (section 7). A run that cannot be fully removed is reported in the
// result and does not stop start-up.
func Startup(ctx context.Context, rt runtime.Runtime, log *slog.Logger) (StartupReport, error) {
	var report StartupReport

	if err := rt.Ping(ctx); err != nil {
		return report, fmt.Errorf("container runtime check: %w", err)
	}

	runs, err := rt.Runs(ctx)
	if err != nil {
		return report, fmt.Errorf("find leftover runs: %w", err)
	}

	for _, id := range runs {
		if err := rt.RemoveRun(ctx, id); err != nil {
			if errors.Is(err, context.Canceled) {
				return report, err
			}
			log.Warn("could not remove a leftover run", "run", id, "error", err)
			report.Failures = append(report.Failures, RunFailure{RunID: id, Err: err})
			continue
		}
		log.Info("removed a leftover run", "run", id)
		report.RemovedRuns = append(report.RemovedRuns, id)
	}
	return report, nil
}
