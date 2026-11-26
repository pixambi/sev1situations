//go:build integration

package app

import (
	"os"
	osexec "os/exec"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/engine"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/report"
)

func userCache(t *testing.T) string {
	t.Helper()
	d, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("no user cache directory: %v", err)
	}
	return d
}

func shellOut(name string, args ...string) (string, error) {
	out, err := osexec.Command(name, args...).CombinedOutput()
	return string(out), err
}

// unsealKeysFrom reads the unseal keys the Cluster tab shows, in order.
func unsealKeysFrom(v product.ClusterView) []string {
	var keys []string
	for _, sec := range v.Sections {
		for _, f := range sec.Facts {
			if f.Secret && strings.HasPrefix(f.Label, "Unseal key") {
				keys = append(keys, f.Value)
			}
		}
	}
	return keys
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// rid is the ID of the report a finished run saved.
func rid(s engine.Snapshot) string { return report.NewID(s.RunID, s.StartedAt) }
