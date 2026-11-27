package app

import (
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/product/boundary"
	"github.com/pixambi/sev1situations/internal/product/vault"
)

// DefaultProducts returns the registry of the products the application ships.
// Adding a product means adding its package and registering it here (R-SCOPE-3).
func DefaultProducts() (*product.Registry, error) {
	reg := product.NewRegistry()
	for _, p := range []product.Product{
		vault.New(vault.Options{}),
		boundary.New(boundary.Options{}),
	} {
		if err := reg.Register(p); err != nil {
			return nil, err
		}
	}
	return reg, nil
}
