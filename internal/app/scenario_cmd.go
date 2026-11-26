package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/scenario"
)

// Options are what the headless commands need from the program around them.
type Options struct {
	// Builtin holds the embedded scenario tree, with the scenarios under a
	// folder named "scenarios". It may be nil.
	Builtin fs.FS
	// UserDir is the user's scenario directory. Empty means the default
	// (UserScenarioDir).
	UserDir string
}

// builtinRoot is the folder inside Options.Builtin that holds the scenarios.
const builtinRoot = "scenarios"

// knownProducts lists the product names a scenario may use. It is a temporary
// list until the product registry exists (T-050).
func knownProducts() []string { return []string{"vault", "boundary"} }

const scenarioUsage = `Usage: s1s scenario <command>

Commands:
  list                         list the built-in scenarios and your own
  validate [path]              check scenario folders (all of them if no path is given)
  new <id> --product <name>    create a scenario folder to fill in
                               (--dir <parent> chooses where, default the current directory)
`

// scenarioRegistry builds a registry with the generic primitives, so
// validation can check names and arguments.
func scenarioRegistry() (*scenario.Registry, error) {
	prims := primitive.NewRegistry()
	if err := primitive.RegisterGeneric(prims); err != nil {
		return nil, fmt.Errorf("register primitives: %w", err)
	}
	return scenario.NewRegistry(scenario.Options{Primitives: prims, Products: knownProducts()}), nil
}

// scenarioCommand runs `s1s scenario ...`.
func scenarioCommand(args []string, stdout, stderr io.Writer, opts Options) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, scenarioUsage)
		return exitUsage
	}
	switch args[0] {
	case "list":
		return scenarioList(stdout, stderr, opts)
	case "validate":
		return scenarioValidate(args[1:], stdout, stderr, opts)
	case "new":
		return scenarioNew(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, scenarioUsage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "s1s scenario: unknown command %q\n\n%s", args[0], scenarioUsage)
		return exitUsage
	}
}

// loadCatalogue loads the built-in and user scenarios. The returned error
// lists every scenario that did not load.
func loadCatalogue(opts Options) (*scenario.Registry, error) {
	reg, _, err := loadCatalogueAt(opts)
	return reg, err
}

// loadCatalogueAt is loadCatalogue that also returns the user directory it used.
func loadCatalogueAt(opts Options) (*scenario.Registry, string, error) {
	reg, err := scenarioRegistry()
	if err != nil {
		return nil, "", err
	}
	var problems []error
	if opts.Builtin != nil {
		if err := reg.Load(opts.Builtin, builtinRoot, scenario.BuiltIn); err != nil {
			problems = append(problems, err)
		}
	}
	dir := opts.UserDir
	if dir == "" {
		d, err := UserScenarioDir()
		if err != nil {
			return reg, "", err
		}
		dir = d
	}
	if err := reg.Load(os.DirFS(dir), ".", scenario.User); err != nil {
		problems = append(problems, prefixFiles(err, dir))
	}
	return reg, dir, errors.Join(problems...)
}

// prefixFiles makes the file names in a load error real paths. Scenarios loaded
// from a directory are named relative to it, which would hide where they are.
func prefixFiles(err error, dir string) error {
	var le *scenario.LoadError
	if !errors.As(err, &le) {
		return err
	}
	out := &scenario.LoadError{}
	for _, p := range le.Problems {
		p.File = filepath.Join(dir, p.File)
		out.Problems = append(out.Problems, p)
	}
	return out
}

func scenarioList(stdout, stderr io.Writer, opts Options) int {
	reg, err := loadCatalogue(opts)
	entries := reg.List()

	if len(entries) == 0 {
		fmt.Fprintln(stdout, "No scenarios found.")
	} else {
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tPRODUCT\tDIFFICULTY\tMINUTES\tSOURCE\tNAME")
		for _, e := range entries {
			s := e.Scenario
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n", s.ID, s.Product, s.Difficulty, s.EstimatedMinutes, e.Source, s.Name)
		}
		_ = tw.Flush()
	}

	if err != nil {
		fmt.Fprintf(stderr, "\nSome scenarios could not be loaded:\n%v\n", err)
		return exitFail
	}
	return exitOK
}

func scenarioValidate(args []string, stdout, stderr io.Writer, opts Options) int {
	if len(args) > 1 {
		fmt.Fprint(stderr, "Usage: s1s scenario validate [path]\n")
		return exitUsage
	}

	var reg *scenario.Registry
	var err error
	// where turns a scenario's folder into a path the reader can find.
	var where func(scenario.Entry) string
	if len(args) == 1 {
		reg, err = validatePath(args[0])
		where = func(e scenario.Entry) string { return filepath.Join(args[0], e.Scenario.Dir) }
	} else {
		var userDir string
		reg, userDir, err = loadCatalogueAt(opts)
		where = func(e scenario.Entry) string {
			if e.Source == scenario.User {
				return filepath.Join(userDir, e.Scenario.Dir)
			}
			return "built-in:" + e.Scenario.Dir
		}
	}

	entries := reg.List()
	for _, e := range entries {
		fmt.Fprintf(stdout, "ok: %s (%s)\n", where(e), e.Scenario.ID)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFail
	}
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "No scenarios found.")
	}
	return exitOK
}

// validatePath loads every scenario under a directory on disk.
func validatePath(path string) (*scenario.Registry, error) {
	reg, err := scenarioRegistry()
	if err != nil {
		return nil, err
	}
	info, statErr := os.Stat(path)
	if statErr != nil || !info.IsDir() {
		return reg, fmt.Errorf("%s is not a directory", path)
	}
	return reg, prefixFiles(reg.Load(os.DirFS(path), ".", scenario.User), path)
}

var kebab = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func scenarioNew(args []string, stdout, stderr io.Writer) int {
	var id string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	fl := flag.NewFlagSet("scenario new", flag.ContinueOnError)
	fl.SetOutput(stderr)
	product := fl.String("product", "", "the product the scenario is about (vault or boundary)")
	dir := fl.String("dir", ".", "the folder to create the scenario folder in")
	if err := fl.Parse(args); err != nil {
		return exitUsage
	}
	if id == "" && fl.NArg() == 1 {
		id = fl.Arg(0)
	}
	if id == "" || fl.NArg() > 1 || (id != "" && fl.NArg() == 1 && fl.Arg(0) != id) {
		fmt.Fprint(stderr, "Usage: s1s scenario new <id> --product <name> [--dir <parent>]\n")
		return exitUsage
	}

	if err := createScenario(*dir, id, *product, stdout); err != nil {
		fmt.Fprintf(stderr, "s1s scenario new: %v\n", err)
		return exitFail
	}
	return exitOK
}

// createScenario writes a scenario folder from the template and checks that it
// passes validation, so a contributor starts from something valid.
func createScenario(parent, id, product string, stdout io.Writer) error {
	if !kebab.MatchString(id) {
		return fmt.Errorf("the id %q must be kebab-case: lower-case letters and digits separated by single dashes", id)
	}
	if !contains(knownProducts(), product) {
		return fmt.Errorf("--product must be one of: %s", strings.Join(knownProducts(), ", "))
	}

	target := filepath.Join(parent, id)
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("%s already exists", target)
	}

	files := scaffoldFiles(id, product)
	for name, body := range files {
		path := filepath.Join(target, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}

	reg, err := validatePath(target)
	if err != nil || len(reg.List()) != 1 {
		return fmt.Errorf("the new scenario does not pass validation, which is a bug in the template: %w", err)
	}
	fmt.Fprintf(stdout, "Created %s\n\nIt passes `s1s scenario validate`. Search it for TODO and replace each one with real text, then validate again.\n", target)
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
