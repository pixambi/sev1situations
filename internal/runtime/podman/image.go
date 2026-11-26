package podman

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/pixambi/sev1situations/internal/runtime"
)

// EnsureImage implements runtime.Runtime. It asks whether the image is local
// and pulls it only if it is not. Nothing here ever deletes an image (R-STATE-5).
func (r *Runtime) EnsureImage(ctx context.Context, ref string, progress func(string)) error {
	if ref == "" {
		return errors.New("ensure image: reference is required")
	}

	_, err := r.c.do(ctx, http.MethodGet, "/images/"+url.PathEscape(ref)+"/exists", nil, nil, nil)
	if err == nil {
		return nil
	}
	if !errors.Is(err, runtime.ErrNotFound) {
		return fmt.Errorf("check image %q: %w", ref, err)
	}
	return r.pull(ctx, ref, progress)
}

// pull downloads an image. Podman streams one JSON object per line, and reports
// a failure inside the stream as an "error" field rather than a status code.
func (r *Runtime) pull(ctx context.Context, ref string, progress func(string)) error {
	req, err := r.c.request(ctx, http.MethodPost, "/images/pull", url.Values{"reference": {ref}}, nil)
	if err != nil {
		return err
	}
	resp, err := r.c.send(req)
	if err != nil {
		return fmt.Errorf("pull image %q: %w", ref, err)
	}
	defer func() { _ = resp.Body.Close() }()

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var msg struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			return fmt.Errorf("pull image %q: unexpected output %q: %w", ref, line, err)
		}
		if msg.Error != "" {
			return fmt.Errorf("pull image %q: %s", ref, msg.Error)
		}
		if progress != nil && msg.Stream != "" {
			progress(strings.TrimSpace(msg.Stream))
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("pull image %q: %w", ref, err)
	}
	return nil
}
