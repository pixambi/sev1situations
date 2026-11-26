//go:build ignore

// Spike S1: can Go reach the Podman machine socket with net/http alone and do
// everything the runtime needs? Throwaway code. Run: go run spikes/s1-podman/main.go
package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	api   = "http://podman/v5.0.0/libpod"
	image = "docker.io/library/alpine:3"
	label = "s1s.run=spike"
	port  = 19000
)

var client *http.Client

func main() {
	sock := socketPath()
	fmt.Println("socket:", sock)
	client = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	ctx := context.Background()

	var v map[string]any
	must(get(ctx, "/version", &v))
	fmt.Printf("engine: %v\n", v["Version"])

	// Cleanup anything left from an earlier run, by label.
	cleanup(ctx)

	// Network, with a label.
	must(call(ctx, "POST", "/networks/create", nil, map[string]any{
		"name": "s1s-spike", "labels": map[string]string{"s1s.run": "spike"},
	}, nil))
	fmt.Println("network: created")

	// Container on that network, port published on loopback only, labelled.
	var created struct{ Id string }
	must(call(ctx, "POST", "/containers/create", nil, map[string]any{
		"name":    "s1s-spike-web",
		"image":   image,
		"command": []string{"sh", "-c", `while true; do { printf 'HTTP/1.1 200 OK\r\nContent-Length: 15\r\nConnection: close\r\n\r\n'; cat /tmp/hello.txt; } | nc -l -p 8080; done`},
		"labels":  map[string]string{"s1s.run": "spike"},
		"netns":   map[string]any{"nsmode": "bridge"},
		"networks": map[string]any{"s1s-spike": map[string]any{}},
		"portmappings": []map[string]any{{
			"host_ip": "127.0.0.1", "host_port": port, "container_port": 8080, "protocol": "tcp",
		}},
	}, &created))
	id := created.Id
	fmt.Println("container: created", id[:12])

	// Copy into a created (not yet started) container.
	must(putFile(ctx, id, "/tmp", "hello.txt", []byte("hello from s1s\n")))
	fmt.Println("copy: into a created container ok")

	must(call(ctx, "POST", "/containers/"+id+"/start", nil, nil, nil))
	fmt.Println("container: started")

	// Exec: captured stdout, stderr and the exit code.
	out, errOut, code := exec1(ctx, id, []string{"sh", "-c", "echo to-stdout; echo to-stderr 1>&2; exit 3"})
	fmt.Printf("exec: stdout=%q stderr=%q exit=%d\n", out, errOut, code)

	// Copy into the running container and read it back with exec.
	must(putFile(ctx, id, "/tmp", "late.txt", []byte("copied while running\n")))
	late, _, _ := exec1(ctx, id, []string{"cat", "/tmp/late.txt"})
	fmt.Printf("copy: into a running container -> exec cat = %q\n", late)

	// Fetch the first file from the host through the published loopback port.
	time.Sleep(500 * time.Millisecond)
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/hello.txt", port))
	must(err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("published port: GET 127.0.0.1:%d/hello.txt -> %q\n", port, body)

	// Logs: stream with follow while generating a line.
	go func() {
		time.Sleep(300 * time.Millisecond)
		exec1(ctx, id, []string{"sh", "-c", "echo marker-line > /proc/1/fd/1"})
	}()
	fmt.Println("logs: first lines seen:", firstLogLines(ctx, id, "marker-line", 5*time.Second))

	// List by label.
	var list []struct {
		Id     string
		Names  []string
		Labels map[string]string
	}
	must(get(ctx, "/containers/json?all=true&filters="+url.QueryEscape(`{"label":["`+label+`"]}`), &list))
	fmt.Printf("list by label: %d container(s): %v\n", len(list), list[0].Names)

	cleanup(ctx)
	must(get(ctx, "/containers/json?all=true&filters="+url.QueryEscape(`{"label":["`+label+`"]}`), &list))
	fmt.Printf("after cleanup: %d container(s)\n", len(list))
}

// socketPath asks podman where the machine's API socket is.
func socketPath() string {
	out, err := exec.Command("podman", "machine", "inspect", "--format", "{{.ConnectionInfo.PodmanSocket.Path}}").Output()
	must(err)
	return strings.TrimSpace(string(out))
}

func cleanup(ctx context.Context) {
	var list []struct{ Id string }
	_ = get(ctx, "/containers/json?all=true&filters="+url.QueryEscape(`{"label":["`+label+`"]}`), &list)
	for _, c := range list {
		_ = call(ctx, "DELETE", "/containers/"+c.Id+"?force=true", nil, nil, nil)
	}
	_ = call(ctx, "DELETE", "/networks/s1s-spike?force=true", nil, nil, nil)
}

func exec1(ctx context.Context, id string, cmd []string) (stdout, stderr string, code int) {
	var ex struct{ Id string }
	must(call(ctx, "POST", "/containers/"+id+"/exec", nil, map[string]any{
		"Cmd": cmd, "AttachStdout": true, "AttachStderr": true,
	}, &ex))
	resp := raw(ctx, "POST", "/exec/"+ex.Id+"/start", map[string]any{"Detach": false})
	defer resp.Body.Close()
	var so, se bytes.Buffer
	demux(resp.Body, &so, &se)
	var info struct {
		ExitCode int
		Running  bool
	}
	must(get(ctx, "/exec/"+ex.Id+"/json", &info))
	return so.String(), se.String(), info.ExitCode
}

// demux splits the 8-byte-header multiplexed stream into stdout and stderr.
func demux(r io.Reader, stdout, stderr io.Writer) {
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(hdr[4:])
		w := stdout
		if hdr[0] == 2 {
			w = stderr
		}
		if _, err := io.CopyN(w, r, int64(n)); err != nil {
			return
		}
	}
}

func putFile(ctx context.Context, id, dir, name string, data []byte) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))})
	_, _ = tw.Write(data)
	_ = tw.Close()
	req, _ := http.NewRequestWithContext(ctx, "PUT", api+"/containers/"+id+"/archive?path="+url.QueryEscape(dir), &buf)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("copy: %s: %s", resp.Status, b)
	}
	return nil
}

func firstLogLines(ctx context.Context, id, want string, timeout time.Duration) string {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", api+"/containers/"+id+"/logs?follow=true&stdout=true&stderr=true", nil)
	resp, err := client.Do(req)
	must(err)
	defer resp.Body.Close()
	pr, pw := io.Pipe()
	go func() { demux(resp.Body, pw, pw); pw.Close() }()
	sc := bufio.NewScanner(pr)
	for sc.Scan() {
		if strings.Contains(sc.Text(), want) {
			return fmt.Sprintf("%q (streamed, then closed by cancel)", sc.Text())
		}
	}
	return "NOT SEEN"
}

func get(ctx context.Context, path string, out any) error { return call(ctx, "GET", path, nil, nil, out) }

func call(ctx context.Context, method, path string, _ any, in, out any) error {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(ctx, method, api+path, body)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, bytes.TrimSpace(b))
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

func raw(ctx context.Context, method, path string, in any) *http.Response {
	b, _ := json.Marshal(in)
	req, _ := http.NewRequestWithContext(ctx, method, api+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	must(err)
	if resp.StatusCode >= 300 {
		bb, _ := io.ReadAll(resp.Body)
		must(fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, bb))
	}
	return resp
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}
