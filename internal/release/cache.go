package release

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// maxBinarySize caps how much one extracted binary may write, as a guard
// against a malformed archive.
const maxBinarySize = 2 << 30

// ErrChecksum means a download did not match the published SHA-256. The
// download is discarded and nothing is cached (R-RUN-6).
var ErrChecksum = errors.New("checksum mismatch")

// Progress is called while a download runs. total is -1 if the size is unknown.
type Progress func(done, total int64)

// Cache keeps verified product binaries on disk, one per product, version and
// architecture, so each is downloaded once (R-RUN-8). It is safe to delete.
type Cache struct {
	dir    string
	client *Client

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewCache returns a cache rooted at dir that downloads through client.
func NewCache(dir string, client *Client) *Cache {
	return &Cache{dir: dir, client: client, locks: map[string]*sync.Mutex{}}
}

// Ensure returns the path of the verified binary for a version and linux
// architecture, downloading it first if it is not cached. A cached binary needs
// no network (R-RUN-9). progress may be nil.
func (k *Cache) Ensure(ctx context.Context, v Version, arch string, progress Progress) (string, error) {
	dest := k.path(v, arch)

	lock := k.lock(dest)
	lock.Lock()
	defer lock.Unlock()

	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	if err := k.fetch(ctx, v, arch, dest, progress); err != nil {
		return "", fmt.Errorf("fetch %s %s (%s): %w", v.Product, v.Full, arch, err)
	}
	return dest, nil
}

// path is where a version's binary lives: <dir>/<product>/<full version>/<arch>/<product>.
func (k *Cache) path(v Version, arch string) string {
	return filepath.Join(k.dir, string(v.Product), v.Full, arch, string(v.Product))
}

// lock returns the mutex for one destination, so two callers asking for the
// same binary download it once.
func (k *Cache) lock(dest string) *sync.Mutex {
	k.mu.Lock()
	defer k.mu.Unlock()
	if l, ok := k.locks[dest]; ok {
		return l
	}
	l := &sync.Mutex{}
	k.locks[dest] = l
	return l
}

func (k *Cache) fetch(ctx context.Context, v Version, arch, dest string, progress Progress) error {
	idx, err := k.client.index(ctx, v.Product)
	if err != nil {
		return err
	}
	iv, ok := idx.Versions[v.Full]
	if !ok {
		return fmt.Errorf("version %s: %w", v.Full, ErrNotFound)
	}
	build, ok := findBuild(iv, "linux", arch)
	if !ok {
		return fmt.Errorf("no linux/%s build of %s: %w", arch, v.Full, ErrNotFound)
	}

	sums, err := k.client.getText(ctx, fmt.Sprintf("%s/%s/%s/%s", k.client.baseURL, v.Product, v.Full, iv.SHASums))
	if err != nil {
		return fmt.Errorf("get checksums: %w", err)
	}
	want, ok := lookupSum(sums, build.Filename)
	if !ok {
		return fmt.Errorf("%s is not listed in %s: %w", build.Filename, iv.SHASums, ErrChecksum)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".download-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	got, err := k.download(ctx, build.URL, tmp, progress)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if got != want {
		_ = tmp.Close()
		return fmt.Errorf("%s: downloaded %s, published %s: %w", build.Filename, got, want, ErrChecksum)
	}

	if err := extractBinary(tmp.Name(), string(v.Product), dest); err != nil {
		_ = tmp.Close()
		return err
	}
	return tmp.Close()
}

// download streams the archive into f and returns its SHA-256 as it goes, so
// verification costs no extra pass over the file.
func (k *Cache) download(ctx context.Context, url string, f *os.File, progress Progress) (string, error) {
	resp, err := k.client.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	h := sha256.New()
	dst := io.MultiWriter(f, h)
	if progress != nil {
		dst = &progressWriter{w: dst, total: resp.ContentLength, fn: progress}
	}
	if _, err := io.Copy(dst, resp.Body); err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type progressWriter struct {
	w     io.Writer
	done  int64
	total int64
	fn    Progress
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	p.fn(p.done, p.total)
	return n, err
}

func findBuild(iv indexVersion, goos, arch string) (indexBuild, bool) {
	for _, b := range iv.Builds {
		if b.OS == goos && b.Arch == arch {
			return b, true
		}
	}
	return indexBuild{}, false
}

// lookupSum finds the digest for a file name in a SHA256SUMS body, whose lines
// are "<sha256>  <filename>".
func lookupSum(sums, name string) (string, bool) {
	sc := bufio.NewScanner(strings.NewReader(sums))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && f[1] == name {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

// extractBinary writes the entry named name from the zip at zipPath to dest as
// an executable. Only that one entry is read, so a hostile archive cannot write
// elsewhere. The file appears at dest only once it is complete.
func extractBinary(zipPath, name, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		if f.UncompressedSize64 > maxBinarySize {
			return fmt.Errorf("%s is too large (%d bytes)", name, f.UncompressedSize64)
		}
		src, err := f.Open()
		if err != nil {
			return fmt.Errorf("open %s in archive: %w", name, err)
		}
		defer func() { _ = src.Close() }()

		out, err := os.CreateTemp(filepath.Dir(dest), ".extract-*")
		if err != nil {
			return fmt.Errorf("create temporary file: %w", err)
		}
		defer func() { _ = os.Remove(out.Name()) }()

		if _, err := io.Copy(out, io.LimitReader(src, maxBinarySize)); err != nil {
			_ = out.Close()
			return fmt.Errorf("extract %s: %w", name, err)
		}
		if err := out.Close(); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
		if err := os.Chmod(out.Name(), 0o755); err != nil {
			return fmt.Errorf("make %s executable: %w", name, err)
		}
		if err := os.Rename(out.Name(), dest); err != nil {
			return fmt.Errorf("install %s: %w", name, err)
		}
		return nil
	}
	return fmt.Errorf("archive has no %q entry", name)
}
