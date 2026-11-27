package vault

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/runtime"
)

// Names of what ProvideSeal creates on the provider.
const (
	transitMount  = "transit/"
	transitKey    = "autounseal"
	transitPolicy = "autounseal"
	policyPath    = "/tmp/autounseal.hcl"
)

// transitPolicyHCL is the least a cluster needs to unseal itself through the key: encrypt and decrypt with it.
const transitPolicyHCL = `path "transit/encrypt/autounseal" {
  capabilities = ["update"]
}

path "transit/decrypt/autounseal" {
  capabilities = ["update"]
}
`

var _ product.SealProvider = (*Product)(nil)

// ProvideSeal implements product.SealProvider. It prepares a deployed Vault to unseal other clusters
// with its transit secrets engine: it enables the engine, creates the key, writes a policy that allows
// only that key to be used, and creates a token with it. The address it returns is the first node's
// address on the run's network, which is reachable from the other clusters' nodes.
func (p *Product) ProvideSeal(ctx context.Context, rt runtime.Runtime, d product.Deployment) (product.SealSpec, error) {
	if len(d.Nodes) == 0 {
		return product.SealSpec{}, fmt.Errorf("cluster %q has no node to provide a seal from", d.Cluster)
	}
	first, root := d.Nodes[0], d.Creds["rootToken"]
	c := &cluster{p: p, rt: rt}

	run := func(what string, args ...string) (runtime.ExecResult, error) {
		res, err := c.vault(ctx, first, root, args...)
		if err != nil || res.ExitCode != 0 {
			return res, fmt.Errorf("%s on %s: %w", what, first.Name, execProblem(res, err))
		}
		return res, nil
	}
	if _, err := run("enable the transit engine", "secrets", "enable", "-path="+trimSlash(transitMount), "transit"); err != nil {
		return product.SealSpec{}, err
	}
	if _, err := run("create the transit key", "write", "-f", "transit/keys/"+transitKey); err != nil {
		return product.SealSpec{}, err
	}
	if err := rt.Copy(ctx, first.ContainerID, fileOf(policyPath, 0o644, transitPolicyHCL)); err != nil {
		return product.SealSpec{}, fmt.Errorf("copy the policy to %s: %w", first.Name, err)
	}
	if _, err := run("write the transit policy", "policy", "write", transitPolicy, policyPath); err != nil {
		return product.SealSpec{}, err
	}
	// Orphaned, so it does not die with a parent, and periodic, so it does not expire during a session.
	res, err := run("create the seal token", "token", "create", "-orphan", "-policy="+transitPolicy, "-period=720h", "-format=json")
	if err != nil {
		return product.SealSpec{}, err
	}
	var out struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if json.Unmarshal([]byte(res.Stdout), &out) != nil || out.Auth.ClientToken == "" {
		return product.SealSpec{}, fmt.Errorf("create the seal token on %s: the output was not a token", first.Name) // the output holds the token, so it is not echoed
	}

	return product.SealSpec{Type: "transit", Transit: product.TransitSeal{
		Address:   fmt.Sprintf("http://%s:%d", first.IP, apiPort),
		Token:     out.Auth.ClientToken,
		MountPath: transitMount,
		KeyName:   transitKey,
	}}, nil
}

func trimSlash(s string) string {
	if n := len(s); n > 0 && s[n-1] == '/' {
		return s[:n-1]
	}
	return s
}
