package boundary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/pixambi/sev1situations/internal/primitive"
	"github.com/pixambi/sev1situations/internal/product"
	"github.com/pixambi/sev1situations/internal/product/deployutil"
	"github.com/pixambi/sev1situations/internal/runtime"
)

// client runs the Boundary command line inside a node. The deployment, Describe, revive and the primitives all
// use it, so there is one way of signing in and one way of keeping a token off the command line.
type client struct {
	rt runtime.Runtime
}

// login is who to sign in as.
type login struct {
	AuthMethodID string
	Name         string
	Password     string
}

// adminOf is the admin account the database was initialised with.
func adminOf(d product.Deployment) login {
	return login{AuthMethodID: d.State["authMethodId"], Name: d.State["adminLogin"], Password: d.Creds["adminPassword"]}
}

// localAddr is a controller's own API, which it reaches without the load balancer.
func localAddr() string { return fmt.Sprintf("http://127.0.0.1:%d", apiPort) }

// run runs a Boundary command in a node against the given API address. A token goes in the command's
// environment and is named on the command line as env://BOUNDARY_TOKEN, so it is never in the process list.
func (c client) run(ctx context.Context, n primitive.Node, addr string, token string, args ...string) (runtime.ExecResult, error) {
	env := map[string]string{"BOUNDARY_ADDR": addr}
	if token != "" {
		env["BOUNDARY_TOKEN"] = token
		args = append(args, "-token=env://BOUNDARY_TOKEN")
	}
	return c.rt.Exec(ctx, n.ContainerID, append([]string{binaryPath}, args...), runtime.ExecOptions{Env: env})
}

// signIn signs in with a password and returns a token. The password is passed in the environment. The note says
// why it failed, for the message of a wait that runs out; the error is only for a sign-in that could not even be tried.
func (c client) signIn(ctx context.Context, from primitive.Node, addr string, who login) (token string, note string, err error) {
	res, err := c.rt.Exec(ctx, from.ContainerID, []string{binaryPath, "authenticate", "password",
		"-auth-method-id=" + who.AuthMethodID, "-login-name=" + who.Name, "-password=env://BOUNDARY_PASSWORD",
		"-keyring-type=none", "-format=json"},
		runtime.ExecOptions{Env: map[string]string{"BOUNDARY_ADDR": addr, "BOUNDARY_PASSWORD": who.Password}})
	if err != nil {
		return "", "", err
	}
	if res.ExitCode != 0 {
		note = deployutil.FirstLine(res.Stderr)
		return "", note, fmt.Errorf("sign in as %s: %s", who.Name, note)
	}
	var out struct {
		Item struct {
			Attributes struct {
				Token string `json:"token"`
			} `json:"attributes"`
		} `json:"item"`
	}
	if json.Unmarshal([]byte(res.Stdout), &out) != nil || out.Item.Attributes.Token == "" {
		// The output holds the token, so it is not quoted.
		return "", "the sign-in did not return a token", errors.New("the sign-in did not return a token")
	}
	return out.Item.Attributes.Token, "", nil
}

// workerRecord is what a controller knows of a worker.
type workerRecord struct {
	Address    string
	LastStatus string
}

// workers reads the output of `boundary workers list -format=json` as each worker's name and what is known of it. A worker
// that is removed leaves its record, so a name alone does not say a worker is there: its address must be the one the node
// has now, and a worker that was only restarted has the same address, so what shows it is back is that its last status time
// has moved on from what it was before.
func workers(out string) (map[string]workerRecord, error) {
	var list struct {
		Items []struct {
			Name       string `json:"name"`
			Address    string `json:"address"`
			LastStatus string `json:"last_status_time"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, errors.New("read the list of workers: it is not valid JSON")
	}
	got := map[string]workerRecord{}
	for _, w := range list.Items {
		got[w.Name] = workerRecord{Address: w.Address, LastStatus: w.LastStatus}
	}
	return got, nil
}

// registered reports whether the node is a registered worker: listed by name with its current address. If before is not
// empty it is the last status time the record had before the worker was started again, and the worker counts only once that
// has changed.
func registered(all map[string]workerRecord, n primitive.Node, before string) bool {
	w, ok := all[n.Name]
	if !ok || w.Address != fmt.Sprintf("%s:%d", n.IP, proxyPort) {
		return false
	}
	return before == "" || w.LastStatus != before
}

// workerStatus reads a worker's record through a controller, signed in as the admin. It returns an empty record if the
// worker is not listed or could not be read.
func (c client) workerStatus(ctx context.Context, via primitive.Node, d product.Deployment, name string) workerRecord {
	token, _, err := c.signIn(ctx, via, localAddr(), adminOf(d))
	if err != nil {
		return workerRecord{}
	}
	res, err := c.run(ctx, via, localAddr(), token, "workers", "list", "-format=json")
	if err != nil || res.ExitCode != 0 {
		return workerRecord{}
	}
	all, err := workers(res.Stdout)
	if err != nil {
		return workerRecord{}
	}
	return all[name]
}
