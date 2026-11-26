// Package fake is an in-memory runtime.Runtime for tests (R-TEST-2). It keeps
// containers, networks and files in memory, lets a test script what Exec
// returns, inject failures, feed log lines and make the runtime unavailable.
//
// It does not run anything. Exec returns an empty successful result unless a
// test sets an ExecFunc.
package fake

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/pixambi/sev1situations/internal/runtime"
)

// Operation names accepted by FailNext.
const (
	OpPing           = "Ping"
	OpEnsureImage    = "EnsureImage"
	OpCreateNetwork  = "CreateNetwork"
	OpStartContainer = "StartContainer"
	OpExec           = "Exec"
	OpAttach         = "Attach"
	OpLogs           = "Logs"
	OpCopy           = "Copy"
	OpStart          = "Start"
	OpStop           = "Stop"
	OpRemove         = "Remove"
	OpList           = "List"
	OpRuns           = "Runs"
	OpRemoveRun      = "RemoveRun"
)

// ExecFunc decides what Exec returns for a container and command.
type ExecFunc func(c runtime.Container, cmd []string, o runtime.ExecOptions) (runtime.ExecResult, error)

// StoredFile is a file a test can read back after Copy.
type StoredFile struct {
	// Mode is the mode the file was copied with.
	Mode fs.FileMode
	// Content is the file's bytes.
	Content []byte
}

// Runtime is the fake. The zero value is not usable: call New.
type Runtime struct {
	mu          sync.Mutex
	seq         int
	networks    map[string]*network
	containers  map[string]*container
	execFn      ExecFunc
	failures    map[string][]error
	unavailable error
	images      map[string]bool
	strict      bool
	pulls       []string
	terminals   map[string][]*Terminal
}

type network struct {
	info   runtime.Network
	number int
	nextIP int
}

type container struct {
	info      runtime.Container
	networkID string
	files     map[string]StoredFile
	log       *logBuffer
}

// New returns an empty fake runtime.
func New() *Runtime {
	return &Runtime{
		networks:   map[string]*network{},
		containers: map[string]*container{},
		failures:   map[string][]error{},
		images:     map[string]bool{},
		terminals:  map[string][]*Terminal{},
	}
}

// AddImage marks an image as already present locally.
func (r *Runtime) AddImage(ref string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.images[ref] = true
}

// RequireImages makes StartContainer fail for an image that is not local, as
// Podman does. By default the fake accepts any image.
func (r *Runtime) RequireImages() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.strict = true
}

// Pulled returns the images EnsureImage had to pull, in order.
func (r *Runtime) Pulled() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.pulls...)
}

// EnsureImage implements runtime.Runtime. An image that is not local is
// "pulled": it is recorded in Pulled and becomes local.
func (r *Runtime) EnsureImage(_ context.Context, ref string, progress func(string)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.enter(OpEnsureImage); err != nil {
		return err
	}
	if ref == "" {
		return errors.New("ensure image: reference is required")
	}
	if r.images[ref] {
		return nil
	}
	if progress != nil {
		progress("Pulling " + ref)
	}
	r.images[ref] = true
	r.pulls = append(r.pulls, ref)
	return nil
}

// SetExec sets the function that answers Exec. Pass nil to restore the default.
func (r *Runtime) SetExec(fn ExecFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.execFn = fn
}

// FailNext makes the next call of op return err. Calls queue: two FailNext
// calls fail the next two calls. RemoveRun consumes OpRemove once per
// container it removes.
func (r *Runtime) FailNext(op string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures[op] = append(r.failures[op], err)
}

// SetUnavailable makes every call fail with an error matching
// runtime.ErrUnavailable, as when Podman is stopped. Pass nil to recover.
func (r *Runtime) SetUnavailable(reason error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unavailable = reason
}

// Feed appends a line to a container's log and delivers it to followers.
func (r *Runtime) Feed(id, line string) error {
	r.mu.Lock()
	c, ok := r.containers[id]
	r.mu.Unlock()
	if !ok {
		return notFound("container", id)
	}
	c.log.append(strings.TrimSuffix(line, "\n"))
	return nil
}

// File returns a file copied into a container.
func (r *Runtime) File(id, path string) (StoredFile, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.containers[id]
	if !ok {
		return StoredFile{}, false
	}
	f, ok := c.files[path]
	return f, ok
}

// enter runs the checks every call makes: availability, then injected failures.
// The caller must hold r.mu.
func (r *Runtime) enter(op string) error {
	if r.unavailable != nil {
		return fmt.Errorf("%s: %w: %w", op, runtime.ErrUnavailable, r.unavailable)
	}
	if q := r.failures[op]; len(q) > 0 {
		r.failures[op] = q[1:]
		return q[0]
	}
	return nil
}

// Ping implements runtime.Runtime.
func (r *Runtime) Ping(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enter(OpPing)
}

// CreateNetwork implements runtime.Runtime.
func (r *Runtime) CreateNetwork(_ context.Context, o runtime.NetworkOptions) (runtime.Network, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.enter(OpCreateNetwork); err != nil {
		return runtime.Network{}, err
	}
	if o.Run.ID == "" || o.Name == "" {
		return runtime.Network{}, errors.New("create network: run ID and name are required")
	}
	name := fullName(o.Run.ID, o.Name)
	for _, n := range r.networks {
		if n.info.Name == name {
			return runtime.Network{}, fmt.Errorf("create network: %q already exists", name)
		}
	}
	r.seq++
	n := &network{
		info:   runtime.Network{ID: fmt.Sprintf("fake-n-%d", r.seq), Name: name, Run: o.Run},
		number: r.seq,
		nextIP: 2,
	}
	r.networks[n.info.ID] = n
	return n.info, nil
}

// StartContainer implements runtime.Runtime.
func (r *Runtime) StartContainer(_ context.Context, o runtime.ContainerOptions) (runtime.Container, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.enter(OpStartContainer); err != nil {
		return runtime.Container{}, err
	}
	if o.Run.ID == "" || o.Name == "" || o.Image == "" {
		return runtime.Container{}, errors.New("start container: run ID, name and image are required")
	}
	if r.strict && !r.images[o.Image] {
		return runtime.Container{}, fmt.Errorf("start container: image %q is not local: %w", o.Image, runtime.ErrNotFound)
	}
	name := fullName(o.Run.ID, o.Name)
	for _, c := range r.containers {
		if c.info.Name == name {
			return runtime.Container{}, fmt.Errorf("start container: name %q is already in use", name)
		}
		for _, have := range c.info.Ports {
			for _, want := range o.Ports {
				if have.Host == want.Host {
					return runtime.Container{}, fmt.Errorf("start container: host port %d is already allocated", want.Host)
				}
			}
		}
	}

	ip := ""
	if o.Network != "" {
		n, ok := r.networks[o.Network]
		if !ok {
			return runtime.Container{}, notFound("network", o.Network)
		}
		ip = fmt.Sprintf("10.89.%d.%d", n.number, n.nextIP)
		n.nextIP++
	}

	r.seq++
	c := &container{
		info: runtime.Container{
			ID: fmt.Sprintf("fake-c-%d", r.seq), Name: name, Image: o.Image, Run: o.Run,
			State: runtime.StateRunning, IP: ip, Ports: append([]runtime.PortMapping(nil), o.Ports...),
		},
		networkID: o.Network,
		files:     map[string]StoredFile{},
		log:       newLogBuffer(),
	}
	r.containers[c.info.ID] = c
	return c.info, nil
}

// Exec implements runtime.Runtime.
func (r *Runtime) Exec(ctx context.Context, id string, cmd []string, o runtime.ExecOptions) (runtime.ExecResult, error) {
	r.mu.Lock()
	if err := r.enter(OpExec); err != nil {
		r.mu.Unlock()
		return runtime.ExecResult{}, err
	}
	c, err := r.running(id)
	fn := r.execFn
	r.mu.Unlock()
	if err != nil {
		return runtime.ExecResult{}, err
	}
	if fn == nil {
		return runtime.ExecResult{}, nil
	}
	// Like the real runtime, stop waiting when the context ends, even if the
	// script is still running.
	type outcome struct {
		res runtime.ExecResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := fn(c.info, cmd, o)
		done <- outcome{res, err}
	}()
	select {
	case out := <-done:
		if o.Detach {
			// A detached command returns as soon as it has started, so its output
			// is never seen. The script still runs, which is how a test can make
			// the process it starts take effect.
			return runtime.ExecResult{}, nil
		}
		return out.res, out.err
	case <-ctx.Done():
		return runtime.ExecResult{}, ctx.Err()
	}
}

// Attach implements runtime.Runtime. The terminal echoes what is written to it.
func (r *Runtime) Attach(_ context.Context, id string, _ []string) (runtime.Terminal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.enter(OpAttach); err != nil {
		return nil, err
	}
	if _, err := r.running(id); err != nil {
		return nil, err
	}
	t := newTerminal()
	r.terminals[id] = append(r.terminals[id], t)
	return t, nil
}

// Terminals returns the terminals attached to a container so far, oldest first, so
// a test can look at what was resized or close one as the container would.
func (r *Runtime) Terminals(id string) []*Terminal {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*Terminal(nil), r.terminals[id]...)
}

// Logs implements runtime.Runtime.
func (r *Runtime) Logs(ctx context.Context, id string, o runtime.LogOptions) (io.ReadCloser, error) {
	r.mu.Lock()
	if err := r.enter(OpLogs); err != nil {
		r.mu.Unlock()
		return nil, err
	}
	c, ok := r.containers[id]
	r.mu.Unlock()
	if !ok {
		return nil, notFound("container", id)
	}
	return c.log.reader(ctx, o), nil
}

// Copy implements runtime.Runtime.
func (r *Runtime) Copy(_ context.Context, id string, f runtime.File) error {
	r.mu.Lock()
	if err := r.enter(OpCopy); err != nil {
		r.mu.Unlock()
		return err
	}
	c, ok := r.containers[id]
	r.mu.Unlock()
	if !ok {
		return notFound("container", id)
	}
	if !strings.HasPrefix(f.Path, "/") {
		return fmt.Errorf("copy: path %q must be absolute", f.Path)
	}

	var buf bytes.Buffer
	n, err := io.Copy(&buf, f.Content)
	if err != nil {
		return fmt.Errorf("copy %s: read content: %w", f.Path, err)
	}
	if n != f.Size {
		return fmt.Errorf("copy %s: content has %d bytes, size says %d", f.Path, n, f.Size)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	c.files[f.Path] = StoredFile{Mode: f.Mode, Content: buf.Bytes()}
	return nil
}

// Start implements runtime.Runtime.
func (r *Runtime) Start(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.enter(OpStart); err != nil {
		return err
	}
	c, ok := r.containers[id]
	if !ok {
		return notFound("container", id)
	}
	c.info.State = runtime.StateRunning
	return nil
}

// Stop implements runtime.Runtime.
func (r *Runtime) Stop(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.enter(OpStop); err != nil {
		return err
	}
	c, ok := r.containers[id]
	if !ok {
		return notFound("container", id)
	}
	c.info.State = runtime.StateStopped
	return nil
}

// Remove implements runtime.Runtime.
func (r *Runtime) Remove(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.remove(id)
}

func (r *Runtime) remove(id string) error {
	if err := r.enter(OpRemove); err != nil {
		return err
	}
	c, ok := r.containers[id]
	if !ok {
		return notFound("container", id)
	}
	c.log.close()
	for _, t := range r.terminals[id] {
		_ = t.Close() // a removed container ends the sessions in it
	}
	delete(r.terminals, id)
	delete(r.containers, id)
	return nil
}

// List implements runtime.Runtime.
func (r *Runtime) List(_ context.Context, runID string) ([]runtime.Container, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.enter(OpList); err != nil {
		return nil, err
	}
	out := []runtime.Container{}
	for _, c := range r.containers {
		if c.info.Run.ID == runID {
			out = append(out, c.info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Runs implements runtime.Runtime.
func (r *Runtime) Runs(context.Context) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.enter(OpRuns); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, c := range r.containers {
		seen[c.info.Run.ID] = true
	}
	for _, n := range r.networks {
		seen[n.info.Run.ID] = true
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// RemoveRun implements runtime.Runtime. It removes containers, then networks,
// keeps going past failures and returns them joined.
func (r *Runtime) RemoveRun(_ context.Context, runID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.enter(OpRemoveRun); err != nil {
		return err
	}

	var ids []string
	for id, c := range r.containers {
		if c.info.Run.ID == runID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	var errs []error
	for _, id := range ids {
		if err := r.remove(id); err != nil {
			errs = append(errs, fmt.Errorf("remove container %s: %w", id, err))
		}
	}
	for id, n := range r.networks {
		if n.info.Run.ID == runID && !r.networkInUse(id) {
			delete(r.networks, id)
		}
	}
	return errors.Join(errs...)
}

// networkInUse reports whether a container still uses the network.
func (r *Runtime) networkInUse(networkID string) bool {
	for _, c := range r.containers {
		if c.networkID == networkID {
			return true
		}
	}
	return false
}

// running returns the container if it exists and is running. The caller must hold r.mu.
func (r *Runtime) running(id string) (*container, error) {
	c, ok := r.containers[id]
	if !ok {
		return nil, notFound("container", id)
	}
	if c.info.State != runtime.StateRunning {
		return nil, fmt.Errorf("container %s is not running", id)
	}
	return c, nil
}

func fullName(runID, name string) string { return "s1s-" + runID + "-" + name }

func notFound(kind, id string) error {
	return fmt.Errorf("%s %q: %w", kind, id, runtime.ErrNotFound)
}
