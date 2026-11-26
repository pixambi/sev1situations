package vault

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/primitive/primitivetest"
	"github.com/pixambi/sev1situations/internal/runtime"
	rtfake "github.com/pixambi/sev1situations/internal/runtime/fake"
)

const (
	pToken = "hvs.PrimitiveRootToken"
	pKeyA  = "unseal-key-aaaa"
	pKeyB  = "unseal-key-bbbb"
	pKeyC  = "unseal-key-cccc"
	pKeyD  = "unseal-key-dddd"
)

type pm = map[string]any

// execCall is one vault command a primitive ran.
type execCall struct {
	args []string
	env  map[string]string
}

// primRig runs Vault primitives against a scripted exec.
type primRig struct {
	t      *testing.T
	reg    *primitive.Registry
	env    primitive.Env
	mu     sync.Mutex
	calls  []execCall
	script func(args []string, env map[string]string) runtime.ExecResult
}

func newPrimRig(t *testing.T) *primRig {
	t.Helper()
	rt := rtfake.New()
	r := &primRig{t: t, reg: primitive.NewRegistry()}
	for _, p := range New(Options{}).Primitives() {
		if err := r.reg.Register(p); err != nil {
			t.Fatal(err)
		}
	}

	nodes := map[string]primitive.Node{}
	for _, name := range []string{"node-1", "node-2"} {
		c, err := rt.StartContainer(context.Background(), runtime.ContainerOptions{Run: runtime.Run{ID: "p1"}, Name: name, Image: "alpine:3"})
		if err != nil {
			t.Fatal(err)
		}
		nodes[name] = primitive.Node{Name: name, Cluster: "primary", ContainerID: c.ID, IP: "10.89.1.2"}
	}
	creds := map[string]map[string]string{"primary": {
		"rootToken": pToken, "unsealKey1": pKeyA, "unsealKey2": pKeyB, "unsealKey3": pKeyC, "unsealKey4": pKeyD,
	}}
	r.env = primitive.Env{Runtime: rt, Nodes: nodes, Creds: creds, Redactor: primitive.NewRedactor(pToken, pKeyA, pKeyB, pKeyC, pKeyD)}

	rt.SetExec(func(_ runtime.Container, cmd []string, o runtime.ExecOptions) (runtime.ExecResult, error) {
		if cmd[0] != binaryPath {
			return runtime.ExecResult{}, nil
		}
		r.mu.Lock()
		r.calls = append(r.calls, execCall{args: append([]string(nil), cmd[1:]...), env: o.Env})
		script := r.script
		r.mu.Unlock()
		if script == nil {
			return runtime.ExecResult{}, nil
		}
		return script(cmd[1:], o.Env), nil
	})
	return r
}

func (r *primRig) run(name string, args pm) (primitive.Result, error) {
	r.t.Helper()
	return r.reg.Run(context.Background(), name, r.env, args)
}

func (r *primRig) pass(name string, args pm) primitive.Result {
	r.t.Helper()
	res, err := r.run(name, args)
	if err != nil || res.Status != primitive.Pass {
		r.t.Fatalf("%s = %+v, %v; want a pass", name, res, err)
	}
	return res
}

func (r *primRig) fail(name string, args pm, msg string) primitive.Result {
	r.t.Helper()
	res, err := r.run(name, args)
	if err != nil || res.Status != primitive.Fail || !strings.Contains(res.Message, msg) {
		r.t.Fatalf("%s = %+v, %v; want a fail containing %q", name, res, err, msg)
	}
	return res
}

func (r *primRig) errors(name string, args pm, msg string) {
	r.t.Helper()
	res, err := r.run(name, args)
	if err == nil || !strings.Contains(err.Error(), msg) {
		r.t.Fatalf("%s = %+v, %v; want an error containing %q", name, res, err, msg)
	}
}

func (r *primRig) ran(prefix ...string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if len(c.args) >= len(prefix) && strings.Join(c.args[:len(prefix)], " ") == strings.Join(prefix, " ") {
			n++
		}
	}
	return n
}

func (r *primRig) last() execCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		r.t.Fatal("no vault command ran")
	}
	return r.calls[len(r.calls)-1]
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

const (
	activeStatus  = `{"initialized":true,"sealed":false,"version":"2.2.2","ha_enabled":true,"is_self":true,"leader_address":"http://10.89.1.2:8200"}`
	standbyStatus = `{"initialized":true,"sealed":false,"version":"2.2.2","ha_enabled":true,"leader_address":"http://10.89.1.2:8200"}`
	noLeader      = `{"initialized":true,"sealed":false,"version":"2.2.2","ha_enabled":true}`
	sealedStatus  = `{"initialized":true,"sealed":true,"version":"2.2.2","ha_enabled":true}`
)

func statusReturning(out string, code int) func([]string, map[string]string) runtime.ExecResult {
	return func(args []string, _ map[string]string) runtime.ExecResult {
		return runtime.ExecResult{ExitCode: code, Stdout: out}
	}
}

func TestEveryVaultPrimitiveMeetsTheContract(t *testing.T) {
	valid := map[string]pm{
		"vault.cli":              {"node": "node-1", "command": []any{"status"}},
		"vault.status":           {"node": "node-1"},
		"vault.raft-peers":       {"node": "node-1"},
		"vault.expect-no-leader": {"node": "node-1"},
		"vault.kv-put":           {"node": "node-1", "path": "app", "data": pm{"k": "v"}},
		"vault.kv-get":           {"node": "node-1", "path": "app"},
		"vault.unseal":           {"node": "node-1"},
		"vault.init":             {"node": "node-1"},
	}
	prims := New(Options{}).Primitives()
	if len(prims) != len(valid) {
		t.Fatalf("%d primitives, the test knows %d", len(prims), len(valid))
	}
	for _, p := range prims {
		args, ok := valid[p.Spec().Name]
		if !ok {
			t.Fatalf("no valid arguments for %s", p.Spec().Name)
		}
		t.Run(p.Spec().Name, func(t *testing.T) {
			r := newPrimRig(t)
			primitivetest.Contract(t, p, primitivetest.Options{ValidArgs: args, Env: r.env})
		})
	}
}

func TestCLIInjectsTheAddressAndTheRootToken(t *testing.T) {
	r := newPrimRig(t)
	r.script = func(args []string, _ map[string]string) runtime.ExecResult {
		return runtime.ExecResult{Stdout: "  Key  Value\nsealed false\n"}
	}

	res := r.pass("vault.cli", pm{"node": "node-1", "command": []any{"read", "sys/health"}, "save": "health", "contains": "sealed"})
	c := r.last()
	if strings.Join(c.args, " ") != "read sys/health" || c.env["VAULT_ADDR"] != "http://127.0.0.1:8200" || c.env["VAULT_TOKEN"] != pToken {
		t.Errorf("command = %v env = %v, want the address and the cluster's root token in the environment", c.args, c.env)
	}
	for _, a := range c.args {
		if strings.Contains(a, pToken) {
			t.Errorf("the token is on the command line: %v (R-QUAL-3)", c.args)
		}
	}
	if res.Outputs["health"] != "Key  Value\nsealed false" {
		t.Errorf("outputs = %v", res.Outputs)
	}

	// An explicit token replaces the root token.
	r.pass("vault.cli", pm{"node": "node-1", "command": []any{"status"}, "token": "hvs.other"})
	if r.last().env["VAULT_TOKEN"] != "hvs.other" {
		t.Errorf("env = %v, want the token that was given", r.last().env)
	}

	r.script = statusReturning("", 2)
	r.fail("vault.cli", pm{"node": "node-1", "command": []any{"status"}}, "exited with 2 on node-1, expected 0")
	r.pass("vault.cli", pm{"node": "node-1", "command": []any{"status"}, "expectExit": 2})
	r.errors("vault.cli", pm{"node": "node-9", "command": []any{"status"}}, `node "node-9" is not part of this deployment`)
}

func TestCLIEvidenceHasTheTokenRemoved(t *testing.T) {
	r := newPrimRig(t)
	r.script = func([]string, map[string]string) runtime.ExecResult {
		return runtime.ExecResult{ExitCode: 2, Stderr: "permission denied for token " + pToken}
	}
	res, err := r.run("vault.cli", pm{"node": "node-1", "command": []any{"read", "x"}})
	if err != nil || strings.Contains(res.Evidence, pToken) || !strings.Contains(res.Evidence, "[redacted]") {
		t.Errorf("evidence = %q, %v, want the token replaced (R-QUAL-3)", res.Evidence, err)
	}
}

func TestStatus(t *testing.T) {
	r := newPrimRig(t)

	r.script = statusReturning(activeStatus, 0)
	res := r.pass("vault.status", pm{"node": "node-1", "initialized": true, "sealed": false, "active": true})
	if res.Outputs["role"] != "active" || res.Outputs["version"] != "2.2.2" || res.Outputs["sealed"] != "false" {
		t.Errorf("outputs = %v", res.Outputs)
	}
	if c := r.last(); c.env["VAULT_TOKEN"] != "" {
		t.Errorf("status carried a token it does not need: %v", c.env)
	}

	r.script = statusReturning(standbyStatus, 0)
	r.fail("vault.status", pm{"node": "node-2", "active": true}, "active is false, expected true")

	r.script = statusReturning(sealedStatus, 2)
	r.fail("vault.status", pm{"node": "node-1", "sealed": false}, "sealed is true, expected false")
	r.pass("vault.status", pm{"node": "node-1", "sealed": true})

	// Unreachable.
	r.script = func([]string, map[string]string) runtime.ExecResult {
		return runtime.ExecResult{ExitCode: 1, Stderr: "connection refused"}
	}
	r.fail("vault.status", pm{"node": "node-1"}, "does not answer on node-1")
	r.pass("vault.status", pm{"node": "node-1", "reachable": false})

	r.script = statusReturning(activeStatus, 0)
	r.fail("vault.status", pm{"node": "node-1", "reachable": false}, "was expected to be down")
}

func TestRaftPeers(t *testing.T) {
	r := newPrimRig(t)
	peersJSON := js(pm{"data": pm{"config": pm{"servers": []pm{
		{"node_id": "node-1", "leader": true, "voter": true},
		{"node_id": "node-2", "leader": false, "voter": true},
		{"node_id": "node-3", "leader": false, "voter": false},
	}}}})
	r.script = statusReturning(peersJSON, 0)

	res := r.pass("vault.raft-peers", pm{"node": "node-1", "servers": 3, "voters": 2, "leader": "node-1"})
	if res.Outputs["servers"] != "3" || res.Outputs["voters"] != "2" || res.Outputs["leader"] != "node-1" {
		t.Errorf("outputs = %v", res.Outputs)
	}
	if c := r.last(); c.env["VAULT_TOKEN"] != pToken || strings.Join(c.args, " ") != "operator raft list-peers -format=json" {
		t.Errorf("command = %v env = %v, want list-peers as the root token", c.args, c.env)
	}

	r.fail("vault.raft-peers", pm{"node": "node-1", "voters": 3}, "2 voters, expected 3")
	r.fail("vault.raft-peers", pm{"node": "node-1", "servers": 5, "leader": "node-2"}, "5")
	r.pass("vault.raft-peers", pm{"node": "node-1"}) // nothing to compare: it only has to be readable

	// With no leader the command itself fails, which is the condition not being met.
	r.script = func([]string, map[string]string) runtime.ExecResult {
		return runtime.ExecResult{ExitCode: 2, Stderr: "Error reading the raft configuration: no cluster leader\n"}
	}
	r.fail("vault.raft-peers", pm{"node": "node-1", "voters": 1}, "no cluster leader")

	r.script = statusReturning("not json", 0)
	r.errors("vault.raft-peers", pm{"node": "node-1"}, "list-peers")
}

func TestExpectNoLeader(t *testing.T) {
	tests := []struct {
		name   string
		out    string
		code   int
		pass   bool
		reason string
	}{
		{"unsealed and no leader known", noLeader, 0, true, ""},
		{"the active node", activeStatus, 0, false, "is the active node"},
		{"a standby that knows its leader", standbyStatus, 0, false, "reports a leader at http://10.89.1.2:8200"},
		{"a sealed node", sealedStatus, 2, false, "is sealed"},
		{"a node that does not answer", "", 1, false, "does not answer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newPrimRig(t)
			r.script = statusReturning(tt.out, tt.code)
			if tt.pass {
				r.pass("vault.expect-no-leader", pm{"node": "node-1"})
			} else {
				r.fail("vault.expect-no-leader", pm{"node": "node-1"}, tt.reason)
			}
		})
	}
}

func TestKVPutEnablesTheMountOnlyWhenMissing(t *testing.T) {
	r := newPrimRig(t)
	mounted := false
	r.script = func(args []string, _ map[string]string) runtime.ExecResult {
		switch {
		case args[0] == "secrets" && args[1] == "list":
			if mounted {
				return runtime.ExecResult{Stdout: `{"secret/":{"type":"kv"},"sys/":{"type":"system"}}`}
			}
			return runtime.ExecResult{Stdout: `{"sys/":{"type":"system"}}`}
		case args[0] == "secrets" && args[1] == "enable":
			mounted = true
			return runtime.ExecResult{}
		}
		return runtime.ExecResult{}
	}

	r.pass("vault.kv-put", pm{"node": "node-1", "path": "app/db", "data": pm{"user": "ada", "pass": "s3cret", "port": 5432}})
	if r.ran("secrets", "enable", "-path=secret", "-version=2", "kv") != 1 {
		t.Error("the missing mount was not enabled")
	}
	put := r.last()
	if strings.Join(put.args, " ") != "kv put -mount=secret app/db pass=s3cret port=5432 user=ada" {
		t.Errorf("put = %v, want the pairs in a fixed order", put.args)
	}
	if put.env["VAULT_TOKEN"] != pToken {
		t.Errorf("env = %v", put.env)
	}

	r.pass("vault.kv-put", pm{"node": "node-1", "path": "app/db", "data": pm{"k": "v"}})
	if r.ran("secrets", "enable") != 1 {
		t.Error("an existing mount was enabled again")
	}

	r.errors("vault.kv-put", pm{"node": "node-1", "path": "x", "data": pm{}}, "at least one key")
}

func TestKVPutToleratesARaceOnTheMount(t *testing.T) {
	r := newPrimRig(t)
	r.script = func(args []string, _ map[string]string) runtime.ExecResult {
		if args[0] == "secrets" && args[1] == "enable" {
			return runtime.ExecResult{ExitCode: 2, Stderr: "path is already in use at secret/"}
		}
		return runtime.ExecResult{Stdout: `{}`}
	}
	r.pass("vault.kv-put", pm{"node": "node-1", "path": "x", "data": pm{"k": "v"}})

	r.script = func(args []string, _ map[string]string) runtime.ExecResult {
		if args[0] == "secrets" && args[1] == "enable" {
			return runtime.ExecResult{ExitCode: 2, Stderr: "permission denied"}
		}
		return runtime.ExecResult{Stdout: `{}`}
	}
	r.errors("vault.kv-put", pm{"node": "node-1", "path": "x", "data": pm{"k": "v"}}, "permission denied")
}

func TestKVPutFailureIsAFail(t *testing.T) {
	r := newPrimRig(t)
	r.script = func(args []string, _ map[string]string) runtime.ExecResult {
		if args[0] == "kv" {
			return runtime.ExecResult{ExitCode: 2, Stderr: "Error writing data: 503 Vault is sealed"}
		}
		return runtime.ExecResult{Stdout: `{"secret/":{}}`}
	}
	r.fail("vault.kv-put", pm{"node": "node-1", "path": "x", "data": pm{"k": "v"}}, "writing secret/x failed: Error writing data: 503 Vault is sealed")
}

func TestKVGet(t *testing.T) {
	r := newPrimRig(t)
	secret := js(pm{"data": pm{"data": pm{"user": "ada", "pass": "hunter2", "port": 5432}}})
	r.script = statusReturning(secret, 0)

	res := r.pass("vault.kv-get", pm{"node": "node-1", "path": "app/db", "field": "user", "equals": "ada", "save": "u"})
	if res.Outputs["u"] != "ada" {
		t.Errorf("outputs = %v", res.Outputs)
	}
	if got := strings.Join(r.last().args, " "); got != "kv get -mount=secret -format=json app/db" {
		t.Errorf("command = %q", got)
	}
	r.pass("vault.kv-get", pm{"node": "node-1", "path": "app/db", "field": "port", "equals": "5432"})

	whole := r.pass("vault.kv-get", pm{"node": "node-1", "path": "app/db", "save": "all"})
	if !strings.Contains(whole.Outputs["all"], `"user": "ada"`) && !strings.Contains(whole.Outputs["all"], `"user":"ada"`) {
		t.Errorf("whole secret output = %q", whole.Outputs["all"])
	}

	// A wrong value is a fail, and the values are not repeated in the message.
	res = r.fail("vault.kv-get", pm{"node": "node-1", "path": "app/db", "field": "pass", "equals": "wrong"}, "does not have the expected value")
	if strings.Contains(res.Message, "hunter2") || strings.Contains(res.Message, "wrong") {
		t.Errorf("the message repeats a value: %q", res.Message)
	}
	r.fail("vault.kv-get", pm{"node": "node-1", "path": "app/db", "field": "nope"}, `has no field "nope"`)
	r.errors("vault.kv-get", pm{"node": "node-1", "path": "app/db", "equals": "x"}, "needs a field")

	r.script = func([]string, map[string]string) runtime.ExecResult {
		return runtime.ExecResult{ExitCode: 2, Stderr: "No value found at secret/data/app/db"}
	}
	r.fail("vault.kv-get", pm{"node": "node-1", "path": "app/db"}, "cannot be read: No value found")
}

func TestUnseal(t *testing.T) {
	r := newPrimRig(t)
	sealed, progress := true, 0
	r.script = func(args []string, env map[string]string) runtime.ExecResult {
		switch args[0] {
		case "status":
			if sealed {
				return runtime.ExecResult{ExitCode: 2, Stdout: sealedStatus}
			}
			return runtime.ExecResult{Stdout: standbyStatus}
		case "operator":
			if progress++; progress >= 3 {
				sealed = false
			}
		}
		return runtime.ExecResult{}
	}

	res := r.pass("vault.unseal", pm{"node": "node-2"})
	if r.ran("operator", "unseal") != 3 || !strings.Contains(res.Message, "3 key(s)") {
		t.Errorf("unseal ran %d times (%q), want the threshold, 3", r.ran("operator", "unseal"), res.Message)
	}
	for _, c := range r.calls {
		if len(c.args) > 1 && c.args[1] == "unseal" && c.env["VAULT_TOKEN"] != "" {
			t.Errorf("unseal carried a token it does not need: %v", c.env)
		}
	}
	if strings.Contains(res.Message, pKeyA) || strings.Contains(res.Evidence, pKeyA) {
		t.Error("a key is in the result")
	}

	// Already unsealed: nothing more to do.
	before := r.ran("operator", "unseal")
	res = r.pass("vault.unseal", pm{"node": "node-2"})
	if r.ran("operator", "unseal") != before || !strings.Contains(res.Message, "already unsealed") {
		t.Error("an unsealed node was unsealed again")
	}
}

func TestUnsealFailures(t *testing.T) {
	t.Run("not enough keys on record", func(t *testing.T) {
		r := newPrimRig(t)
		r.script = statusReturning(sealedStatus, 2)
		r.errors("vault.unseal", pm{"node": "node-1", "keys": 9}, "4 unseal key(s) on record, and 9 were asked for")
	})
	t.Run("still sealed afterwards", func(t *testing.T) {
		r := newPrimRig(t)
		r.script = func(args []string, _ map[string]string) runtime.ExecResult {
			if args[0] == "status" {
				return runtime.ExecResult{ExitCode: 2, Stdout: sealedStatus}
			}
			return runtime.ExecResult{}
		}
		r.fail("vault.unseal", pm{"node": "node-1", "keys": 2}, "still sealed after 2 key(s)")
	})
	t.Run("a key is rejected", func(t *testing.T) {
		r := newPrimRig(t)
		r.script = func(args []string, _ map[string]string) runtime.ExecResult {
			if args[0] == "status" {
				return runtime.ExecResult{ExitCode: 2, Stdout: sealedStatus}
			}
			return runtime.ExecResult{ExitCode: 2, Stderr: "invalid key " + pKeyA}
		}
		res, err := r.run("vault.unseal", pm{"node": "node-1"})
		if err != nil || res.Status != primitive.Fail || strings.Contains(res.Message, pKeyA) {
			t.Errorf("result = %+v, %v, want a fail that does not repeat the key", res, err)
		}
	})
	t.Run("a node that does not answer", func(t *testing.T) {
		r := newPrimRig(t)
		r.script = func([]string, map[string]string) runtime.ExecResult {
			return runtime.ExecResult{ExitCode: 1, Stderr: "connection refused"}
		}
		r.errors("vault.unseal", pm{"node": "node-1"}, "does not answer on node-1")
	})
}

func TestInit(t *testing.T) {
	r := newPrimRig(t)
	r.script = statusReturning(js(pm{"unseal_keys_b64": []string{"k1", "k2", "k3", "k4", "k5"}, "root_token": "hvs.NewRoot"}), 0)

	res := r.pass("vault.init", pm{"node": "node-1"})
	if res.Outputs["rootToken"] != "hvs.NewRoot" || res.Outputs["unsealKey1"] != "k1" || res.Outputs["unsealKey5"] != "k5" {
		t.Errorf("outputs = %v", res.Outputs)
	}
	if res.Evidence != "" || strings.Contains(res.Message, "hvs.NewRoot") || strings.Contains(res.Message, "k1") {
		t.Errorf("the result shows the new credentials: %+v (R-QUAL-3)", res)
	}
	c := r.last()
	if strings.Join(c.args, " ") != "operator init -key-shares=5 -key-threshold=3 -format=json" || c.env["VAULT_TOKEN"] != "" {
		t.Errorf("command = %v env = %v, want init with five shares and a threshold of three, and no token", c.args, c.env)
	}

	r.pass("vault.init", pm{"node": "node-1", "shares": 1, "threshold": 1})
	if got := strings.Join(r.last().args, " "); got != "operator init -key-shares=1 -key-threshold=1 -format=json" {
		t.Errorf("command = %q", got)
	}

	r.errors("vault.init", pm{"node": "node-1", "shares": 3, "threshold": 5}, "threshold 5 must be from 1")

	r.script = func([]string, map[string]string) runtime.ExecResult {
		return runtime.ExecResult{ExitCode: 2, Stderr: "Vault is already initialized"}
	}
	r.fail("vault.init", pm{"node": "node-1"}, "already initialized")
}

func TestPrimitivesNeedTheirNodeToBeRunning(t *testing.T) {
	r := newPrimRig(t)
	if err := r.env.Runtime.Stop(context.Background(), r.env.Nodes["node-1"].ContainerID); err != nil {
		t.Fatal(err)
	}
	// A container that is stopped means the primitive could not run: an error, not a fail.
	for name, args := range map[string]pm{
		"vault.status": {"node": "node-1"}, "vault.cli": {"node": "node-1", "command": []any{"status"}},
		"vault.raft-peers": {"node": "node-1"}, "vault.expect-no-leader": {"node": "node-1"},
	} {
		if _, err := r.run(name, args); err == nil || !strings.Contains(err.Error(), "not running") {
			t.Errorf("%s on a stopped node = %v, want an error saying the container is not running", name, err)
		}
	}
}

func TestOnlyVaultPrimitivesCarryTheVaultPrefix(t *testing.T) {
	for _, p := range New(Options{}).Primitives() {
		if !strings.HasPrefix(p.Spec().Name, "vault.") {
			t.Errorf("%s does not start with vault.", p.Spec().Name)
		}
	}
}
