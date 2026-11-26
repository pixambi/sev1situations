package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/settings"
)

func settingsOf(t *testing.T) (*SettingsService, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := settings.Open(dir,
		settings.Field{Key: "vault.license", Label: "Vault Enterprise license", Help: "Needed for Enterprise.", Secret: true},
		settings.Field{Key: ThemeKey, Label: "Theme"},
	)
	if err != nil {
		t.Fatal(err)
	}
	return NewSettingsService(store), dir
}

func field(v SettingsView, key string) SettingField {
	for _, f := range v.Fields {
		if f.Key == key {
			return f
		}
	}
	return SettingField{}
}

func TestSettingsListsEveryRegisteredFieldWithItsLabel(t *testing.T) {
	svc, _ := settingsOf(t)
	v := svc.Get()
	if len(v.Fields) != 2 {
		t.Fatalf("fields = %+v", v.Fields)
	}
	lic := field(v, "vault.license")
	if lic.Label != "Vault Enterprise license" || !lic.Secret || lic.Set || lic.Display != "" {
		t.Errorf("license = %+v", lic)
	}
}

func TestASavedLicenseIsShownMaskedAndNeverReturned(t *testing.T) {
	svc, dir := settingsOf(t)
	const secret = "02MV4UU43BK5.LICENSEVALUE.xyz"

	v, err := svc.SaveField("vault.license", "  "+secret+"\n")
	if err != nil {
		t.Fatal(err)
	}
	lic := field(v, "vault.license")
	if !lic.Set || lic.Display == "" || strings.Contains(lic.Display, "LICENSE") {
		t.Errorf("license = %+v, want set and masked", lic)
	}
	raw, _ := json.Marshal(svc.Get())
	if strings.Contains(string(raw), "LICENSEVALUE") {
		t.Errorf("the page's data holds the license: %s", raw)
	}

	// The core can still read it to deploy with it, and the file is private to the user.
	info, err := os.Stat(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("settings file mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestAnEmptyValueClearsASetting(t *testing.T) {
	svc, _ := settingsOf(t)
	_, _ = svc.SaveField("vault.license", "abc")
	v, err := svc.SaveField("vault.license", "")
	if err != nil || field(v, "vault.license").Set {
		t.Errorf("after clearing: %+v, %v", field(v, "vault.license"), err)
	}
}

func TestTheInstructorNameIsSavedTrimmedAndChecked(t *testing.T) {
	svc, _ := settingsOf(t)
	v, err := svc.SaveInstructor("  Ada Lovelace ")
	if err != nil || v.Instructor != "Ada Lovelace" {
		t.Fatalf("got %q, %v", v.Instructor, err)
	}
	for _, bad := range []string{strings.Repeat("x", 101), "two\nlines"} {
		if _, err := svc.SaveInstructor(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if got := svc.Get().Instructor; got != "Ada Lovelace" {
		t.Errorf("a refused name changed the saved one to %q", got)
	}
}

func TestOnlyKnownThemesAreAccepted(t *testing.T) {
	svc, _ := settingsOf(t)
	for _, ok := range []string{"light", "dark", ""} {
		if _, err := svc.SaveField(ThemeKey, ok); err != nil {
			t.Errorf("theme %q: %v", ok, err)
		}
	}
	v, err := svc.SaveField(ThemeKey, "neon")
	if err == nil || !strings.Contains(err.Error(), "light, dark") {
		t.Errorf("err = %v", err)
	}
	if field(v, ThemeKey).Display == "neon" {
		t.Error("a refused theme was saved")
	}
}

func TestErrorsNameTheProblemButNeverTheValue(t *testing.T) {
	svc, _ := settingsOf(t)
	_, err := svc.SaveField("nope", "secret-value")
	if err == nil || !strings.Contains(err.Error(), "no such setting") || strings.Contains(err.Error(), "secret-value") {
		t.Errorf("unknown field: %v", err)
	}
	long := "SECRETSTART" + strings.Repeat("x", 9000)
	_, err = svc.SaveField("vault.license", long)
	if err == nil || strings.Contains(err.Error(), "SECRETSTART") {
		t.Errorf("a long value: %v", err)
	}
	if _, err = svc.SaveField("vault.license", "line1\nline2"); err == nil || strings.Contains(err.Error(), "line1") {
		t.Errorf("a control character: %v", err)
	}
}
