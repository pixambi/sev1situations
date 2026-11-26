// Package vault is the Vault product: what the application knows about
// deploying, describing and acting on HashiCorp Vault and Vault Enterprise. It
// is the only place that does (R-SCOPE-2).
package vault

import (
	"context"
	"errors"
	"fmt"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/settings"
)

// Name is the product's name, as scenarios write it.
const Name = "vault"

// LicenseKey is the settings field that holds the Enterprise license (R-SET-3).
const LicenseKey = Name + ".license"

// Base images (spike S2). Alpine runs every build except the HSM build, which
// is a cgo binary linked to glibc and so needs a glibc image. The exception is
// recorded here, in one place (R-RUN-10).
const (
	// AlpineImage runs the community and Enterprise builds.
	AlpineImage = "docker.io/library/alpine:3"
	// GlibcImage runs the Enterprise HSM build.
	GlibcImage = "docker.io/library/debian:stable-slim"
)

// Options configures the product.
type Options struct {
	// Releases lists and fetches versions. It defaults to the public release site.
	Releases *release.Client
	// Timing sets how long the deployment waits for Vault. Zero values take the
	// defaults, which suit a real machine.
	Timing Timing
}

// Product is the Vault product.
type Product struct {
	releases *release.Client
	timing   Timing
}

var _ product.Product = (*Product)(nil)

// New returns the Vault product.
func New(o Options) *Product {
	if o.Releases == nil {
		o.Releases = release.NewClient()
	}
	return &Product{releases: o.Releases, timing: o.Timing.withDefaults()}
}

// Name implements product.Product.
func (*Product) Name() string { return Name }

// Editions implements product.Product. Both community and Enterprise are
// supported (R-RUN-7), and Enterprise has an HSM build for the auto-unseal
// scenario.
func (*Product) Editions() []release.Edition {
	return []release.Edition{release.Community, release.Enterprise, release.EnterpriseHSM}
}

// BaseImage implements product.Product.
func (*Product) BaseImage(e release.Edition) string {
	if e == release.EnterpriseHSM {
		return GlibcImage
	}
	return AlpineImage
}

// Versions implements product.Product. Versions are listed per edition, because
// the same number does not exist in every edition (spike S7).
func (p *Product) Versions(ctx context.Context, e release.Edition) ([]release.Version, error) {
	if !supported(e) {
		return nil, fmt.Errorf("vault has no %q edition", e)
	}
	return p.releases.Versions(ctx, release.Vault, e)
}

func supported(e release.Edition) bool {
	switch e {
	case release.Community, release.Enterprise, release.EnterpriseHSM:
		return true
	}
	return false
}

// IsEnterprise reports whether an edition needs a license.
func IsEnterprise(e release.Edition) bool { return e != release.Community }

// SettingsFields implements product.Product: the Enterprise license (R-SET-1).
func (*Product) SettingsFields() []settings.Field {
	return []settings.Field{{
		Key:    LicenseKey,
		Label:  "Vault Enterprise license",
		Help:   "Needed to run a scenario with an Enterprise edition of Vault. It is kept in the settings file, shown masked, and only put into a deployment that needs it.",
		Secret: true,
	}}
}

// ErrNoLicense means an Enterprise edition was asked for and no license is saved.
var ErrNoLicense = errors.New("no Vault Enterprise license is saved")

// RequireLicense returns the saved license for an Enterprise edition, and
// "" for the community edition, which needs none. Enterprise refuses to start
// without one (spike S2), so a deployment fails early with a message the
// instructor can act on, and not with a log line from a container that exited.
func RequireLicense(s product.Settings, e release.Edition) (string, error) {
	if !IsEnterprise(e) {
		return "", nil
	}
	license, ok, err := s.Get(LicenseKey)
	if err != nil {
		return "", fmt.Errorf("read the Vault license: %w", err)
	}
	if !ok || license == "" {
		return "", fmt.Errorf("%w. The %s edition needs one: add it in Settings", ErrNoLicense, e)
	}
	return license, nil
}

// Describe implements product.Product. It is built in T-053.
func (*Product) Describe(_ context.Context, _ runtime.Runtime, d product.Deployment) product.ClusterView {
	return product.ClusterView{Sections: []product.Section{{Title: d.Cluster}}}
}

// LogStreams implements product.Product. It is built in T-053.
func (*Product) LogStreams(primitive.Node) []product.LogStream {
	return []product.LogStream{{ID: "operational", Label: "Operational log"}}
}

// Primitives implements product.Product. They are built in T-054.
func (*Product) Primitives() []primitive.Primitive { return nil }
