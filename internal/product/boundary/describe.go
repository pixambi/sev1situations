package boundary

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
)

// unknown is shown for anything that could not be read.
const unknown = "unknown"

// roleOf says what a node is. The deployment records it, and where only a name is at hand, as in LogStreams, it
// comes from the name, which each part of the deployment makes by a fixed rule.
func roleOf(d product.Deployment, name string) string {
	if r := d.State["role:"+name]; r != "" {
		return r
	}
	return roleFromName(name)
}

func roleFromName(name string) string {
	has := func(short string) bool {
		return strings.HasPrefix(name, short+"-node-") || strings.Contains(name, "-"+short+"-node-")
	}
	switch {
	case has("worker"):
		return RoleWorker
	case has("lb"):
		return RoleLB
	case has("db"):
		return RoleDatabase
	case has("target"):
		return RoleTarget
	}
	return RoleController
}

// Describe implements product.Product. It shows the minimum the Cluster tab always has for Boundary (section 5.5):
// where to sign in, the IDs of what was created, and each part's live state. What cannot be read is shown as unknown,
// so a stopped or removed node never makes the tab fail.
func (p *Product) Describe(ctx context.Context, rt runtime.Runtime, d product.Deployment) product.ClusterView {
	sec := product.Section{Title: clusterTitle(d.Cluster), Facts: p.facts(d)}

	infos := make([]product.NodeInfo, len(d.Nodes))
	workers := p.registeredWorkers(ctx, rt, d)
	var wg sync.WaitGroup
	for i, n := range d.Nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			infos[i] = p.nodeInfo(ctx, rt, d, n, workers)
		}()
	}
	wg.Wait()
	sec.Nodes = infos
	return product.ClusterView{Sections: []product.Section{sec}}
}

// clusterTitle turns edge into "Edge cluster".
func clusterTitle(name string) string {
	words := strings.ReplaceAll(name, "-", " ")
	if words == "" {
		return "Boundary"
	}
	return strings.ToUpper(words[:1]) + words[1:] + " cluster"
}

func (p *Product) facts(d product.Deployment) []product.Fact {
	st := d.State
	hostURL := func(node string) string {
		if port := st["port:"+node]; port != "" {
			return "http://127.0.0.1:" + port
		}
		return "not published to this computer"
	}
	var lb string
	for _, n := range d.Nodes {
		if roleOf(d, n.Name) == RoleLB {
			lb = n.Name
		}
	}
	facts := []product.Fact{
		{Label: "Cluster URL", Value: hostURL(lb)},
		{Label: "Version", Value: d.Version.Core + " " + editionName(d.Version.Edition)},
	}
	for _, n := range d.Nodes {
		if roleOf(d, n.Name) == RoleController {
			facts = append(facts, product.Fact{Label: "Controller " + n.Name, Value: hostURL(n.Name)})
		}
	}
	add := func(label, value string, secret bool) {
		if value != "" {
			facts = append(facts, product.Fact{Label: label, Value: value, Secret: secret})
		}
	}
	add("Admin login name", st["adminLogin"], false)
	add("Admin password", d.Creds["adminPassword"], true)
	add("Auth method ID", st["authMethodId"], false)
	add("Org scope ID", st["orgScopeId"], false)
	add("Project scope ID", st["projectScopeId"], false)
	add("Host catalog ID", st["hostCatalogId"], false)
	add("Host set ID", st["hostSetId"], false)
	add("Host ID", st["hostId"], false)
	add("Target ID", st["targetId"], false)
	return facts
}

func editionName(e release.Edition) string {
	if e == release.Enterprise {
		return "Enterprise"
	}
	return "community"
}

// registeredWorkers asks the controllers which workers are registered, by name. It returns nil if that could not be
// read, in which case a worker's state is shown as unknown.
func (p *Product) registeredWorkers(ctx context.Context, rt runtime.Runtime, d product.Deployment) map[string]bool {
	ctx, cancel := context.WithTimeout(ctx, p.timing.Describe)
	defer cancel()
	c := client{rt}
	for _, n := range d.Nodes {
		if roleOf(d, n.Name) != RoleController {
			continue
		}
		token, _, err := c.signIn(ctx, n, localAddr(), adminOf(d))
		if err != nil {
			continue // this controller is down, so try the next
		}
		res, err := c.run(ctx, n, localAddr(), token, "workers", "list", "-format=json")
		if err != nil || res.ExitCode != 0 {
			continue
		}
		got, err := workers(res.Stdout)
		if err != nil {
			continue
		}
		names := map[string]bool{}
		for _, w := range d.Nodes {
			if roleOf(d, w.Name) == RoleWorker && registered(got, w, "") {
				names[w.Name] = true
			}
		}
		return names
	}
	return nil
}

// nodeInfo reads one node's live state.
func (p *Product) nodeInfo(ctx context.Context, rt runtime.Runtime, d product.Deployment, n primitive.Node, workers map[string]bool) product.NodeInfo {
	role := roleOf(d, n.Name)
	state := p.liveState(ctx, rt, n, role, workers)

	details := []product.Fact{{Label: "Role", Value: role}, {Label: "State", Value: state}}
	switch role {
	case RoleController, RoleWorker:
		details = append(details, product.Fact{Label: "Version", Value: d.Version.Core + " " + editionName(d.Version.Edition)})
	case RoleDatabase:
		details = append(details,
			product.Fact{Label: "Database", Value: dbName}, product.Fact{Label: "User", Value: dbUser},
			product.Fact{Label: "Password", Value: d.Creds["databasePassword"], Secret: true})
	case RoleTarget:
		details = append(details, product.Fact{Label: "SSH user", Value: d.State["targetUser"]},
			product.Fact{Label: "SSH password", Value: d.Creds["targetPassword"], Secret: true})
	}
	return product.NodeInfo{Name: n.Name, Details: details}
}

// liveState asks a node how it is doing under a short limit, in a word.
func (p *Product) liveState(ctx context.Context, rt runtime.Runtime, n primitive.Node, role string, workers map[string]bool) string {
	ctx, cancel := context.WithTimeout(ctx, p.timing.Describe)
	defer cancel()
	exec := func(cmd ...string) (runtime.ExecResult, error) {
		return rt.Exec(ctx, n.ContainerID, cmd, runtime.ExecOptions{})
	}
	switch role {
	case RoleController:
		res, err := exec("wget", "-q", "-T", "2", "-O", "/dev/null", fmt.Sprintf("http://127.0.0.1:%d/health", opsPort))
		return word(res, err, "healthy", "not answering")
	case RoleWorker:
		res, err := exec("pidof", "boundary")
		switch {
		case err != nil:
			return unknown
		case res.ExitCode != 0:
			return "not running"
		case workers == nil:
			return unknown
		case workers[n.Name]:
			return "registered"
		}
		return "running, not registered"
	case RoleLB:
		res, err := exec("pidof", "haproxy")
		return word(res, err, "running", "not running")
	case RoleDatabase:
		res, err := exec("pg_isready", "-h", "127.0.0.1", "-U", dbUser, "-d", dbName)
		return word(res, err, "accepting connections", "not answering")
	case RoleTarget:
		res, err := exec("sh", "-c", fmt.Sprintf("nc -w 2 127.0.0.1 %d </dev/null", sshPort))
		if err == nil && strings.Contains(res.Stdout, "SSH-") {
			return "SSH answering"
		}
		return word(res, err, "SSH answering", "SSH not answering")
	}
	return unknown
}

// word turns a command's outcome into one of two words, or unknown if it could not run.
func word(res runtime.ExecResult, err error, ok, notOK string) string {
	switch {
	case err != nil:
		return unknown
	case res.ExitCode == 0:
		return ok
	}
	return notOK
}

// LogStreams implements product.Product: the operational log of a controller or a worker, the balancer's log, the
// database's own output and the SSH server's log, the minimum for every node (R-VIEW-8).
func (*Product) LogStreams(n primitive.Node) []product.LogStream {
	switch roleFromName(n.Name) {
	case RoleLB:
		return []product.LogStream{{ID: "operational", Label: "Load balancer log", File: HAProxyLog}}
	case RoleDatabase:
		// The database logs to its container's own output, which the runtime reads.
		return []product.LogStream{{ID: "database", Label: "Database log"}}
	case RoleTarget:
		return []product.LogStream{{ID: "ssh", Label: "SSH server log", File: SSHLog}}
	}
	return []product.LogStream{{ID: "operational", Label: "Operational log", File: OperationalLog}}
}

// NodeActions implements product.Product. Every part can be stopped, started and removed, and all but the database
// can be revived. A removed database has lost its data and could not be brought back as it was, so it is not offered.
func (*Product) NodeActions(d product.Deployment, node string) []product.NodeAction {
	if roleOf(d, node) == RoleDatabase {
		return []product.NodeAction{product.ActionStop, product.ActionStart}
	}
	return []product.NodeAction{product.ActionStop, product.ActionStart, product.ActionRemove, product.ActionRevive}
}
