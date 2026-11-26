package vault

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/product/producttest"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/settings"
)

// releaseServer serves a trimmed copy of a real vault index.json, recorded in
// the release package's test data.
func releaseServer(t *testing.T) *release.Client {
	t.Helper()
	body, err := os.ReadFile("../../release/testdata/vault-index.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vault/index.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return release.NewClientFor(srv.URL, srv.Client())
}

func TestTheContract(t *testing.T) {
	producttest.Contract(t, New(Options{Releases: releaseServer(t)}), producttest.Options{})
}

func TestEditionsAndBaseImages(t *testing.T) {
	p := New(Options{})
	if got := len(p.Editions()); got != 3 {
		t.Errorf("editions = %d, want community, Enterprise and Enterprise HSM", got)
	}
	tests := []struct {
		edition release.Edition
		want    string
	}{
		{release.Community, AlpineImage},
		{release.Enterprise, AlpineImage},
		// The HSM build is linked against glibc and does not run on Alpine (S2).
		{release.EnterpriseHSM, GlibcImage},
	}
	for _, tt := range tests {
		if got := p.BaseImage(tt.edition); got != tt.want {
			t.Errorf("BaseImage(%q) = %q, want %q", tt.edition, got, tt.want)
		}
	}
	if !strings.Contains(GlibcImage, "debian") || !strings.Contains(AlpineImage, "alpine") {
		t.Error("the base images are not the ones spike S2 proved")
	}
}

func TestVersionsComeFromTheReleaseSitePerEdition(t *testing.T) {
	p := New(Options{Releases: releaseServer(t)})
	ctx := context.Background()

	community, err := p.Versions(ctx, release.Community)
	if err != nil || len(community) == 0 || community[0].Full != "2.2.2" {
		t.Fatalf("community versions = %+v, %v, want 2.2.2 first", community, err)
	}
	ent, err := p.Versions(ctx, release.Enterprise)
	if err != nil || ent[0].Full != "2.2.2+ent" {
		t.Fatalf("enterprise versions = %+v, %v", ent, err)
	}
	hsm, err := p.Versions(ctx, release.EnterpriseHSM)
	if err != nil || hsm[0].Full != "2.2.2+ent.hsm" {
		t.Fatalf("hsm versions = %+v, %v", hsm, err)
	}

	// FIPS builds exist upstream but this product does not offer them.
	if _, err := p.Versions(ctx, release.Edition("ent.fips1403")); err == nil {
		t.Error("an edition the product does not support was listed")
	}
}

func TestTheLicenseIsASecretSettingsField(t *testing.T) {
	fields := New(Options{}).SettingsFields()
	if len(fields) != 1 || fields[0].Key != "vault.license" || !fields[0].Secret {
		t.Fatalf("fields = %+v, want one secret field named vault.license (R-SET-1, R-SET-3)", fields)
	}

	// The store built from the product's fields masks it.
	store, err := settings.Open(t.TempDir(), fields...)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(LicenseKey, "02MV4UU43BK5HGYYTOJZWFQMTMNNEWU33JJRKFC6SNKVNGSVJRLE6VKUZ5"); err != nil {
		t.Fatal(err)
	}
	if d := store.View().Fields[0].Display; strings.Contains(d, "02MV4") {
		t.Errorf("the license is shown as %q, want it masked", d)
	}
}

// settingsStub satisfies product.Settings with fixed answers.
type settingsStub struct {
	value string
	ok    bool
	err   error
}

func (s settingsStub) Get(string) (string, bool, error) { return s.value, s.ok, s.err }

func TestRequireLicense(t *testing.T) {
	tests := []struct {
		name    string
		edition release.Edition
		s       product.Settings
		want    string
		wantErr string
	}{
		{"community needs none", release.Community, settingsStub{}, "", ""},
		{"enterprise with a license", release.Enterprise, settingsStub{value: "LICENSE", ok: true}, "LICENSE", ""},
		{"hsm with a license", release.EnterpriseHSM, settingsStub{value: "LICENSE", ok: true}, "LICENSE", ""},
		{"enterprise with none saved", release.Enterprise, settingsStub{}, "", "add it in Settings"},
		{"enterprise with an empty one", release.Enterprise, settingsStub{ok: true}, "", "add it in Settings"},
		{"settings that cannot be read", release.Enterprise, settingsStub{err: errors.New("disk")}, "", "read the Vault license"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RequireLicense(tt.s, tt.edition)
			if tt.wantErr == "" {
				if err != nil || got != tt.want {
					t.Errorf("RequireLicense = %q, %v; want %q", got, err, tt.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("RequireLicense = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}

	_, err := RequireLicense(settingsStub{}, release.Enterprise)
	if !errors.Is(err, ErrNoLicense) {
		t.Errorf("error = %v, want it to match ErrNoLicense so the UI can say what to do", err)
	}
	if strings.Contains(err.Error(), "LICENSE") {
		t.Errorf("the error holds a license value: %v", err)
	}
}
