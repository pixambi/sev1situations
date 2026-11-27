package boundary

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/pixambi/sev1situations/internal/runtime"
	rtfake "github.com/pixambi/sev1situations/internal/runtime/fake"
)

const (
	simPassword = "SimAdminPassword12345"
	simToken    = "at_SimulatedToken"
)

// call is one command run in a container, in the order it ran.
type call struct {
	node string
	argv []string
	env  map[string]string
}

// sim plays Boundary, PostgreSQL, HAProxy and an SSH server behind the fake runtime, just enough to deploy.
type sim struct {
	mu    sync.Mutex
	rt    *rtfake.Runtime
	names map[string]string // container ID -> node name

	calls []call
	// events is the order things happened in, for tests of sequence.
	events []string

	started map[string]bool // node name -> its server was started
	dbReady int             // pg_isready calls that still fail

	// failure switches
	initFails        bool
	controllerNever  map[string]bool // controllers that never report healthy
	workerNeverShows bool
}

func newSim(rt *rtfake.Runtime) *sim {
	s := &sim{rt: rt, names: map[string]string{}, started: map[string]bool{}, controllerNever: map[string]bool{}}
	rt.SetExec(s.exec)
	return s
}

func nodeOf(c runtime.Container) string {
	// Container names are s1s-<run>-<node>, so the node is what follows the run ID.
	return strings.TrimPrefix(c.Name, "s1s-"+c.Run.ID+"-")
}

func (s *sim) note(format string, a ...any) { s.events = append(s.events, fmt.Sprintf(format, a...)) }

func (s *sim) exec(c runtime.Container, cmd []string, o runtime.ExecOptions) (runtime.ExecResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	node := nodeOf(c)
	s.calls = append(s.calls, call{node: node, argv: append([]string(nil), cmd...), env: o.Env})
	joined := strings.Join(cmd, " ")

	switch cmd[0] {
	case "mkdir":
		return runtime.ExecResult{}, nil
	case "pg_isready":
		if s.dbReady > 0 {
			s.dbReady--
			return runtime.ExecResult{ExitCode: 2, Stdout: "127.0.0.1:5432 - no response"}, nil
		}
		s.note("db ready")
		return runtime.ExecResult{Stdout: "accepting connections"}, nil
	case "wget":
		if strings.Contains(joined, "/health") {
			if s.started[node] && !s.controllerNever[node] {
				s.note("healthy %s", node)
				return runtime.ExecResult{}, nil
			}
			return runtime.ExecResult{ExitCode: 1, Stderr: "wget: can't connect to remote host (127.0.0.1): Connection refused"}, nil
		}
	case "sh":
		script := cmd[len(cmd)-1]
		switch {
		case strings.Contains(script, "boundary server"):
			s.started[node] = true
			s.note("start %s", node)
		case strings.Contains(script, "haproxy"):
			s.started[node] = true
			s.note("start %s", node)
		case strings.Contains(script, "apk add"):
			s.note("install ssh on %s", node)
		case strings.Contains(script, "nc -w"):
			return runtime.ExecResult{Stdout: "SSH-2.0-OpenSSH_sim\r\n"}, nil
		}
		return runtime.ExecResult{}, nil
	case binaryPath:
		return s.boundary(node, cmd[1:], o), nil
	}
	return runtime.ExecResult{ExitCode: 127, Stderr: "unknown command " + cmd[0]}, nil
}

func (s *sim) boundary(node string, args []string, o runtime.ExecOptions) runtime.ExecResult {
	switch strings.Join(args[:min(2, len(args))], " ") {
	case "database init":
		if s.initFails {
			return runtime.ExecResult{ExitCode: 1, Stderr: "could not connect to the database", Stdout: "SECRET-INIT-OUTPUT " + simPassword}
		}
		s.note("init %s", node)
		b, _ := json.Marshal(map[string]any{
			"auth_method":      map[string]any{"auth_method_id": "ampw_sim", "login_name": "admin", "password": simPassword, "user_id": "u_sim"},
			"org_scope":        map[string]any{"scope_id": "o_sim"},
			"project_scope":    map[string]any{"scope_id": "p_sim"},
			"host_resources":   map[string]any{"host_catalog_id": "hcst_sim", "host_set_id": "hsst_sim", "host_id": "hst_sim"},
			"target":           map[string]any{"target_id": "ttcp_direct"},
			"target_secondary": map[string]any{"target_id": "ttcp_sim"},
		})
		return runtime.ExecResult{Stdout: string(b)}

	case "authenticate password":
		// A sign-in through the load balancer needs the balancer and a controller to be up.
		if !strings.Contains(o.Env["BOUNDARY_ADDR"], "127.0.0.1") && !s.anyStartedLB() {
			return runtime.ExecResult{ExitCode: 1, Stderr: "connection refused"}
		}
		if o.Env["BOUNDARY_PASSWORD"] != simPassword {
			return runtime.ExecResult{ExitCode: 1, Stderr: "authentication failed"}
		}
		return runtime.ExecResult{Stdout: `{"item":{"attributes":{"token":"` + simToken + `"}}}`}

	case "workers list":
		if o.Env["BOUNDARY_TOKEN"] != simToken {
			return runtime.ExecResult{ExitCode: 1, Stderr: "unauthorized"}
		}
		if s.workerNeverShows || !s.startedRole("worker") {
			return runtime.ExecResult{Stdout: `{"items":[]}`}
		}
		s.note("worker registered")
		var items []string
		for n, ok := range s.started {
			if ok && (strings.HasPrefix(n, "worker-node-") || strings.Contains(n, "-worker-node-")) {
				items = append(items, `{"name":"`+n+`"}`)
			}
		}
		return runtime.ExecResult{Stdout: `{"items":[` + strings.Join(items, ",") + `]}`}

	case "hosts update":
		s.note("hosts update")
		return runtime.ExecResult{}
	}
	return runtime.ExecResult{ExitCode: 1, Stderr: "unsupported boundary command: " + strings.Join(args, " ")}
}

func (s *sim) anyStartedLB() bool { return s.startedRole("lb") }
func (s *sim) startedRole(r string) bool {
	for n, ok := range s.started {
		if ok && (strings.HasPrefix(n, r+"-node-") || strings.Contains(n, "-"+r+"-node-")) {
			return true
		}
	}
	return false
}

func (s *sim) eventLog() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...)
}

func (s *sim) callsOn(node string, prefix ...string) []call {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []call
	for _, c := range s.calls {
		if node != "" && c.node != node {
			continue
		}
		if len(c.argv) < len(prefix) {
			continue
		}
		match := true
		for i, p := range prefix {
			match = match && c.argv[i] == p
		}
		if match {
			out = append(out, c)
		}
	}
	return out
}
