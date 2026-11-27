// Package boundary is the Boundary product: what the application knows about deploying, describing and
// acting on HashiCorp Boundary. It is the only place that does (R-SCOPE-2).
//
// A Boundary deployment is several containers: a PostgreSQL database, one or more controllers, a worker,
// an HAProxy load balancer in front of the controllers, and a target host running OpenSSH for sessions to
// reach. The product builds all of them, so a scenario names one cluster and gets the whole thing.
package boundary

import (
	"context"
	"fmt"

	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/settings"
)

// Name is the product's name, as scenarios write it.
const Name = "boundary"

// LicenseKey is the settings field that holds the Enterprise license (R-SET-3).
const LicenseKey = Name + ".license"

// Images. Boundary runs on Alpine (spike S2). The supporting services run on their own images (R-RUN-5), pinned
// to a major version so a deployment does not change under the instructor.
const (
	// AlpineImage runs Boundary and the target host.
	AlpineImage = "docker.io/library/alpine:3"
	// PostgresImage is the database.
	PostgresImage = "docker.io/library/postgres:17-alpine"
	// HAProxyImage is the load balancer.
	HAProxyImage = "docker.io/library/haproxy:3.0-alpine"
)

// Options configures the product.
type Options struct {
	// Releases lists and fetches versions. It defaults to the public release site.
	Releases *release.Client
	// Timing sets how long the deployment waits for each stage. Zero values take the defaults.
	Timing Timing
}

// Product is the Boundary product.
type Product struct {
	releases *release.Client
	timing   Timing
}

var _ product.Product = (*Product)(nil)

// New returns the Boundary product.
func New(o Options) *Product {
	if o.Releases == nil {
		o.Releases = release.NewClient()
	}
	return &Product{releases: o.Releases, timing: o.Timing.withDefaults()}
}

// Name implements product.Product.
func (*Product) Name() string { return Name }

// Editions implements product.Product.
func (*Product) Editions() []release.Edition {
	return []release.Edition{release.Community, release.Enterprise}
}

// BaseImage implements product.Product.
func (*Product) BaseImage(release.Edition) string { return AlpineImage }

// Versions implements product.Product. Versions are listed per edition: Boundary's community and Enterprise builds
// are numbered differently (spike S2).
func (p *Product) Versions(ctx context.Context, e release.Edition) ([]release.Version, error) {
	if e != release.Community && e != release.Enterprise {
		return nil, fmt.Errorf("boundary has no %q edition", e)
	}
	return p.releases.Versions(ctx, release.Boundary, e)
}

// IsEnterprise reports whether an edition needs a license.
func IsEnterprise(e release.Edition) bool { return e != release.Community }

// SettingsFields implements product.Product: the Enterprise license (R-SET-1).
func (*Product) SettingsFields() []settings.Field {
	return []settings.Field{{
		Key:    LicenseKey,
		Label:  "Boundary Enterprise license",
		Help:   "Needed to run a scenario with an Enterprise edition of Boundary. It is kept in the settings file, shown masked, and only put into a deployment that needs it.",
		Secret: true,
	}}
}
