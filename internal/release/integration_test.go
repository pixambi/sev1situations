//go:build integration

package release

import (
	"context"
	"testing"
	"time"
)

// TestRealSiteListing checks the live index still has the shape the client
// expects. It lists only and downloads nothing. Needs network access.
func TestRealSiteListing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := NewClient()

	for _, tt := range []struct {
		p Product
		e Edition
	}{
		{Vault, Community}, {Vault, Enterprise}, {Vault, EnterpriseHSM},
		{Boundary, Community}, {Boundary, Enterprise},
	} {
		got, err := c.Versions(ctx, tt.p, tt.e)
		if err != nil {
			t.Fatalf("%s %q: %v", tt.p, tt.e, err)
		}
		if len(got) == 0 {
			t.Errorf("%s %q: no versions listed", tt.p, tt.e)
			continue
		}
		if len(got[0].Arches) == 0 || got[0].Core == "" {
			t.Errorf("%s %q: newest version looks wrong: %+v", tt.p, tt.e, got[0])
		}
	}
}
