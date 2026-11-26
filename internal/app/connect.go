package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	goruntime "runtime"

	"github.com/pixambi/sev1situations/internal/deploy"
	"github.com/pixambi/sev1situations/internal/engine"
	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/runtime/podman"
	"github.com/pixambi/sev1situations/internal/scenario"
	"github.com/pixambi/sev1situations/internal/settings"
)

// Connection is the running core: the engine, joined to Podman, and what the
// window needs beside it.
type Connection struct {
	// Engine runs the one run.
	Engine *engine.Engine
	// Deployer builds environments and describes them for the Cluster tab.
	Deployer *deploy.Deployer
	// Settings is the saved settings store.
	Settings *settings.Store
	// Products are the registered products.
	Products *product.Registry
	// Startup says what start-up cleanup found and removed.
	Startup StartupReport
}

// Connect joins the core to Podman and removes what an earlier launch left behind
// (F-10, F-11). It fails if Podman cannot be reached, and never tries to start the
// Podman machine (section 7). onEvent, if set, receives the engine's events and
// must not block.
func Connect(ctx context.Context, opts Options, log *slog.Logger, onEvent func(engine.Event)) (*Connection, error) {
	products := opts.Products
	if products == nil {
		var err error
		if products, err = DefaultProducts(); err != nil {
			return nil, err
		}
	}
	dataDir, err := DataDir()
	if err != nil {
		return nil, err
	}
	store := opts.Settings
	if store == nil {
		if store, err = OpenSettings(opts); err != nil {
			return nil, err
		}
	}
	rt, err := podman.New(ctx)
	if err != nil {
		return nil, err
	}
	report, err := Startup(ctx, rt, log)
	if err != nil {
		return nil, err
	}

	deployer := &deploy.Deployer{
		Products: products, Runtime: rt, Settings: store, Ports: runtime.NewPortAllocator(), Arch: goruntime.GOARCH,
		Cache: release.NewCache(filepath.Join(dataDir, "cache", "binaries"), release.NewClient()),
	}
	// The steps' primitives: the generic ones and each product's (R-SCOPE-3).
	prims := primitive.NewRegistry()
	if err := primitive.RegisterGeneric(prims); err != nil {
		return nil, fmt.Errorf("register primitives: %w", err)
	}
	if err := products.RegisterPrimitives(prims); err != nil {
		return nil, fmt.Errorf("register product primitives: %w", err)
	}
	eng := engine.New(engine.Deps{
		Registry: prims, Deployer: deployer, Runtime: rt, OnEvent: onEvent,
	})
	return &Connection{Engine: eng, Deployer: deployer, Settings: store, Products: products, Startup: report}, nil
}

// Notices describes what start-up cleanup did, as sentences for the instructor.
func (r StartupReport) Notices() []string {
	var out []string
	for _, id := range r.RemovedRuns {
		out = append(out, fmt.Sprintf("Removed what run %s left behind when the application last closed.", id))
	}
	for _, f := range r.Failures {
		out = append(out, fmt.Sprintf("Could not fully remove run %s: %v", f.RunID, f.Err))
	}
	return out
}

// LoadCatalogue loads the built-in and the user's scenarios. The error lists the
// scenarios that did not load, and the registry still holds the others.
func LoadCatalogue(opts Options) (*scenario.Registry, error) { return loadCatalogue(opts) }

// OpenSettings opens the saved settings with the registered products' fields. The
// window opens it first so the catalogue can show which scenarios need a license
// before Podman is reachable, and Connect then uses the same store.
func OpenSettings(opts Options) (*settings.Store, error) {
	products := opts.Products
	if products == nil {
		var err error
		if products, err = DefaultProducts(); err != nil {
			return nil, err
		}
	}
	dir, err := DataDir()
	if err != nil {
		return nil, err
	}
	return settings.Open(dir, products.SettingsFields()...)
}

// Licensed returns a check of whether a license is saved for a product. Products
// keep it in the setting "<product>.license" (R-SET-3).
func Licensed(store *settings.Store) func(product string) bool {
	return func(product string) bool {
		v, ok, err := store.Get(product + ".license")
		if errors.Is(err, settings.ErrUnknownField) {
			return true // the product has no license setting, so it needs none
		}
		return err == nil && ok && v != ""
	}
}

// Versions returns a lister of a product's versions for the Start form, newest
// first, as the core spells them (D1).
func Versions(products *product.Registry) func(ctx context.Context, name, edition string) ([]string, error) {
	return func(ctx context.Context, name, edition string) ([]string, error) {
		p, ok := products.Get(name)
		if !ok {
			return nil, fmt.Errorf("there is no product %q", name)
		}
		var ed release.Edition
		switch edition {
		case "community":
			ed = release.Community
		case "enterprise":
			ed = release.Enterprise
		default:
			return nil, fmt.Errorf("%q is not an edition", edition)
		}
		versions, err := p.Versions(ctx, ed)
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(versions))
		for _, v := range versions {
			out = append(out, v.Core)
		}
		return out, nil
	}
}
