package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pixambi/sev1situations/internal/product"
	productfake "github.com/pixambi/sev1situations/internal/product/fake"
)

// scaffoldInto adds a valid scenario, taken from the scaffold, to a MapFS.
func scaffoldInto(fsys fstest.MapFS, dir, id, product string) {
	for name, body := range scaffoldFiles(id, product) {
		fsys[dir+"/"+name] = &fstest.MapFile{Data: []byte(body)}
	}
}

// writeScaffold writes a valid scenario to a real directory.
func writeScaffold(t *testing.T, root, id, product string) string {
	t.Helper()
	dir := filepath.Join(root, id)
	for name, body := range scaffoldFiles(id, product) {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func cli(t *testing.T, opts Options, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(context.Background(), args, &out, &errb, fakeSystem{}, opts)
	return code, out.String(), errb.String()
}

func TestScenarioList(t *testing.T) {
	builtin := fstest.MapFS{}
	scaffoldInto(builtin, "scenarios/vault/sandbox", "vault-sandbox", "vault")
	scaffoldInto(builtin, "scenarios/boundary/sandbox", "boundary-sandbox", "boundary")
	userDir := t.TempDir()
	writeScaffold(t, userDir, "vault-mine", "vault")

	code, out, errOut := cli(t, Options{Builtin: builtin, UserDir: userDir}, "scenario", "list")
	if code != exitOK || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "ID") {
		t.Fatalf("output = %q, want a header and three scenarios", out)
	}
	// Ordered by product, then name.
	for i, want := range []string{"boundary-sandbox", "vault-mine", "vault-sandbox"} {
		if !strings.HasPrefix(lines[i+1], want) {
			t.Errorf("line %d = %q, want it to start with %s", i+1, lines[i+1], want)
		}
	}
	if !strings.Contains(lines[2], "user") || !strings.Contains(lines[1], "built-in") {
		t.Errorf("the source column is wrong:\n%s", out)
	}
}

func TestScenarioListWithNothing(t *testing.T) {
	code, out, _ := cli(t, Options{UserDir: filepath.Join(t.TempDir(), "none")}, "scenario", "list")
	if code != exitOK || !strings.Contains(out, "No scenarios found.") {
		t.Errorf("exit %d output %q, want a clear message and success", code, out)
	}
}

func TestListShowsTheGoodAndReportsTheBroken(t *testing.T) {
	userDir := t.TempDir()
	writeScaffold(t, userDir, "vault-good", "vault")
	bad := writeScaffold(t, userDir, "vault-bad", "vault")
	path := filepath.Join(bad, "scenario.yaml")
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "difficulty: beginner", "difficulty: hard", 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := cli(t, Options{UserDir: userDir}, "scenario", "list")
	if code != exitFail {
		t.Errorf("exit = %d, want %d because one scenario is broken", code, exitFail)
	}
	if !strings.Contains(out, "vault-good") || strings.Contains(out, "vault-bad") {
		t.Errorf("output = %q, want the good scenario listed and the broken one left out", out)
	}
	if !strings.Contains(errOut, path+":") || !strings.Contains(errOut, "difficulty") {
		t.Errorf("stderr = %q, want the real path of the file, its line and the field", errOut)
	}
}

func TestScenarioValidate(t *testing.T) {
	root := t.TempDir()
	good := writeScaffold(t, root, "vault-ok", "vault")

	t.Run("a good folder", func(t *testing.T) {
		code, out, errOut := cli(t, Options{}, "scenario", "validate", good)
		if code != exitOK || errOut != "" || !strings.Contains(out, "ok: "+good+" (vault-ok)") {
			t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
		}
	})

	t.Run("a folder of folders", func(t *testing.T) {
		writeScaffold(t, root, "boundary-ok", "boundary")
		code, out, _ := cli(t, Options{}, "scenario", "validate", root)
		if code != exitOK || strings.Count(out, "ok: ") != 2 {
			t.Errorf("exit %d output %q, want both scenarios found", code, out)
		}
	})

	t.Run("problems name the file, line and field", func(t *testing.T) {
		bad := writeScaffold(t, t.TempDir(), "vault-bad", "vault")
		p := filepath.Join(bad, "scenario.yaml")
		data, _ := os.ReadFile(p)
		broken := strings.Replace(string(data), "kind: breakage", "kind: celebration", 1)
		broken = strings.Replace(broken, "guide: steps/02-fix.md", "guide: steps/missing.md", 1)
		broken = strings.Replace(broken, "primitive: container.exec\n        args: { node: node-1, command: [sh, -c, \"echo TODO check that it is fixed\"] }", "primitive: container.explode", 1)
		if err := os.WriteFile(p, []byte(broken), 0o644); err != nil {
			t.Fatal(err)
		}

		code, _, errOut := cli(t, Options{}, "scenario", "validate", bad)
		if code != exitFail {
			t.Errorf("exit = %d, want %d", code, exitFail)
		}
		for _, want := range []string{p + ":", "steps[0].kind", "not a step kind", "steps[1].guide", "does not exist", "container.explode", "not a known primitive"} {
			if !strings.Contains(errOut, want) {
				t.Errorf("stderr is missing %q:\n%s", want, errOut)
			}
		}
	})

	t.Run("a path that is not a folder", func(t *testing.T) {
		code, _, errOut := cli(t, Options{}, "scenario", "validate", filepath.Join(root, "nope"))
		if code != exitFail || !strings.Contains(errOut, "is not a directory") {
			t.Errorf("exit %d stderr %q", code, errOut)
		}
	})

	t.Run("with no path it checks the built-in and user scenarios", func(t *testing.T) {
		builtin := fstest.MapFS{}
		scaffoldInto(builtin, "scenarios/vault/a", "vault-a", "vault")
		code, out, _ := cli(t, Options{Builtin: builtin, UserDir: t.TempDir()}, "scenario", "validate")
		if code != exitOK || !strings.Contains(out, "ok: built-in:scenarios/vault/a (vault-a)") {
			t.Errorf("exit %d output %q, want the built-in scenario labelled as such", code, out)
		}
	})

	t.Run("too many arguments", func(t *testing.T) {
		if code, _, _ := cli(t, Options{}, "scenario", "validate", "a", "b"); code != exitUsage {
			t.Errorf("exit = %d, want %d", code, exitUsage)
		}
	})
}

func TestScenarioNewProducesAFolderThatPassesValidate(t *testing.T) {
	parent := t.TempDir()
	code, out, errOut := cli(t, Options{}, "scenario", "new", "vault-raft-demo", "--product", "vault", "--dir", parent)
	if code != exitOK || errOut != "" {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	target := filepath.Join(parent, "vault-raft-demo")
	if !strings.Contains(out, target) || !strings.Contains(out, "TODO") {
		t.Errorf("output = %q, want the path and a pointer to the TODOs", out)
	}

	for _, f := range []string{"scenario.yaml", "steps/01-break.md", "steps/02-fix.md", "templates/.gitkeep", "fixtures/.gitkeep"} {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(f))); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	yaml, _ := os.ReadFile(filepath.Join(target, "scenario.yaml"))
	for _, want := range []string{"id: vault-raft-demo", "name: Vault Raft Demo", "product: vault", "label: Vault version"} {
		if !strings.Contains(string(yaml), want) {
			t.Errorf("scenario.yaml is missing %q", want)
		}
	}

	// The acceptance test for T-040: what `new` makes passes `validate`, through the real command.
	code, vout, verr := cli(t, Options{}, "scenario", "validate", target)
	if code != exitOK || !strings.Contains(vout, "(vault-raft-demo)") {
		t.Errorf("validate of the new scenario: exit %d stdout %q stderr %q", code, vout, verr)
	}
}

func TestScenarioNewForBoundaryAndFlagOrder(t *testing.T) {
	parent := t.TempDir()
	// Flags before the id work too.
	code, _, errOut := cli(t, Options{}, "scenario", "new", "--product", "boundary", "--dir", parent, "boundary-grants")
	if code != exitOK {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	if code, _, e := cli(t, Options{}, "scenario", "validate", filepath.Join(parent, "boundary-grants")); code != exitOK {
		t.Errorf("validate: %d %s", code, e)
	}
}

func TestScenarioNewRefusesBadInput(t *testing.T) {
	parent := t.TempDir()
	writeScaffold(t, parent, "taken", "vault")
	tests := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"an id that is not kebab-case", []string{"Bad_ID", "--product", "vault"}, exitFail, "kebab-case"},
		{"an unknown product", []string{"x", "--product", "nomad"}, exitFail, "must be one of: vault, boundary"},
		{"no product", []string{"x"}, exitFail, "must be one of"},
		{"no id", []string{"--product", "vault"}, exitUsage, "Usage"},
		{"a folder that exists", []string{"taken", "--product", "vault", "--dir", parent}, exitFail, "already exists"},
		{"an unknown flag", []string{"x", "--colour", "red"}, exitUsage, ""},
		{"two ids", []string{"a", "b", "--product", "vault"}, exitUsage, "Usage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"scenario", "new"}, tt.args...)
			if !strings.Contains(strings.Join(tt.args, " "), "--dir") {
				args = append(args, "--dir", parent)
			}
			code, _, errOut := cli(t, Options{}, args...)
			if code != tt.code || !strings.Contains(errOut, tt.want) {
				t.Errorf("exit %d stderr %q, want exit %d containing %q", code, errOut, tt.code, tt.want)
			}
		})
	}

	entries, _ := os.ReadDir(parent)
	if len(entries) != 1 {
		t.Errorf("the folder holds %d entries, want a refused command to create nothing", len(entries))
	}
	// The existing folder must be untouched.
	if _, err := os.Stat(filepath.Join(parent, "taken", "scenario.yaml")); err != nil {
		t.Error("an existing scenario folder was damaged")
	}
}

func TestScenarioUsage(t *testing.T) {
	if code, _, e := cli(t, Options{}, "scenario"); code != exitUsage || !strings.Contains(e, "Usage: s1s scenario") {
		t.Errorf("no subcommand: %d %q", code, e)
	}
	if code, _, e := cli(t, Options{}, "scenario", "bake"); code != exitUsage || !strings.Contains(e, `unknown command "bake"`) {
		t.Errorf("unknown subcommand: %d %q", code, e)
	}
	if code, o, _ := cli(t, Options{}, "scenario", "help"); code != exitOK || !strings.Contains(o, "validate [path]") {
		t.Errorf("help: %d %q", code, o)
	}
}

func TestDataDirectories(t *testing.T) {
	t.Setenv("S1S_DATA_DIR", "/tmp/s1s-test-data")
	dir, err := DataDir()
	if err != nil || dir != "/tmp/s1s-test-data" {
		t.Errorf("DataDir = %q, %v", dir, err)
	}
	if got, _ := UserScenarioDir(); got != "/tmp/s1s-test-data/scenarios" {
		t.Errorf("UserScenarioDir = %q", got)
	}

	t.Setenv("S1S_DATA_DIR", "")
	dir, err = DataDir()
	if err != nil || !strings.HasSuffix(dir, "Sev1Situations") {
		t.Errorf("default DataDir = %q, %v, want it to end in Sev1Situations (D5)", dir, err)
	}
}

func TestTitleFromID(t *testing.T) {
	if got := titleFromID("vault-raft-lost-quorum"); got != "Vault Raft Lost Quorum" {
		t.Errorf("titleFromID = %q", got)
	}
}

func TestScenarioCommandsUseTheProductRegistry(t *testing.T) {
	products := product.NewRegistry()
	for _, name := range []string{"vault", "postgres"} {
		if err := products.Register(productfake.New(name)); err != nil {
			t.Fatal(err)
		}
	}
	opts := Options{Products: products}
	parent := t.TempDir()

	// A product that is registered is accepted, and one that is not is refused with the real list.
	if code, _, e := cli(t, opts, "scenario", "new", "ok-one", "--product", "postgres", "--dir", parent); code != exitOK {
		t.Errorf("a registered product was refused: %d %s", code, e)
	}
	code, _, errOut := cli(t, opts, "scenario", "new", "no-one", "--product", "boundary", "--dir", parent)
	if code != exitFail || !strings.Contains(errOut, "must be one of: postgres, vault") {
		t.Errorf("exit %d stderr %q, want the registered products named", code, errOut)
	}

	// Validation refuses a scenario for a product nobody registered (R-SCOPE-3).
	writeScaffold(t, parent, "boundary-one", "boundary")
	code, _, errOut = cli(t, opts, "scenario", "validate", filepath.Join(parent, "boundary-one"))
	if code != exitFail || !strings.Contains(errOut, `"boundary" is not a known product`) {
		t.Errorf("exit %d stderr %q", code, errOut)
	}
}

func TestDefaultProducts(t *testing.T) {
	reg, err := DefaultProducts()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(reg.Names(), ","); got != "vault" {
		t.Errorf("products = %q, want the products that ship", got)
	}
	fields := reg.SettingsFields()
	if len(fields) != 1 || fields[0].Key != "vault.license" {
		t.Errorf("settings fields = %+v, want the Vault license, brought by the product (R-SET-4)", fields)
	}

	// The shipped scenario commands work with it.
	parent := t.TempDir()
	if code, _, e := cli(t, Options{Products: reg}, "scenario", "new", "vault-one", "--product", "vault", "--dir", parent); code != exitOK {
		t.Errorf("scenario new with the real registry: %d %s", code, e)
	}
	if code, o, e := cli(t, Options{Products: reg}, "scenario", "validate", parent); code != exitOK || !strings.Contains(o, "(vault-one)") {
		t.Errorf("scenario validate with the real registry: %d %q %s", code, o, e)
	}
}

// TestTheBuiltInScenariosAreValid loads the scenarios shipped in the repository's
// scenarios folder, from disk so the test needs no built frontend, against the real
// product registry. A scenario that does not pass validate must not ship (R-SCN-13).
func TestTheBuiltInScenariosAreValid(t *testing.T) {
	products, err := DefaultProducts()
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Builtin: os.DirFS("../.."), UserDir: t.TempDir(), Products: products}

	code, out, errOut := cli(t, opts, "scenario", "validate")
	if code != exitOK || errOut != "" {
		t.Fatalf("validate: exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if !strings.Contains(out, "(vault-sandbox)") {
		t.Errorf("validate output = %q, want the Vault sandbox among the built-in scenarios", out)
	}

	code, out, _ = cli(t, opts, "scenario", "list")
	if code != exitOK || !strings.Contains(out, "vault-sandbox") || !strings.Contains(out, "built-in") {
		t.Errorf("list = %q", out)
	}
}
