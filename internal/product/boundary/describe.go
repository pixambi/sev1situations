package boundary

import (
	"context"
	"errors"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/runtime"
)

// Describe implements product.Product.
func (p *Product) Describe(context.Context, runtime.Runtime, product.Deployment) product.ClusterView {
	return product.ClusterView{}
}

// LogStreams implements product.Product.
func (*Product) LogStreams(primitive.Node) []product.LogStream { return nil }

// NodeActions implements product.Product.
func (*Product) NodeActions(product.Deployment, string) []product.NodeAction { return nil }

// Revive implements product.Product.
func (*Product) Revive(context.Context, runtime.Runtime, product.DeploySpec, product.Deployment, string) (primitive.Node, error) {
	return primitive.Node{}, errors.New("reviving a Boundary node is not supported yet")
}

// Primitives implements product.Product.
func (*Product) Primitives() []primitive.Primitive { return nil }
