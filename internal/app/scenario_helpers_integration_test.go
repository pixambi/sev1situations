//go:build integration

package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/engine"
	"github.com/pixambi/sev1situations/internal/report"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/scenario"
)

func userCache(t *testing.T) string {
	t.Helper()
	d, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("no user cache directory: %v", err)
	}
	return d
}

func shellOut(name string, args ...string) (string, error) {
	out, err := osexec.Command(name, args...).CombinedOutput()
	return string(out), err
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// rid is the ID of the report a finished run saved.
func rid(s engine.Snapshot) string { return report.NewID(s.RunID, s.StartedAt) }

// realRun is a built-in scenario about to be played on a real Podman machine, with the core joined to it
// and its own data folder, in which the cached Vault binary has been placed.
type realRun struct {
	t     *testing.T
	ctx   context.Context
	conn  *Connection
	eng   *engine.Engine
	entry scenario.Entry
	newer string // the newest community version of the scenario's product
}

// playScenario sets up a real run of the built-in scenario with the given ID. It skips the test when
// there is no Podman machine or no cached binary of the product.
func playScenario(t *testing.T, id, productName string, timeout time.Duration) *realRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)

	cache := filepath.Join(userCache(t), "sev1situations-integration", productName)
	if _, err := os.Stat(cache); err != nil {
		t.Skipf("no cached %s binary at %s: run that product's integration tests first", productName, cache)
	}
	data := t.TempDir()
	t.Setenv("S1S_DATA_DIR", data)
	if err := os.MkdirAll(filepath.Join(data, "cache", "binaries"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := shellOut("cp", "-r", cache, filepath.Join(data, "cache", "binaries")); err != nil {
		t.Fatalf("seed the binary cache: %v %s", err, out)
	}

	opts := Options{Builtin: os.DirFS("../.."), Stdin: strings.NewReader("")}
	conn, err := Connect(ctx, opts, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Skipf("Podman is not available: %v", err)
	}
	t.Cleanup(func() { _, _ = conn.Engine.End(context.Background()) })

	opts.Products = conn.Products // so the scenario's vault primitives are known when it is checked
	reg, err := LoadCatalogue(opts)
	if err != nil {
		t.Fatalf("catalogue: %v", err)
	}
	entry, ok := reg.Get(id)
	if !ok {
		t.Fatalf("scenario %q is not in the catalogue", id)
	}
	versions, err := Versions(conn.Products)(ctx, productName, "community")
	if err != nil || len(versions) == 0 {
		t.Skipf("cannot list Vault versions: %v", err)
	}
	return &realRun{t: t, ctx: ctx, conn: conn, eng: conn.Engine, entry: entry, newer: versions[0]}
}

// step runs one stage and logs how long it took.
func (r *realRun) step(what string, f func() error) {
	r.t.Helper()
	t0 := time.Now()
	if err := f(); err != nil {
		r.t.Fatalf("%s: %v", what, err)
	}
	r.t.Logf("%-40s %s", what, time.Since(t0).Round(time.Second))
}

// start deploys the scenario as a community edition of the newest Vault.
func (r *realRun) start() {
	r.t.Helper()
	r.step("deploy", func() error {
		return r.eng.Start(r.ctx, r.entry.Scenario, engine.StartInput{
			Instructor: "Ada", Trainee: "Grace", Params: map[string]any{"version": r.newer, "edition": "community"},
		})
	})
}

func (r *realRun) outcome(stepID string) *engine.Outcome {
	for _, s := range r.eng.Snapshot().Steps {
		if s.ID == stepID {
			return s.Last
		}
	}
	return nil
}

func (r *realRun) mustPass(stepID string) {
	r.t.Helper()
	if o := r.outcome(stepID); o == nil || o.Status != "pass" {
		r.t.Fatalf("step %s did not pass: %+v", stepID, o)
	}
}

// sh runs a shell command in a node, as an instructor would in a terminal, and returns the result.
func (r *realRun) sh(node, script string, env map[string]string, detach bool) runtime.ExecResult {
	r.t.Helper()
	id, err := r.eng.ContainerOf(node)
	if err != nil {
		r.t.Fatal(err)
	}
	res, err := r.conn.Runtime.Exec(r.ctx, id, []string{"sh", "-c", script}, runtime.ExecOptions{Env: env, Detach: detach})
	if err != nil {
		r.t.Fatalf("%s: %q: %v", node, script, err)
	}
	return res
}

// secretFact reads a secret fact from the Cluster tab, as the instructor would copy it.
func (r *realRun) secretFact(section, label string) string {
	r.t.Helper()
	for _, sec := range r.conn.Deployer.View(r.ctx, r.eng.Snapshot().RunID).Sections {
		if !strings.Contains(strings.ToLower(sec.Title), section) {
			continue
		}
		for _, f := range sec.Facts {
			if f.Label == label {
				return f.Value
			}
		}
	}
	r.t.Fatalf("the Cluster tab has no %q under %q", label, section)
	return ""
}

// finish ends the run and checks that a report was saved and nothing was left on the machine.
func (r *realRun) finish(steps int) {
	r.t.Helper()
	final, err := r.eng.End(r.ctx)
	if err != nil {
		r.t.Fatal(err)
	}
	rep, err := r.conn.Reports.Get(rid(final))
	if err != nil {
		r.t.Fatalf("no report: %v", err)
	}
	if rep.Result != "completed" || len(rep.Steps) != steps {
		r.t.Errorf("report = %s with %d steps, want completed with %d", rep.Result, len(rep.Steps), steps)
	}
	if left, _ := r.conn.Runtime.Runs(r.ctx); len(left) != 0 {
		r.t.Errorf("runs left behind: %v", left)
	}
}
