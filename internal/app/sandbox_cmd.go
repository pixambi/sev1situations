package app

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"text/tabwriter"

	"github.com/pixambi/sev1situations/internal/engine"
	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/scenario"
)

// This file is a temporary harness (T-057). It runs the Vault sandbox through the
// real engine on real Podman and prints the Cluster tab as text, so the first
// vertical slice can be proved before any UI exists. It goes when the window does.

const sandboxScenarioDir = builtinRoot + "/vault/sandbox"

const sandboxUsage = `Usage: s1s sandbox [--nodes N] [--edition community|enterprise] [--version V]

Deploys the Vault sandbox on Podman, prints what the Cluster tab will show, and
waits. Press Enter or Ctrl-C to tear everything down. This is a temporary command
for trying the engine before the window exists.
`

func sandboxCommand(ctx context.Context, args []string, stdout, stderr io.Writer, opts Options) int {
	fl := flag.NewFlagSet("sandbox", flag.ContinueOnError)
	fl.SetOutput(stderr)
	fl.Usage = func() { fmt.Fprint(stderr, sandboxUsage) }
	nodes := fl.Int("nodes", 3, "number of Vault nodes, 1 to 9")
	edition := fl.String("edition", "community", "community or enterprise")
	version := fl.String("version", "", "Vault version, default the newest")
	if err := fl.Parse(args); err != nil {
		return exitUsage
	}
	if fl.NArg() > 0 {
		fl.Usage()
		return exitUsage
	}

	if err := runSandbox(ctx, stdout, stderr, opts, sandboxChoice{Nodes: *nodes, Edition: *edition, Version: *version}); err != nil {
		fmt.Fprintf(stderr, "s1s sandbox: %v\n", err)
		return exitFail
	}
	return exitOK
}

type sandboxChoice struct {
	Nodes   int
	Edition string
	Version string
}

func runSandbox(ctx context.Context, stdout, stderr io.Writer, opts Options, choice sandboxChoice) error {
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	conn, err := Connect(ctx, opts, log, func(ev engine.Event) {
		if ev.Kind == engine.EventDeployProgress || ev.Kind == engine.EventDeployFailed || ev.Kind == engine.EventTeardown {
			fmt.Fprintf(stdout, "  %s\n", ev.Message)
		}
	})
	if err != nil {
		return err
	}
	for _, id := range conn.Startup.RemovedRuns {
		fmt.Fprintf(stdout, "removed what run %s left behind\n", id)
	}
	for _, f := range conn.Startup.Failures {
		fmt.Fprintf(stdout, "could not fully remove run %s: %v\n", f.RunID, f.Err)
	}
	products, store, deployer, eng := conn.Products, conn.Settings, conn.Deployer, conn.Engine

	sc, err := loadSandbox(opts, products)
	if err != nil {
		return err
	}
	params, err := sandboxParams(ctx, products, choice)
	if err != nil {
		return err
	}

	instructor := store.Instructor()
	if instructor == "" {
		instructor = "Instructor"
	}
	fmt.Fprintf(stdout, "Deploying the Vault sandbox: %d node(s), %s edition\n", choice.Nodes, choice.Edition)
	if err := eng.Start(ctx, sc, engine.StartInput{Instructor: instructor, Trainee: "Trainee", Params: params}); err != nil {
		// A failed deployment leaves its containers so they can be inspected. Clean up and report.
		_, endErr := eng.End(context.WithoutCancel(ctx))
		return errors.Join(err, endErr)
	}

	snap := eng.Snapshot()
	fmt.Fprintf(stdout, "\nRun %s is up.\n\n", snap.RunID)
	renderCluster(stdout, deployer.View(ctx, snap.RunID), snap.Nodes)
	fmt.Fprint(stdout, "\nPress Enter or Ctrl-C to tear everything down.\n")

	waitForEnter(ctx, opts.Stdin)

	fmt.Fprintln(stdout, "\nTearing down")
	final, err := eng.End(context.WithoutCancel(ctx))
	if err != nil {
		return fmt.Errorf("teardown: %w", err)
	}
	fmt.Fprintf(stdout, "Run %s ended: %s. Everything it created was removed.\n", final.RunID, final.Result)
	return nil
}

// loadSandbox loads the built-in sandbox scenario.
func loadSandbox(opts Options, products *product.Registry) (*scenario.Scenario, error) {
	if opts.Builtin == nil {
		return nil, errors.New("this build has no built-in scenarios")
	}
	sc, err := scenario.Load(opts.Builtin, path.Clean(sandboxScenarioDir))
	if err != nil {
		return nil, err
	}
	prims := primitive.NewRegistry()
	if err := primitive.RegisterGeneric(prims); err != nil {
		return nil, err
	}
	if err := products.RegisterPrimitives(prims); err != nil {
		return nil, err
	}
	if err := sc.Validate(scenario.Options{Primitives: prims, Products: products.Names()}); err != nil {
		return nil, err
	}
	return sc, nil
}

// sandboxParams turns the flags into the Start step's parameters. With no version
// chosen the newest is used (D1).
func sandboxParams(ctx context.Context, products *product.Registry, c sandboxChoice) (map[string]any, error) {
	if c.Nodes < 1 || c.Nodes > 9 {
		return nil, fmt.Errorf("--nodes must be from 1 to 9, not %d", c.Nodes)
	}
	var ed release.Edition
	switch c.Edition {
	case "community":
		ed = release.Community
	case "enterprise":
		ed = release.Enterprise
	default:
		return nil, fmt.Errorf("--edition must be community or enterprise, not %q", c.Edition)
	}
	version := c.Version
	if version == "" {
		p, ok := products.Get("vault")
		if !ok {
			return nil, errors.New("the Vault product is not registered")
		}
		versions, err := p.Versions(ctx, ed)
		if err != nil || len(versions) == 0 {
			return nil, fmt.Errorf("find the newest Vault version (are you online?): %w", errOrNone(err))
		}
		version = versions[0].Core
	}
	return map[string]any{"version": version, "edition": c.Edition, "nodes": c.Nodes}, nil
}

func errOrNone(err error) error {
	if err != nil {
		return err
	}
	return errors.New("no versions were listed")
}

// waitForEnter blocks until a line arrives on in or the context ends.
func waitForEnter(ctx context.Context, in io.Reader) {
	if in == nil {
		<-ctx.Done()
		return
	}
	got := make(chan struct{}, 1)
	go func() {
		_, _ = bufio.NewReader(in).ReadString('\n')
		got <- struct{}{}
	}()
	select {
	case <-got:
	case <-ctx.Done():
	}
}

// renderCluster prints what the Cluster tab shows: each section's facts, then each
// node with its address, container state and the product's own details.
func renderCluster(w io.Writer, view product.ClusterView, nodes []engine.NodeView) {
	byName := map[string]engine.NodeView{}
	for _, n := range nodes {
		byName[n.Name] = n
	}
	for _, sec := range view.Sections {
		fmt.Fprintf(w, "%s\n", sec.Title)
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		for _, f := range sec.Facts {
			label := f.Label
			if f.Secret {
				label += " (secret)"
			}
			fmt.Fprintf(tw, "  %s\t%s\n", label, f.Value)
		}
		_ = tw.Flush()

		fmt.Fprintln(w)
		tw = tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "  NODE\tADDRESS\tCONTAINER\tDETAILS")
		for _, ni := range sec.Nodes {
			n := byName[ni.Name]
			var details []string
			for _, d := range ni.Details {
				if d.Label != "Node ID" {
					details = append(details, d.Label+": "+d.Value)
				}
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", ni.Name, n.IP, n.State, strings.Join(details, "  "))
		}
		_ = tw.Flush()
		fmt.Fprintln(w)
	}
}
