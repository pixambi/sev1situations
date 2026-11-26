package settings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	vaultLicense = Field{Key: "vault.license", Label: "Vault Enterprise license", Secret: true}
	region       = Field{Key: "general.region", Label: "Region"}
)

const secret = "02MV4UU43BK5HGYYTOJZWFQMTMNNEWU33JJRKFC6SNKVNGSVJRLE6VKUZ5"

func open(t *testing.T, dir string, fields ...Field) *Store {
	t.Helper()
	s, err := Open(dir, fields...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestValuesPersistAcrossLaunches(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, vaultLicense, region)
	if err := s.SetInstructor("  Ada Lovelace "); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("vault.license", secret+"\n"); err != nil { // pasted with a trailing newline
		t.Fatal(err)
	}
	if err := s.Set("general.region", "au"); err != nil {
		t.Fatal(err)
	}

	again := open(t, dir, vaultLicense, region)
	if got := again.Instructor(); got != "Ada Lovelace" {
		t.Errorf("Instructor = %q, want it trimmed and saved", got)
	}
	got, ok, err := again.Get("vault.license")
	if err != nil || !ok || got != secret {
		t.Errorf("Get = %q, %v, %v; want the license, trimmed", got, ok, err)
	}
}

func TestFileAndDirectoryPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Sev1Situations")
	s := open(t, dir, vaultLicense)
	if err := s.Set("vault.license", secret); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Stat(filepath.Join(dir, "settings.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("settings file mode = %v, %v; want 0600 (R-SET-3)", info.Mode().Perm(), err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("settings directory mode = %v, %v; want 0700", info.Mode().Perm(), err)
	}
}

func TestALooseFileIsTightenedOnOpen(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, vaultLicense)
	if err := s.Set("vault.license", secret); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	open(t, dir, vaultLicense)
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode after Open = %v, want it tightened to 0600", info.Mode().Perm())
	}
}

func TestViewMasksSecretsOnly(t *testing.T) {
	s := open(t, t.TempDir(), vaultLicense, region)
	if err := s.Set("vault.license", secret); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("general.region", "au"); err != nil {
		t.Fatal(err)
	}

	view := s.View()
	if len(view.Fields) != 2 || view.Fields[0].Key != "general.region" || view.Fields[1].Key != "vault.license" {
		t.Fatalf("fields = %+v, want them in key order", view.Fields)
	}
	if f := view.Fields[1]; !f.Set || f.Display == "" || strings.Contains(f.Display, secret[:6]) {
		t.Errorf("secret field = %+v, want it set and masked", f)
	}
	if f := view.Fields[0]; f.Display != "au" {
		t.Errorf("plain field display = %q, want the value", f.Display)
	}
}

func TestAnUnsetFieldShowsNothing(t *testing.T) {
	view := open(t, t.TempDir(), vaultLicense).View()
	if f := view.Fields[0]; f.Set || f.Display != "" {
		t.Errorf("unset field = %+v, want not set and no display", f)
	}
}

func TestNoErrorEverContainsAValue(t *testing.T) {
	s := open(t, t.TempDir(), vaultLicense)
	bad := secret + "\nsecond line"
	errs := []error{
		s.Set("vault.license", bad), // control character
		s.Set("vault.license", strings.Repeat("x", maxValueLen+1)),
		s.Set("not.registered", secret),
	}
	for i, err := range errs {
		if err == nil {
			t.Errorf("case %d: want an error", i)
			continue
		}
		if strings.Contains(err.Error(), secret[:10]) {
			t.Errorf("case %d: the error leaks the value: %v (R-QUAL-3)", i, err)
		}
	}
}

func TestErrorsMatchTheirSentinels(t *testing.T) {
	s := open(t, t.TempDir(), vaultLicense)
	if err := s.Set("nope", "x"); !errors.Is(err, ErrUnknownField) {
		t.Errorf("Set unknown = %v, want ErrUnknownField", err)
	}
	if _, _, err := s.Get("nope"); !errors.Is(err, ErrUnknownField) {
		t.Errorf("Get unknown = %v, want ErrUnknownField", err)
	}
	if err := s.Set("vault.license", "a\tb"); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("Set with a tab = %v, want ErrInvalidValue", err)
	}
	if err := s.SetInstructor(strings.Repeat("a", maxNameLen+1)); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("SetInstructor too long = %v, want ErrInvalidValue", err)
	}
}

func TestClearingAValue(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, vaultLicense)
	if err := s.Set("vault.license", secret); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("vault.license", "  "); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := open(t, dir, vaultLicense).Get("vault.license"); ok {
		t.Error("a cleared value came back after reopening")
	}
}

func TestRegisterRules(t *testing.T) {
	s := open(t, t.TempDir(), vaultLicense)
	if err := s.Register(vaultLicense); err == nil {
		t.Error("registering a key twice succeeded, want an error")
	}
	if err := s.Register(Field{Label: "no key"}); err == nil {
		t.Error("registering a field with no key succeeded")
	}
	// A product registered later shows up without any change to the page (R-SET-4).
	if err := s.Register(Field{Key: "boundary.license", Label: "Boundary Enterprise license", Secret: true}); err != nil {
		t.Fatal(err)
	}
	if n := len(s.View().Fields); n != 2 {
		t.Errorf("View has %d fields, want 2", n)
	}
}

func TestValuesOfUnregisteredFieldsSurviveASave(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, vaultLicense, Field{Key: "boundary.license", Label: "Boundary", Secret: true})
	if err := s.Set("boundary.license", "boundary-secret"); err != nil {
		t.Fatal(err)
	}

	// A later launch without the Boundary product still must not lose its value.
	s2 := open(t, dir, vaultLicense)
	if err := s2.Set("vault.license", secret); err != nil {
		t.Fatal(err)
	}
	s3 := open(t, dir, vaultLicense, Field{Key: "boundary.license", Label: "Boundary", Secret: true})
	if v, ok, _ := s3.Get("boundary.license"); !ok || v != "boundary-secret" {
		t.Errorf("the Boundary license was lost: %q, %v", v, ok)
	}
}

func TestACorruptFileIsReportedNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	content := `{"instructor": "Ada", "values": {"vault.license": "` + secret + `"` // truncated
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Open(dir, vaultLicense)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("Open = %v, want an error naming the file", err)
	}
	if strings.Contains(err.Error(), secret[:10]) {
		t.Errorf("the error leaks file content: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != content {
		t.Error("a corrupt settings file was overwritten")
	}
}

func TestDamagedFileMessagesAreReadable(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{"truncated", `{"instructor": "Ada", "values": {"vault.license": "` + secret, "syntax error at byte"},
		{"wrong type", `{"values": {"vault.license": 5}}`, "has the wrong type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Open(dir, vaultLicense)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Open = %v, want a message containing %q", err, tt.want)
			}
			if strings.Contains(err.Error(), "%!") || strings.Contains(err.Error(), secret[:10]) {
				t.Errorf("the message is malformed or leaks content: %v", err)
			}
		})
	}
}

func TestANewerFileFormatIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"version": 99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, vaultLicense); err == nil || !strings.Contains(err.Error(), "newer version") {
		t.Errorf("Open = %v, want a refusal for a newer format", err)
	}
}

func TestSavingLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, vaultLicense)
	for range 3 {
		if err := s.Set("vault.license", secret); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "settings.json" {
		t.Errorf("directory holds %v, want only settings.json", entries)
	}
}

func TestAFailedSaveKeepsTheOldValue(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, vaultLicense)
	if err := s.Set("vault.license", "old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil { // read-only: saving must fail
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := s.Set("vault.license", "new"); err == nil {
		t.Skip("the directory could still be written (running as root?)")
	}
	if v, _, _ := s.Get("vault.license"); v != "old" {
		t.Errorf("after a failed save the value is %q, want the memory to match the disk (old)", v)
	}
}
