package boundary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/product/deployutil"
	"github.com/pixambi/sev1situations/internal/release"
	"github.com/pixambi/sev1situations/internal/runtime"
)

// Roles a node can have in a deployment, recorded in the deployment's State as role:<node>.
const (
	RoleController = "controller"
	RoleWorker     = "worker"
	RoleLB         = "load balancer"
	RoleDatabase   = "database"
	RoleTarget     = "target host"
)

// Limits on a deployment's size.
const (
	minControllers = 1
	maxControllers = 5
)

// keepAlive is the main process of the containers Boundary runs in. The Boundary process is started with a separate command,
// so that one that stops leaves its container in place for the instructor to inspect, as a real outage would.
var keepAlive = []string{"tail", "-f", "/dev/null"}

// roleName is the name of the single node that has a role other than controller. The first cluster of a scenario
// names them worker-node-1, db-node-1 and so on, and later clusters put their name in front (section 3.6.1).
func roleName(spec product.DeploySpec, short string) string {
	base := short + "-node-1"
	if spec.NamePrefix != "" {
		return spec.NamePrefix + "-" + base
	}
	return base
}

// Deploy implements product.Product. It builds a whole Boundary deployment: the database, the controllers,
// the load balancer in front of them, a worker and a target host. If it fails part way, the containers it created are
// left in place and returned in the deployment, so their logs can be read. The engine removes them on Retry or End.
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
	c.poller = deployutil.Poller{Interval: p.timing.Poll, Tail: c.tail}
	if c.k, err = newKeys(); err != nil {
		return d, err
	}
	if c.dbPassword, err = randomPassword(); err != nil {
		return d, err
	}

	for _, step := range []func(context.Context, *product.Deployment) error{
		c.pullImages, c.startContainers, c.startDatabase, c.installControllers, c.initDatabase,
		c.startControllers, c.startLoadBalancer, c.startWorker, c.startTarget, c.seed,
	} {
		if err := step(ctx, &d); err != nil {
			return d, err
		}
	}
	c.say("Boundary %s is up with %d controller(s)", spec.Version.Full, spec.Nodes)
	return d, nil
}

// check validates the request before anything is created, so a mistake costs nothing. It returns the license the
// deployment needs, if any.
func (p *Product) check(spec product.DeploySpec) (string, error) {
	switch {
	case spec.Nodes < minControllers || spec.Nodes > maxControllers:
		return "", fmt.Errorf("a Boundary deployment has %d to %d controllers, not %d", minControllers, maxControllers, spec.Nodes)
	case spec.Edition != release.Community && spec.Edition != release.Enterprise:
		return "", fmt.Errorf("boundary has no %q edition", spec.Edition)
	case IsEnterprise(spec.Edition):
		return "", errors.New("the Enterprise edition of Boundary is not supported yet: it arrives with the session recording scenario")
	case spec.Image == "" || spec.Binary == "" || spec.Cluster == "":
		return "", errors.New("the deploy spec needs an image, a binary and a cluster name")
	}
	if _, err := os.Stat(spec.Binary); err != nil {
		return "", fmt.Errorf("the Boundary binary %s cannot be read: %w", spec.Binary, err)
	}
	return "", nil
}

// cluster carries what the deployment steps share.
type cluster struct {
	p          *Product
	rt         runtime.Runtime
	spec       product.DeploySpec
	license    string
	poller     deployutil.Poller
	k          keys
	dbPassword string

	// containers by node name, filled in as they start.
	nodes map[string]primitive.Node
}

func (c *cluster) say(format string, args ...any) {
	if c.spec.Progress != nil {
		c.spec.Progress(fmt.Sprintf(format, args...))
	}
}

// tail is the end of a node's log for a wait that ran out.
func (c *cluster) tail(ctx context.Context, name string) string {
	n, ok := c.nodes[name]
	if !ok {
		return ""
	}
	path := OperationalLog
	switch {
	case strings.HasPrefix(name, roleName(c.spec, "lb")):
		path = HAProxyLog
	case strings.HasPrefix(name, roleName(c.spec, "db")):
		return "" // the database logs to its container, not a file
	}
	return deployutil.TailLog(ctx, c.rt, n.ContainerID, name, path)
}

func (c *cluster) pullImages(ctx context.Context, _ *product.Deployment) error {
	for _, image := range []string{PostgresImage, HAProxyImage} {
		c.say("making sure the image %s is available", image)
		if err := c.rt.EnsureImage(ctx, image, c.spec.Progress); err != nil {
			return fmt.Errorf("get the image %s: %w", image, err)
		}
	}
	return nil
}

// controllers returns the controller nodes, in order.
func (c *cluster) controllers(d *product.Deployment) []primitive.Node {
	var out []primitive.Node
	for _, n := range d.Nodes {
		if d.State["role:"+n.Name] == RoleController {
			out = append(out, n)
		}
	}
	return out
}

func (c *cluster) start(ctx context.Context, d *product.Deployment, name, role, image string, command []string, env map[string]string, publish []int) error {
	var ports []runtime.PortMapping
	if c.spec.Ports != nil {
		for _, container := range publish {
			host, err := c.spec.Ports.Allocate()
			if err != nil {
				return fmt.Errorf("publish port %d of %s: %w", container, name, err)
			}
			ports = append(ports, runtime.PortMapping{Host: host, Container: container})
		}
	}
	ct, err := c.rt.StartContainer(ctx, runtime.ContainerOptions{
		Run: c.spec.Run, Name: name, Image: image, Command: command, Env: env,
		Network: c.spec.Network, Hostname: name, Ports: ports,
	})
	if err != nil {
		for _, pm := range ports {
			c.spec.Ports.Release(pm.Host)
		}
		return fmt.Errorf("start %s: %w", name, err)
	}
	n := primitive.Node{Name: name, Cluster: c.spec.Cluster, ContainerID: ct.ID, IP: ct.IP}
	d.Nodes = append(d.Nodes, n)
	d.State["role:"+name] = role
	for _, pm := range ports {
		d.Published = append(d.Published, pm)
		d.State["port:"+name] = fmt.Sprint(pm.Host)
	}
	if c.nodes == nil {
		c.nodes = map[string]primitive.Node{}
	}
	c.nodes[name] = n
	if ct.IP == "" {
		return fmt.Errorf("%s has no address on the run's network", name)
	}
	return nil
}

// startContainers creates every container. They are all created before any is configured, because each one's
// configuration names the addresses of the others.
func (c *cluster) startContainers(ctx context.Context, d *product.Deployment) error {
	c.say("starting the containers")
	for i := 1; i <= c.spec.Nodes; i++ {
		if err := c.start(ctx, d, product.NodeName(c.spec, i), RoleController, c.spec.Image, keepAlive, nil, []int{apiPort}); err != nil {
			return err
		}
	}
	if err := c.start(ctx, d, roleName(c.spec, "worker"), RoleWorker, c.spec.Image, keepAlive, nil, nil); err != nil {
		return err
	}
	if err := c.start(ctx, d, roleName(c.spec, "lb"), RoleLB, HAProxyImage, keepAlive, nil, []int{apiPort}); err != nil {
		return err
	}
	db := map[string]string{"POSTGRES_USER": dbUser, "POSTGRES_PASSWORD": c.dbPassword, "POSTGRES_DB": dbName}
	if err := c.start(ctx, d, roleName(c.spec, "db"), RoleDatabase, PostgresImage, nil, db, nil); err != nil {
		return err
	}
	return c.start(ctx, d, roleName(c.spec, "target"), RoleTarget, c.spec.Image, keepAlive, nil, nil)
}

func (c *cluster) startDatabase(ctx context.Context, d *product.Deployment) error {
	db := c.nodes[roleName(c.spec, "db")]
	c.say("waiting for the database to accept connections")
	return c.poller.Wait(ctx, db.Name, c.p.timing.Database, "accept connections", func() (bool, string, error) {
		// Over TCP, because while the image sets itself up its temporary server listens on a socket only.
		res, err := c.rt.Exec(ctx, db.ContainerID, []string{"pg_isready", "-h", "127.0.0.1", "-U", dbUser, "-d", dbName}, runtime.ExecOptions{})
		if err != nil {
			return false, "", err
		}
		return res.ExitCode == 0, deployutil.FirstLine(res.Stdout + res.Stderr), nil
	})
}

// installControllers puts the binary and each controller's configuration in place.
func (c *cluster) installControllers(ctx context.Context, d *product.Deployment) error {
	c.say("installing Boundary %s on the controllers", c.spec.Version.Full)
	url := dbURL(c.dbPassword, c.nodes[roleName(c.spec, "db")].IP)
	for _, n := range c.controllers(d) {
		if err := c.install(ctx, n, controllerConfig, controllerHCL(n.Name, n.IP, url, c.k)); err != nil {
			return err
		}
	}
	return nil
}

// install copies the binary and a configuration file into a node and makes its folders.
func (c *cluster) install(ctx context.Context, n primitive.Node, configPath, config string) error {
	res, err := c.rt.Exec(ctx, n.ContainerID, []string{"mkdir", "-p", confDir, logDir}, runtime.ExecOptions{})
	if err != nil || res.ExitCode != 0 {
		return fmt.Errorf("create the folders on %s: %w", n.Name, deployutil.ExecProblem(res, err))
	}
	bin, err := os.Open(c.spec.Binary)
	if err != nil {
		return fmt.Errorf("open the Boundary binary: %w", err)
	}
	defer func() { _ = bin.Close() }()
	info, err := bin.Stat()
	if err != nil {
		return fmt.Errorf("read the Boundary binary: %w", err)
	}
	for _, f := range []runtime.File{
		{Path: binaryPath, Mode: 0o755, Size: info.Size(), Content: bin},
		deployutil.File(configPath, 0o644, config),
	} {
		if err := c.rt.Copy(ctx, n.ContainerID, f); err != nil {
			return fmt.Errorf("copy %s to %s: %w", f.Path, n.Name, err)
		}
	}
	return nil
}

// boundary runs the Boundary command line inside a node, signed in with a token if there is one.
func (c *cluster) boundary(ctx context.Context, n primitive.Node, token *string, args ...string) (runtime.ExecResult, error) {
	env := map[string]string{"BOUNDARY_ADDR": fmt.Sprintf("http://127.0.0.1:%d", apiPort)}
	if token != nil {
		env["BOUNDARY_TOKEN"] = *token
		args = append(args, "-token=env://BOUNDARY_TOKEN")
	}
	return c.rt.Exec(ctx, n.ContainerID, append([]string{binaryPath}, args...), runtime.ExecOptions{Env: env})
}

// initResult is the part of `boundary database init -format=json` we use. It holds the generated admin
// password, so the whole output is never logged or put in an error.
type initResult struct {
	AuthMethod struct {
		ID        string `json:"auth_method_id"`
		LoginName string `json:"login_name"`
		Password  string `json:"password"`
		UserID    string `json:"user_id"`
	} `json:"auth_method"`
	OrgScope struct {
		ID string `json:"scope_id"`
	} `json:"org_scope"`
	ProjectScope struct {
		ID string `json:"scope_id"`
	} `json:"project_scope"`
	Hosts struct {
		CatalogID string `json:"host_catalog_id"`
		SetID     string `json:"host_set_id"`
		HostID    string `json:"host_id"`
	} `json:"host_resources"`
	// Target is the generated target that reaches a host through the host set, which is the one used here.
	Target struct {
		ID string `json:"target_id"`
	} `json:"target_secondary"`
}

func parseInit(out string) (initResult, error) {
	var r initResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		// The output holds the admin password, so it is not echoed.
		return r, errors.New("read the output of boundary database init: it is not valid JSON")
	}
	if r.AuthMethod.ID == "" || r.AuthMethod.Password == "" || r.ProjectScope.ID == "" || r.Target.ID == "" || r.Hosts.HostID == "" {
		return r, errors.New("boundary database init did not report an auth method, an admin password, a project, a host and a target")
	}
	return r, nil
}

func (c *cluster) initDatabase(ctx context.Context, d *product.Deployment) error {
	first := c.controllers(d)[0]
	c.say("initialising the database from %s", first.Name)
	res, err := c.boundary(ctx, first, nil, "database", "init", "-config="+controllerConfig, "-format=json")
	if err != nil || res.ExitCode != 0 {
		// The output of a successful init holds the admin password, so on failure only standard error is reported.
		return fmt.Errorf("initialise the database from %s: %w", first.Name, deployutil.ExecProblem(runtime.ExecResult{ExitCode: res.ExitCode, Stderr: res.Stderr}, err))
	}
	r, err := parseInit(res.Stdout)
	if err != nil {
		return fmt.Errorf("initialise the database from %s: %w", first.Name, err)
	}
	d.Creds["adminPassword"] = r.AuthMethod.Password
	d.Creds["kmsRoot"], d.Creds["kmsWorkerAuth"], d.Creds["kmsRecovery"] = c.k.Root, c.k.WorkerAuth, c.k.Recovery
	d.Creds["databasePassword"] = c.dbPassword
	for k, v := range map[string]string{
		"adminLogin": r.AuthMethod.LoginName, "authMethodId": r.AuthMethod.ID, "userId": r.AuthMethod.UserID,
		"orgScopeId": r.OrgScope.ID, "projectScopeId": r.ProjectScope.ID,
		"hostCatalogId": r.Hosts.CatalogID, "hostSetId": r.Hosts.SetID, "hostId": r.Hosts.HostID, "targetId": r.Target.ID,
	} {
		d.State[k] = v
	}
	return nil
}

// serve starts a Boundary server in a node from a configuration file. It is detached, so Boundary keeps running after the
// command returns, and its output goes to the file the Logs tab follows.
func (c *cluster) serve(ctx context.Context, n primitive.Node, configPath string) error {
	cmd := fmt.Sprintf("exec %s server -config=%s >> %s 2>&1", binaryPath, configPath, OperationalLog)
	if _, err := c.rt.Exec(ctx, n.ContainerID, []string{"sh", "-c", cmd}, runtime.ExecOptions{Detach: true}); err != nil {
		return fmt.Errorf("start Boundary on %s: %w", n.Name, err)
	}
	return nil
}

// startControllers starts the first controller and waits for it to be healthy, then starts the rest. A fresh
// database has the host plugins registered by the first controller that starts, and controllers that start together
// race to do it: one of them fails with a duplicate key and exits. Starting them one after another, as an operator
// would, avoids that.
func (c *cluster) startControllers(ctx context.Context, d *product.Deployment) error {
	ctls := c.controllers(d)
	for i, n := range ctls {
		if i == 0 {
			c.say("starting %s, the first controller", n.Name)
		} else {
			c.say("starting %s", n.Name)
		}
		if err := c.serve(ctx, n, controllerConfig); err != nil {
			return err
		}
		c.say("waiting for %s to report healthy", n.Name)
		err := c.poller.Wait(ctx, n.Name, c.p.timing.Controller, "report healthy", func() (bool, string, error) {
			res, err := c.rt.Exec(ctx, n.ContainerID, []string{"wget", "-q", "-T", "3", "-O", "/dev/null", fmt.Sprintf("http://127.0.0.1:%d/health", opsPort)}, runtime.ExecOptions{})
			if err != nil {
				return false, "", err
			}
			return res.ExitCode == 0, deployutil.FirstLine(res.Stderr), nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *cluster) startLoadBalancer(ctx context.Context, d *product.Deployment) error {
	lb := c.nodes[roleName(c.spec, "lb")]
	c.say("starting the load balancer in front of the controllers")
	for _, f := range []runtime.File{deployutil.File(haproxyConfig, 0o644, haproxyCfg(c.controllers(d)))} {
		if err := c.rt.Copy(ctx, lb.ContainerID, f); err != nil {
			return fmt.Errorf("copy %s to %s: %w", f.Path, lb.Name, err)
		}
	}
	if _, err := c.rt.Exec(ctx, lb.ContainerID, []string{"sh", "-c", "exec haproxy -f " + haproxyConfig + " >> " + HAProxyLog + " 2>&1"}, runtime.ExecOptions{Detach: true}); err != nil {
		return fmt.Errorf("start HAProxy on %s: %w", lb.Name, err)
	}
	// The way through is checked by signing in through it, which is also how the deployment gets a token to seed with.
	first := c.controllers(d)[0]
	return c.poller.Wait(ctx, lb.Name, c.p.timing.Controller, "pass a sign-in through to a controller", func() (bool, string, error) {
		_, note, err := c.signIn(ctx, d, first, lb.IP)
		return err == nil, note, nil
	})
}

// signIn authenticates as the admin through the given host and returns a token. via is the address of the API to use:
// the load balancer's, or empty for the node's own.
func (c *cluster) signIn(ctx context.Context, d *product.Deployment, from primitive.Node, via string) (string, string, error) {
	addr := fmt.Sprintf("http://127.0.0.1:%d", apiPort)
	if via != "" {
		addr = fmt.Sprintf("http://%s:%d", via, apiPort)
	}
	res, err := c.rt.Exec(ctx, from.ContainerID, []string{binaryPath, "authenticate", "password",
		"-auth-method-id=" + d.State["authMethodId"], "-login-name=" + d.State["adminLogin"], "-password=env://BOUNDARY_PASSWORD",
		"-keyring-type=none", "-format=json"},
		runtime.ExecOptions{Env: map[string]string{"BOUNDARY_ADDR": addr, "BOUNDARY_PASSWORD": d.Creds["adminPassword"]}})
	if err != nil {
		return "", "", err
	}
	if res.ExitCode != 0 {
		return "", deployutil.FirstLine(res.Stderr), fmt.Errorf("sign in: %s", deployutil.FirstLine(res.Stderr))
	}
	var out struct {
		Item struct {
			Attributes struct {
				Token string `json:"token"`
			} `json:"attributes"`
		} `json:"item"`
	}
	if json.Unmarshal([]byte(res.Stdout), &out) != nil || out.Item.Attributes.Token == "" {
		return "", "the sign-in did not return a token", errors.New("the sign-in did not return a token") // the output holds the token
	}
	return out.Item.Attributes.Token, "", nil
}

func (c *cluster) startWorker(ctx context.Context, d *product.Deployment) error {
	w, lb := c.nodes[roleName(c.spec, "worker")], c.nodes[roleName(c.spec, "lb")]
	c.say("starting the worker, which registers through the load balancer")
	if err := c.install(ctx, w, workerConfig, workerHCL(w.Name, w.IP, fmt.Sprintf("%s:%d", lb.IP, clusterPort), c.k)); err != nil {
		return err
	}
	if err := c.serve(ctx, w, workerConfig); err != nil {
		return err
	}
	first := c.controllers(d)[0]
	return c.poller.Wait(ctx, w.Name, c.p.timing.Worker, "register with the controllers", func() (bool, string, error) {
		token, note, err := c.signIn(ctx, d, first, "")
		if err != nil {
			return false, note, nil
		}
		res, err := c.boundary(ctx, first, &token, "workers", "list", "-format=json")
		if err != nil {
			return false, "", err
		}
		return res.ExitCode == 0 && strings.Contains(res.Stdout, `"`+w.Name+`"`), deployutil.FirstLine(res.Stderr), nil
	})
}

func (c *cluster) startTarget(ctx context.Context, d *product.Deployment) error {
	t := c.nodes[roleName(c.spec, "target")]
	pw, err := randomPassword()
	if err != nil {
		return err
	}
	c.say("installing an SSH server on the target host")
	script := "apk add --no-cache openssh >/dev/null && ssh-keygen -A >/dev/null && " +
		"adduser -D -s /bin/sh " + targetUser + " && echo '" + targetUser + ":" + pw + "' | chpasswd && " +
		"mkdir -p /run/sshd && /usr/sbin/sshd -e"
	err = c.poller.Wait(ctx, t.Name, c.p.timing.Install, "install and start its SSH server", func() (bool, string, error) {
		res, err := c.rt.Exec(ctx, t.ContainerID, []string{"sh", "-c", script}, runtime.ExecOptions{})
		if err != nil {
			return false, "", err
		}
		return res.ExitCode == 0, deployutil.FirstLine(res.Stderr), nil
	})
	if err != nil {
		return err
	}
	d.Creds["targetPassword"] = pw
	d.State["targetUser"] = targetUser
	return c.poller.Wait(ctx, t.Name, c.p.timing.Install, "answer on the SSH port", func() (bool, string, error) {
		res, err := c.rt.Exec(ctx, t.ContainerID, []string{"sh", "-c", fmt.Sprintf("nc -w 2 127.0.0.1 %d </dev/null", sshPort)}, runtime.ExecOptions{})
		if err != nil {
			return false, "", err
		}
		return strings.Contains(res.Stdout, "SSH-"), deployutil.FirstLine(res.Stderr), nil
	})
}

// seed points the generated host at the target host, so the generated target reaches a real SSH server.
func (c *cluster) seed(ctx context.Context, d *product.Deployment) error {
	first, t := c.controllers(d)[0], c.nodes[roleName(c.spec, "target")]
	c.say("pointing the generated host at the target host")
	token, _, err := c.signIn(ctx, d, first, "")
	if err != nil {
		return fmt.Errorf("sign in to seed the resources: %w", err)
	}
	res, err := c.boundary(ctx, first, &token, "hosts", "update", "static", "-id="+d.State["hostId"], "-address="+t.IP, "-name=target-host", "-description=The SSH server of the target host")
	if err != nil || res.ExitCode != 0 {
		return fmt.Errorf("point the generated host at %s: %w", t.Name, deployutil.ExecProblem(res, err))
	}
	return nil
}

func randomPassword() (string, error) {
	k, err := newKeys()
	if err != nil {
		return "", err
	}
	// Alphanumeric only, so it needs no quoting in a URL or a shell.
	var b strings.Builder
	for _, r := range k.Root {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()[:24], nil
}
