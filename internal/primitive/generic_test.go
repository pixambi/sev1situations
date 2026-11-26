package primitive_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/primitive/primitivetest"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/runtime/fake"
	"github.com/pixambi/sev1situations/internal/scenario"
)

// rig is a small deployment on the fake runtime with every generic primitive registered.
type rig struct {
	t     *testing.T
	fake  *fake.Runtime
	reg   *primitive.Registry
	env   primitive.Env
	ids   map[string]string // node name -> container ID
	mu    sync.Mutex
	execs []execCall
}

type execCall struct {
	cmd  []string
	opts runtime.ExecOptions
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, fake: fake.New(), reg: primitive.NewRegistry(), ids: map[string]string{}}
	if err := primitive.RegisterGeneric(r.reg); err != nil {
		t.Fatal(err)
	}

	run := runtime.Run{ID: "r1", Scenario: "test"}
	nodes := map[string]primitive.Node{}
	for _, name := range []string{"node-1", "node-2", "node-3"} {
		c, err := r.fake.StartContainer(context.Background(), runtime.ContainerOptions{Run: run, Name: name, Image: "alpine:3"})
		if err != nil {
			t.Fatal(err)
		}
		r.ids[name] = c.ID
		nodes[name] = primitive.Node{Name: name, Cluster: "primary", ContainerID: c.ID, IP: "10.89.1.2"}
	}

	files := map[string]string{"fixtures/data.json": `{"a":1}`, "templates/vault.hcl.tmpl": `api_addr = "http://{{ .Nodes.node_1.IP }}:8200"` + "\n"}
	data := scenario.TemplateData{Nodes: map[string]scenario.NodeData{"node_1": {Name: "node-1", IP: "10.89.1.2", Cluster: "primary"}}}
	r.env = primitive.Env{
		Runtime: r.fake, Run: run, Nodes: nodes,
		ReadFile: func(rel string) ([]byte, error) {
			if b, ok := files[rel]; ok {
				return []byte(b), nil
			}
			return nil, fmt.Errorf("%s: no such file", rel)
		},
		Render: func(src string) (string, error) { return scenario.Render(src, data) },
	}

	// Record every exec, and answer with whatever the test sets.
	r.script(func(execCall) runtime.ExecResult { return runtime.ExecResult{} })
	return r
}

// script sets how the fake answers exec, and records each call.
func (r *rig) script(fn func(execCall) runtime.ExecResult) {
	r.fake.SetExec(func(_ runtime.Container, cmd []string, o runtime.ExecOptions) (runtime.ExecResult, error) {
		c := execCall{cmd: cmd, opts: o}
		r.mu.Lock()
		r.execs = append(r.execs, c)
		r.mu.Unlock()
		return fn(c), nil
	})
}

func (r *rig) last() execCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.execs) == 0 {
		r.t.Fatal("no exec was made")
	}
	return r.execs[len(r.execs)-1]
}

func (r *rig) run(name string, args map[string]any) (primitive.Result, error) {
	r.t.Helper()
	return r.reg.Run(context.Background(), name, r.env, args)
}

func (r *rig) mustPass(name string, args map[string]any) primitive.Result {
	r.t.Helper()
	res, err := r.run(name, args)
	if err != nil || res.Status != primitive.Pass {
		r.t.Fatalf("%s = %+v, %v; want a pass", name, res, err)
	}
	return res
}

func (r *rig) mustFail(name string, args map[string]any, msg string) primitive.Result {
	r.t.Helper()
	res, err := r.run(name, args)
	if err != nil || res.Status != primitive.Fail || !strings.Contains(res.Message, msg) {
		r.t.Fatalf("%s = %+v, %v; want a fail containing %q", name, res, err, msg)
	}
	return res
}

func (r *rig) mustError(name string, args map[string]any, msg string) {
	r.t.Helper()
	res, err := r.run(name, args)
	if err == nil || !strings.Contains(err.Error(), msg) {
		r.t.Fatalf("%s = %+v, %v; want an error containing %q", name, res, err, msg)
	}
}

func (r *rig) state(node string) runtime.State {
	r.t.Helper()
	cs, err := r.fake.List(context.Background(), "r1")
	if err != nil {
		r.t.Fatal(err)
	}
	for _, c := range cs {
		if c.ID == r.ids[node] {
			return c.State
		}
	}
	return "removed"
}

type m = map[string]any

func TestEveryGenericPrimitiveFollowsTheContract(t *testing.T) {
	r := newRig(t)
	valid := map[string]m{
		"container.exec":         {"node": "node-1", "command": []any{"true"}},
		"container.stop":         {"nodes": []any{"node-1"}},
		"container.start":        {"nodes": []any{"node-1"}},
		"container.remove":       {"nodes": []any{"node-1"}},
		"container.kill-process": {"node": "node-1", "process": "vault"},
		"container.copy-in":      {"node": "node-1", "source": "fixtures/data.json", "path": "/tmp/d.json"},
		"file.render":            {"node": "node-1", "template": "templates/vault.hcl.tmpl", "path": "/tmp/v.hcl"},
		"file.write":             {"node": "node-1", "path": "/tmp/x", "content": "x"},
		"http.request":           {"url": "http://127.0.0.1:1/"},
		"wait.until":             {"check": m{"primitive": "assert.equals", "args": m{"actual": "a", "expected": "a"}}},
		"wait.sleep":             {"duration": "1ms"},
		"assert.equals":          {"actual": "a", "expected": "a"},
		"assert.contains":        {"text": "abc", "substring": "b"},
		"assert.matches":         {"text": "abc", "pattern": "b"},
		"assert.json-path":       {"json": `{"a":1}`, "path": "a"},
	}
	names := r.reg.Names()
	if len(names) != len(valid) {
		t.Fatalf("registered %d primitives %v, the test knows %d", len(names), names, len(valid))
	}
	for _, name := range names {
		args, ok := valid[name]
		if !ok {
			t.Fatalf("no valid arguments for %s", name)
		}
		p, _ := r.reg.Get(name)
		t.Run(name, func(t *testing.T) {
			// Each contract run gets its own deployment so one cannot disturb the next.
			fresh := newRig(t)
			primitivetest.Contract(t, p, primitivetest.Options{ValidArgs: args, Env: fresh.env})
		})
	}
}

func TestContainerExec(t *testing.T) {
	r := newRig(t)
	r.script(func(c execCall) runtime.ExecResult {
		switch c.cmd[0] {
		case "fail":
			return runtime.ExecResult{ExitCode: 2, Stderr: "boom\n"}
		case "three":
			return runtime.ExecResult{ExitCode: 3}
		}
		return runtime.ExecResult{Stdout: "  leader: node-1  \n", Stderr: "warn\n"}
	})

	res := r.mustPass("container.exec", m{"node": "node-1", "command": []any{"vault", "status"}, "save": "status"})
	if res.Outputs["status"] != "leader: node-1" || !strings.Contains(res.Evidence, "leader: node-1") {
		t.Errorf("result = %+v, want trimmed stdout saved and the output as evidence", res)
	}
	r.mustPass("container.exec", m{"node": "node-1", "command": []any{"three"}, "expectExit": 3})
	r.mustPass("container.exec", m{"node": "node-1", "command": []any{"x"}, "contains": "warn"}) // stderr counts

	res = r.mustFail("container.exec", m{"node": "node-1", "command": []any{"fail"}}, "exited with 2, expected 0")
	if !strings.Contains(res.Evidence, "boom") {
		t.Errorf("evidence = %q, want the command output", res.Evidence)
	}
	r.mustFail("container.exec", m{"node": "node-1", "command": []any{"x"}, "contains": "sealed"}, `does not contain "sealed"`)

	r.mustPass("container.exec", m{"node": "node-2", "command": []any{"x"}, "user": "vault", "workDir": "/etc", "env": m{"VAULT_ADDR": "http://x", "N": 5}})
	if o := r.last().opts; o.User != "vault" || o.WorkDir != "/etc" || o.Env["VAULT_ADDR"] != "http://x" || o.Env["N"] != "5" {
		t.Errorf("exec options = %+v", o)
	}

	r.mustError("container.exec", m{"node": "node-9", "command": []any{"x"}}, `node "node-9" is not part of this deployment (nodes: node-1, node-2, node-3)`)
	if err := r.fake.Stop(context.Background(), r.ids["node-3"]); err != nil {
		t.Fatal(err)
	}
	r.mustError("container.exec", m{"node": "node-3", "command": []any{"x"}}, "not running") // could not run is an error, not a fail
}

func TestContainerStopStartRemove(t *testing.T) {
	r := newRig(t)

	r.mustPass("container.stop", m{"nodes": []any{"node-2", "node-3"}})
	if r.state("node-2") != runtime.StateStopped || r.state("node-3") != runtime.StateStopped || r.state("node-1") != runtime.StateRunning {
		t.Error("stop did not stop exactly the named nodes")
	}
	r.mustPass("container.stop", m{"nodes": []any{"node-2"}}) // already stopped

	r.mustPass("container.start", m{"nodes": []any{"node-2"}})
	r.mustPass("container.start", m{"nodes": []any{"node-2"}}) // already running
	if r.state("node-2") != runtime.StateRunning {
		t.Error("start did not start the node")
	}

	r.mustPass("container.remove", m{"nodes": []any{"node-2", "node-3"}})
	if r.state("node-2") != "removed" || r.state("node-3") != "removed" {
		t.Error("remove did not remove the nodes")
	}
	r.mustPass("container.remove", m{"nodes": []any{"node-2"}}) // already gone counts as removed

	// One unknown name does not stop the others from being handled.
	r.mustError("container.stop", m{"nodes": []any{"node-9", "node-1"}}, `"node-9"`)
	if r.state("node-1") != runtime.StateStopped {
		t.Error("an unknown node stopped the primitive from acting on the valid one")
	}

	boom := errors.New("device busy")
	r.fake.FailNext(fake.OpStop, boom)
	r.mustError("container.stop", m{"nodes": []any{"node-1"}}, "device busy")
}

func TestContainerKillProcess(t *testing.T) {
	r := newRig(t)
	code := 0
	r.script(func(execCall) runtime.ExecResult { return runtime.ExecResult{ExitCode: code, Stderr: "usage: pkill"} })

	r.mustPass("container.kill-process", m{"node": "node-1", "process": "vault"})
	if got := strings.Join(r.last().cmd, " "); got != "pkill -KILL -x vault" {
		t.Errorf("command = %q", got)
	}
	r.mustPass("container.kill-process", m{"node": "node-1", "process": "vault", "signal": "sigterm"})
	if got := strings.Join(r.last().cmd, " "); got != "pkill -TERM -x vault" {
		t.Errorf("command = %q, want the signal normalised", got)
	}

	code = 1
	r.mustFail("container.kill-process", m{"node": "node-1", "process": "vault"}, `no process named "vault"`)
	code = 2
	r.mustError("container.kill-process", m{"node": "node-1", "process": "vault"}, "exited with 2")
	r.mustError("container.kill-process", m{"node": "node-1", "process": "vault", "signal": "KILL; rm -rf /"}, "not a signal name")
}

func TestCopyRenderAndWrite(t *testing.T) {
	r := newRig(t)
	id := r.ids["node-1"]

	r.mustPass("container.copy-in", m{"node": "node-1", "source": "fixtures/data.json", "path": "/etc/data.json", "mode": "0600"})
	if f, ok := r.fake.File(id, "/etc/data.json"); !ok || string(f.Content) != `{"a":1}` || f.Mode != 0o600 {
		t.Errorf("copy-in stored %+v, %v", f, ok)
	}

	r.mustPass("file.render", m{"node": "node-1", "template": "templates/vault.hcl.tmpl", "path": "/etc/vault.hcl"})
	if f, _ := r.fake.File(id, "/etc/vault.hcl"); string(f.Content) != "api_addr = \"http://10.89.1.2:8200\"\n" || f.Mode != 0o644 {
		t.Errorf("file.render stored %q mode %o, want the template filled in with the default mode", f.Content, f.Mode)
	}

	r.mustPass("file.write", m{"node": "node-1", "path": "/usr/local/bin/run.sh", "content": "#!/bin/sh\n", "mode": "0755"})
	if f, _ := r.fake.File(id, "/usr/local/bin/run.sh"); f.Mode != 0o755 || string(f.Content) != "#!/bin/sh\n" {
		t.Errorf("file.write stored %+v", f)
	}

	r.mustError("container.copy-in", m{"node": "node-1", "source": "fixtures/missing", "path": "/x"}, "no such file")
	r.mustError("file.render", m{"node": "node-1", "template": "templates/missing", "path": "/x"}, "no such file")
	r.mustError("file.write", m{"node": "node-1", "path": "/x", "content": "c", "mode": "rwx"}, "octal")
	r.mustError("file.write", m{"node": "node-1", "path": "/x", "content": "c", "mode": "99999"}, "octal")
	r.mustError("file.write", m{"node": "node-9", "path": "/x", "content": "c"}, "node-9")
	r.mustError("file.write", m{"node": "node-1", "path": "relative", "content": "c"}, "absolute")

	// A template that reads a value the run does not have is an error, never an empty value.
	r.env.Render = func(string) (string, error) { return "", errors.New("map has no entry for key") }
	r.mustError("file.render", m{"node": "node-1", "template": "templates/vault.hcl.tmpl", "path": "/x"}, "no entry for key")

	r.env.ReadFile, r.env.Render = nil, nil
	r.mustError("container.copy-in", m{"node": "node-1", "source": "a", "path": "/x"}, "no scenario files")
	r.mustError("file.render", m{"node": "node-1", "template": "a", "path": "/x"}, "no scenario files")
}

func TestHTTPRequest(t *testing.T) {
	var gotMethod, gotBody, gotHeader atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		gotMethod.Store(req.Method)
		gotBody.Store(string(b))
		gotHeader.Store(req.Header.Get("X-Token"))
		_, _ = io.WriteString(w, `{"sealed":false}`)
	})
	mux.HandleFunc("/down", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	})
	mux.HandleFunc("/away", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "http://example.com/", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	r := newRig(t)

	res := r.mustPass("http.request", m{"url": srv.URL + "/ok", "contains": "sealed", "save": "health"})
	if res.Outputs["health"] != `{"sealed":false}` || !strings.Contains(res.Evidence, "200") {
		t.Errorf("result = %+v", res)
	}

	r.mustPass("http.request", m{"method": "post", "url": srv.URL + "/ok", "body": "payload", "headers": m{"X-Token": "abc"}})
	if gotMethod.Load() != "POST" || gotBody.Load() != "payload" || gotHeader.Load() != "abc" {
		t.Errorf("server saw %v %v %v", gotMethod.Load(), gotBody.Load(), gotHeader.Load())
	}

	r.mustPass("http.request", m{"url": srv.URL + "/down", "expectStatus": 503})
	r.mustFail("http.request", m{"url": srv.URL + "/down"}, "returned 503, expected 200")
	r.mustFail("http.request", m{"url": srv.URL + "/ok", "contains": "nope"}, `does not contain "nope"`)

	// A redirect off this computer is not followed, so the 302 is what is checked.
	r.mustFail("http.request", m{"url": srv.URL + "/away"}, "returned 302")

	r.mustError("http.request", m{"url": "http://example.com/"}, "only loopback addresses")
	r.mustError("http.request", m{"url": "http://10.89.1.2:8200/"}, "only loopback addresses")
	r.mustError("http.request", m{"url": "ftp://127.0.0.1/"}, "http://")
	r.mustError("http.request", m{"url": srv.URL, "method": "TRACE"}, "not one of")
	r.mustError("http.request", m{"url": "http://127.0.0.1:1/"}, "connection refused")
}

// flaky is a primitive that passes only from its nth call on.
type flaky struct {
	calls atomic.Int32
	after int32
	err   bool
}

func (f *flaky) Spec() primitive.Spec {
	return primitive.Spec{Name: "t.flaky", Summary: "Passes from its nth call.", Timeout: time.Second}
}

func (f *flaky) Run(context.Context, primitive.Env, primitive.Args) (primitive.Result, error) {
	n := f.calls.Add(1)
	switch {
	case n >= f.after:
		return primitive.Result{Status: primitive.Pass, Message: "up", Outputs: map[string]string{"n": fmt.Sprint(n)}}, nil
	case f.err:
		return primitive.Result{}, errors.New("not reachable yet")
	}
	return primitive.Result{Status: primitive.Fail, Message: "still broken"}, nil
}

func TestWaitUntil(t *testing.T) {
	check := func(after int32, useErr bool) (*rig, *flaky) {
		r := newRig(t)
		f := &flaky{after: after, err: useErr}
		if err := r.reg.Register(f); err != nil {
			t.Fatal(err)
		}
		return r, f
	}
	until := func(timeout string) m {
		return m{"check": m{"primitive": "t.flaky"}, "interval": "10ms", "timeout": timeout}
	}

	t.Run("passes once the check does", func(t *testing.T) {
		r, f := check(4, false)
		res := r.mustPass("wait.until", until("5s"))
		if f.calls.Load() != 4 || !strings.Contains(res.Message, "attempt 4") || res.Outputs["n"] != "4" {
			t.Errorf("calls=%d result=%+v, want it to stop at the passing call and keep its outputs", f.calls.Load(), res)
		}
	})
	t.Run("an error from the check is retried too", func(t *testing.T) {
		r, f := check(3, true)
		r.mustPass("wait.until", until("5s"))
		if f.calls.Load() != 3 {
			t.Errorf("calls = %d, want 3", f.calls.Load())
		}
	})
	t.Run("running out of time is a fail that shows the last result", func(t *testing.T) {
		r, _ := check(1_000_000, false)
		r.mustFail("wait.until", until("150ms"), "still broken")
	})
	t.Run("running out of time while errors continue is also a fail", func(t *testing.T) {
		r, _ := check(1_000_000, true)
		r.mustFail("wait.until", until("150ms"), "not reachable yet")
	})
	t.Run("the run ending is not a fail", func(t *testing.T) {
		r, _ := check(1_000_000, false)
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(60 * time.Millisecond); cancel() }()
		_, err := r.reg.Run(ctx, "wait.until", r.env, until("30s"))
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run = %v, want context.Canceled", err)
		}
	})
	t.Run("a check that is not a primitive is an error", func(t *testing.T) {
		r := newRig(t)
		r.mustError("wait.until", m{"check": m{"args": m{}}}, "needs a primitive")
		start := time.Now()
		r.mustError("wait.until", m{"check": m{"primitive": "no.such"}, "interval": "10ms", "timeout": "30s"}, "unknown primitive")
		r.mustError("wait.until", m{"check": m{"primitive": "assert.equals", "args": m{"nope": 1}}, "interval": "10ms", "timeout": "30s"}, "unknown argument")
		if time.Since(start) > 2*time.Second {
			t.Error("a misconfigured check was retried until the timeout instead of failing at once")
		}
	})
	t.Run("an interval that would spin is an error", func(t *testing.T) {
		r, _ := check(1, false)
		r.mustError("wait.until", m{"check": m{"primitive": "t.flaky"}, "interval": "1ms"}, "too short")
	})
}

func TestWaitUntilChecksTheNestedCallWhenAScenarioLoads(t *testing.T) {
	r := newRig(t)
	tests := []struct {
		name string
		args m
		want string
	}{
		{"valid", m{"check": m{"primitive": "assert.equals", "args": m{"actual": "a", "expected": "a"}}}, ""},
		{"a typo in the nested arguments", m{"check": m{"primitive": "assert.equals", "args": m{"actual": "a", "expectd": "a"}}}, `unknown argument "expectd"`},
		{"a missing nested argument", m{"check": m{"primitive": "assert.equals", "args": m{"actual": "a"}}}, `missing required argument "expected"`},
		{"an unknown nested primitive", m{"check": m{"primitive": "assert.nope"}}, `"assert.nope" is not a known primitive`},
		{"no nested primitive", m{"check": m{}}, "needs a primitive"},
		{"a nested template is left for run time", m{"check": m{"primitive": "assert.equals", "args": m{"actual": "{{ .Outputs.x }}", "expected": "a"}}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Join(r.reg.CheckArgs("wait.until", tt.args), "; ")
			if tt.want == "" && got != "" || !strings.Contains(got, tt.want) {
				t.Errorf("CheckArgs = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWaitSleep(t *testing.T) {
	r := newRig(t)
	start := time.Now()
	r.mustPass("wait.sleep", m{"duration": "30ms"})
	if time.Since(start) < 30*time.Millisecond {
		t.Error("it returned before the time was up")
	}
	r.mustError("wait.sleep", m{"duration": "31s"}, "use wait.until")
	r.mustError("wait.sleep", m{"duration": "0s"}, "between")

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if _, err := r.reg.Run(ctx, "wait.sleep", r.env, m{"duration": "20s"}); !errors.Is(err, context.Canceled) {
		t.Errorf("Run = %v, want it to stop when the run ends", err)
	}
}

func TestAssertEqualsContainsMatches(t *testing.T) {
	r := newRig(t)

	r.mustPass("assert.equals", m{"actual": "node-1\n", "expected": "node-1"})
	r.mustFail("assert.equals", m{"actual": "node-1\n", "expected": "node-1", "trim": false}, "expected")
	r.mustFail("assert.equals", m{"actual": "a", "expected": "b"}, `expected "b", got "a"`)

	r.mustPass("assert.contains", m{"text": "cluster is healthy", "substring": "healthy"})
	res := r.mustFail("assert.contains", m{"text": "cluster is sealed", "substring": "healthy"}, `does not contain "healthy"`)
	if res.Evidence != "cluster is sealed" {
		t.Errorf("evidence = %q, want the text that was searched", res.Evidence)
	}

	r.mustPass("assert.matches", m{"text": "voters: 3", "pattern": `voters: \d+`})
	r.mustFail("assert.matches", m{"text": "voters: x", "pattern": `^voters: \d+$`}, "does not match")
	r.mustError("assert.matches", m{"text": "x", "pattern": "("}, "not a valid regular expression")
}

func TestAssertJSONPath(t *testing.T) {
	r := newRig(t)
	doc := `{"data":{"sealed":false,"voters":3,"ratio":1.5,"name":"vault","peers":[{"id":"a"},{"id":"b"}],"none":null}}`
	tests := []struct {
		path, want string
	}{
		{"data.sealed", "false"},
		{"data.voters", "3"},
		{"data.ratio", "1.5"},
		{"data.name", "vault"},
		{"data.peers[1].id", "b"},
		{"data.none", "null"},
		{"data.peers[0]", `{"id":"a"}`},
	}
	for _, tt := range tests {
		res := r.mustPass("assert.json-path", m{"json": doc, "path": tt.path, "equals": tt.want, "save": "v"})
		if res.Outputs["v"] != tt.want {
			t.Errorf("%s saved %q, want %q", tt.path, res.Outputs["v"], tt.want)
		}
	}

	r.mustPass("assert.json-path", m{"json": doc, "path": "data.name"}) // exists is enough when equals is not given
	r.mustFail("assert.json-path", m{"json": doc, "path": "data.voters", "equals": "5"}, `is "3", expected "5"`)
	r.mustFail("assert.json-path", m{"json": doc, "path": "data.missing"}, `there is no "missing"`)
	r.mustFail("assert.json-path", m{"json": doc, "path": "data.peers[5]"}, "there is no item 5")
	r.mustFail("assert.json-path", m{"json": doc, "path": "data.name.deeper"}, "not an object")
	r.mustFail("assert.json-path", m{"json": doc, "path": "data..name"}, "not valid")
	r.mustError("assert.json-path", m{"json": "not json", "path": "a"}, "not valid JSON")
}
