//go:build integration

package app

import (
	"context"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/runtime/podman"
)

// TestDeployFlowWithPodman is the M2 acceptance test against a real Podman
// machine. Needs: podman machine start, and a local alpine:3 image.
func TestDeployFlowWithPodman(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rt, err := podman.New(ctx)
	if err != nil {
		t.Skipf("Podman is not available: %v", err)
	}
	if err := rt.Ping(ctx); err != nil {
		t.Skipf("Podman is not reachable: %v", err)
	}
	deployFlow(t, rt, "docker.io/library/alpine:3", []string{"sleep", "300"})
}
