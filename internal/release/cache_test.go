package release

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

const fakeBinary = "#!/bin/sh\necho vault\n"

// releaseSite is a fake releases.hashicorp.com serving one version of vault.
type releaseSite struct {
	srv      *httptest.Server
	zipBytes []byte
	zipHits  atomic.Int32
	allHits  atomic.Int32
	// sums overrides the SHA256SUMS body when not empty.
	sums string
}

func makeZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newSite(t *testing.T, entries map[string]string) *releaseSite {
	t.Helper()
	s := &releaseSite{zipBytes: makeZip(t, entries)}
	const name = "vault_1.0.0_linux_arm64.zip"

	mux := http.NewServeMux()
	mux.HandleFunc("/vault/index.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"versions":{"1.0.0":{"shasums":"vault_1.0.0_SHA256SUMS","builds":[{"os":"linux","arch":"arm64","filename":%q,"url":"%s/files/%s"}]}}}`,
			name, s.srv.URL, name)
	})
	mux.HandleFunc("/vault/1.0.0/vault_1.0.0_SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) {
		if s.sums != "" {
			fmt.Fprint(w, s.sums)
			return
		}
		sum := sha256.Sum256(s.zipBytes)
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	})
	mux.HandleFunc("/files/"+name, func(w http.ResponseWriter, _ *http.Request) {
		s.zipHits.Add(1)
		_, _ = w.Write(s.zipBytes)
	})

	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.allHits.Add(1)
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *releaseSite) cache(t *testing.T) (*Cache, string) {
	t.Helper()
	dir := t.TempDir()
	return NewCache(dir, NewClientFor(s.srv.URL, s.srv.Client())), dir
}

var vault100 = Version{Product: Vault, Edition: Community, Core: "1.0.0", Full: "1.0.0"}

// regularFiles lists every file under dir.
func regularFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEnsureDownloadsVerifiesAndCaches(t *testing.T) {
	site := newSite(t, map[string]string{"vault": fakeBinary, "LICENSE.txt": "license"})
	cache, dir := site.cache(t)

	var last, calls int64
	var total int64
	path, err := cache.Ensure(context.Background(), vault100, "arm64", func(done, tot int64) {
		calls++
		last, total = done, tot
	})
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(dir, "vault", "1.0.0", "arm64", "vault")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != fakeBinary {
		t.Errorf("binary = %q, %v", got, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", info.Mode().Perm())
	}
	if files := regularFiles(t, dir); len(files) != 1 {
		t.Errorf("cache holds %v, want only the binary (no archive, no temporary files)", files)
	}
	if calls == 0 || last != int64(len(site.zipBytes)) || total != int64(len(site.zipBytes)) {
		t.Errorf("progress: %d calls, last=%d total=%d, want it to reach %d", calls, last, total, len(site.zipBytes))
	}
}

func TestEnsureUsesTheCacheWithoutTheNetwork(t *testing.T) {
	site := newSite(t, map[string]string{"vault": fakeBinary})
	cache, _ := site.cache(t)
	if _, err := cache.Ensure(context.Background(), vault100, "arm64", nil); err != nil {
		t.Fatal(err)
	}

	site.srv.Close() // R-RUN-9: the second use needs no network
	before := site.allHits.Load()
	path, err := cache.Ensure(context.Background(), vault100, "arm64", nil)
	if err != nil {
		t.Fatalf("second Ensure with the site down: %v", err)
	}
	if site.allHits.Load() != before {
		t.Error("the second Ensure made a request")
	}
	if _, err := os.Stat(path); err != nil {
		t.Error(err)
	}
}

func TestEnsureRejectsAChecksumMismatch(t *testing.T) {
	site := newSite(t, map[string]string{"vault": fakeBinary})
	site.sums = "0000000000000000000000000000000000000000000000000000000000000000  vault_1.0.0_linux_arm64.zip\n"
	cache, dir := site.cache(t)

	_, err := cache.Ensure(context.Background(), vault100, "arm64", nil)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("Ensure = %v, want ErrChecksum", err)
	}
	if files := regularFiles(t, dir); len(files) != 0 {
		t.Errorf("a failed verification left %v in the cache (R-RUN-6)", files)
	}
}

func TestEnsureRejectsAFileMissingFromTheChecksums(t *testing.T) {
	site := newSite(t, map[string]string{"vault": fakeBinary})
	site.sums = "abc123  some_other_file.zip\n"
	cache, dir := site.cache(t)

	if _, err := cache.Ensure(context.Background(), vault100, "arm64", nil); !errors.Is(err, ErrChecksum) {
		t.Fatalf("Ensure = %v, want ErrChecksum", err)
	}
	if files := regularFiles(t, dir); len(files) != 0 {
		t.Errorf("left %v in the cache", files)
	}
}

func TestEnsureErrors(t *testing.T) {
	tests := []struct {
		name    string
		entries map[string]string
		version Version
		arch    string
		want    error
	}{
		{"unknown version", map[string]string{"vault": fakeBinary}, Version{Product: Vault, Full: "9.9.9"}, "arm64", ErrNotFound},
		{"no build for the architecture", map[string]string{"vault": fakeBinary}, vault100, "amd64", ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site := newSite(t, tt.entries)
			cache, dir := site.cache(t)
			if _, err := cache.Ensure(context.Background(), tt.version, tt.arch, nil); !errors.Is(err, tt.want) {
				t.Errorf("Ensure = %v, want %v", err, tt.want)
			}
			if files := regularFiles(t, dir); len(files) != 0 {
				t.Errorf("left %v in the cache", files)
			}
		})
	}
}

func TestEnsureOnlyReadsTheProductEntry(t *testing.T) {
	// A verified archive can still hold entries that must never be written.
	site := newSite(t, map[string]string{
		"../../escape": "evil", "/abs/path": "evil", "sub/vault": "wrong one", "LICENSE.txt": "license",
	})
	cache, dir := site.cache(t)

	if _, err := cache.Ensure(context.Background(), vault100, "arm64", nil); err == nil {
		t.Fatal("Ensure succeeded on an archive with no vault entry, want an error")
	}
	if files := regularFiles(t, dir); len(files) != 0 {
		t.Errorf("left %v in the cache", files)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "escape")); err == nil {
		t.Error("an entry escaped the cache directory")
	}
}

func TestConcurrentEnsureDownloadsOnce(t *testing.T) {
	site := newSite(t, map[string]string{"vault": fakeBinary})
	cache, _ := site.cache(t)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cache.Ensure(context.Background(), vault100, "arm64", nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := site.zipHits.Load(); n != 1 {
		t.Errorf("the archive was downloaded %d times, want once", n)
	}
}

func TestEnsureStopsWhenCancelled(t *testing.T) {
	site := newSite(t, map[string]string{"vault": fakeBinary})
	cache, dir := site.cache(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := cache.Ensure(ctx, vault100, "arm64", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("Ensure with a cancelled context = %v, want context.Canceled", err)
	}
	if files := regularFiles(t, dir); len(files) != 0 {
		t.Errorf("left %v in the cache", files)
	}
}

func TestLookupSum(t *testing.T) {
	sums := "ABC123  vault_1.0.0_linux_amd64.zip\ndef456  vault_1.0.0_linux_arm64.zip\n"
	if got, ok := lookupSum(sums, "vault_1.0.0_linux_arm64.zip"); !ok || got != "def456" {
		t.Errorf("lookupSum = %q, %v", got, ok)
	}
	if got, _ := lookupSum(sums, "vault_1.0.0_linux_amd64.zip"); got != "abc123" {
		t.Errorf("digests are compared in lower case, got %q", got)
	}
	if _, ok := lookupSum(sums, "missing.zip"); ok {
		t.Error("found a file that is not listed")
	}
}
