// Package product defines what a product must provide and registers products
// (R-SCOPE-2, R-SCOPE-3). Everything that is specific to Vault or Boundary
// lives in a package under product/, behind the Product interface, so the
// engine, the runtime layer and the UI shell contain no product-specific code.
//
// Adding a product means adding a package and registering it. There is no plugin
// system and none is planned until a second real product needs one (R-SCOPE-4).
//
// The interface follows the sketch in section 3.5 with these changes: Describe
// takes a context and the runtime, because a node's state (sealed, leader) is
// read live; Editions and BaseImage let a product say what it supports and what
// it runs on (R-RUN-10); and Deploy takes everything it needs in a DeploySpec.
package product

import (
	"context"
	"fmt"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
	"github.com/pixambi/sev1situations/internal/settings"
)

// Product is what a product provides.
type Product interface {
	// Name is the product's lower-case name, for example vault. Scenarios use it
	// in their product fields, and the primitives and settings it adds are
	// prefixed with it.
	Name() string
	// Editions lists the editions the product supports (R-RUN-7).
	Editions() []release.Edition
	// BaseImage is the container image the product runs on for an edition. It is
	// the smallest that works (R-RUN-10), and the exceptions are recorded in the
	// product's own documentation.
	BaseImage(edition release.Edition) string
	// Versions lists the available versions of an edition, newest first.
	Versions(ctx context.Context, edition release.Edition) ([]release.Version, error)
	// Deploy builds one cluster of the product and returns it, ready to use
	// (section 7): the binary is installed, the configuration is rendered, the
	// nodes are started and initialised and the product reports ready.
	Deploy(ctx context.Context, rt runtime.Runtime, spec DeploySpec) (Deployment, error)
	// Describe returns what the Cluster tab shows for a deployment, including
	// live state it reads from the nodes. It never fails: what cannot be read is
	// shown as unknown.
	Describe(ctx context.Context, rt runtime.Runtime, d Deployment) ClusterView
	// LogStreams lists the logs available for a node (R-VIEW-3, R-VIEW-8).
	LogStreams(n primitive.Node) []LogStream
	// NodeActions declares which actions the instructor may take on a node
	// (R-VIEW-6). Which of them apply at a given moment depends on the node's
	// state, which the engine checks.
	NodeActions(d Deployment, node string) []NodeAction
	// Revive recreates a node that was removed, with its original configuration,
	// and joins it back to its cluster. The deployment is the one Deploy returned
	// and spec is the one it was given, so the node comes back as it was.
	Revive(ctx context.Context, rt runtime.Runtime, spec DeploySpec, d Deployment, node string) (primitive.Node, error)
	// Primitives are the verbs the product adds for scenarios, such as
	// vault.status. Their names start with the product's name.
	Primitives() []primitive.Primitive
	// SettingsFields are the settings the product registers, such as its
	// Enterprise license (R-SET-4). Their keys start with the product's name.
	SettingsFields() []settings.Field
}

// NodeAction is something the instructor can do to a node (R-VIEW-6).
type NodeAction string

// Node actions.
const (
	// ActionStop stops a running node.
	ActionStop NodeAction = "stop"
	// ActionStart starts a stopped node.
	ActionStart NodeAction = "start"
	// ActionRemove removes a node. It stays visible as a placeholder.
	ActionRemove NodeAction = "remove"
	// ActionRevive recreates a removed node with its original configuration.
	ActionRevive NodeAction = "revive"
)

// Settings gives a product read access to the saved settings, such as a license.
// The settings store satisfies it.
type Settings interface {
	// Get returns the saved value of a registered field, and whether one is saved.
	Get(key string) (value string, ok bool, err error)
}

// DeploySpec is everything a product needs to build one cluster.
type DeploySpec struct {
	// Run owns every object the deployment creates.
	Run runtime.Run
	// Network is the ID of the run's network.
	Network string
	// Cluster is the name of the cluster in the scenario's topology. It is the
	// key under which the cluster's credentials are kept.
	Cluster string
	// NamePrefix is empty for the first cluster of a scenario, whose nodes are
	// named node-1, node-2 and so on, and is the cluster's name for later ones,
	// whose nodes are <cluster>-node-1 and so on (section 3.6.1). Use NodeName.
	NamePrefix string
	// Nodes is the number of nodes to build.
	Nodes int
	// Storage is the storage backend, for example raft.
	Storage string
	// Edition and Version say which build to run.
	Edition release.Edition
	Version release.Version
	// Binary is the path of the verified, cached binary for the machine's
	// architecture. The deploy step has already fetched and checked it.
	Binary string
	// Image is the base image, which the deploy step has already made local.
	Image string
	// Seal, if set, says the cluster is unsealed automatically by another cluster's
	// product, instead of by key shares. The deploy step fills it in from the cluster's
	// provider (see SealProvider) before this cluster is deployed.
	Seal *SealSpec
	// Settings gives access to the saved settings, such as a license.
	Settings Settings
	// Ports allocates host ports for services that are published to the Mac.
	Ports *runtime.PortAllocator
	// Progress receives status lines shown to the instructor.
	Progress func(string)
}

// NodeName returns the name of node i of a cluster, counting from 1, following
// section 3.6.1.
func NodeName(spec DeploySpec, i int) string {
	if spec.NamePrefix == "" {
		return fmt.Sprintf("node-%d", i)
	}
	return fmt.Sprintf("%s-node-%d", spec.NamePrefix, i)
}

// Deployment is one cluster the product built.
type Deployment struct {
	// Cluster is the name from the topology.
	Cluster string
	// Product is the product's name.
	Product string
	// Version is the version that was deployed.
	Version release.Version
	// Nodes are the cluster's nodes in order.
	Nodes []primitive.Node
	// Creds are the cluster's credentials, for example rootToken. They are
	// shown on the Cluster tab only and never in reports (R-VIEW-5).
	Creds map[string]string
	// Published are the host ports the cluster's services are published on, on
	// 127.0.0.1.
	Published []runtime.PortMapping
	// State holds what the product itself needs to remember to act on the
	// cluster later, for example to revive a node. Nothing else reads it.
	State map[string]string
}

// ClusterView is what a product shows on the Cluster tab for one deployment.
type ClusterView struct {
	// Sections are the groups on the tab, for example "Primary cluster".
	Sections []Section
}

// Section is one group on the Cluster tab.
type Section struct {
	// Title is the section's heading.
	Title string
	// Facts are the section's details, such as the API address and root token.
	Facts []Fact
	// Nodes describe each node of the section.
	Nodes []NodeInfo
}

// Fact is one labelled value.
type Fact struct {
	// Label says what the value is.
	Label string
	// Value is the value.
	Value string
	// Secret marks a credential. The tab shows it to the instructor, and it is
	// never written to a report (R-VIEW-5, R-QUAL-3).
	Secret bool
}

// NodeInfo is the product's own details for one node. The node's name, address
// and container status come from the engine.
type NodeInfo struct {
	// Name is the node's name.
	Name string
	// Details are what the product knows, for example sealed or unsealed and the
	// HA role.
	Details []Fact
}

// LogStream is one log the instructor can follow for a node (R-VIEW-3).
type LogStream struct {
	// ID identifies the stream within the node, for example operational.
	ID string
	// Label is the name shown to the instructor.
	Label string
	// File is a log file inside the node's container. Empty means the container's
	// own output.
	File string
}

// Registry holds the products the application knows.
type Registry struct {
	products map[string]Product
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{products: map[string]Product{}}
}

// SealSpec says how a cluster is unsealed automatically.
type SealSpec struct {
	// Type is the kind of seal. Today that is "transit".
	Type string
	// Transit holds what a transit seal needs, when Type is "transit".
	Transit TransitSeal
}

// TransitSeal is how a cluster reaches the transit secrets engine of another Vault, which
// holds the key that unseals it.
type TransitSeal struct {
	// Address is the API address of the Vault that provides the seal, reachable from the
	// cluster's nodes.
	Address string
	// Token is the credential the cluster uses there. It is a secret.
	Token string
	// MountPath is where the transit engine is mounted, for example "transit/".
	MountPath string
	// KeyName is the transit key that wraps the cluster's keys.
	KeyName string
}

// SealProvider is implemented by a product that can unseal another cluster, as Vault can
// through its transit engine. The deploy step calls it on a deployed cluster to get what the
// clusters it unseals need.
type SealProvider interface {
	// ProvideSeal prepares a deployed cluster to act as the seal's provider, and returns the
	// seal that clusters of this product use to reach it.
	ProvideSeal(ctx context.Context, rt runtime.Runtime, d Deployment) (SealSpec, error)
}
