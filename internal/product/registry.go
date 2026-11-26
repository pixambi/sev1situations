package product

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/settings"
)

var nameRule = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// Register adds a product. Its name must be lower-case letters and digits, and
// the primitives and settings it brings must be named after it, so two products
// can never collide (R-SCOPE-3, R-SET-4).
func (r *Registry) Register(p Product) error {
	name := p.Name()
	if !nameRule.MatchString(name) {
		return fmt.Errorf("register product %q: the name must be lower-case letters and digits", name)
	}
	if _, ok := r.products[name]; ok {
		return fmt.Errorf("register product %q: already registered", name)
	}
	if len(p.Editions()) == 0 {
		return fmt.Errorf("register product %q: it must support at least one edition", name)
	}
	for _, e := range p.Editions() {
		if p.BaseImage(e) == "" {
			return fmt.Errorf("register product %q: it names no base image for the %q edition", name, e)
		}
	}
	for _, prim := range p.Primitives() {
		if !strings.HasPrefix(prim.Spec().Name, name+".") {
			return fmt.Errorf("register product %q: the primitive %q must start with %q", name, prim.Spec().Name, name+".")
		}
	}
	for _, f := range p.SettingsFields() {
		if !strings.HasPrefix(f.Key, name+".") {
			return fmt.Errorf("register product %q: the setting %q must start with %q", name, f.Key, name+".")
		}
	}
	r.products[name] = p
	return nil
}

// Get returns the product with the given name.
func (r *Registry) Get(name string) (Product, bool) {
	p, ok := r.products[name]
	return p, ok
}

// Names returns the registered product names in order. A scenario may only name
// these.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.products))
	for n := range r.products {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// SettingsFields returns every product's settings fields, so the settings page
// and store are built from the products and not edited per product (R-SET-4).
func (r *Registry) SettingsFields() []settings.Field {
	var out []settings.Field
	for _, n := range r.Names() {
		out = append(out, r.products[n].SettingsFields()...)
	}
	return out
}

// RegisterPrimitives adds every product's primitives to a primitive registry.
func (r *Registry) RegisterPrimitives(into *primitive.Registry) error {
	for _, n := range r.Names() {
		for _, prim := range r.products[n].Primitives() {
			if err := into.Register(prim); err != nil {
				return fmt.Errorf("product %s: %w", n, err)
			}
		}
	}
	return nil
}
