//go:build !production

package gui

import "testing"

func TestDevelopmentBuildsLeaveTheDevServerAlone(t *testing.T) {
	if assetMiddleware() != nil {
		t.Fatal("a development build applies the production policy, which would block hot reload")
	}
}
