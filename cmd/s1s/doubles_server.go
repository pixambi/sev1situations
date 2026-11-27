//go:build server

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"time"

	"github.com/pixambi/sev1situations/internal/app"
	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/product/fake"
	"github.com/pixambi/sev1situations/internal/release"
	runtimefake "github.com/pixambi/sev1situations/internal/runtime/fake"
)

// useTestDoubles puts the application on a fake runtime, a fake product named vault and a local release site, when
// S1S_E2E_FIXTURES names a folder of scenarios. The end-to-end tests use it to cover the run page without containers. It is
// compiled only into the server build, which is never released.
func useTestDoubles(opts *app.Options) error {
	dir := os.Getenv("S1S_E2E_FIXTURES")
	if dir == "" {
		return nil
	}
	site, err := releaseSite()
	if err != nil {
		return err
	}

	p := fake.New("vault")
	p.Prims = outagePrimitives()
	reg := product.NewRegistry()
	if err := reg.Register(p); err != nil {
		return err
	}
	opts.Products = reg
	opts.Builtin = os.DirFS(dir)
	opts.Runtime = runtimefake.New()
	opts.Releases = release.NewClientFor(site.URL, site.Client())
	return nil
}

// releaseSite serves a "vault" release whose binary is a script, for every version the fake product offers.
func releaseSite() (*httptest.Server, error) {
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, err := zw.Create("vault")
	if err != nil {
		return nil, err
	}
	if _, err := w.Write([]byte("#!/bin/sh\n")); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	sum := hex.EncodeToString(func() []byte { s := sha256.Sum256(zbuf.Bytes()); return s[:] }())

	versions := []string{"2.0.0", "1.9.0", "1.8.0"}
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/vault/index.json", func(w http.ResponseWriter, _ *http.Request) {
		out := `{"versions":{`
		for i, v := range versions {
			if i > 0 {
				out += ","
			}
			out += fmt.Sprintf(`%q:{"shasums":"SHA256SUMS","builds":[`, v)
			for j, arch := range []string{"arm64", "amd64"} {
				if j > 0 {
					out += ","
				}
				name := fmt.Sprintf("vault_%s_linux_%s.zip", v, arch)
				out += fmt.Sprintf(`{"os":"linux","arch":%q,"filename":%q,"url":"%s/files/%s"}`, arch, name, srv.URL, name)
			}
			out += "]}"
		}
		fmt.Fprint(w, out+"}}")
	})
	for _, v := range versions {
		v := v
		mux.HandleFunc("/vault/"+v+"/SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) {
			for _, arch := range []string{"arm64", "amd64"} {
				fmt.Fprintf(w, "%s  vault_%s_linux_%s.zip\n", sum, v, arch)
			}
		})
	}
	mux.HandleFunc("/files/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(zbuf.Bytes()) })
	srv = httptest.NewServer(mux)
	return srv, nil
}

// outage is the state the test primitives share: whether the fault is in place, and how many times the fix has been looked for.
type outage struct {
	mu     sync.Mutex
	broken bool
	looks  int
}

func outagePrimitives() []primitive.Primitive {
	o := &outage{}
	spec := func(name, summary string) primitive.Spec {
		return primitive.Spec{Name: name, Summary: summary, Timeout: 5 * time.Second}
	}
	return []primitive.Primitive{
		testPrim{spec("vault.e2e-break", "Puts the test fault in place."), func() (bool, string) {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.broken, o.looks = true, 0
			return true, "the fault is in place"
		}},
		testPrim{spec("vault.e2e-check-broken", "Passes while the test fault is in place."), func() (bool, string) {
			o.mu.Lock()
			defer o.mu.Unlock()
			return o.broken, "the fault is in place"
		}},
		// The first look finds it still broken, as a trainee who has not fixed it yet. Every later look finds it fixed.
		testPrim{spec("vault.e2e-check-fixed", "Fails on the first look and passes after."), func() (bool, string) {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.looks++
			if o.looks < 2 {
				return false, "the cluster is still broken"
			}
			o.broken = false
			return true, "the cluster is fixed"
		}},
	}
}

// testPrim is a primitive that runs one function.
type testPrim struct {
	spec primitive.Spec
	run  func() (bool, string)
}

func (t testPrim) Spec() primitive.Spec { return t.spec }

func (t testPrim) Run(_ context.Context, _ primitive.Env, _ primitive.Args) (primitive.Result, error) {
	ok, msg := t.run()
	if ok {
		return primitive.Result{Status: primitive.Pass, Message: msg}, nil
	}
	return primitive.Result{Status: primitive.Fail, Message: msg}, nil
}
