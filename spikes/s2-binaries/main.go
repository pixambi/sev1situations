//go:build ignore

// Spike S2: do the HashiCorp binaries run on the candidate base images?
// Downloads the newest stable linux/arm64 build of each product and edition
// (verified against SHA256SUMS), then runs `<binary> version` in each image.
// Throwaway code. Run: go run spikes/s2-binaries/main.go
package main

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const base = "https://releases.hashicorp.com"

var images = []string{"docker.io/library/alpine:3", "docker.io/library/debian:stable-slim"}

var targets = []struct{ product, suffix string }{
	{"vault", ""},
	{"vault", "ent"},
	{"vault", "ent.hsm"},
	{"boundary", ""},
	{"boundary", "ent"},
}

type index struct {
	Versions map[string]struct {
		SHASums string `json:"shasums"`
		Builds  []struct{ OS, Arch, Filename, URL string }
	}
}

func main() {
	cache := filepath.Join(must1(os.UserCacheDir()), "sev1situations-spike-s2")
	must(os.MkdirAll(cache, 0o755))
	fmt.Println("cache:", cache)

	for _, t := range targets {
		var idx index
		must(json.Unmarshal(get(fmt.Sprintf("%s/%s/index.json", base, t.product)), &idx))
		ver := newest(idx, t.suffix)
		v := idx.Versions[ver]
		var url, name string
		for _, b := range v.Builds {
			if b.OS == "linux" && b.Arch == "arm64" {
				url, name = b.URL, b.Filename
			}
		}
		dir := filepath.Join(cache, strings.ReplaceAll(ver, "+", "_")+"_"+t.product)
		bin := filepath.Join(dir, t.product)
		if _, err := os.Stat(bin); err != nil {
			must(os.MkdirAll(dir, 0o755))
			zipPath := filepath.Join(dir, name)
			download(url, zipPath)
			want := lookup(string(get(fmt.Sprintf("%s/%s/%s/%s", base, t.product, ver, v.SHASums))), name)
			if got := sha256File(zipPath); got != want {
				must(fmt.Errorf("%s: checksum mismatch", name))
			}
			unzip(zipPath, dir)
			os.Remove(zipPath)
		}
		fmt.Printf("\n%s %s (checksum ok)\n", t.product, ver)
		for _, img := range images {
			out, err := exec.Command("podman", "run", "--rm", "-v", dir+":/b:ro,z", img, "/b/"+t.product, "version").CombinedOutput()
			status := "RUNS"
			if err != nil {
				status = "FAILS"
			}
			fmt.Printf("  %-38s %-5s %s\n", img, status, firstLine(string(out)))
		}
	}
}

func newest(idx index, suffix string) string {
	best, bestV := "", [3]int{}
	for v := range idx.Versions {
		core, ed, _ := strings.Cut(v, "+")
		if strings.Contains(core, "-") || ed != suffix {
			continue
		}
		var n [3]int
		for i, p := range strings.SplitN(core, ".", 3) {
			n[i], _ = strconv.Atoi(p)
		}
		if best == "" || n[0] > bestV[0] || n[0] == bestV[0] && (n[1] > bestV[1] || n[1] == bestV[1] && n[2] > bestV[2]) {
			best, bestV = v, n
		}
	}
	return best
}

func unzip(zipPath, dir string) {
	r := must1(zip.OpenReader(zipPath))
	defer r.Close()
	for _, f := range r.File {
		rc := must1(f.Open())
		out := must1(os.OpenFile(filepath.Join(dir, filepath.Base(f.Name)), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755))
		_, err := io.Copy(out, rc)
		must(err)
		out.Close()
		rc.Close()
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func lookup(sums, name string) string {
	sc := bufio.NewScanner(strings.NewReader(sums))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && f[1] == name {
			return f[0]
		}
	}
	return ""
}

func sha256File(p string) string {
	f := must1(os.Open(p))
	defer f.Close()
	h := sha256.New()
	_, err := io.Copy(h, f)
	must(err)
	return hex.EncodeToString(h.Sum(nil))
}

func get(url string) []byte {
	resp := must1(http.Get(url))
	defer resp.Body.Close()
	return must1(io.ReadAll(resp.Body))
}

func download(url, path string) {
	resp := must1(http.Get(url))
	defer resp.Body.Close()
	f := must1(os.Create(path))
	defer f.Close()
	_, err := io.Copy(f, resp.Body)
	must(err)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func must1[T any](v T, err error) T { must(err); return v }
