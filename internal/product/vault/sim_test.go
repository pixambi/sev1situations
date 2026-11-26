package vault

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/pixambi/sev1situations/internal/runtime"
	rtfake "github.com/pixambi/sev1situations/internal/runtime/fake"
)

const (
	simToken = "hvs.SimulatedRootToken"
	simLog   = "2026-11-26T09:00:00Z [INFO] core: security barrier not initialized\n2026-11-26T09:00:01Z [ERROR] storage: boom"
)

var simKeys = []string{"KEY-ONE-aaaa", "KEY-TWO-bbbb", "KEY-THREE-cccc", "KEY-FOUR-dddd", "KEY-FIVE-eeee"}

// call records one vault command.
type call struct {
	node string
	argv []string
	env  map[string]string
}

type simNode struct {
	name        string
	started     bool
	initialized bool
	sealed      bool
	progress    int
}

// sim plays Vault behind the fake runtime, just enough to deploy a cluster.
type sim struct {
	mu    sync.Mutex
	rt    *rtfake.Runtime
	nodes map[string]*simNode // by container ID

	initialized  bool
	leader       string   // container ID
	voters       []string // container IDs, the first is the leader
	auditEnabled int
	calls        []call

	// promotion models autopilot: a node that has just joined is a non-voter for
	// this many list-peers calls before it is promoted.
	promoteAfter int
	pending      map[string]int // container ID -> list-peers calls left as a non-voter

	// failure switches
	neverStart   map[string]bool // by node name
	joinFailures int
	initFails    bool
}

func newSim(rt *rtfake.Runtime) *sim {
	s := &sim{rt: rt, nodes: map[string]*simNode{}, neverStart: map[string]bool{}}
	rt.SetExec(s.exec)
	return s
}

func nameOf(c runtime.Container) string {
	return c.Name[strings.LastIndex(c.Name, "-node-")+1:]
}

func (s *sim) node(c runtime.Container) *simNode {
	n, ok := s.nodes[c.ID]
	if !ok {
		n = &simNode{name: nameOf(c), sealed: true}
		s.nodes[c.ID] = n
	}
	return n
}

func ok() runtime.ExecResult { return runtime.ExecResult{} }

func simFail(code int, stderr string) runtime.ExecResult {
	return runtime.ExecResult{ExitCode: code, Stderr: stderr}
}

func (s *sim) exec(c runtime.Container, cmd []string, o runtime.ExecOptions) (runtime.ExecResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.node(c)

	switch cmd[0] {
	case "mkdir":
		return ok(), nil
	case "tail":
		return runtime.ExecResult{Stdout: simLog}, nil
	case "sh":
		if o.Detach && strings.Contains(strings.Join(cmd, " "), "vault server") {
			_, hasConfig := s.rt.File(c.ID, configPath)
			_, hasBinary := s.rt.File(c.ID, binaryPath)
			if hasConfig && hasBinary && !s.neverStart[n.name] {
				n.started = true
			}
		}
		return ok(), nil
	case binaryPath:
		return s.vault(c, n, cmd, o), nil
	}
	return simFail(127, "unknown command "+cmd[0]), nil
}

func (s *sim) vault(c runtime.Container, n *simNode, cmd []string, o runtime.ExecOptions) runtime.ExecResult {
	s.calls = append(s.calls, call{node: n.name, argv: append([]string(nil), cmd...), env: o.Env})
	args := cmd[1:]
	if !n.started {
		return simFail(1, "Error checking seal status: connection refused")
	}
	hasToken := o.Env["VAULT_TOKEN"] == simToken

	switch args[0] {
	case "status":
		// The fields a real Vault 2.2.2 prints for a Raft node.
		st := map[string]any{"initialized": n.initialized, "sealed": n.sealed, "version": "2.2.2", "ha_enabled": true}
		if !n.sealed && s.leader != "" {
			st["leader_address"] = "http://10.89.1.2:8200"
			if s.leader == c.ID {
				st["is_self"] = true
			}
		}
		b, _ := json.Marshal(st)
		code := 0
		if n.sealed {
			code = 2
		}
		return runtime.ExecResult{ExitCode: code, Stdout: string(b)}

	case "audit":
		if !hasToken {
			return simFail(2, "permission denied")
		}
		s.auditEnabled++
		return ok()

	case "operator":
		switch args[1] {
		case "init":
			if s.initFails {
				return runtime.ExecResult{ExitCode: 2, Stderr: "init exploded", Stdout: "SECRET-OUT " + simToken}
			}
			if s.initialized {
				return simFail(2, "Vault is already initialized")
			}
			s.initialized, n.initialized = true, true
			s.voters = []string{c.ID}
			b, _ := json.Marshal(map[string]any{"unseal_keys_b64": simKeys, "root_token": simToken})
			return runtime.ExecResult{Stdout: string(b)}

		case "unseal":
			valid := false
			for _, k := range simKeys {
				valid = valid || k == args[2]
			}
			if !valid {
				return simFail(2, "invalid key "+args[2])
			}
			if n.progress++; n.progress >= keyThreshold {
				n.sealed, n.progress = false, 0
				if s.leader == "" {
					s.leader = c.ID
				}
			}
			return ok()

		case "raft":
			switch args[2] {
			case "join":
				if s.joinFailures > 0 {
					s.joinFailures--
					return simFail(2, "leader not ready")
				}
				if s.leader == "" || n.initialized {
					return simFail(2, "leader not ready")
				}
				n.initialized = true
				s.dropVoterNamed(n.name) // a node that rejoins replaces its old membership
				s.voters = append(s.voters, c.ID)
				if s.promoteAfter > 0 {
					if s.pending == nil {
						s.pending = map[string]int{}
					}
					s.pending[c.ID] = s.promoteAfter
				}
				return ok()
			case "remove-peer":
				if !hasToken {
					return simFail(2, "permission denied")
				}
				if !s.dropVoterNamed(args[3]) {
					return simFail(2, "peer not found")
				}
				return ok()
			case "list-peers":
				if !hasToken {
					return simFail(2, "permission denied")
				}
				var servers []map[string]any
				for _, id := range s.voters {
					voter := true
					if s.pending[id] > 0 {
						s.pending[id]--
						voter = false
					}
					servers = append(servers, map[string]any{"node_id": s.nodes[id].name, "leader": id == s.leader, "voter": voter})
				}
				b, _ := json.Marshal(map[string]any{"data": map[string]any{"config": map[string]any{"servers": servers}}})
				return runtime.ExecResult{Stdout: string(b)}
			}
		}
	}
	return simFail(2, "unsupported command: "+strings.Join(args, " "))
}

// dropVoterNamed removes the voter with the given node name, and says whether there was one.
func (s *sim) dropVoterNamed(name string) bool {
	kept := s.voters[:0:0]
	found := false
	for _, id := range s.voters {
		if s.nodes[id].name == name {
			found = true
			continue
		}
		kept = append(kept, id)
	}
	s.voters = kept
	return found
}

// helpers for assertions ------------------------------------------------------

func (s *sim) count(match func(call) bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.calls {
		if match(c) {
			n++
		}
	}
	return n
}

func isCmd(node string, args ...string) func(call) bool {
	return func(c call) bool {
		if node != "" && c.node != node {
			return false
		}
		if len(c.argv) < len(args)+1 {
			return false
		}
		for i, a := range args {
			if c.argv[i+1] != a {
				return false
			}
		}
		return true
	}
}

func (s *sim) state(name string) simNode {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.nodes {
		if n.name == name {
			return *n
		}
	}
	return simNode{}
}

func (s *sim) voterCount() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.voters) }

func describe(c call) string { return fmt.Sprintf("%s: %s", c.node, strings.Join(c.argv[1:], " ")) }
