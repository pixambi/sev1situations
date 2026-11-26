package podman

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/pixambi/sev1situations/internal/runtime"
)

// stopTimeoutSeconds is how long Podman waits for a container to exit before it kills it.
const stopTimeoutSeconds = 2

// Runtime is a runtime.Runtime backed by Podman.
type Runtime struct {
	c *client
}

var _ runtime.Runtime = (*Runtime)(nil)

// New finds the Podman machine's socket and returns a Runtime for it.
func New(ctx context.Context) (*Runtime, error) {
	socket, err := DiscoverSocket(ctx)
	if err != nil {
		return nil, err
	}
	return NewWithSocket(socket), nil
}

// NewWithSocket returns a Runtime that talks to the given unix socket.
func NewWithSocket(socket string) *Runtime {
	return &Runtime{c: newClient(socket)}
}

func fullName(runID, name string) string { return "s1s-" + runID + "-" + name }

func runLabels(run runtime.Run) map[string]string {
	return map[string]string{
		runtime.LabelApp:      runtime.AppLabelValue,
		runtime.LabelRun:      run.ID,
		runtime.LabelScenario: run.Scenario,
	}
}

// labelFilter builds the filters query for objects of this application, and of
// one run when runID is not empty.
func labelFilter(runID string) url.Values {
	labels := []string{runtime.LabelApp + "=" + runtime.AppLabelValue}
	if runID != "" {
		labels = append(labels, runtime.LabelRun+"="+runID)
	}
	b, _ := json.Marshal(map[string][]string{"label": labels})
	return url.Values{"filters": {string(b)}}
}

// Ping implements runtime.Runtime.
func (r *Runtime) Ping(ctx context.Context) error { return r.c.ping(ctx) }

// CreateNetwork implements runtime.Runtime. Podman addresses a network by its
// name, so the returned ID is the full name.
func (r *Runtime) CreateNetwork(ctx context.Context, o runtime.NetworkOptions) (runtime.Network, error) {
	if o.Run.ID == "" || o.Name == "" {
		return runtime.Network{}, errors.New("create network: run ID and name are required")
	}
	name := fullName(o.Run.ID, o.Name)
	_, err := r.c.do(ctx, http.MethodPost, "/networks/create", nil,
		map[string]any{"name": name, "labels": runLabels(o.Run)}, nil)
	if err != nil {
		return runtime.Network{}, fmt.Errorf("create network %s: %w", name, err)
	}
	return runtime.Network{ID: name, Name: name, Run: o.Run}, nil
}

// StartContainer implements runtime.Runtime.
func (r *Runtime) StartContainer(ctx context.Context, o runtime.ContainerOptions) (runtime.Container, error) {
	if o.Run.ID == "" || o.Name == "" || o.Image == "" {
		return runtime.Container{}, errors.New("start container: run ID, name and image are required")
	}
	name := fullName(o.Run.ID, o.Name)

	ports := make([]map[string]any, 0, len(o.Ports))
	for _, p := range o.Ports {
		if p.Host < 1 || p.Host > 65535 || p.Container < 1 || p.Container > 65535 {
			return runtime.Container{}, fmt.Errorf("start container %s: invalid port mapping %d:%d", name, p.Host, p.Container)
		}
		// Published ports bind to the loopback interface only (R-APP-3).
		ports = append(ports, map[string]any{
			"host_ip": "127.0.0.1", "host_port": p.Host, "container_port": p.Container, "protocol": "tcp",
		})
	}

	hostname := o.Hostname
	if hostname == "" {
		hostname = o.Name
	}
	spec := map[string]any{
		"name":         name,
		"image":        o.Image,
		"labels":       runLabels(o.Run),
		"hostname":     hostname,
		"netns":        map[string]any{"nsmode": "bridge"},
		"portmappings": ports,
	}
	if len(o.Command) > 0 {
		spec["command"] = o.Command
	}
	if len(o.Env) > 0 {
		spec["env"] = o.Env
	}
	if len(o.CapAdd) > 0 {
		spec["cap_add"] = o.CapAdd
	}
	if o.Network != "" {
		spec["networks"] = map[string]any{o.Network: map[string]any{}}
	}

	var created struct {
		ID string `json:"Id"`
	}
	if _, err := r.c.do(ctx, http.MethodPost, "/containers/create", nil, spec, &created); err != nil {
		return runtime.Container{}, fmt.Errorf("create container %s: %w", name, err)
	}
	if _, err := r.c.do(ctx, http.MethodPost, "/containers/"+created.ID+"/start", nil, nil, nil); err != nil {
		// Do not leave a created-but-dead container behind.
		_, _ = r.c.do(context.WithoutCancel(ctx), http.MethodDelete, "/containers/"+created.ID,
			url.Values{"force": {"true"}}, nil, nil)
		return runtime.Container{}, fmt.Errorf("start container %s: %w", name, err)
	}

	in, err := r.inspect(ctx, created.ID)
	if err != nil {
		return runtime.Container{}, fmt.Errorf("inspect container %s: %w", name, err)
	}
	c := in.container()
	c.Ports = append([]runtime.PortMapping(nil), o.Ports...)
	return c, nil
}

// inspectResult is the part of a container inspect response we use.
type inspectResult struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Status string `json:"Status"`
	} `json:"State"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

func (in inspectResult) container() runtime.Container {
	ip := ""
	names := make([]string, 0, len(in.NetworkSettings.Networks))
	for n := range in.NetworkSettings.Networks {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if a := in.NetworkSettings.Networks[n].IPAddress; a != "" {
			ip = a
			break
		}
	}
	return runtime.Container{
		ID:    in.ID,
		Name:  trimSlash(in.Name),
		Image: in.Config.Image,
		Run: runtime.Run{
			ID:       in.Config.Labels[runtime.LabelRun],
			Scenario: in.Config.Labels[runtime.LabelScenario],
		},
		State: toState(in.State.Status),
		IP:    ip,
	}
}

func (r *Runtime) inspect(ctx context.Context, id string) (inspectResult, error) {
	var in inspectResult
	_, err := r.c.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/json", nil, nil, &in)
	return in, err
}

// owned inspects a container and refuses to go on unless this application
// created it, so that no call can touch a container that is not ours (R-RUN-4).
func (r *Runtime) owned(ctx context.Context, id string) (inspectResult, error) {
	in, err := r.inspect(ctx, id)
	if err != nil {
		return in, fmt.Errorf("container %q: %w", id, err)
	}
	if in.Config.Labels[runtime.LabelApp] != runtime.AppLabelValue {
		return in, fmt.Errorf("container %q was not created by this application, refusing to touch it", id)
	}
	return in, nil
}

// Stop implements runtime.Runtime.
func (r *Runtime) Stop(ctx context.Context, id string) error {
	if _, err := r.owned(ctx, id); err != nil {
		return err
	}
	// 204 on success and 304 when it was already stopped: both are fine.
	_, err := r.c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/stop",
		url.Values{"timeout": {strconv.Itoa(stopTimeoutSeconds)}}, nil, nil)
	if err != nil {
		return fmt.Errorf("stop container %q: %w", id, err)
	}
	return nil
}

// Remove implements runtime.Runtime.
func (r *Runtime) Remove(ctx context.Context, id string) error {
	if _, err := r.owned(ctx, id); err != nil {
		return err
	}
	return r.remove(ctx, id)
}

func (r *Runtime) remove(ctx context.Context, id string) error {
	_, err := r.c.do(ctx, http.MethodDelete, "/containers/"+url.PathEscape(id),
		url.Values{"force": {"true"}, "v": {"true"}}, nil, nil)
	if err != nil {
		return fmt.Errorf("remove container %q: %w", id, err)
	}
	return nil
}

// listItem is the part of a container list entry we use.
type listItem struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
	Ports  []struct {
		HostPort      int `json:"host_port"`
		ContainerPort int `json:"container_port"`
	} `json:"Ports"`
}

func (r *Runtime) listContainers(ctx context.Context, runID string) ([]listItem, error) {
	q := labelFilter(runID)
	q.Set("all", "true")
	var items []listItem
	if _, err := r.c.do(ctx, http.MethodGet, "/containers/json", q, nil, &items); err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	return items, nil
}

// List implements runtime.Runtime.
func (r *Runtime) List(ctx context.Context, runID string) ([]runtime.Container, error) {
	items, err := r.listContainers(ctx, runID)
	if err != nil {
		return nil, err
	}
	out := make([]runtime.Container, 0, len(items))
	for _, it := range items {
		c := runtime.Container{
			ID:    it.ID,
			Image: it.Image,
			Run:   runtime.Run{ID: it.Labels[runtime.LabelRun], Scenario: it.Labels[runtime.LabelScenario]},
			State: toState(it.State),
		}
		if len(it.Names) > 0 {
			c.Name = trimSlash(it.Names[0])
		}
		for _, p := range it.Ports {
			c.Ports = append(c.Ports, runtime.PortMapping{Host: p.HostPort, Container: p.ContainerPort})
		}
		// The list response has no address, so ask for it.
		if in, err := r.inspect(ctx, it.ID); err == nil {
			c.IP = in.container().IP
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// networkItem is the part of a network list entry we use.
type networkItem struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
}

func (r *Runtime) listNetworks(ctx context.Context, runID string) ([]networkItem, error) {
	var items []networkItem
	if _, err := r.c.do(ctx, http.MethodGet, "/networks/json", labelFilter(runID), nil, &items); err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}
	return items, nil
}

// Runs implements runtime.Runtime.
func (r *Runtime) Runs(ctx context.Context) ([]string, error) {
	containers, err := r.listContainers(ctx, "")
	if err != nil {
		return nil, err
	}
	networks, err := r.listNetworks(ctx, "")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, c := range containers {
		if id := c.Labels[runtime.LabelRun]; id != "" {
			seen[id] = true
		}
	}
	for _, n := range networks {
		if id := n.Labels[runtime.LabelRun]; id != "" {
			seen[id] = true
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// RemoveRun implements runtime.Runtime. Selection is by label only, and it
// keeps going past a failed removal (section 7).
func (r *Runtime) RemoveRun(ctx context.Context, runID string) error {
	if runID == "" {
		return errors.New("remove run: run ID is required")
	}
	containers, err := r.listContainers(ctx, runID)
	if err != nil {
		return err
	}
	networks, err := r.listNetworks(ctx, runID)
	if err != nil {
		return err
	}

	var errs []error
	for _, c := range containers {
		if err := r.remove(ctx, c.ID); err != nil && !errors.Is(err, runtime.ErrNotFound) {
			errs = append(errs, err)
		}
	}
	for _, n := range networks {
		_, err := r.c.do(ctx, http.MethodDelete, "/networks/"+url.PathEscape(n.Name), nil, nil, nil)
		if err != nil && !errors.Is(err, runtime.ErrNotFound) {
			errs = append(errs, fmt.Errorf("remove network %q: %w", n.Name, err))
		}
	}
	return errors.Join(errs...)
}

func toState(status string) runtime.State {
	if status == "running" {
		return runtime.StateRunning
	}
	return runtime.StateStopped
}

func trimSlash(name string) string {
	if len(name) > 0 && name[0] == '/' {
		return name[1:]
	}
	return name
}
