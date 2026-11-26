package app

import (
	"testing"

	"github.com/pixambi/sev1situations/internal/settings"
)

func TestLicensedFollowsTheSavedLicense(t *testing.T) {
	store, err := settings.Open(t.TempDir(), settings.Field{Key: "vault.license", Label: "License", Secret: true})
	if err != nil {
		t.Fatal(err)
	}
	licensed := Licensed(store)

	if licensed("vault") {
		t.Error("vault counts as licensed with nothing saved")
	}
	if !licensed("boundary") {
		t.Error("a product with no license setting should need none")
	}
	if err := store.Set("vault.license", "02MV4UU43BK5"); err != nil {
		t.Fatal(err)
	}
	if !licensed("vault") {
		t.Error("vault does not count as licensed after saving one")
	}
}
