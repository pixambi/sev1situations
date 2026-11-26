package podman

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/pixambi/sev1situations/internal/runtime"
)

// newServer starts an HTTP server on a unix socket and returns a Runtime for it.
// The socket path is kept short because unix socket paths are limited in length.
func newServer(t *testing.T, h http.Handler) *Runtime {
	t.Helper()
	dir, err := os.MkdirTemp("", "s1s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sock := filepath.Join(dir, "p.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	return NewWithSocket(sock)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type fakeCommands struct {
	lookErr error
	out     string
	outErr  error
}

func (f fakeCommands) LookPath(string) (string, error) { return "/usr/bin/podman", f.lookErr }
func (f fakeCommands) Output(context.Context, string, ...string) ([]byte, error) {
	return []byte(f.out), f.outErr
}

func TestDiscoverSocket(t *testing.T) {
	tests := []struct {
		name    string
		cmds    fakeCommands
		want    string
		wantErr string
	}{
		{name: "found", cmds: fakeCommands{out: "/tmp/podman/m-api.sock\n"}, want: "/tmp/podman/m-api.sock"},
		{name: "not installed", cmds: fakeCommands{lookErr: errors.New("nope")}, wantErr: "not installed"},
		{name: "no machine", cmds: fakeCommands{outErr: errors.New("exit status 125")}, wantErr: "podman machine init"},
		{name: "empty path", cmds: fakeCommands{out: "  \n"}, wantErr: "podman machine start"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := discoverSocket(context.Background(), tt.cmds)
			if tt.wantErr == "" {
				if err != nil || got != tt.want {
					t.Fatalf("discoverSocket = %q, %v; want %q", got, err, tt.want)
				}
				return
			}
			if !errors.Is(err, runtime.ErrUnavailable) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("discoverSocket error = %v; want ErrUnavailable mentioning %q", err, tt.wantErr)
			}
		})
	}
}

func TestUnreachableSocketIsUnavailable(t *testing.T) {
	rt := NewWithSocket(filepath.Join(t.TempDir(), "missing.sock"))
	err := rt.Ping(context.Background())
	if !errors.Is(err, runtime.ErrUnavailable) {
		t.Fatalf("Ping = %v, want ErrUnavailable", err)
	}
	if !strings.Contains(err.Error(), "podman machine start") {
		t.Errorf("error %q does not tell the user how to fix it", err)
	}
}

func TestNotFoundMapsToErrNotFound(t *testing.T) {
	rt := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 404, map[string]any{"cause": "no such container", "message": "no container with name or ID \"x\" found", "response": 404})
	}))
	err := rt.Remove(context.Background(), "x")
	if !errors.Is(err, runtime.ErrNotFound) {
		t.Fatalf("Remove = %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "no container with name or ID") {
		t.Errorf("error %q lost Podman's message", err)
	}
}

func TestRefusesAContainerItDidNotCreate(t *testing.T) {
	var mu sync.Mutex
	var deletes int
	rt := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			mu.Lock()
			deletes++
			mu.Unlock()
			w.WriteHeader(200)
		case strings.HasSuffix(r.URL.Path, "/json"):
			writeJSON(w, 200, map[string]any{"Id": "abc", "Config": map[string]any{"Labels": map[string]string{"owner": "someone-else"}}})
		}
	}))

	ctx := context.Background()
	calls := map[string]error{
		"Remove": rt.Remove(ctx, "abc"),
		"Stop":   rt.Stop(ctx, "abc"),
	}
	_, calls["Exec"] = rt.Exec(ctx, "abc", []string{"true"}, runtime.ExecOptions{})
	for op, err := range calls {
		if err == nil || !strings.Contains(err.Error(), "not created by this application") {
			t.Errorf("%s on a foreign container = %v, want a refusal (R-RUN-4)", op, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if deletes != 0 {
		t.Errorf("server saw %d DELETE request(s), want none", deletes)
	}
}

func TestStartContainerRequest(t *testing.T) {
	var created map[string]any
	rt := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			_ = json.NewDecoder(r.Body).Decode(&created)
			writeJSON(w, 201, map[string]any{"Id": "cid"})
		case strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/json"):
			writeJSON(w, 200, map[string]any{
				"Id": "cid", "Name": "/s1s-r1-vault", "State": map[string]any{"Status": "running"},
				"Config":          map[string]any{"Image": "alpine:3", "Labels": map[string]string{"s1s.run": "r1", "s1s.scenario": "vault-sandbox"}},
				"NetworkSettings": map[string]any{"Networks": map[string]any{"s1s-r1-net": map[string]any{"IPAddress": "10.89.1.2"}}},
			})
		}
	}))

	c, err := rt.StartContainer(context.Background(), runtime.ContainerOptions{
		Run: runtime.Run{ID: "r1", Scenario: "vault-sandbox"}, Name: "vault", Image: "alpine:3",
		Command: []string{"sleep", "300"}, Network: "s1s-r1-net",
		Ports: []runtime.PortMapping{{Host: 19000, Container: 8200}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "cid" || c.Name != "s1s-r1-vault" || c.IP != "10.89.1.2" || c.State != runtime.StateRunning {
		t.Errorf("container = %+v", c)
	}

	if created["name"] != "s1s-r1-vault" {
		t.Errorf("name = %v, want the run ID in it", created["name"])
	}
	labels, _ := created["labels"].(map[string]any)
	if labels["s1s.app"] != "sev1situations" || labels["s1s.run"] != "r1" || labels["s1s.scenario"] != "vault-sandbox" {
		t.Errorf("labels = %v, want the app, run and scenario labels (R-RUN-4)", labels)
	}
	ports, _ := created["portmappings"].([]any)
	if len(ports) != 1 || ports[0].(map[string]any)["host_ip"] != "127.0.0.1" {
		t.Errorf("portmappings = %v, want the host side bound to 127.0.0.1 (R-APP-3)", created["portmappings"])
	}
	if nets, _ := created["networks"].(map[string]any); nets["s1s-r1-net"] == nil {
		t.Errorf("networks = %v, want the run network", created["networks"])
	}
}

func TestStartContainerRemovesTheCreatedContainerWhenStartFails(t *testing.T) {
	var mu sync.Mutex
	var deleted bool
	rt := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			writeJSON(w, 201, map[string]any{"Id": "cid"})
		case strings.HasSuffix(r.URL.Path, "/start"):
			writeJSON(w, 500, map[string]any{"message": "crun: executable file `httpd` not found in $PATH", "response": 500})
		case r.Method == http.MethodDelete:
			mu.Lock()
			deleted = true
			mu.Unlock()
			w.WriteHeader(200)
		}
	}))

	_, err := rt.StartContainer(context.Background(), runtime.ContainerOptions{
		Run: runtime.Run{ID: "r1"}, Name: "web", Image: "alpine:3",
	})
	if err == nil || !strings.Contains(err.Error(), "executable file `httpd` not found") {
		t.Fatalf("StartContainer = %v, want Podman's reason", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !deleted {
		t.Error("the container created before the failed start was left behind")
	}
}

func TestStartContainerRejectsBadInput(t *testing.T) {
	rt := NewWithSocket("/unused")
	tests := []struct {
		name string
		o    runtime.ContainerOptions
	}{
		{"no run", runtime.ContainerOptions{Name: "a", Image: "x"}},
		{"no name", runtime.ContainerOptions{Run: runtime.Run{ID: "r"}, Image: "x"}},
		{"no image", runtime.ContainerOptions{Run: runtime.Run{ID: "r"}, Name: "a"}},
		{"port zero", runtime.ContainerOptions{Run: runtime.Run{ID: "r"}, Name: "a", Image: "x", Ports: []runtime.PortMapping{{Host: 0, Container: 80}}}},
		{"port too big", runtime.ContainerOptions{Run: runtime.Run{ID: "r"}, Name: "a", Image: "x", Ports: []runtime.PortMapping{{Host: 70000, Container: 80}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := rt.StartContainer(context.Background(), tt.o); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestCopyStreamsATarArchive(t *testing.T) {
	var gotName, gotDir string
	var gotMode, gotSize int64
	var gotBody string
	rt := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/json"):
			writeJSON(w, 200, map[string]any{"Id": "cid", "Config": map[string]any{"Labels": map[string]string{"s1s.app": "sev1situations"}}})
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/archive"):
			gotDir = r.URL.Query().Get("path")
			tr := tar.NewReader(r.Body)
			h, err := tr.Next()
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			b, _ := io.ReadAll(tr)
			gotName, gotMode, gotSize, gotBody = h.Name, h.Mode, h.Size, string(b)
			w.WriteHeader(200)
		}
	}))

	body := "#!/bin/sh\necho vault\n"
	err := rt.Copy(context.Background(), "cid", runtime.File{
		Path: "/usr/local/bin/vault", Mode: 0o755, Size: int64(len(body)), Content: strings.NewReader(body),
	})
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if gotDir != "/usr/local/bin" || gotName != "vault" || gotMode != 0o755 || gotSize != int64(len(body)) || gotBody != body {
		t.Errorf("server got dir=%q name=%q mode=%o size=%d body=%q", gotDir, gotName, gotMode, gotSize, gotBody)
	}
}

func TestCopyRejectsBadFiles(t *testing.T) {
	rt := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/json"):
			writeJSON(w, 200, map[string]any{"Id": "cid", "Config": map[string]any{"Labels": map[string]string{"s1s.app": "sev1situations"}}})
		default:
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(200)
		}
	}))
	tests := []struct {
		name string
		f    runtime.File
	}{
		{"relative path", runtime.File{Path: "bin/x", Size: 1, Content: strings.NewReader("x")}},
		{"directory path", runtime.File{Path: "/bin/", Size: 1, Content: strings.NewReader("x")}},
		{"content shorter than size", runtime.File{Path: "/tmp/x", Mode: 0o644, Size: 10, Content: strings.NewReader("short")}},
		{"content longer than size", runtime.File{Path: "/tmp/x", Mode: 0o644, Size: 2, Content: strings.NewReader("too long")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := rt.Copy(context.Background(), "cid", tt.f); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestDemux(t *testing.T) {
	frame := func(stream byte, s string) []byte {
		h := make([]byte, 8)
		h[0] = stream
		binary.BigEndian.PutUint32(h[4:], uint32(len(s)))
		return append(h, s...)
	}
	var in bytes.Buffer
	in.Write(frame(1, "out1\n"))
	in.Write(frame(2, "err1\n"))
	in.Write(frame(1, "out2\n"))

	var stdout, stderr bytes.Buffer
	if err := demux(&in, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "out1\nout2\n" || stderr.String() != "err1\n" {
		t.Errorf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	// A frame cut short is an error, not a clean end.
	short := frame(1, "hello")[:10]
	if err := demux(bytes.NewReader(short), io.Discard, io.Discard); err == nil {
		t.Error("a truncated frame returned nil, want an error")
	}
}

func TestRemoveRunSelectsByLabelAndKeepsGoing(t *testing.T) {
	var mu sync.Mutex
	var filters []string
	var removed []string
	rt := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			filters = append(filters, r.URL.Query().Get("filters"))
			writeJSON(w, 200, []map[string]any{{"Id": "c1"}, {"Id": "c2"}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/networks/json"):
			writeJSON(w, 200, []map[string]any{{"name": "s1s-r1-net"}})
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/containers/c1"):
			removed = append(removed, "c1")
			writeJSON(w, 500, map[string]any{"message": "device busy", "response": 500})
		case r.Method == http.MethodDelete:
			removed = append(removed, strings.TrimPrefix(r.URL.Path, "/v5.0.0/libpod"))
			w.WriteHeader(200)
		}
	}))

	err := rt.RemoveRun(context.Background(), "r1")
	if err == nil || !strings.Contains(err.Error(), "device busy") {
		t.Fatalf("RemoveRun = %v, want the failed removal reported", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(filters) == 0 || !strings.Contains(filters[0], "s1s.run=r1") || !strings.Contains(filters[0], "s1s.app=sev1situations") {
		t.Errorf("filters = %v, want selection by the app and run labels only", filters)
	}
	want := []string{"c1", "/containers/c2", "/networks/s1s-r1-net"}
	if strings.Join(removed, ",") != strings.Join(want, ",") {
		t.Errorf("removed = %v, want %v (it must go on past c1 and still remove the network)", removed, want)
	}

	if err := rt.RemoveRun(context.Background(), ""); err == nil {
		t.Error("RemoveRun with no run ID succeeded, want an error")
	}
}

func TestEnsureImageSkipsThePullWhenLocal(t *testing.T) {
	var mu sync.Mutex
	var pulls int
	rt := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/exists"):
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/images/pull"):
			mu.Lock()
			pulls++
			mu.Unlock()
		}
	}))
	if err := rt.EnsureImage(context.Background(), "docker.io/library/alpine:3", nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if pulls != 0 {
		t.Errorf("a local image was pulled %d time(s), want none (R-RUN-9)", pulls)
	}
}

func TestEnsureImagePullsWhenMissingAndReportsProgress(t *testing.T) {
	var ref string
	rt := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/exists"):
			writeJSON(w, 404, map[string]any{"message": "image not known", "response": 404})
		case strings.HasSuffix(r.URL.Path, "/images/pull"):
			ref = r.URL.Query().Get("reference")
			_, _ = io.WriteString(w, `{"stream":"Trying to pull docker.io/library/postgres:16..."}`+"\n")
			_, _ = io.WriteString(w, `{"stream":"Copying blob sha256:abc"}`+"\n\n")
			_, _ = io.WriteString(w, `{"images":["deadbeef"],"id":"deadbeef"}`+"\n")
		}
	}))

	var lines []string
	if err := rt.EnsureImage(context.Background(), "docker.io/library/postgres:16", func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatal(err)
	}
	if ref != "docker.io/library/postgres:16" {
		t.Errorf("pulled %q", ref)
	}
	want := "Trying to pull docker.io/library/postgres:16...|Copying blob sha256:abc"
	if strings.Join(lines, "|") != want {
		t.Errorf("progress = %q, want %q", strings.Join(lines, "|"), want)
	}
}

func TestEnsureImageReportsAFailureInsideTheStream(t *testing.T) {
	rt := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/exists") {
			writeJSON(w, 404, map[string]any{"message": "image not known", "response": 404})
			return
		}
		_, _ = io.WriteString(w, `{"stream":"Trying to pull..."}`+"\n")
		_, _ = io.WriteString(w, `{"error":"requested access to the resource is denied"}`+"\n")
	}))
	err := rt.EnsureImage(context.Background(), "docker.io/library/nope:1", nil)
	if err == nil || !strings.Contains(err.Error(), "requested access to the resource is denied") {
		t.Errorf("EnsureImage = %v, want the pull error", err)
	}
	if err := rt.EnsureImage(context.Background(), "", nil); err == nil {
		t.Error("EnsureImage with no reference succeeded")
	}
}
