package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
)

// Timing says how long the deployment waits for each stage. Waiting always has
// a limit, and a stage that runs out of time says which container and shows the
// end of its log (section 7).
type Timing struct {
	// Poll is how often a condition is checked.
	Poll time.Duration
	// NodeReady is how long a node has to start answering after Vault is started.
	NodeReady time.Duration
	// Leader is how long the first node has to become the active node after unsealing.
	Leader time.Duration
	// Join is how long the other nodes have to join and the cluster to show every voter.
	Join time.Duration
	// Describe is how long the Cluster tab waits for one node to report its state
	// before showing it as unknown.
	Describe time.Duration
}

func (t Timing) withDefaults() Timing {
	if t.Poll <= 0 {
		t.Poll = 500 * time.Millisecond
	}
	if t.NodeReady <= 0 {
		t.NodeReady = 90 * time.Second
	}
	if t.Leader <= 0 {
		t.Leader = 60 * time.Second
	}
	if t.Join <= 0 {
		t.Join = 90 * time.Second
	}
	if t.Describe <= 0 {
		t.Describe = 5 * time.Second
	}
	return t
}

// Limits on a cluster's size. The sandbox allows one to nine nodes (5.2).
const (
	minNodes = 1
	maxNodes = 9
)

// keepAlive is the container's main process. Vault itself is started with a
// separate command, so that a Vault that stops leaves its container in place for
// the instructor to inspect, as a real outage would.
var keepAlive = []string{"tail", "-f", "/dev/null"}

// Deploy implements product.Product. It builds one Raft cluster: it starts a
// container per node, installs the binary and the configuration, starts Vault,
// initialises the first node with five key shares and a threshold of three,
// unseals it, joins and unseals the other nodes, and turns audit on.
//
// If it fails part way, the containers it created are left in place and returned
// in the deployment, so their logs can be read. The engine removes them on Retry
// or End.
func (p *Product) Deploy(ctx context.Context, rt runtime.Runtime, spec product.DeploySpec) (product.Deployment, error) {
	d := product.Deployment{
		Cluster: spec.Cluster, Product: Name, Version: spec.Version,
		Creds: map[string]string{}, State: map[string]string{},
	}
	license, err := p.check(spec)
	if err != nil {
		return d, err
	}

	c := &cluster{p: p, rt: rt, spec: spec, license: license}
	if err := c.start(ctx, &d); err != nil {
		return d, err
	}
	if err := c.install(ctx, d.Nodes); err != nil {
		return d, err
	}
	if err := c.bootstrap(ctx, &d); err != nil {
		return d, err
	}
	if err := c.join(ctx, &d); err != nil {
		return d, err
	}
	c.say("Vault %s is up with %d node(s)", spec.Version.Full, spec.Nodes)
	return d, nil
}

// check validates the request before anything is created, so a mistake costs
// nothing. It returns the license the cluster needs, if any.
func (p *Product) check(spec product.DeploySpec) (string, error) {
	switch {
	case spec.Nodes < minNodes || spec.Nodes > maxNodes:
		return "", fmt.Errorf("a Vault cluster has %d to %d nodes, not %d", minNodes, maxNodes, spec.Nodes)
	case spec.Storage != "" && spec.Storage != "raft":
		return "", fmt.Errorf("the %q storage backend is not supported: Vault is deployed with Raft", spec.Storage)
	case !supported(spec.Edition):
		return "", fmt.Errorf("vault has no %q edition", spec.Edition)
	case spec.Edition == release.EnterpriseHSM:
		return "", errors.New("the Enterprise HSM edition is not supported yet: it arrives with the HSM scenario")
	case spec.Image == "" || spec.Binary == "" || spec.Cluster == "":
		return "", errors.New("the deploy spec needs an image, a binary and a cluster name")
	}
	if _, err := os.Stat(spec.Binary); err != nil {
		return "", fmt.Errorf("the Vault binary %s cannot be read: %w", spec.Binary, err)
	}
	if spec.Settings == nil && IsEnterprise(spec.Edition) {
		return "", fmt.Errorf("%w. The %s edition needs one: add it in Settings", ErrNoLicense, spec.Edition)
	}
	if spec.Settings == nil {
		return "", nil
	}
	return RequireLicense(spec.Settings, spec.Edition)
}

// cluster carries what the deployment steps share.
type cluster struct {
	p       *Product
	rt      runtime.Runtime
	spec    product.DeploySpec
	license string
}

func (c *cluster) say(format string, args ...any) {
	if c.spec.Progress != nil {
		c.spec.Progress(fmt.Sprintf(format, args...))
	}
}

// start creates one container per node, each publishing its API port on the
// loopback interface if a port allocator was given.
func (c *cluster) start(ctx context.Context, d *product.Deployment) error {
	c.say("starting %d container(s) from %s", c.spec.Nodes, c.spec.Image)
	for i := 1; i <= c.spec.Nodes; i++ {
		name := product.NodeName(c.spec, i)

		var ports []runtime.PortMapping
		if c.spec.Ports != nil {
			host, err := c.spec.Ports.Allocate()
			if err != nil {
				return fmt.Errorf("publish the API of %s: %w", name, err)
			}
			ports = []runtime.PortMapping{{Host: host, Container: apiPort}}
		}

		ct, err := c.rt.StartContainer(ctx, runtime.ContainerOptions{
			Run: c.spec.Run, Name: name, Image: c.spec.Image, Command: keepAlive,
			Network: c.spec.Network, Hostname: name, Ports: ports,
		})
		if err != nil {
			for _, pm := range ports {
				c.spec.Ports.Release(pm.Host)
			}
			return fmt.Errorf("start %s: %w", name, err)
		}
		d.Nodes = append(d.Nodes, primitive.Node{Name: name, Cluster: c.spec.Cluster, ContainerID: ct.ID, IP: ct.IP})
		d.Published = append(d.Published, ports...)
		if ct.IP == "" {
			return fmt.Errorf("%s has no address on the run's network", name)
		}
	}
	return nil
}

// install puts the binary, the configuration and, for Enterprise, the license
// into each node, and starts Vault there.
func (c *cluster) install(ctx context.Context, nodes []primitive.Node) error {
	c.say("installing Vault %s", c.spec.Version.Full)
	for _, n := range nodes {
		if err := c.prepare(ctx, n); err != nil {
			return err
		}
	}
	c.say("starting Vault on every node")
	for _, n := range nodes {
		// Detached, so Vault keeps running after the command returns. Its output goes to a file
		// the Logs tab can follow.
		cmd := fmt.Sprintf("exec %s server -config=%s >> %s 2>&1", binaryPath, configPath, OperationalLog)
		if _, err := c.rt.Exec(ctx, n.ContainerID, []string{"sh", "-c", cmd}, runtime.ExecOptions{Detach: true}); err != nil {
			return fmt.Errorf("start Vault on %s: %w", n.Name, err)
		}
	}
	return nil
}

func (c *cluster) prepare(ctx context.Context, n primitive.Node) error {
	res, err := c.rt.Exec(ctx, n.ContainerID, []string{"mkdir", "-p", dataDir, logDir, "/etc/vault.d"}, runtime.ExecOptions{})
	if err != nil || res.ExitCode != 0 {
		return fmt.Errorf("create the Vault folders on %s: %w", n.Name, execProblem(res, err))
	}

	bin, err := os.Open(c.spec.Binary)
	if err != nil {
		return fmt.Errorf("open the Vault binary: %w", err)
	}
	defer func() { _ = bin.Close() }()
	info, err := bin.Stat()
	if err != nil {
		return fmt.Errorf("read the Vault binary: %w", err)
	}
	files := []runtime.File{
		{Path: binaryPath, Mode: 0o755, Size: info.Size(), Content: bin},
		fileOf(configPath, 0o644, nodeConfig{NodeID: n.Name, IP: n.IP, Enterprise: IsEnterprise(c.spec.Edition)}.render()),
	}
	if c.license != "" {
		// Owner-only, and only in the containers that need it (R-SET-3).
		files = append(files, fileOf(licensePath, 0o600, c.license))
	}
	for _, f := range files {
		if err := c.rt.Copy(ctx, n.ContainerID, f); err != nil {
			return fmt.Errorf("copy %s to %s: %w", f.Path, n.Name, err)
		}
	}
	return nil
}

func fileOf(path string, mode os.FileMode, content string) runtime.File {
	return runtime.File{Path: path, Mode: mode, Size: int64(len(content)), Content: strings.NewReader(content)}
}

// bootstrap initialises the first node, unseals it, waits for it to be active and
// turns audit on.
func (c *cluster) bootstrap(ctx context.Context, d *product.Deployment) error {
	first := d.Nodes[0]
	c.say("waiting for %s to answer", first.Name)
	if err := c.waitStatus(ctx, first, c.p.timing.NodeReady, func(s status) bool { return true }, "answer"); err != nil {
		return err
	}

	c.say("initialising %s: %d key shares, threshold %d", first.Name, keyShares, keyThreshold)
	res, err := c.vault(ctx, first, "", "operator", "init",
		"-key-shares="+strconv.Itoa(keyShares), "-key-threshold="+strconv.Itoa(keyThreshold), "-format=json")
	if err != nil || res.ExitCode != 0 {
		// The output holds the keys, so only the exit code is reported.
		return fmt.Errorf("initialise Vault on %s: %w", first.Name, execProblem(runtime.ExecResult{ExitCode: res.ExitCode}, err))
	}
	keys, err := parseInit(res.Stdout)
	if err != nil {
		return fmt.Errorf("initialise Vault on %s: %w", first.Name, err)
	}
	for i, k := range keys.UnsealKeys {
		d.Creds["unsealKey"+strconv.Itoa(i+1)] = k
	}
	d.Creds["rootToken"] = keys.RootToken
	d.State["unsealThreshold"] = strconv.Itoa(keyThreshold)
	d.State["unsealKeys"] = strconv.Itoa(len(keys.UnsealKeys))

	c.say("unsealing %s", first.Name)
	if err := c.unseal(ctx, first, keys.UnsealKeys); err != nil {
		return err
	}
	c.say("waiting for %s to become the active node", first.Name)
	if err := c.waitStatus(ctx, first, c.p.timing.Leader, func(s status) bool { return s.role() == roleActive }, "become active"); err != nil {
		return err
	}

	c.say("enabling the audit log")
	res, err = c.vault(ctx, first, keys.RootToken, "audit", "enable", "file", "file_path="+AuditLog)
	if err != nil || res.ExitCode != 0 {
		return fmt.Errorf("enable audit on %s: %w", first.Name, execProblem(res, err))
	}
	return nil
}

// join brings the other nodes into the cluster and unseals them, then waits for
// the cluster to show every node as a voter.
func (c *cluster) join(ctx context.Context, d *product.Deployment) error {
	first, token := d.Nodes[0], d.Creds["rootToken"]
	keys := unsealKeys(d)

	for _, n := range d.Nodes[1:] {
		c.say("waiting for %s to answer", n.Name)
		if err := c.waitStatus(ctx, n, c.p.timing.NodeReady, func(s status) bool { return true }, "answer"); err != nil {
			return err
		}

		c.say("joining %s to the cluster", n.Name)
		leaderAddr := fmt.Sprintf("http://%s:%d", first.IP, apiPort)
		err := c.poll(ctx, n, c.p.timing.Join, "join the cluster", func() (bool, string, error) {
			res, err := c.vault(ctx, n, "", "operator", "raft", "join", leaderAddr)
			if err != nil {
				return false, "", err
			}
			return res.ExitCode == 0, firstLine(res.Stderr + res.Stdout), nil
		})
		if err != nil {
			return err
		}

		c.say("unsealing %s", n.Name)
		if err := c.unseal(ctx, n, keys); err != nil {
			return err
		}
	}

	c.say("waiting for %d voter(s)", len(d.Nodes))
	return c.poll(ctx, first, c.p.timing.Join, "show every node as a voter", func() (bool, string, error) {
		res, err := c.vault(ctx, first, token, "operator", "raft", "list-peers", "-format=json")
		if err != nil {
			return false, "", err
		}
		if res.ExitCode != 0 {
			return false, firstLine(res.Stderr), nil
		}
		peers, err := parsePeers(res.Stdout)
		if err != nil {
			return false, err.Error(), nil
		}
		n, leader := peers.voters()
		return n == len(d.Nodes) && leader != "", fmt.Sprintf("%d of %d voters, leader %q", n, len(d.Nodes), leader), nil
	})
}

// unsealKeys returns the keys held in the deployment, in order.
func unsealKeys(d *product.Deployment) []string {
	var keys []string
	for i := 1; ; i++ {
		k, ok := d.Creds["unsealKey"+strconv.Itoa(i)]
		if !ok {
			return keys
		}
		keys = append(keys, k)
	}
}

// unseal supplies the threshold number of keys to a node.
func (c *cluster) unseal(ctx context.Context, n primitive.Node, keys []string) error {
	for _, k := range keys[:keyThreshold] {
		res, err := c.vault(ctx, n, "", "operator", "unseal", k)
		if err != nil || res.ExitCode != 0 {
			// The command line holds a key, so it is not repeated in the error.
			return fmt.Errorf("unseal %s: %w", n.Name, execProblem(runtime.ExecResult{ExitCode: res.ExitCode, Stderr: scrub(res.Stderr, keys)}, err))
		}
	}
	return c.waitStatus(ctx, n, c.p.timing.NodeReady, func(s status) bool { return !s.Sealed }, "unseal")
}

func scrub(text string, secrets []string) string {
	for _, s := range secrets {
		if s != "" {
			text = strings.ReplaceAll(text, s, "[redacted]")
		}
	}
	return text
}

// vault runs the vault command in a node. The address is always local, and a
// token is passed in the environment, never on the command line.
func (c *cluster) vault(ctx context.Context, n primitive.Node, token string, args ...string) (runtime.ExecResult, error) {
	env := map[string]string{"VAULT_ADDR": fmt.Sprintf("http://127.0.0.1:%d", apiPort)}
	if token != "" {
		env["VAULT_TOKEN"] = token
	}
	return c.rt.Exec(ctx, n.ContainerID, append([]string{binaryPath}, args...), runtime.ExecOptions{Env: env})
}

// waitStatus waits until a node's `vault status` satisfies ok. A node whose API is
// not up yet makes the command fail, which is just "not yet".
func (c *cluster) waitStatus(ctx context.Context, n primitive.Node, limit time.Duration, ok func(status) bool, what string) error {
	return c.poll(ctx, n, limit, what, func() (bool, string, error) {
		res, err := c.vault(ctx, n, "", "status", "-format=json")
		if err != nil {
			return false, "", err
		}
		if res.ExitCode == 1 { // exit 1 is "cannot reach Vault", 0 and 2 both print the status
			return false, firstLine(res.Stderr), nil
		}
		s, err := parseStatus(res.Stdout)
		if err != nil {
			return false, err.Error(), nil
		}
		return ok(s), fmt.Sprintf("initialized=%v sealed=%v role=%q", s.Initialized, s.Sealed, s.role()), nil
	})
}

// poll runs check until it reports done, an error is returned, the time runs out
// or the context ends. On a timeout the error names the container and shows the end
// of its log, because "it did not come up" helps nobody (section 7).
func (c *cluster) poll(ctx context.Context, n primitive.Node, limit time.Duration, what string, check func() (done bool, last string, err error)) error {
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	ticker := time.NewTicker(c.p.timing.Poll)
	defer ticker.Stop()

	var last string
	for {
		done, note, err := check()
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case err != nil:
			// A command that could not run at all, such as one in a container that
			// is gone or stopped, will not start working if we wait.
			return fmt.Errorf("%s: could not %s: %w", n.Name, what, err)
		case done:
			return nil
		case note != "":
			last = note
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("%s did not %s within %s. Last seen: %s\n%s", n.Name, what, limit, orNone(last), c.logTail(ctx, n))
		case <-ticker.C:
		}
	}
}

func orNone(s string) string {
	if s == "" {
		return "nothing"
	}
	return s
}

// logTail returns the end of a node's Vault log for an error message. It uses a
// fresh short deadline, because the context that ran out may be the reason we are here.
func (c *cluster) logTail(ctx context.Context, n primitive.Node) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	res, err := c.rt.Exec(ctx, n.ContainerID, []string{"tail", "-n", "20", OperationalLog}, runtime.ExecOptions{})
	if err != nil || strings.TrimSpace(res.Stdout) == "" {
		return "(no log lines could be read from " + n.Name + ")"
	}
	return "Last lines of the log of " + n.Name + ":\n" + res.Stdout
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// execProblem describes a command that failed or could not run.
func execProblem(res runtime.ExecResult, err error) error {
	if err != nil {
		return err
	}
	msg := firstLine(res.Stderr)
	if msg == "" {
		return fmt.Errorf("exit code %d", res.ExitCode)
	}
	return fmt.Errorf("exit code %d: %s", res.ExitCode, msg)
}
