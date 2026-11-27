package boundary

import (
	"regexp"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/primitive"
)

func testKeys() keys {
	return keys{Root: "ROOTKEYROOTKEYROOTKEYROOTKEYROOT=", WorkerAuth: "WORKERKEYWORKERKEYWORKERKEYWOR=", Recovery: "RECOVERYKEYRECOVERYKEYRECOVER="}
}

func TestEachDeploymentGetsItsOwnRandomKeys(t *testing.T) {
	a, err := newKeys()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := newKeys()
	b64 := regexp.MustCompile(`^[A-Za-z0-9+/]{43}=$`) // 32 bytes
	for _, k := range []string{a.Root, a.WorkerAuth, a.Recovery} {
		if !b64.MatchString(k) {
			t.Errorf("%q is not a base64 key of 32 bytes", k)
		}
	}
	if a.Root == a.WorkerAuth || a.Root == a.Recovery || a.WorkerAuth == a.Recovery || a == b {
		t.Error("keys are repeated, within a deployment or between two")
	}
}

func TestPasswordsAreAlphanumericSoTheyNeedNoQuoting(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		p, err := randomPassword()
		if err != nil {
			t.Fatal(err)
		}
		if len(p) != 24 || !regexp.MustCompile(`^[A-Za-z0-9]+$`).MatchString(p) || seen[p] {
			t.Fatalf("password %q is not 24 fresh alphanumeric characters", p)
		}
		seen[p] = true
	}
}

func TestAControllerConfigurationHasItsDatabaseListenersAndAllThreeKeys(t *testing.T) {
	got := controllerHCL("node-2", "10.89.1.3", "postgresql://boundary:pw@10.89.1.9:5432/boundary?sslmode=disable", testKeys())
	for _, want := range []string{
		"disable_mlock = true",
		`name                = "node-2"`,
		`public_cluster_addr = "10.89.1.3:9201"`,
		`url = "postgresql://boundary:pw@10.89.1.9:5432/boundary?sslmode=disable"`,
		`address     = "0.0.0.0:9200"`, `purpose     = "api"`,
		`address     = "0.0.0.0:9201"`, `purpose     = "cluster"`,
		`address     = "0.0.0.0:9203"`, `purpose     = "ops"`,
		`purpose   = "root"`, `purpose   = "worker-auth"`, `purpose   = "recovery"`,
		`key_id    = "global_root"`, `key_id    = "global_worker-auth"`,
		`key       = "ROOTKEYROOTKEYROOTKEYROOTKEYROOT="`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the controller configuration lacks %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "tls_disable = true") != 3 {
		t.Error("each listener should have TLS off, as in a lab")
	}
}

func TestAWorkerConfigurationRegistersThroughTheLoadBalancerWithOnlyTheWorkerKey(t *testing.T) {
	got := workerHCL("worker-node-1", "10.89.1.6", "10.89.1.5:9201", testKeys())
	for _, want := range []string{
		`name              = "worker-node-1"`,
		`public_addr       = "10.89.1.6:9202"`,
		`initial_upstreams = ["10.89.1.5:9201"]`,
		`purpose     = "proxy"`,
		`purpose   = "worker-auth"`,
		`key       = "WORKERKEYWORKERKEYWORKERKEYWOR="`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the worker configuration lacks %q:\n%s", want, got)
		}
	}
	// A worker is not a controller, so it must not hold the keys that protect the data or recover access.
	for _, leak := range []string{"ROOTKEY", "RECOVERYKEY", `purpose   = "root"`, `purpose   = "recovery"`, "database"} {
		if strings.Contains(got, leak) {
			t.Errorf("the worker configuration contains %q", leak)
		}
	}
}

func TestTheLoadBalancerPassesTheAPIAndTheClusterPortToEveryControllerThatIsUp(t *testing.T) {
	got := haproxyCfg([]primitive.Node{{Name: "node-1", IP: "10.89.1.2"}, {Name: "node-2", IP: "10.89.1.3"}})
	for _, want := range []string{
		"mode tcp",
		"frontend api\n  bind *:9200", "backend controllers_api",
		"frontend cluster\n  bind *:9201", "backend controllers_cluster",
		"server node-1 10.89.1.2:9200 check port 9203",
		"server node-2 10.89.1.3:9201 check port 9203",
		"option httpchk GET /health",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the load balancer configuration lacks %q:\n%s", want, got)
		}
	}
}

func TestInitOutputIsReadForTheIDsTheClusterTabNeeds(t *testing.T) {
	out := `{"auth_method":{"auth_method_id":"ampw_1","login_name":"admin","password":"s3cret","user_id":"u_1"},
"org_scope":{"scope_id":"o_1"},"project_scope":{"scope_id":"p_1"},
"host_resources":{"host_catalog_id":"hcst_1","host_set_id":"hsst_1","host_id":"hst_1"},
"target":{"target_id":"ttcp_direct"},"target_secondary":{"target_id":"ttcp_hosts"}}`
	r, err := parseInit(out)
	if err != nil {
		t.Fatal(err)
	}
	if r.AuthMethod.ID != "ampw_1" || r.AuthMethod.Password != "s3cret" || r.ProjectScope.ID != "p_1" || r.OrgScope.ID != "o_1" ||
		r.Hosts.HostID != "hst_1" || r.Hosts.SetID != "hsst_1" || r.Hosts.CatalogID != "hcst_1" {
		t.Errorf("parsed = %+v", r)
	}
	if r.Target.ID != "ttcp_hosts" {
		t.Errorf("target = %q, want the one that reaches a host through the host set", r.Target.ID)
	}
}

func TestUnusableInitOutputIsRefusedWithoutEchoingIt(t *testing.T) {
	for name, out := range map[string]string{
		"not JSON":          `Initial login: admin / HUNTER2-PASSWORD`,
		"no password":       `{"auth_method":{"auth_method_id":"a"},"project_scope":{"scope_id":"p"},"host_resources":{"host_id":"h"},"target_secondary":{"target_id":"t"}}`,
		"no target":         `{"auth_method":{"auth_method_id":"a","password":"HUNTER2-PASSWORD"},"project_scope":{"scope_id":"p"},"host_resources":{"host_id":"h"}}`,
		"empty":             `{}`,
		"truncated with pw": `{"auth_method":{"auth_method_id":"a","password":"HUNTER2-PASSWORD"`,
	} {
		_, err := parseInit(out)
		if err == nil || strings.Contains(err.Error(), "HUNTER2") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
