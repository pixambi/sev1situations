package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/runtime"
)

// Primitives implements product.Product: the verbs a scenario uses on Vault
// (section 4.5). They find the cluster's root token themselves, so a scenario
// never carries a credential, and they pass it to Vault in the environment and
// never on a command line.
func (*Product) Primitives() []primitive.Primitive {
	return []primitive.Primitive{
		cli{}, statusCheck{}, raftPeers{}, expectNoLeader{}, kvPut{}, kvGet{}, unseal{}, initialise{},
	}
}

const primitiveTimeout = 30 * time.Second

var (
	nodeArg  = primitive.Arg{Name: "node", Type: primitive.String, Required: true, Help: "The node to run the command on, for example node-1."}
	tokenArg = primitive.Arg{Name: "token", Type: primitive.String, Help: "A token to use instead of the cluster's root token."}
	saveArg  = primitive.Arg{Name: "save", Type: primitive.String, Help: "If set, the value is kept under this name for later steps as .Outputs.<name>."}
	mountArg = primitive.Arg{Name: "mount", Type: primitive.String, Default: "secret", Help: "The KV version 2 mount."}
)

// vaultRun runs the vault command in a node as the cluster. The token is the one
// given, else the cluster's root token.
func vaultRun(ctx context.Context, env primitive.Env, n primitive.Node, token string, args ...string) (runtime.ExecResult, error) {
	if token == "" {
		token, _ = env.Credential(n.Cluster, "rootToken")
	}
	return vaultExec(ctx, env, n, token, args...)
}

// vaultExec runs the vault command in a node with exactly the token given, which
// may be none. The address is always local, and a token goes in the environment
// only, never on the command line.
func vaultExec(ctx context.Context, env primitive.Env, n primitive.Node, token string, args ...string) (runtime.ExecResult, error) {
	vars := map[string]string{"VAULT_ADDR": fmt.Sprintf("http://127.0.0.1:%d", apiPort)}
	if token != "" {
		vars["VAULT_TOKEN"] = token
	}
	res, err := env.Runtime.Exec(ctx, n.ContainerID, append([]string{binaryPath}, args...), runtime.ExecOptions{Env: vars})
	if err != nil {
		return res, fmt.Errorf("run vault on %s: %w", n.Name, err)
	}
	return res, nil
}

func pass(format string, a ...any) primitive.Result {
	return primitive.Result{Status: primitive.Pass, Message: fmt.Sprintf(format, a...)}
}

func fail(evidence, format string, a ...any) primitive.Result {
	return primitive.Result{Status: primitive.Fail, Message: fmt.Sprintf(format, a...), Evidence: evidence}
}

// unreachable reports whether the exit code means Vault could not be contacted.
func unreachable(res runtime.ExecResult) bool { return res.ExitCode == 1 }

// readStatus runs vault status on a node. A node Vault does not answer on returns
// ok false with the evidence.
func readStatus(ctx context.Context, env primitive.Env, n primitive.Node) (st status, res runtime.ExecResult, ok bool, err error) {
	res, err = vaultExec(ctx, env, n, "", "status", "-format=json")
	if err != nil || unreachable(res) {
		return st, res, false, err
	}
	st, err = parseStatus(res.Stdout)
	return st, res, err == nil, err
}

// vault.cli ------------------------------------------------------------------

type cli struct{}

func (cli) Spec() primitive.Spec {
	return primitive.Spec{
		Name:    "vault.cli",
		Summary: "Runs a Vault command in a node with the address and the cluster's root token set for it, and checks the exit code and output. Safe to run again only if the command is.",
		Timeout: primitiveTimeout,
		Changes: true,
		Args: []primitive.Arg{
			nodeArg,
			{Name: "command", Type: primitive.StringList, Required: true, Help: "The Vault arguments without the word vault, for example [read, sys/health]."},
			tokenArg,
			{Name: "expectExit", Type: primitive.Int, Default: 0, Help: "The exit code that counts as a pass."},
			{Name: "contains", Type: primitive.String, Help: "If set, the combined output must contain this text."},
			saveArg,
		},
	}
}

func (cli) Run(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error) {
	n, err := env.Lookup(a.String("node"))
	if err != nil {
		return primitive.Result{}, err
	}
	res, err := vaultRun(ctx, env, n, a.String("token"), a.List("command")...)
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
		out.Status, out.Message = primitive.Fail, fmt.Sprintf("vault exited with %d on %s, expected %d", res.ExitCode, n.Name, want)
	case a.String("contains") != "" && !strings.Contains(combined, a.String("contains")):
		out.Status, out.Message = primitive.Fail, fmt.Sprintf("the output does not contain %q", a.String("contains"))
	default:
		out.Status, out.Message = primitive.Pass, fmt.Sprintf("vault exited with %d on %s", res.ExitCode, n.Name)
	}
	return out, nil
}

// vault.status ---------------------------------------------------------------

type statusCheck struct{}

func (statusCheck) Spec() primitive.Spec {
	return primitive.Spec{
		Name:    "vault.status",
		Summary: "Checks a node's Vault status: whether it answers, and optionally whether it is initialised, sealed or the active node. It changes nothing and is safe to run again.",
		Timeout: primitiveTimeout,
		Args: []primitive.Arg{
			nodeArg,
			{Name: "reachable", Type: primitive.Bool, Default: true, Help: "Whether Vault should answer. Set false to check that it is down."},
			{Name: "initialized", Type: primitive.Bool, Help: "If set, whether Vault should be initialised."},
			{Name: "sealed", Type: primitive.Bool, Help: "If set, whether Vault should be sealed."},
			{Name: "active", Type: primitive.Bool, Help: "If set, whether the node should be the active node."},
		},
	}
}

func (statusCheck) Run(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error) {
	n, err := env.Lookup(a.String("node"))
	if err != nil {
		return primitive.Result{}, err
	}
	st, res, ok, err := readStatus(ctx, env, n)
	if err != nil {
		return primitive.Result{}, err
	}

	if !ok {
		if !a.Bool("reachable") {
			return pass("Vault does not answer on %s, as expected", n.Name), nil
		}
		return fail(res.Stderr, "Vault does not answer on %s", n.Name), nil
	}
	if !a.Bool("reachable") {
		return fail(res.Stdout, "Vault answers on %s but was expected to be down", n.Name), nil
	}

	var wrong []string
	check := func(arg string, got bool) {
		if a.Has(arg) && a.Bool(arg) != got {
			wrong = append(wrong, fmt.Sprintf("%s is %v, expected %v", arg, got, a.Bool(arg)))
		}
	}
	check("initialized", st.Initialized)
	check("sealed", st.Sealed)
	check("active", st.role() == roleActive)

	out := primitive.Result{
		Evidence: res.Stdout,
		Outputs: map[string]string{
			"sealed": strconv.FormatBool(st.Sealed), "initialized": strconv.FormatBool(st.Initialized),
			"role": st.role(), "version": st.Version,
		},
	}
	if len(wrong) > 0 {
		out.Status, out.Message = primitive.Fail, n.Name+": "+strings.Join(wrong, "; ")
		return out, nil
	}
	out.Status, out.Message = primitive.Pass, fmt.Sprintf("%s: initialised=%v sealed=%v role=%q", n.Name, st.Initialized, st.Sealed, st.role())
	return out, nil
}

// vault.raft-peers -----------------------------------------------------------

type raftPeers struct{}

func (raftPeers) Spec() primitive.Spec {
	return primitive.Spec{
		Name:    "vault.raft-peers",
		Summary: "Reads the Raft cluster's members from a node and checks how many servers and voters there are and which node leads. It changes nothing and is safe to run again.",
		Timeout: primitiveTimeout,
		Args: []primitive.Arg{
			nodeArg, tokenArg,
			{Name: "servers", Type: primitive.Int, Help: "If set, how many servers the cluster should list."},
			{Name: "voters", Type: primitive.Int, Help: "If set, how many of them should be voters."},
			{Name: "leader", Type: primitive.String, Help: "If set, the node that should be the leader, for example node-1."},
			saveArg,
		},
	}
}

func (raftPeers) Run(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error) {
	n, err := env.Lookup(a.String("node"))
	if err != nil {
		return primitive.Result{}, err
	}
	res, err := vaultRun(ctx, env, n, a.String("token"), "operator", "raft", "list-peers", "-format=json")
	if err != nil {
		return primitive.Result{}, err
	}
	if res.ExitCode != 0 {
		// With no leader the command fails. That is the condition not being met, not an error.
		return fail(res.Stdout+res.Stderr, "the peers of %s cannot be read: %s", n.Name, firstLine(res.Stderr+res.Stdout)), nil
	}
	p, err := parsePeers(res.Stdout)
	if err != nil {
		return primitive.Result{}, err
	}
	voters, leader := p.voters()
	servers := len(p.Data.Config.Servers)

	var wrong []string
	if a.Has("servers") && servers != a.Int("servers") {
		wrong = append(wrong, fmt.Sprintf("%d servers, expected %d", servers, a.Int("servers")))
	}
	if a.Has("voters") && voters != a.Int("voters") {
		wrong = append(wrong, fmt.Sprintf("%d voters, expected %d", voters, a.Int("voters")))
	}
	if a.Has("leader") && leader != a.String("leader") {
		wrong = append(wrong, fmt.Sprintf("leader is %q, expected %q", leader, a.String("leader")))
	}

	out := primitive.Result{Evidence: res.Stdout, Outputs: map[string]string{
		"servers": strconv.Itoa(servers), "voters": strconv.Itoa(voters), "leader": leader,
	}}
	if len(wrong) > 0 {
		out.Status, out.Message = primitive.Fail, strings.Join(wrong, "; ")
		return out, nil
	}
	out.Status, out.Message = primitive.Pass, fmt.Sprintf("%d server(s), %d voter(s), leader %q", servers, voters, leader)
	return out, nil
}

// vault.expect-no-leader -----------------------------------------------------

type expectNoLeader struct{}

func (expectNoLeader) Spec() primitive.Spec {
	return primitive.Spec{
		Name:    "vault.expect-no-leader",
		Summary: "Checks that a node is running and unsealed but knows of no leader, as after a cluster loses quorum. It changes nothing and is safe to run again.",
		Timeout: primitiveTimeout,
		Args:    []primitive.Arg{nodeArg},
	}
}

func (expectNoLeader) Run(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error) {
	n, err := env.Lookup(a.String("node"))
	if err != nil {
		return primitive.Result{}, err
	}
	st, res, ok, err := readStatus(ctx, env, n)
	if err != nil {
		return primitive.Result{}, err
	}
	switch {
	case !ok:
		return fail(res.Stderr, "Vault does not answer on %s, so there is no way to tell", n.Name), nil
	case st.Sealed:
		return fail(res.Stdout, "%s is sealed, so it cannot say whether there is a leader", n.Name), nil
	case st.IsSelf:
		return fail(res.Stdout, "%s is the active node", n.Name), nil
	case st.LeaderAddress != "":
		return fail(res.Stdout, "%s reports a leader at %s", n.Name, st.LeaderAddress), nil
	}
	return primitive.Result{Status: primitive.Pass, Message: n.Name + " is unsealed and knows of no leader", Evidence: res.Stdout}, nil
}

// KV --------------------------------------------------------------------------

// ensureKV makes sure a KV version 2 secrets engine is mounted. A freshly
// initialised cluster has none.
func ensureKV(ctx context.Context, env primitive.Env, n primitive.Node, token, mount string) error {
	res, err := vaultRun(ctx, env, n, token, "secrets", "list", "-format=json")
	if err != nil {
		return err
	}
	if res.ExitCode == 0 {
		var mounts map[string]json.RawMessage
		if json.Unmarshal([]byte(res.Stdout), &mounts) == nil {
			if _, ok := mounts[strings.TrimSuffix(mount, "/")+"/"]; ok {
				return nil
			}
		}
	}
	res, err = vaultRun(ctx, env, n, token, "secrets", "enable", "-path="+mount, "-version=2", "kv")
	if err != nil {
		return err
	}
	if res.ExitCode != 0 && !strings.Contains(res.Stderr+res.Stdout, "already in use") {
		return fmt.Errorf("enable the %s secrets engine on %s: %s", mount, n.Name, firstLine(res.Stderr+res.Stdout))
	}
	return nil
}

type kvPut struct{}

func (kvPut) Spec() primitive.Spec {
	return primitive.Spec{
		Name:    "vault.kv-put",
		Summary: "Writes a secret to a KV version 2 mount, enabling the mount first if it is missing. Writing again adds a new version, so it is safe to run again.",
		Timeout: primitiveTimeout,
		Changes: true,
		Args: []primitive.Arg{
			nodeArg, tokenArg, mountArg,
			{Name: "path", Type: primitive.String, Required: true, Help: "The secret's path inside the mount."},
			{Name: "data", Type: primitive.Map, Required: true, Help: "The key and value pairs to write."},
		},
	}
}

func (kvPut) Run(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error) {
	n, err := env.Lookup(a.String("node"))
	if err != nil {
		return primitive.Result{}, err
	}
	data := a.Map("data")
	if len(data) == 0 {
		return primitive.Result{}, errors.New("data needs at least one key")
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if err := ensureKV(ctx, env, n, a.String("token"), a.String("mount")); err != nil {
		return primitive.Result{}, err
	}
	args := []string{"kv", "put", "-mount=" + a.String("mount"), a.String("path")}
	for _, k := range keys {
		args = append(args, k+"="+fmt.Sprint(data[k]))
	}
	res, err := vaultRun(ctx, env, n, a.String("token"), args...)
	if err != nil {
		return primitive.Result{}, err
	}
	if res.ExitCode != 0 {
		return fail(res.Stdout+res.Stderr, "writing %s/%s failed: %s", a.String("mount"), a.String("path"), firstLine(res.Stderr+res.Stdout)), nil
	}
	return pass("wrote %s/%s", a.String("mount"), a.String("path")), nil
}

type kvGet struct{}

func (kvGet) Spec() primitive.Spec {
	return primitive.Spec{
		Name:    "vault.kv-get",
		Summary: "Reads a secret from a KV version 2 mount and optionally checks one field. It changes nothing and is safe to run again.",
		Timeout: primitiveTimeout,
		Args: []primitive.Arg{
			nodeArg, tokenArg, mountArg, saveArg,
			{Name: "path", Type: primitive.String, Required: true, Help: "The secret's path inside the mount."},
			{Name: "field", Type: primitive.String, Help: "If set, the field to read. Without it the whole secret is read as JSON."},
			{Name: "equals", Type: primitive.String, Help: "If set, the field's value must equal this. It needs a field."},
		},
	}
}

func (kvGet) Run(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error) {
	n, err := env.Lookup(a.String("node"))
	if err != nil {
		return primitive.Result{}, err
	}
	if a.Has("equals") && !a.Has("field") {
		return primitive.Result{}, errors.New("equals needs a field to compare")
	}
	res, err := vaultRun(ctx, env, n, a.String("token"), "kv", "get", "-mount="+a.String("mount"), "-format=json", a.String("path"))
	if err != nil {
		return primitive.Result{}, err
	}
	if res.ExitCode != 0 {
		return fail(res.Stderr+res.Stdout, "%s/%s cannot be read: %s", a.String("mount"), a.String("path"), firstLine(res.Stderr+res.Stdout)), nil
	}

	var doc struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &doc); err != nil {
		return primitive.Result{}, errors.New("the secret is not valid JSON")
	}

	value := res.Stdout
	if a.Has("field") {
		raw, ok := doc.Data.Data[a.String("field")]
		if !ok {
			return fail("", "%s/%s has no field %q", a.String("mount"), a.String("path"), a.String("field")), nil
		}
		value = fmt.Sprint(raw)
		if a.Has("equals") && value != a.String("equals") {
			// The values are not repeated: they may be what the scenario treats as secret.
			return fail("", "the field %q of %s/%s does not have the expected value", a.String("field"), a.String("mount"), a.String("path")), nil
		}
	}
	out := pass("read %s/%s", a.String("mount"), a.String("path"))
	if name := a.String("save"); name != "" {
		out.Outputs = map[string]string{name: strings.TrimSpace(value)}
	}
	return out, nil
}

// vault.unseal ---------------------------------------------------------------

type unseal struct{}

func (unseal) Spec() primitive.Spec {
	return primitive.Spec{
		Name:    "vault.unseal",
		Summary: "Unseals a node with the cluster's unseal keys. A node that is already unsealed passes, so it is safe to run again.",
		Timeout: 60 * time.Second,
		Changes: true,
		Args: []primitive.Arg{
			nodeArg,
			{Name: "keys", Type: primitive.Int, Help: "How many keys to supply. The default is the cluster's threshold."},
		},
	}
}

func (unseal) Run(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error) {
	n, err := env.Lookup(a.String("node"))
	if err != nil {
		return primitive.Result{}, err
	}
	st, res, ok, err := readStatus(ctx, env, n)
	if err != nil {
		return primitive.Result{}, err
	}
	if !ok {
		return primitive.Result{}, fmt.Errorf("vault does not answer on %s: %s", n.Name, firstLine(res.Stderr))
	}
	if !st.Sealed {
		return pass("%s is already unsealed", n.Name), nil
	}

	keys := credentialKeys(env, n.Cluster)
	want := a.Int("keys")
	if !a.Has("keys") {
		want = keyThreshold
	}
	if want < 1 || len(keys) < want {
		return primitive.Result{}, fmt.Errorf("%s has %d unseal key(s) on record, and %d were asked for", n.Cluster, len(keys), want)
	}
	for _, k := range keys[:want] {
		res, err := vaultExec(ctx, env, n, "", "operator", "unseal", k)
		if err != nil {
			return primitive.Result{}, err
		}
		if res.ExitCode != 0 {
			return fail(res.Stderr, "unsealing %s failed: %s", n.Name, firstLine(res.Stderr)), nil
		}
	}

	st, res, ok, err = readStatus(ctx, env, n)
	if err != nil || !ok {
		return primitive.Result{}, fmt.Errorf("read the status of %s after unsealing: %w", n.Name, errOr(err, "Vault does not answer"))
	}
	if st.Sealed {
		return fail(res.Stdout, "%s is still sealed after %d key(s)", n.Name, want), nil
	}
	return pass("unsealed %s with %d key(s)", n.Name, want), nil
}

func errOr(err error, msg string) error {
	if err != nil {
		return err
	}
	return errors.New(msg)
}

// credentialKeys returns a cluster's unseal keys in order.
func credentialKeys(env primitive.Env, cluster string) []string {
	var keys []string
	for i := 1; ; i++ {
		k, ok := env.Credential(cluster, "unsealKey"+strconv.Itoa(i))
		if !ok {
			return keys
		}
		keys = append(keys, k)
	}
}

// vault.init -----------------------------------------------------------------

type initialise struct{}

func (initialise) Spec() primitive.Spec {
	return primitive.Spec{
		Name: "vault.init",
		Summary: "Initialises a node that is not initialised yet and keeps the new root token and unseal keys as outputs " +
			"(rootToken and unsealKey1, unsealKey2 and so on). It fails if Vault is already initialised.",
		Timeout: primitiveTimeout,
		Changes: true,
		Args: []primitive.Arg{
			nodeArg,
			{Name: "shares", Type: primitive.Int, Default: keyShares, Help: "How many unseal key shares to make."},
			{Name: "threshold", Type: primitive.Int, Default: keyThreshold, Help: "How many shares unseal Vault."},
		},
	}
}

func (initialise) Run(ctx context.Context, env primitive.Env, a primitive.Args) (primitive.Result, error) {
	n, err := env.Lookup(a.String("node"))
	if err != nil {
		return primitive.Result{}, err
	}
	if a.Int("threshold") < 1 || a.Int("threshold") > a.Int("shares") {
		return primitive.Result{}, fmt.Errorf("the threshold %d must be from 1 to the number of shares (%d)", a.Int("threshold"), a.Int("shares"))
	}
	res, err := vaultExec(ctx, env, n, "", "operator", "init",
		"-key-shares="+strconv.Itoa(a.Int("shares")), "-key-threshold="+strconv.Itoa(a.Int("threshold")), "-format=json")
	if err != nil {
		return primitive.Result{}, err
	}
	if res.ExitCode != 0 {
		// The output of a failed init has no keys, so it is safe to show.
		return fail(res.Stderr+res.Stdout, "initialising %s failed: %s", n.Name, firstLine(res.Stderr+res.Stdout)), nil
	}
	keys, err := parseInit(res.Stdout)
	if err != nil {
		return primitive.Result{}, err
	}
	out := primitive.Result{Status: primitive.Pass, Message: fmt.Sprintf("initialised %s with %d shares and a threshold of %d", n.Name, a.Int("shares"), a.Int("threshold")),
		Outputs: map[string]string{"rootToken": keys.RootToken}}
	for i, k := range keys.UnsealKeys {
		out.Outputs["unsealKey"+strconv.Itoa(i+1)] = k
	}
	return out, nil // no evidence: the output holds the keys
}
