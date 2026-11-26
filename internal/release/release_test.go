package release

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// fixtureServer serves testdata/vault-index.json, a trimmed copy of a real
// response from releases.hashicorp.com, and counts the requests it gets.
func fixtureServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	body, err := os.ReadFile("testdata/vault-index.json")
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/vault/index.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func cores(vs []Version) string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Full
	}
	return strings.Join(out, " ")
}

func TestVersionsPerEdition(t *testing.T) {
	srv, _ := fixtureServer(t)
	c := NewClientFor(srv.URL, srv.Client())

	tests := []struct {
		name    string
		edition Edition
		want    string
	}{
		{"community, newest first, no pre-releases", Community, "2.2.2 2.2.1 2.2.0 1.21.2 1.21.0"},
		{"enterprise sorts numerically, not as text", Enterprise, "2.2.2+ent 2.2.1+ent 1.21.10+ent 1.21.0+ent"},
		{"hsm is its own edition", EnterpriseHSM, "2.2.2+ent.hsm 2.2.1+ent.hsm"},
		{"other editions are listed only when asked for by name", Edition("ent.fips1403"), "2.2.2+ent.fips1403"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.Versions(context.Background(), Vault, tt.edition)
			if err != nil {
				t.Fatal(err)
			}
			if cores(got) != tt.want {
				t.Errorf("Versions = %q, want %q", cores(got), tt.want)
			}
		})
	}
}

func TestVersionFields(t *testing.T) {
	srv, _ := fixtureServer(t)
	c := NewClientFor(srv.URL, srv.Client())

	got, err := c.Versions(context.Background(), Vault, EnterpriseHSM)
	if err != nil {
		t.Fatal(err)
	}
	v := got[0]
	if v.Product != Vault || v.Edition != EnterpriseHSM || v.Core != "2.2.2" || v.Full != "2.2.2+ent.hsm" {
		t.Errorf("version = %+v", v)
	}
	// The real HSM release also has an s390x build. The application deploys only
	// the ones the Podman machine runs, so Arches reports what upstream lists.
	if strings.Join(v.Arches, ",") != "amd64,arm64,s390x" {
		t.Errorf("arches = %v, want the linux architectures only, sorted", v.Arches)
	}
}

func TestIndexIsFetchedOnce(t *testing.T) {
	srv, hits := fixtureServer(t)
	c := NewClientFor(srv.URL, srv.Client())

	for _, e := range []Edition{Community, Enterprise, EnterpriseHSM} {
		if _, err := c.Versions(context.Background(), Vault, e); err != nil {
			t.Fatal(err)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("the release site was asked %d times, want the index fetched once", n)
	}
}

func TestUnknownProductIsNotFound(t *testing.T) {
	srv, _ := fixtureServer(t)
	c := NewClientFor(srv.URL, srv.Client())

	_, err := c.Versions(context.Background(), Product("nomad"), Community)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Versions of an unknown product = %v, want ErrNotFound", err)
	}
}

func TestServerErrorIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := NewClientFor(srv.URL, srv.Client()).Versions(context.Background(), Vault, Community)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("Versions = %v, want an error naming the status", err)
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int // sign
	}{
		{"1.21.10", "1.21.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.2.3", "1.2.3", 0},
		{"0.21.3", "1.1.1", -1},
	}
	for _, tt := range tests {
		got := compare(tt.a, tt.b)
		if (got > 0) != (tt.want > 0) || (got < 0) != (tt.want < 0) {
			t.Errorf("compare(%q, %q) = %d, want sign %d", tt.a, tt.b, got, tt.want)
		}
	}
}
