package vault

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
)

// unknown is shown for anything that could not be read.
const unknown = "unknown"

// Describe implements product.Product. It shows the minimum the Cluster tab
// always has for Vault (section 5.5). The node's state, role and version are read
// live, each under its own short limit, and what cannot be read is shown as
// unknown, so a stopped or removed node never makes the tab fail.
func (p *Product) Describe(ctx context.Context, rt runtime.Runtime, d product.Deployment) product.ClusterView {
	sec := product.Section{Title: clusterTitle(d.Cluster), Facts: clusterFacts(d)}

	infos := make([]product.NodeInfo, len(d.Nodes))
	var wg sync.WaitGroup
	for i, n := range d.Nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			infos[i] = p.nodeInfo(ctx, rt, d, i, n)
		}()
	}
	wg.Wait()
	sec.Nodes = infos

	return product.ClusterView{Sections: []product.Section{sec}}
}

// clusterTitle turns dr-secondary into "Dr secondary cluster".
func clusterTitle(name string) string {
	words := strings.ReplaceAll(name, "-", " ")
	if words == "" {
		return "Cluster"
	}
	return strings.ToUpper(words[:1]) + words[1:] + " cluster"
}

// clusterFacts are the cluster-level minimum: role, API address, root token, the
// unseal keys and their threshold, edition and license (R-VIEW-1, R-VIEW-5).
func clusterFacts(d product.Deployment) []product.Fact {
	role := d.State["role"]
	if role == "" {
		role = "primary"
	}
	facts := []product.Fact{
		{Label: "Role", Value: role},
		{Label: "API address", Value: apiAddress(d, 0)},
		{Label: "Version", Value: d.Version.Core + " " + editionName(d.Version.Edition)},
	}
	if IsEnterprise(d.Version.Edition) {
		// The license comes from Settings and is never shown here.
		facts = append(facts, product.Fact{Label: "License", Value: "supplied from Settings"})
	}

	if t, ok := d.Creds["rootToken"]; ok {
		facts = append(facts, product.Fact{Label: "Root token", Value: t, Secret: true})
	}
	keys := unsealKeys(&d)
	for i, k := range keys {
		facts = append(facts, product.Fact{Label: "Unseal key " + strconv.Itoa(i+1), Value: k, Secret: true})
	}
	if len(keys) > 0 {
		facts = append(facts, product.Fact{Label: "Unseal threshold", Value: fmt.Sprintf("%s of %d", d.State["unsealThreshold"], len(keys))})
	}
	return facts
}

// apiAddress is where a node's API can be reached from this computer, or says it
// is not published.
func apiAddress(d product.Deployment, node int) string {
	if node < len(d.Published) {
		return fmt.Sprintf("http://127.0.0.1:%d", d.Published[node].Host)
	}
	return "not published to this computer"
}

func editionName(e release.Edition) string {
	switch e {
	case release.Community:
		return "community"
	case release.Enterprise:
		return "Enterprise"
	case release.EnterpriseHSM:
		return "Enterprise HSM"
	}
	return string(e)
}

// nodeInfo reads one node's live state.
func (p *Product) nodeInfo(ctx context.Context, rt runtime.Runtime, d product.Deployment, i int, n primitive.Node) product.NodeInfo {
	st, state := p.readStatus(ctx, rt, n)

	role, version := unknown, unknown
	if st != nil {
		version = st.Version + " " + editionName(d.Version.Edition)
		role = "-"
		if r := st.role(); r != "" {
			role = r
		}
	}
	return product.NodeInfo{Name: n.Name, Details: []product.Fact{
		{Label: "Node ID", Value: n.Name},
		{Label: "Vault", Value: state},
		{Label: "HA role", Value: role},
		{Label: "Version", Value: version},
		{Label: "API address", Value: apiAddress(d, i)},
	}}
}

// readStatus asks a node for its status under a short limit. It returns the
// status, or nil, and a word for how Vault is doing.
func (p *Product) readStatus(ctx context.Context, rt runtime.Runtime, n primitive.Node) (*status, string) {
	ctx, cancel := context.WithTimeout(ctx, p.timing.Describe)
	defer cancel()

	res, err := rt.Exec(ctx, n.ContainerID, []string{binaryPath, "status", "-format=json"},
		runtime.ExecOptions{Env: map[string]string{"VAULT_ADDR": fmt.Sprintf("http://127.0.0.1:%d", apiPort)}})
	if err != nil || res.ExitCode == 1 { // exit 1 is "cannot reach Vault"
		return nil, unknown
	}
	st, err := parseStatus(res.Stdout)
	if err != nil {
		return nil, unknown
	}
	switch {
	case !st.Initialized:
		return &st, "not initialised"
	case st.Sealed:
		return &st, "sealed"
	}
	return &st, "unsealed"
}

// LogStreams implements product.Product: the operational log and the audit log,
// the minimum for every node (R-VIEW-8). Both are files Vault writes inside the
// container, because the container's own output is only the keep-alive.
func (*Product) LogStreams(primitive.Node) []product.LogStream {
	return []product.LogStream{
		{ID: "operational", Label: "Operational log", File: OperationalLog},
		{ID: "audit", Label: "Audit log", File: AuditLog},
	}
}
