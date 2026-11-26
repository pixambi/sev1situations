// Package release lists, downloads, verifies and caches the HashiCorp product
// binaries from releases.hashicorp.com (R-RUN-6 to R-RUN-9).
//
// Versions are listed per product and edition, because the same number does not
// exist in every edition (Boundary community and Enterprise differ by a major
// version, spike S7). Downloads are checked against the SHA-256 in the
// published SHA256SUMS file. The GPG signature on that file is deliberately
// not checked (decision D7).
package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// DefaultBaseURL is the public release site.
const DefaultBaseURL = "https://releases.hashicorp.com"

// Product is a HashiCorp product the application deploys.
type Product string

// Products.
const (
	// Vault is HashiCorp Vault.
	Vault Product = "vault"
	// Boundary is HashiCorp Boundary.
	Boundary Product = "boundary"
)

// Edition is the build flavour of a product. Its value is the suffix that
// follows "+" in the version string.
type Edition string

// Editions. The FIPS builds exist upstream but are not offered.
const (
	// Community is the open source build. It has no suffix.
	Community Edition = ""
	// Enterprise is the Enterprise build.
	Enterprise Edition = "ent"
	// EnterpriseHSM is the Enterprise build with PKCS#11 HSM support. It links
	// against glibc, so it needs a glibc base image (spike S2).
	EnterpriseHSM Edition = "ent.hsm"
)

// ErrNotFound means the release site has no such product, version or build.
var ErrNotFound = errors.New("release not found")

// Version is one released version of a product in one edition.
type Version struct {
	// Product is the product.
	Product Product
	// Edition is the edition.
	Edition Edition
	// Core is the version without the edition suffix, for example 2.2.2.
	Core string
	// Full is the version as the release site spells it, for example 2.2.2+ent.
	Full string
	// Arches lists the linux architectures that have a build, for example arm64.
	Arches []string
}

// Client reads the release site. It fetches each product's index once and
// keeps it for the life of the client.
type Client struct {
	baseURL string
	http    *http.Client

	mu      sync.Mutex
	indexes map[Product]*index
}

// NewClient returns a client for the public release site.
func NewClient() *Client {
	return NewClientFor(DefaultBaseURL, http.DefaultClient)
}

// NewClientFor returns a client for another base URL, for tests and mirrors.
func NewClientFor(baseURL string, hc *http.Client) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: hc, indexes: map[Product]*index{}}
}

// index is the part of a product's index.json we use.
type index struct {
	Versions map[string]indexVersion `json:"versions"`
}

type indexVersion struct {
	SHASums string       `json:"shasums"`
	Builds  []indexBuild `json:"builds"`
}

type indexBuild struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Filename string `json:"filename"`
	URL      string `json:"url"`
}

func (c *Client) index(ctx context.Context, p Product) (*index, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if idx, ok := c.indexes[p]; ok {
		return idx, nil
	}

	var idx index
	if err := c.getJSON(ctx, fmt.Sprintf("%s/%s/index.json", c.baseURL, p), &idx); err != nil {
		return nil, fmt.Errorf("list %s releases: %w", p, err)
	}
	c.indexes[p] = &idx
	return &idx, nil
}

// Versions returns the stable versions of a product in one edition that have a
// linux build, newest first. Pre-releases are left out.
func (c *Client) Versions(ctx context.Context, p Product, e Edition) ([]Version, error) {
	idx, err := c.index(ctx, p)
	if err != nil {
		return nil, err
	}

	var out []Version
	for full, iv := range idx.Versions {
		core, suffix, _ := strings.Cut(full, "+")
		if strings.Contains(core, "-") || Edition(suffix) != e {
			continue
		}
		arches := linuxArches(iv.Builds)
		if len(arches) == 0 {
			continue
		}
		out = append(out, Version{Product: p, Edition: e, Core: core, Full: full, Arches: arches})
	}
	sort.Slice(out, func(i, j int) bool { return compare(out[i].Core, out[j].Core) > 0 })
	return out, nil
}

func linuxArches(builds []indexBuild) []string {
	seen := map[string]bool{}
	var out []string
	for _, b := range builds {
		if b.OS == "linux" && !seen[b.Arch] {
			seen[b.Arch] = true
			out = append(out, b.Arch)
		}
	}
	sort.Strings(out)
	return out
}

// compare orders two X.Y.Z versions numerically: negative, zero or positive.
func compare(a, b string) int {
	pa, pb := parts(a), parts(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] - pb[i]
		}
	}
	return 0
}

func parts(v string) [3]int {
	var out [3]int
	for i, s := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(s)
	}
	return out
}

func (c *Client) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", url, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", url, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("get %s: %w", url, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("get %s: %s", url, resp.Status)
	}
	return resp, nil
}

func (c *Client) getJSON(ctx context.Context, url string, out any) error {
	resp, err := c.get(ctx, url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", url, err)
	}
	return nil
}

func (c *Client) getText(ctx context.Context, url string) (string, error) {
	resp, err := c.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", url, err)
	}
	return string(b), nil
}
