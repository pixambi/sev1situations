//go:build integration

package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/engine"
	"github.com/pixambi/sev1situations/internal/primitive"
)

// TestEveryGenericPrimitiveOnRealPodman runs the generic primitives, and the Boundary ones no scenario uses, against a real
// deployment, so each has been seen to work on real containers and not only on a fake (R-CONTRIB-4). TestEveryPrimitiveIsTestedAndRunForReal
// fails when a primitive has no such run.
func TestEveryGenericPrimitiveOnRealPodman(t *testing.T) {
	r := playScenario(t, "boundary-sandbox", "boundary", 15*time.Minute)
	r.step("deploy", func() error {
		return r.eng.Start(r.ctx, r.entry.Scenario, engine.StartInput{
			Instructor: "Ada", Trainee: "Grace", Params: map[string]any{"version": r.newer, "edition": "community", "controllers": 1},
		})
	})

	reg := primitive.NewRegistry()
	if err := primitive.RegisterGeneric(reg); err != nil {
		t.Fatal(err)
	}
	if err := r.conn.Products.RegisterPrimitives(reg); err != nil {
		t.Fatal(err)
	}
	nodes := map[string]primitive.Node{}
	for _, n := range r.eng.Snapshot().Nodes {
		id, err := r.eng.ContainerOf(n.Name)
		if err != nil {
			t.Fatal(err)
		}
		nodes[n.Name] = primitive.Node{Name: n.Name, Cluster: "boundary", ContainerID: id}
	}
	files := map[string]string{"fixtures/data.json": `{"copied":"yes"}`, "templates/node.tmpl": "rendered for {{ .Name }}"}
	env := primitive.Env{
		Runtime: r.conn.Runtime, Nodes: nodes,
		Creds: map[string]map[string]string{"boundary": {"adminPassword": r.secretFact("boundary", "Admin password")}},
		ReadFile: func(rel string) ([]byte, error) {
			if c, ok := files[rel]; ok {
				return []byte(c), nil
			}
			return nil, fmt.Errorf("no file %s", rel)
		},
		Render: func(src string) (string, error) { return strings.ReplaceAll(src, "{{ .Name }}", "target-node-1"), nil },
	}
	run := func(name string, args map[string]any) primitive.Result {
		t.Helper()
		res, err := reg.Run(r.ctx, name, env, args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%-28s %s: %s", name, res.Status, tail(res.Message, 90))
		return res
	}
	pass := func(name string, args map[string]any) {
		t.Helper()
		if res := run(name, args); res.Status != primitive.Pass {
			t.Errorf("%s did not pass: %+v", name, res)
		}
	}
	exec := func(node string, contains string, command ...string) {
		t.Helper()
		pass("container.exec", map[string]any{"node": node, "command": command, "contains": contains})
	}

	// Files go into a real node, and can be read back.
	pass("file.write", map[string]any{"node": "target-node-1", "path": "/tmp/written.txt", "content": "written for real"})
	exec("target-node-1", "written for real", "cat", "/tmp/written.txt")
	pass("container.copy-in", map[string]any{"node": "target-node-1", "source": "fixtures/data.json", "path": "/tmp/data.json"})
	exec("target-node-1", `"copied":"yes"`, "cat", "/tmp/data.json")
	pass("file.render", map[string]any{"node": "target-node-1", "template": "templates/node.tmpl", "path": "/tmp/rendered.txt"})
	exec("target-node-1", "rendered for target-node-1", "cat", "/tmp/rendered.txt")

	// A process can be killed, and a node stopped and started.
	exec("target-node-1", "", "sh", "-c", "sleep 300 >/dev/null 2>&1 & sleep 1; pidof sleep")
	pass("container.kill-process", map[string]any{"node": "target-node-1", "process": "sleep", "signal": "KILL"})
	pass("wait.until", map[string]any{"interval": "1s", "timeout": "20s", "check": map[string]any{
		"primitive": "container.exec", "args": map[string]any{"node": "target-node-1", "command": []string{"sh", "-c", "ps -o stat,args | grep '[s]leep 300' | grep -v '^Z'"}, "expectExit": 1}}})
	pass("container.stop", map[string]any{"nodes": []string{"worker-node-1"}})
	pass("container.start", map[string]any{"nodes": []string{"worker-node-1"}})
	exec("worker-node-1", "back", "echo", "back")

	// HTTP is made from this computer, to a port the load balancer publishes.
	url := r.secretFact("boundary", "Cluster URL")
	pass("http.request", map[string]any{"url": url + "/v1/auth-methods?scope_id=global", "contains": "password"})

	// The checks.
	pass("assert.equals", map[string]any{"actual": " a ", "expected": "a"})
	pass("assert.contains", map[string]any{"text": "the quick fox", "substring": "quick"})
	pass("assert.matches", map[string]any{"text": "node-12", "pattern": `^node-\d+$`})
	pass("assert.json-path", map[string]any{"json": `{"a":{"b":[1,2,3]}}`, "path": "a.b[1]", "equals": "2"})
	pass("wait.sleep", map[string]any{"duration": "1s"})

	// Boundary: a session that was asked for is listed, and recordings are an Enterprise feature.
	pass("boundary.authorize-session", map[string]any{"node": "node-1"})
	pass("boundary.session-info", map[string]any{"node": "node-1", "status": "pending", "atLeast": 1})
	res := run("boundary.recordings-list", map[string]any{"node": "node-1", "atLeast": 1})
	if res.Status != primitive.Fail || !strings.Contains(res.Evidence, "Enterprise-only") {
		t.Errorf("a community Boundary did not say recordings are Enterprise-only: %+v", res)
	}

	r.finish(0)
}
