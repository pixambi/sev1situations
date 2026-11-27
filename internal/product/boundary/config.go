package boundary

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/pixambi/sev1situations/internal/primitive"
)

// Paths and ports inside the containers.
const (
	binaryPath       = "/usr/local/bin/boundary"
	confDir          = "/etc/boundary.d"
	controllerConfig = confDir + "/controller.hcl"
	workerConfig     = confDir + "/worker.hcl"
	haproxyConfig    = "/usr/local/etc/haproxy/haproxy.cfg"
	logDir           = "/boundary/logs"
	// OperationalLog is the file a Boundary controller or worker writes its log to.
	OperationalLog = logDir + "/operational.log"
	// HAProxyLog is the file the load balancer writes its log to.
	HAProxyLog = "/tmp/haproxy.log"

	apiPort     = 9200
	clusterPort = 9201
	proxyPort   = 9202
	opsPort     = 9203
	dbPort      = 5432
	sshPort     = 22

	dbName = "boundary"
	dbUser = "boundary"

	// targetUser is the account on the target host that sessions log in as.
	targetUser = "target"
)

// Timing says how long the deployment waits for each stage. A stage that runs out of time says which
// container and shows the end of its log (section 7).
type Timing struct {
	// Poll is how often a condition is checked.
	Poll time.Duration
	// Database is how long the database has to accept connections.
	Database time.Duration
	// Controller is how long a controller has to report healthy after it starts.
	Controller time.Duration
	// Worker is how long the worker has to register with the controllers.
	Worker time.Duration
	// Install is how long the target host has to install and start its SSH server.
	Install time.Duration
	// Describe is how long the Cluster tab waits for one node to report before showing it as unknown.
	Describe time.Duration
}

func (t Timing) withDefaults() Timing {
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&t.Poll, 500*time.Millisecond)
	def(&t.Database, 90*time.Second)
	def(&t.Controller, 120*time.Second)
	def(&t.Worker, 120*time.Second)
	def(&t.Install, 180*time.Second)
	def(&t.Describe, 5*time.Second)
	return t
}

// keys are the three AEAD keys a self-managed Boundary needs. Every controller and the worker share them:
// the root key protects the data, the worker-auth key lets a worker register without a token, and the
// recovery key lets an operator recover access. They are secrets.
type keys struct {
	Root, WorkerAuth, Recovery string
}

func newKeys() (keys, error) {
	gen := func() (string, error) {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return "", fmt.Errorf("make a key: %w", err)
		}
		return base64.StdEncoding.EncodeToString(b), nil
	}
	var k keys
	var err error
	if k.Root, err = gen(); err != nil {
		return k, err
	}
	if k.WorkerAuth, err = gen(); err != nil {
		return k, err
	}
	k.Recovery, err = gen()
	return k, err
}

// kms writes the key blocks for the given purposes, which are root, worker-auth and recovery.
func (k keys) kms(purposes ...string) string {
	var b strings.Builder
	for _, p := range purposes {
		key := map[string]string{"root": k.Root, "worker-auth": k.WorkerAuth, "recovery": k.Recovery}[p]
		fmt.Fprintf(&b, "kms \"aead\" {\n  purpose   = %q\n  aead_type = \"aes-gcm\"\n  key       = %q\n  key_id    = %q\n}\n\n", p, key, "global_"+p)
	}
	return b.String()
}

// dbURL is how a controller reaches the database.
func dbURL(password, host string) string {
	return fmt.Sprintf("postgresql://%s:%s@%s:%d/%s?sslmode=disable", dbUser, password, host, dbPort, dbName)
}

// controllerHCL is a controller's configuration. TLS is off, as in a lab, and the ops listener serves the
// health endpoint the deployment waits on.
func controllerHCL(name, ip, url string, k keys) string {
	var b strings.Builder
	b.WriteString("disable_mlock = true\n\n")
	fmt.Fprintf(&b, "controller {\n  name                = %q\n  description         = %q\n  public_cluster_addr = \"%s:%d\"\n\n  database {\n    url = %q\n  }\n}\n\n", name, "Boundary controller "+name, ip, clusterPort, url)
	for _, l := range []struct {
		port    int
		purpose string
	}{{apiPort, "api"}, {clusterPort, "cluster"}, {opsPort, "ops"}} {
		fmt.Fprintf(&b, "listener \"tcp\" {\n  address     = \"0.0.0.0:%d\"\n  purpose     = %q\n  tls_disable = true\n}\n\n", l.port, l.purpose)
	}
	b.WriteString(k.kms("root", "worker-auth", "recovery"))
	return b.String()
}

// workerHCL is a worker's configuration. It registers with the controllers through the load balancer, using the
// shared worker-auth key, and advertises its own address to clients.
func workerHCL(name, ip, upstream string, k keys) string {
	var b strings.Builder
	b.WriteString("disable_mlock = true\n\n")
	fmt.Fprintf(&b, "worker {\n  name              = %q\n  description       = %q\n  public_addr       = \"%s:%d\"\n  initial_upstreams = [%q]\n}\n\n", name, "Boundary worker "+name, ip, proxyPort, upstream)
	fmt.Fprintf(&b, "listener \"tcp\" {\n  address     = \"0.0.0.0:%d\"\n  purpose     = \"proxy\"\n  tls_disable = true\n}\n\n", proxyPort)
	b.WriteString(k.kms("worker-auth"))
	return b.String()
}

// haproxyCfg is the load balancer's configuration. It passes TCP straight through, so the API and the
// workers' cluster connections reach a controller that its health check says is up.
func haproxyCfg(controllers []primitive.Node) string {
	var b strings.Builder
	b.WriteString("global\n  log stdout format raw local0\n  maxconn 512\n\n")
	b.WriteString("defaults\n  mode tcp\n  log global\n  timeout connect 5s\n  timeout client  1h\n  timeout server  1h\n\n")
	for _, f := range []struct {
		name string
		port int
	}{{"api", apiPort}, {"cluster", clusterPort}} {
		fmt.Fprintf(&b, "frontend %s\n  bind *:%d\n  default_backend controllers_%s\n\n", f.name, f.port, f.name)
		fmt.Fprintf(&b, "backend controllers_%s\n  balance roundrobin\n  option httpchk GET /health\n", f.name)
		for _, c := range controllers {
			// The health check asks the ops listener, which is up when the controller is.
			fmt.Fprintf(&b, "  server %s %s:%d check port %d inter 2s fall 2 rise 1\n", c.Name, c.IP, f.port, opsPort)
		}
		b.WriteString("\n")
	}
	return b.String()
}
