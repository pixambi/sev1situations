package boundary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/runtime"
)

// Primitives implements product.Product: the verbs a scenario uses on Boundary (section 4.5). They sign in themselves,
// as the admin or as a named user, and pass the token to Boundary in the environment and never on a command line, so a
// scenario carries no credential for the admin.
func (*Product) Primitives() []primitive.Primitive {
	return []primitive.Primitive{cliPrim{}, authorizeSession{}, targetInfo{}, sessionInfo{}, recordingsList{}}
}

const primitiveTimeout = 45 * time.Second

// defaultTarget is the name of the target the database init generates that reaches a host through the host set, which is
// the one the deployment points at the target host.
const defaultTarget = "Generated target using host sources"

var (
	nodeArg     = primitive.Arg{Name: "node", Type: primitive.String, Required: true, Help: "The controller to run the command on, for example node-1."}
	asArg       = primitive.Arg{Name: "as", Type: primitive.String, Default: "admin", Help: "The login name to sign in as. The admin needs no password here."}
	passwordArg = primitive.Arg{Name: "password", Type: primitive.String, Help: "The password of the user in as. It is only needed for a user other than the admin."}
	targetArg   = primitive.Arg{Name: "target", Type: primitive.String, Help: "The target's ID or name. It defaults to the generated target that reaches the target host."}
	saveArg     = primitive.Arg{Name: "save", Type: primitive.String, Help: "If set, the value is kept under this name for later steps as .Outputs.<name>."}
)

func pass(format string, a ...any) primitive.Result {
	return primitive.Result{Status: primitive.Pass, Message: fmt.Sprintf(format, a...)}
}

func fail(evidence, format string, a ...any) primitive.Result {
	return primitive.Result{Status: primitive.Fail, Message: fmt.Sprintf(format, a...), Evidence: evidence}
}

// session is a sign-in held for the length of one primitive.
type session struct {
	env   primitive.Env
	node  primitive.Node
	token string
}

// run runs a Boundary command on the node as the signed-in user.
func (s session) run(ctx context.Context, args ...string) (runtime.ExecResult, error) {
	return s.runWith(ctx, nil, args...)
}

// runWith is run with extra environment variables for the command. Boundary takes a password only from the
// environment or a file, never on the command line, so a command that sets one names a variable and gives it here.
func (s session) runWith(ctx context.Context, extra map[string]string, args ...string) (runtime.ExecResult, error) {
	vars := map[string]string{}
	for k, v := range extra {
		vars[k] = v
	}
	vars["BOUNDARY_ADDR"] = localAddr()
	if s.token != "" {
		vars["BOUNDARY_TOKEN"] = s.token
		args = append(args, "-token=env://BOUNDARY_TOKEN")
	}
	res, err := s.env.Runtime.Exec(ctx, s.node.ContainerID, append([]string{binaryPath}, args...), runtime.ExecOptions{Env: vars})
	if err != nil {
		return res, fmt.Errorf("run boundary on %s: %w", s.node.Name, err)
	}
	return res, nil
}

// signIn signs in as a user. The admin's password is the deployment's own. For anyone else the scenario gives it, which
// is a password the scenario itself made up, not a credential of the deployment.
func signIn(ctx context.Context, env primitive.Env, node primitive.Node, as, password string) (session, error) {
	s := session{env: env, node: node}
	if as == "" {
		as = "admin"
	}
	if password == "" {
		if as != "admin" {
			return s, fmt.Errorf("signing in as %s needs a password", as)
		}
		password, _ = env.Credential(node.Cluster, "adminPassword")
		if password == "" {
			return s, errors.New("the deployment has no admin password")
		}
	}

	// The auth method is found by asking, because anyone may list the auth methods of the global scope.
	res, err := s.run(ctx, "auth-methods", "list", "-scope-id=global", "-format=json")
	if err != nil {
		return s, err
	}
	var methods struct {
		Items []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"items"`
	}
	if res.ExitCode != 0 || json.Unmarshal([]byte(res.Stdout), &methods) != nil {
		return s, fmt.Errorf("list the auth methods on %s: %s", node.Name, firstLine(res.Stderr))
	}
	var method string
	for _, m := range methods.Items {
		if m.Type == "password" {
			method = m.ID
			break
		}
	}
	if method == "" {
		return s, errors.New("there is no password auth method to sign in with")
	}

	res, err = env.Runtime.Exec(ctx, node.ContainerID, []string{binaryPath, "authenticate", "password",
		"-auth-method-id=" + method, "-login-name=" + as, "-password=env://BOUNDARY_PASSWORD", "-keyring-type=none", "-format=json"},
		runtime.ExecOptions{Env: map[string]string{"BOUNDARY_ADDR": localAddr(), "BOUNDARY_PASSWORD": password}})
	if err != nil {
		return s, fmt.Errorf("sign in on %s: %w", node.Name, err)
	}
	if res.ExitCode != 0 {
		return s, fmt.Errorf("sign in as %s failed: %s", as, firstLine(res.Stderr))
	}
	var out struct {
		Item struct {
			Attributes struct {
				Token string `json:"token"`
			} `json:"attributes"`
		} `json:"item"`
	}
	if json.Unmarshal([]byte(res.Stdout), &out) != nil || out.Item.Attributes.Token == "" {
		return s, errors.New("the sign-in did not return a token") // the output holds the token, so it is not quoted
	}
	s.token = out.Item.Attributes.Token
	return s, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// signedIn finds the node and signs in as the user the primitive's arguments name.
func signedIn(ctx context.Context, env primitive.Env, a primitive.Args) (session, error) {
	n, err := env.Lookup(a.String("node"))
	if err != nil {
		return session{}, err
	}
	return signIn(ctx, env, n, a.String("as"), a.String("password"))
}

// resolveTarget turns a target's name or ID into its ID. An ID is used as it is. A name is looked for among the targets
// the user can see.
func resolveTarget(ctx context.Context, s session, ref string) (string, error) {
	if ref == "" {
		ref = defaultTarget
	}
	if isID(ref) {
		return ref, nil
	}
	res, err := s.run(ctx, "targets", "list", "-recursive", "-format=json")
	if err != nil {
		return "", err
	}
	var list struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if res.ExitCode != 0 || json.Unmarshal([]byte(res.Stdout), &list) != nil {
		return "", fmt.Errorf("list the targets: %s", firstLine(res.Stderr))
	}
	for _, t := range list.Items {
		if t.Name == ref {
			return t.ID, nil
		}
	}
	return "", fmt.Errorf("there is no target called %q that this user can see", ref)
}

// isID reports whether a reference is a target ID, such as ttcp_1234567890.
func isID(ref string) bool {
	return strings.HasPrefix(ref, "ttcp_") || strings.HasPrefix(ref, "tssh_") || strings.HasPrefix(ref, "trdp_")
}

// boundary.cli ----------------------------------------------------------------

type cliPrim struct{}

func (cliPrim) Spec() primitive.Spec {
	return primitive.Spec{
		Name:    "boundary.cli",
		Summary: "Runs a Boundary command on a controller, signed in as the admin or a named user, and checks the exit code and output. Safe to run again only if the command is.",
		Timeout: primitiveTimeout,
		Changes: true,
		Args: []primitive.Arg{
			nodeArg,
			{Name: "command", Type: primitive.StringList, Required: true, Help: "The Boundary arguments without the word boundary, for example [scopes, list]."},
			asArg, passwordArg,
			{Name: "env", Type: primitive.Map, Help: "Extra environment variables for the command. Boundary reads a password only from one, for example env: {NEW_PASSWORD: secret} with -password=env://NEW_PASSWORD."},
			{Name: "expectExit", Type: primitive.Int, Default: 0, Help: "The exit code that counts as a pass."},
			{Name: "contains", Type: primitive.String, Help: "If set, the combined output must contain this text."},
			saveArg,
		},
	}
}

func (cliPrim) Run(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error) {
	s, err := signedIn(ctx, env, a)
	if err != nil {
		return primitive.Result{}, err
	}
	extra := map[string]string{}
	for k, v := range a.Map("env") {
		extra[k] = fmt.Sprint(v)
	}
	res, err := s.runWith(ctx, extra, a.List("command")...)
	if err != nil {
		return primitive.Result{}, err
	}
	combined := res.Stdout + res.Stderr
	out := primitive.Result{Evidence: combined}
	if name := a.String("save"); name != "" {
		out.Outputs = map[string]string{name: strings.TrimSpace(res.Stdout)}
	}
	switch want := a.Int("expectExit"); {
	case res.ExitCode != want:
		out.Status, out.Message = primitive.Fail, fmt.Sprintf("boundary exited with %d on %s, expected %d", res.ExitCode, s.node.Name, want)
	case a.String("contains") != "" && !strings.Contains(combined, a.String("contains")):
		out.Status, out.Message = primitive.Fail, fmt.Sprintf("the output does not contain %q", a.String("contains"))
	default:
		out.Status, out.Message = primitive.Pass, fmt.Sprintf("boundary exited with %d on %s", res.ExitCode, s.node.Name)
	}
	return out, nil
}

// boundary.authorize-session ---------------------------------------------------

type authorizeSession struct{}

func (authorizeSession) Spec() primitive.Spec {
	return primitive.Spec{
		Name:    "boundary.authorize-session",
		Summary: "Asks Boundary to authorize a session on a target as a user, and checks that it is allowed or that it is denied. It asks for a session and does not connect, so it is safe to run again.",
		Timeout: primitiveTimeout,
		Changes: true, // it records a pending session
		Args: []primitive.Arg{
			nodeArg, asArg, passwordArg, targetArg,
			{Name: "expect", Type: primitive.String, Default: "allowed", Help: "allowed, or denied for a user who should get a 403."},
		},
	}
}

func (authorizeSession) Run(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error) {
	want := a.String("expect")
	if want != "allowed" && want != "denied" {
		return primitive.Result{}, fmt.Errorf("expect must be allowed or denied, not %q", want)
	}
	s, err := signedIn(ctx, env, a)
	if err != nil {
		return primitive.Result{}, err
	}
	id, err := resolveTarget(ctx, s, a.String("target"))
	if err != nil {
		return primitive.Result{}, err
	}
	res, err := s.run(ctx, "targets", "authorize-session", "-id="+id, "-format=json")
	if err != nil {
		return primitive.Result{}, err
	}
	who := a.String("as")
	if who == "" {
		who = "admin"
	}
	combined := res.Stdout + res.Stderr
	switch {
	case res.ExitCode == 0 && want == "allowed":
		return pass("%s is allowed to start a session on %s", who, id), nil
	case res.ExitCode == 0:
		return fail(combined, "%s was allowed to start a session on %s, which should have been denied", who, id), nil
	case isDenied(res):
		if want == "denied" {
			return pass("%s is denied a session on %s, as expected", who, id), nil
		}
		return fail(combined, "%s was denied a session on %s (403, permission denied)", who, id), nil
	}
	// Anything else is not an answer about permission: the controller could not be asked, or failed.
	return fail(combined, "authorizing a session for %s on %s failed for a reason that is not permission: %s", who, id, firstLine(res.Stderr)), nil
}

// isDenied reports whether a failed command was Boundary refusing on permission, which is a 403.
func isDenied(res runtime.ExecResult) bool {
	t := res.Stdout + res.Stderr
	return strings.Contains(t, "403") || strings.Contains(t, "PermissionDenied") || strings.Contains(strings.ToLower(t), "forbidden")
}

// boundary.target-info ---------------------------------------------------------

type targetInfo struct{}

func (targetInfo) Spec() primitive.Spec {
	return primitive.Spec{
		Name:    "boundary.target-info",
		Summary: "Reads a target and optionally checks one of its settings, such as session_max_seconds or session_connection_limit. It changes nothing and is safe to run again.",
		Timeout: primitiveTimeout,
		Args: []primitive.Arg{
			nodeArg, asArg, passwordArg, targetArg,
			{Name: "field", Type: primitive.String, Help: "If set, the field of the target to read, for example session_max_seconds. Without it the target is read as JSON."},
			{Name: "equals", Type: primitive.String, Help: "If set, the field's value must equal this. It needs a field."},
			saveArg,
		},
	}
}

func (targetInfo) Run(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error) {
	if a.Has("equals") && !a.Has("field") {
		return primitive.Result{}, errors.New("equals needs a field to compare")
	}
	s, err := signedIn(ctx, env, a)
	if err != nil {
		return primitive.Result{}, err
	}
	id, err := resolveTarget(ctx, s, a.String("target"))
	if err != nil {
		return primitive.Result{}, err
	}
	res, err := s.run(ctx, "targets", "read", "-id="+id, "-format=json")
	if err != nil {
		return primitive.Result{}, err
	}
	if res.ExitCode != 0 {
		return fail(res.Stderr+res.Stdout, "could not read target %s: %s", id, firstLine(res.Stderr)), nil
	}
	var read struct {
		Item map[string]any `json:"item"`
	}
	if json.Unmarshal([]byte(res.Stdout), &read) != nil || read.Item == nil {
		return primitive.Result{}, errors.New("the target could not be read: the answer was not what Boundary returns")
	}

	out := primitive.Result{Evidence: res.Stdout}
	field := a.String("field")
	if field == "" {
		if name := a.String("save"); name != "" {
			out.Outputs = map[string]string{name: strings.TrimSpace(res.Stdout)}
		}
		out.Status, out.Message = primitive.Pass, "read target "+id
		return out, nil
	}
	v, ok := read.Item[field]
	if !ok {
		return fail(res.Stdout, "target %s has no field %q", id, field), nil
	}
	got := fmt.Sprint(v)
	if f, isNum := v.(float64); isNum && f == float64(int64(f)) {
		got = fmt.Sprint(int64(f)) // a whole number is shown as one, not as 28800 in scientific form
	}
	if name := a.String("save"); name != "" {
		out.Outputs = map[string]string{name: got}
	}
	if a.Has("equals") && got != a.String("equals") {
		out.Status, out.Message = primitive.Fail, fmt.Sprintf("%s of target %s is %s, expected %s", field, id, got, a.String("equals"))
		return out, nil
	}
	out.Status, out.Message = primitive.Pass, fmt.Sprintf("%s of target %s is %s", field, id, got)
	return out, nil
}

// boundary.session-info --------------------------------------------------------

type sessionInfo struct{}

func (sessionInfo) Spec() primitive.Spec {
	return primitive.Spec{
		Name:    "boundary.session-info",
		Summary: "Looks at the sessions on a target and checks that at least a number of them are in a status and, if asked, ended for a reason. It changes nothing and is safe to run again.",
		Timeout: primitiveTimeout,
		Args: []primitive.Arg{
			nodeArg, asArg, passwordArg, targetArg,
			{Name: "status", Type: primitive.String, Help: "If set, the status the sessions should have: pending, active, canceling or terminated."},
			{Name: "reason", Type: primitive.String, Help: "If set, the termination reason the sessions should have, for example connection limit reached."},
			{Name: "atLeast", Type: primitive.Int, Default: 1, Help: "How many sessions must match."},
		},
	}
}

func (sessionInfo) Run(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error) {
	s, err := signedIn(ctx, env, a)
	if err != nil {
		return primitive.Result{}, err
	}
	id, err := resolveTarget(ctx, s, a.String("target"))
	if err != nil {
		return primitive.Result{}, err
	}
	res, err := s.run(ctx, "sessions", "list", "-recursive", "-format=json")
	if err != nil {
		return primitive.Result{}, err
	}
	if res.ExitCode != 0 {
		return fail(res.Stderr+res.Stdout, "could not list the sessions: %s", firstLine(res.Stderr)), nil
	}
	var list struct {
		Items []struct {
			TargetID  string `json:"target_id"`
			Status    string `json:"status"`
			Reason    string `json:"termination_reason"`
			SessionID string `json:"id"`
		} `json:"items"`
	}
	if json.Unmarshal([]byte(res.Stdout), &list) != nil {
		return primitive.Result{}, errors.New("the sessions could not be read: the answer was not what Boundary returns")
	}
	matched := 0
	for _, it := range list.Items {
		if it.TargetID != id {
			continue
		}
		if a.Has("status") && it.Status != a.String("status") {
			continue
		}
		if a.Has("reason") && !strings.EqualFold(it.Reason, a.String("reason")) {
			continue
		}
		matched++
	}
	if want := a.Int("atLeast"); matched < want {
		return fail(res.Stdout, "%d session(s) on %s match, expected at least %d%s", matched, id, want, describeWanted(a)), nil
	}
	return pass("%d session(s) on %s match%s", matched, id, describeWanted(a)), nil
}

func describeWanted(a primitive.Args) string {
	var parts []string
	if a.Has("status") {
		parts = append(parts, "status "+a.String("status"))
	}
	if a.Has("reason") {
		parts = append(parts, "reason "+a.String("reason"))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// boundary.recordings-list -----------------------------------------------------

type recordingsList struct{}

func (recordingsList) Spec() primitive.Spec {
	return primitive.Spec{
		Name:    "boundary.recordings-list",
		Summary: "Lists the session recordings and checks that there are at least a number of them, and that they are in a state. Session recording is an Enterprise feature. It changes nothing and is safe to run again.",
		Timeout: primitiveTimeout,
		Args: []primitive.Arg{
			nodeArg, asArg, passwordArg,
			{Name: "atLeast", Type: primitive.Int, Default: 1, Help: "How many recordings there must be."},
			{Name: "state", Type: primitive.String, Help: "If set, the state they should be in, for example available."},
		},
	}
}

func (recordingsList) Run(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error) {
	s, err := signedIn(ctx, env, a)
	if err != nil {
		return primitive.Result{}, err
	}
	res, err := s.run(ctx, "session-recordings", "list", "-recursive", "-format=json")
	if err != nil {
		return primitive.Result{}, err
	}
	if res.ExitCode != 0 {
		return fail(res.Stderr+res.Stdout, "could not list the session recordings: %s", firstLine(res.Stderr)), nil
	}
	var list struct {
		Items []struct {
			State string `json:"state"`
		} `json:"items"`
	}
	if json.Unmarshal([]byte(res.Stdout), &list) != nil {
		return primitive.Result{}, errors.New("the recordings could not be read: the answer was not what Boundary returns")
	}
	matched := 0
	for _, it := range list.Items {
		if !a.Has("state") || it.State == a.String("state") {
			matched++
		}
	}
	if want := a.Int("atLeast"); matched < want {
		return fail(res.Stdout, "%d recording(s) found, expected at least %d", matched, want), nil
	}
	return pass("%d recording(s) found", matched), nil
}
