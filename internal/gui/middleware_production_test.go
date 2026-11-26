//go:build production

package gui

import "testing"

func TestProductionBuildsApplyThePolicy(t *testing.T) {
	if assetMiddleware() == nil {
		t.Fatal("a production build serves pages without the Content-Security-Policy")
	}
}
