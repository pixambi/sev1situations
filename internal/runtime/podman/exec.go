package podman

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pixambi/sev1situations/internal/runtime"
)

// Exec implements runtime.Runtime.
func (r *Runtime) Exec(ctx context.Context, id string, cmd []string, o runtime.ExecOptions) (runtime.ExecResult, error) {
	if _, err := r.owned(ctx, id); err != nil {
		return runtime.ExecResult{}, err
	}
	execID, err := r.createExec(ctx, id, execSpec(cmd, o, false))
	if err != nil {
		return runtime.ExecResult{}, err
	}

	if o.Detach {
		if _, err := r.c.do(ctx, http.MethodPost, "/exec/"+execID+"/start", nil, map[string]any{"Detach": true}, nil); err != nil {
			return runtime.ExecResult{}, fmt.Errorf("start exec in %q: %w", id, err)
		}
		return runtime.ExecResult{}, nil
	}

	req, err := r.c.request(ctx, http.MethodPost, "/exec/"+execID+"/start", nil, jsonBody(map[string]any{"Detach": false}))
	if err != nil {
		return runtime.ExecResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.c.send(req)
	if err != nil {
		return runtime.ExecResult{}, fmt.Errorf("start exec in %q: %w", id, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var stdout, stderr bytes.Buffer
	if err := demux(resp.Body, &stdout, &stderr); err != nil {
		return runtime.ExecResult{}, fmt.Errorf("read exec output from %q: %w", id, err)
	}

	var info struct {
		ExitCode int `json:"ExitCode"`
	}
	if _, err := r.c.do(ctx, http.MethodGet, "/exec/"+execID+"/json", nil, nil, &info); err != nil {
		return runtime.ExecResult{}, fmt.Errorf("read exit code of exec in %q: %w", id, err)
	}
	return runtime.ExecResult{ExitCode: info.ExitCode, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func execSpec(cmd []string, o runtime.ExecOptions, tty bool) map[string]any {
	spec := map[string]any{"Cmd": cmd, "Tty": tty}
	if !o.Detach {
		spec["AttachStdout"] = true
		spec["AttachStderr"] = !tty
	}
	if o.User != "" {
		spec["User"] = o.User
	}
	if o.WorkDir != "" {
		spec["WorkingDir"] = o.WorkDir
	}
	if len(o.Env) > 0 {
		keys := make([]string, 0, len(o.Env))
		for k := range o.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		env := make([]string, 0, len(keys))
		for _, k := range keys {
			env = append(env, k+"="+o.Env[k])
		}
		spec["Env"] = env
	}
	return spec
}

func (r *Runtime) createExec(ctx context.Context, id string, spec map[string]any) (string, error) {
	var created struct {
		ID string `json:"Id"`
	}
	if _, err := r.c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/exec", nil, spec, &created); err != nil {
		return "", fmt.Errorf("create exec in %q: %w", id, err)
	}
	return created.ID, nil
}

func jsonBody(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

// demux splits Podman's multiplexed stream into stdout and stderr. Each frame
// is an 8-byte header (stream in byte 0, big-endian length in bytes 4 to 7)
// followed by the payload (spike S1). A clean end of stream returns nil.
func demux(r io.Reader, stdout, stderr io.Writer) error {
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		w := stdout
		if hdr[0] == 2 {
			w = stderr
		}
		if _, err := io.CopyN(w, r, int64(binary.BigEndian.Uint32(hdr[4:]))); err != nil {
			return err
		}
	}
}

// Logs implements runtime.Runtime. Standard output and error are merged.
func (r *Runtime) Logs(ctx context.Context, id string, o runtime.LogOptions) (io.ReadCloser, error) {
	if _, err := r.owned(ctx, id); err != nil {
		return nil, err
	}
	q := url.Values{"stdout": {"true"}, "stderr": {"true"}, "follow": {strconv.FormatBool(o.Follow)}}
	if o.Tail > 0 {
		q.Set("tail", strconv.Itoa(o.Tail))
	}
	if !o.Since.IsZero() {
		q.Set("since", strconv.FormatInt(o.Since.Unix(), 10))
	}
	req, err := r.c.request(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/logs", q, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.c.send(req)
	if err != nil {
		return nil, fmt.Errorf("logs of %q: %w", id, err)
	}

	pr, pw := io.Pipe()
	go func() {
		err := demux(resp.Body, pw, pw)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			err = nil // the caller ended the stream
		}
		_ = pw.CloseWithError(err)
	}()
	return &logStream{PipeReader: pr, body: resp.Body}, nil
}

type logStream struct {
	*io.PipeReader
	body io.Closer
}

func (l *logStream) Close() error {
	_ = l.body.Close()
	return l.PipeReader.Close()
}

// Copy implements runtime.Runtime. The file is streamed as a one-entry tar
// archive, so a large binary is never held in memory.
func (r *Runtime) Copy(ctx context.Context, id string, f runtime.File) error {
	if !path.IsAbs(f.Path) || strings.HasSuffix(f.Path, "/") {
		return fmt.Errorf("copy: %q must be an absolute file path", f.Path)
	}
	if _, err := r.owned(ctx, id); err != nil {
		return err
	}

	pr, pw := io.Pipe()
	writeErr := make(chan error, 1)
	go func() { writeErr <- writeTar(pw, f) }()

	req, err := r.c.request(ctx, http.MethodPut, "/containers/"+url.PathEscape(id)+"/archive",
		url.Values{"path": {path.Dir(f.Path)}}, pr)
	if err != nil {
		_ = pr.Close()
		return err
	}
	resp, sendErr := r.c.send(req)
	if sendErr == nil {
		_ = resp.Body.Close()
	}
	_ = pr.Close()

	// The writer's error is the more useful one when the content was the problem.
	if werr := <-writeErr; werr != nil {
		return fmt.Errorf("copy %s into %q: %w", f.Path, id, werr)
	}
	if sendErr != nil {
		return fmt.Errorf("copy %s into %q: %w", f.Path, id, sendErr)
	}
	return nil
}

func writeTar(pw *io.PipeWriter, f runtime.File) (err error) {
	defer func() { _ = pw.CloseWithError(err) }()

	tw := tar.NewWriter(pw)
	hdr := &tar.Header{Name: path.Base(f.Path), Mode: int64(f.Mode.Perm()), Size: f.Size, Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	n, err := io.CopyN(tw, f.Content, f.Size)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("content has %d bytes, size says %d", n, f.Size)
		}
		return err
	}
	// More content than the declared size is a bug in the caller too.
	var extra [1]byte
	if m, _ := f.Content.Read(extra[:]); m > 0 {
		return fmt.Errorf("content has more than the %d bytes the size says", f.Size)
	}
	return tw.Close()
}

// Attach implements runtime.Runtime. It runs cmd with a TTY and hijacks the
// connection, so no pty library is needed (R-TERM-2).
func (r *Runtime) Attach(ctx context.Context, id string, cmd []string) (runtime.Terminal, error) {
	if _, err := r.owned(ctx, id); err != nil {
		return nil, err
	}
	spec := execSpec(cmd, runtime.ExecOptions{}, true)
	spec["AttachStdin"] = true
	execID, err := r.createExec(ctx, id, spec)
	if err != nil {
		return nil, err
	}

	conn, br, err := r.c.hijack(ctx, "/exec/"+execID+"/start", map[string]any{"Detach": false, "Tty": true})
	if err != nil {
		return nil, fmt.Errorf("attach to %q: %w", id, err)
	}
	t := &terminal{conn: conn, br: br, rt: r, execID: execID, stop: make(chan struct{})}
	go func() {
		select {
		case <-ctx.Done():
			_ = t.Close()
		case <-t.stop:
		}
	}()
	return t, nil
}

// hijack sends a POST that Podman answers by switching the connection to a raw
// stream, and returns the connection and a reader holding any bytes already buffered.
func (c *client) hijack(ctx context.Context, path string, body any) (net.Conn, *bufio.Reader, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.socket)
	if err != nil {
		return nil, nil, c.unavailable(err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}

	payload, _ := json.Marshal(body)
	req := fmt.Sprintf("POST /v5.0.0/libpod%s HTTP/1.1\r\nHost: podman\r\nContent-Type: application/json\r\n"+
		"Content-Length: %d\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n%s", path, len(payload), payload)
	if _, err := io.WriteString(conn, req); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("send attach request: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("read attach response: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		defer func() { _ = conn.Close() }()
		return nil, nil, decodeError(resp)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, br, nil
}

// terminal is a hijacked exec session.
type terminal struct {
	conn   net.Conn
	br     *bufio.Reader
	rt     *Runtime
	execID string
	stop   chan struct{}
	once   sync.Once
	err    error
}

func (t *terminal) Read(p []byte) (int, error)  { return t.br.Read(p) }
func (t *terminal) Write(p []byte) (int, error) { return t.conn.Write(p) }

// Close ends the session. It is safe to call more than once and from several goroutines.
func (t *terminal) Close() error {
	t.once.Do(func() {
		close(t.stop)
		t.err = t.conn.Close()
	})
	return t.err
}

func (t *terminal) Resize(ctx context.Context, cols, rows uint16) error {
	q := url.Values{"w": {strconv.Itoa(int(cols))}, "h": {strconv.Itoa(int(rows))}}
	if _, err := t.rt.c.do(ctx, http.MethodPost, "/exec/"+t.execID+"/resize", q, nil, nil); err != nil {
		return fmt.Errorf("resize terminal: %w", err)
	}
	return nil
}
