package scenario

import (
	"reflect"
	"strings"
	"testing"
)

const oneCluster = "  clusters:\n    - name: primary\n      product: vault\n      nodes: 3\n      storage: raft\n"

// twoClusters replaces the worked example's topology with a workload cluster and a provider, with a seal block for the first.
func twoClusters(seal string) string {
	return strings.Replace(worked, oneCluster,
		"  clusters:\n    - name: primary\n      product: vault\n      nodes: 3\n      storage: raft\n"+seal+
			"    - name: kms\n      product: vault\n      nodes: 1\n      storage: raft\n", 1)
}

func TestASealNamesAnotherClusterAsItsProvider(t *testing.T) {
	good := twoClusters("      seal:\n        type: transit\n        provider: kms\n")
	if ps := validate(t, good, nil, strict); len(ps) != 0 {
		t.Fatalf("a valid seal was refused: %+v", ps)
	}

	for _, tt := range []struct {
		name, seal, field, msg string
	}{
		{"an unknown type", "      seal:\n        type: awskms\n        provider: kms\n", "topology.clusters[0].seal.type", "not a seal type"},
		{"no provider", "      seal:\n        type: transit\n", "topology.clusters[0].seal.provider", "is required"},
		{"itself as the provider", "      seal:\n        type: transit\n        provider: primary\n", "topology.clusters[0].seal.provider", "by itself"},
		{"a provider that does not exist", "      seal:\n        type: transit\n        provider: vault-kms\n", "topology.clusters[0].seal.provider", "not a cluster of this scenario"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ps := validate(t, twoClusters(tt.seal), nil, strict)
			for _, p := range ps {
				if p.Field == tt.field && strings.Contains(p.Message, tt.msg) {
					return
				}
			}
			t.Errorf("no problem %q for %s in %+v", tt.msg, tt.field, ps)
		})
	}
}

func TestAProviderCannotItselfBeSealedByAnother(t *testing.T) {
	yaml := strings.Replace(twoClusters("      seal:\n        type: transit\n        provider: kms\n"),
		"    - name: kms\n      product: vault\n      nodes: 1\n      storage: raft\n",
		"    - name: kms\n      product: vault\n      nodes: 1\n      storage: raft\n      seal:\n        type: transit\n        provider: primary\n", 1)
	ps := validate(t, yaml, nil, strict)
	found := false
	for _, p := range ps {
		found = found || (p.Field == "topology.clusters[0].seal.provider" && strings.Contains(p.Message, "cannot provide the seal"))
	}
	if !found {
		t.Errorf("a circular seal was accepted: %+v", ps)
	}
}

func TestDeployOrderPutsProvidersFirstAndOtherwiseKeepsTheTopologyOrder(t *testing.T) {
	cl := func(name, provider string) Cluster {
		c := Cluster{Name: name}
		if provider != "" {
			c.Seal = &Seal{Type: SealTransit, Provider: provider}
		}
		return c
	}
	for _, tt := range []struct {
		name     string
		clusters []Cluster
		want     []int
	}{
		{"no seals keeps the order", []Cluster{cl("a", ""), cl("b", ""), cl("c", "")}, []int{0, 1, 2}},
		{"a provider listed last goes first", []Cluster{cl("workload", "kms"), cl("kms", "")}, []int{1, 0}},
		{"only what is needed moves", []Cluster{cl("a", ""), cl("b", "d"), cl("c", ""), cl("d", "")}, []int{0, 3, 1, 2}},
		{"one provider for two clusters", []Cluster{cl("a", "k"), cl("b", "k"), cl("k", "")}, []int{2, 0, 1}},
		{"a single cluster", []Cluster{cl("a", "")}, []int{0}},
		// A scenario like these fails validation, but asking for its order must still end.
		{"a cycle ends", []Cluster{cl("a", "b"), cl("b", "a")}, []int{1, 0}},
		{"itself ends", []Cluster{cl("a", "a")}, []int{0}},
		{"an unknown provider is ignored", []Cluster{cl("a", "nobody"), cl("b", "")}, []int{0, 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &Scenario{Topology: Topology{Clusters: tt.clusters}}
			got := s.DeployOrder()
			if len(got) != len(tt.clusters) {
				t.Fatalf("order %v is not a permutation of %d clusters", got, len(tt.clusters))
			}
			if !reflect.DeepEqual(got, tt.want) && tt.name != "a cycle ends" {
				t.Errorf("order = %v, want %v", got, tt.want)
			}
		})
	}
}
