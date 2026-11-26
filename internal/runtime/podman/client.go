// Package podman implements runtime.Runtime over Podman's REST API on the
// machine's unix socket, using only net/http (R-LANG-5, spike S1).
package podman

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/pixambi/sev1situations/internal/runtime"
)

// apiBase pins the libpod API version. It is our own constant, not the client's
// version: the machine's engine can be older than the installed client (S1).
const apiBase = "http://podman/v5.0.0/libpod"

// client talks HTTP to a unix socket.
type client struct {
	socket string
	http   *http.Client
}

func newClient(socket string) *client {
	return &client{
		socket: socket,
		http: &http.Client{Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			},
		}},
	}
}

// apiError is an error response from Podman: {"cause","message","response"}.
type apiError struct {
	Status  int
	Message string
	Cause   string
}

func (e *apiError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("podman: %s (status %d)", e.Message, e.Status)
	}
	return fmt.Sprintf("podman: status %d", e.Status)
}

// Is makes a 404 match runtime.ErrNotFound.
func (e *apiError) Is(target error) bool {
	return target == runtime.ErrNotFound && e.Status == http.StatusNotFound
}

// unavailable explains that the socket cannot be reached.
func (c *client) unavailable(err error) error {
	return fmt.Errorf("%w: cannot reach the Podman socket %s: %w. Is the machine running? Try: podman machine start",
		runtime.ErrUnavailable, c.socket, err)
}

// request builds a request against the libpod API.
func (c *client) request(ctx context.Context, method, path string, query url.Values, body io.Reader) (*http.Request, error) {
	u := apiBase + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, fmt.Errorf("build request %s %s: %w", method, path, err)
	}
	return req, nil
}

// send performs a request. It returns the response for the caller to read and
// close, or an error for a transport failure or an error status.
func (c *client) send(req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, c.unavailable(err)
	}
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotModified {
		defer func() { _ = resp.Body.Close() }()
		return nil, decodeError(resp)
	}
	return resp, nil
}

func decodeError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	e := &apiError{Status: resp.StatusCode}
	var payload struct {
		Cause   string `json:"cause"`
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &payload) == nil {
		e.Message, e.Cause = payload.Message, payload.Cause
	} else {
		e.Message = strings.TrimSpace(string(b))
	}
	return e
}

// do sends a JSON request and decodes a JSON response into out, if out is not nil.
// It returns the status code of a successful response.
func (c *client) do(ctx context.Context, method, path string, query url.Values, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, fmt.Errorf("encode request %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(b)
	}
	req, err := c.request(ctx, method, path, query, body)
	if err != nil {
		return 0, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.send(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode response %s %s: %w", method, path, err)
		}
	}
	return resp.StatusCode, nil
}

// ping calls the compatibility ping endpoint, which does not need the API version.
func (c *client) ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://podman/_ping", nil)
	if err != nil {
		return fmt.Errorf("build ping: %w", err)
	}
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}
