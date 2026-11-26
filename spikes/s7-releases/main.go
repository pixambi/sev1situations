//go:build ignore

// Spike S7: list versions and editions from releases.hashicorp.com, download
// one archive and verify it against SHA256SUMS. Throwaway code.
// Run: go run spikes/s7-releases/main.go
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const base = "https://releases.hashicorp.com"

type index struct {
	Versions map[string]struct {
		Version string `json:"version"`
		SHASums string `json:"shasums"`
		Builds  []struct {
			OS       string `json:"os"`
			Arch     string `json:"arch"`
			Filename string `json:"filename"`
			URL      string `json:"url"`
		} `json:"builds"`
	} `json:"versions"`
}

func main() {
	for _, product := range []string{"vault", "boundary"} {
		var idx index
		getJSON(fmt.Sprintf("%s/%s/index.json", base, product), &idx)
		fmt.Printf("\n%s: %d versions in the index\n", product, len(idx.Versions))

		newest := map[string]string{} // edition -> newest stable version string
		for v := range idx.Versions {
			core, edition, stable := split(v)
			if !stable {
				continue
			}
			if cur, ok := newest[edition]; !ok || less(splitCore(cur), splitCore(core)) {
				newest[edition] = v
			}
		}
		editions := make([]string, 0, len(newest))
		for e := range newest {
			editions = append(editions, e)
		}
		sort.Strings(editions)
		for _, e := range editions {
			v := newest[e]
			var files []string
			for _, b := range idx.Versions[v].Builds {
				if b.OS == "linux" && (b.Arch == "arm64" || b.Arch == "amd64") {
					files = append(files, b.Filename)
				}
			}
			label := e
			if label == "" {
				label = "(community)"
			}
			fmt.Printf("  %-22s newest %-26s linux builds: %v\n", label, v, files)
		}
	}

	// Download and verify one archive: boundary community, linux/arm64.
	var idx index
	getJSON(base+"/boundary/index.json", &idx)
	var best string
	for v := range idx.Versions {
		core, edition, stable := split(v)
		if stable && edition == "" && (best == "" || less(splitCore(best), splitCore(core))) {
			best = v
		}
	}
	ver := idx.Versions[best]
	var url, name string
	for _, b := range ver.Builds {
		if b.OS == "linux" && b.Arch == "arm64" {
			url, name = b.URL, b.Filename
		}
	}
	dir, _ := os.MkdirTemp("", "s7-")
	defer os.RemoveAll(dir)
	fmt.Printf("\ndownloading %s\n", url)
	zip := filepath.Join(dir, name)
	size := download(url, zip)
	fmt.Printf("downloaded %d bytes\n", size)

	sums := string(getBytes(fmt.Sprintf("%s/boundary/%s/%s", base, best, ver.SHASums)))
	want := lookup(sums, name)
	got := sha256File(zip)
	fmt.Printf("SHA256SUMS entry: %s\ncomputed:         %s\nmatch: %v\n", want, got, want == got)

	// Corrupt one byte and confirm verification fails.
	f, _ := os.OpenFile(zip, os.O_RDWR, 0)
	_, _ = f.WriteAt([]byte{0xFF}, 100)
	f.Close()
	fmt.Printf("after corrupting one byte, match: %v\n", sha256File(zip) == want)
}

// split returns the core version, the edition suffix and whether it is a stable (non-prerelease) version.
func split(v string) (core, edition string, stable bool) {
	core, edition, _ = strings.Cut(v, "+")
	return core, edition, !strings.Contains(core, "-")
}

func splitCore(v string) [3]int {
	core, _, _ := strings.Cut(v, "+")
	var out [3]int
	for i, p := range strings.SplitN(core, ".", 3) {
		out[i], _ = strconv.Atoi(strings.SplitN(p, "-", 2)[0])
	}
	return out
}

func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// lookup finds "<sha>  <filename>" in a SHA256SUMS body.
func lookup(sums, name string) string {
	sc := bufio.NewScanner(strings.NewReader(sums))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && f[1] == name {
			return f[0]
		}
	}
	return "NOT FOUND"
}

func sha256File(path string) string {
	f, err := os.Open(path)
	must(err)
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	must(err)
	return hex.EncodeToString(h.Sum(nil))
}

func getBytes(url string) []byte {
	resp, err := http.Get(url)
	must(err)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		must(fmt.Errorf("GET %s: %s", url, resp.Status))
	}
	b, err := io.ReadAll(resp.Body)
	must(err)
	return b
}

func getJSON(url string, out any) { must(json.Unmarshal(getBytes(url), out)) }

func download(url, path string) int64 {
	resp, err := http.Get(url)
	must(err)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		must(fmt.Errorf("GET %s: %s", url, resp.Status))
	}
	f, err := os.Create(path)
	must(err)
	defer f.Close()
	n, err := io.Copy(f, resp.Body)
	must(err)
	return n
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}
