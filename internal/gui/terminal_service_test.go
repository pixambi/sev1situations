package gui

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/runtime/fake"
)

func terminalsOf(t *testing.T, b *backend) (*TerminalService, *RunService, *link, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	l := newLink(ctx, func(context.Context) (*Connection, error) { return &Connection{Engine: b.eng, Runtime: b.rt}, nil }, b.out.emit)
	l.ensure()
	return NewTerminalService(ctx, l, b.out.emit), NewRunService(ctx, l.source, b.catalogue, b.out.emit), l, cancel
}

func up(t *testing.T) (*backend, *TerminalService, *RunService, *link) {
	t.Helper()
	b := newBackend(t, map[string]string{"s": scenarioYAML("demo", "Demo", "vault", true)})
	term, runs, l, cancel := terminalsOf(t, b)
	t.Cleanup(cancel)
	if _, err := runs.Start(form()); err != nil {
		t.Fatal(err)
	}
	return b, term, runs, l
}

// output gathers what a terminal has sent, decoded.
func output(b *backend, id string) string {
	var sb strings.Builder
	for _, d := range b.out.named("terminal:out:" + id) {
		raw, err := base64.StdEncoding.DecodeString(d.(string))
		if err != nil {
			return "BAD BASE64: " + err.Error()
		}
		sb.Write(raw)
	}
	return sb.String()
}

func exited(b *backend, id string) *TerminalExit {
	for _, d := range b.out.named("terminal:exit:" + id) {
		x := d.(TerminalExit)
		return &x
	}
	return nil
}

func attached(t *testing.T, b *backend, node string) *fake.Terminal {
	t.Helper()
	c, err := b.eng.ContainerOf(node)
	if err != nil {
		t.Fatal(err)
	}
	ts := b.rt.Terminals(c)
	if len(ts) == 0 {
		t.Fatal("no terminal attached")
	}
	return ts[len(ts)-1]
}

func TestInputReachesTheShellAndOutputComesBackAsBase64(t *testing.T) {
	b, term, _, _ := up(t)
	if err := term.Open("t1", "node-1", 100, 30); err != nil {
		t.Fatal(err)
	}
	if cols, rows := attached(t, b, "node-1").Size(); cols != 100 || rows != 30 {
		t.Errorf("first size = %dx%d, want the size the page reported", cols, rows)
	}

	if err := term.Input("t1", "ls -l\r"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the echo", func() bool { return output(b, "t1") == "ls -l\r" })
}

func TestMultibyteOutputSurvivesBeingSplitMidCharacter(t *testing.T) {
	b, term, _, _ := up(t)
	_ = term.Open("t1", "node-1", 80, 24)
	// The fake echoes what it is given. A real shell can split a character across reads, which is why
	// the bytes travel as base64: the text below is sent one byte at a time.
	word := "日本語 ✓ é"
	for i := 0; i < len(word); i++ {
		if err := term.Input("t1", word[i:i+1]); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "all bytes", func() bool { return output(b, "t1") == word })
}

func TestAFastStreamIsBatchedNotSentReadByRead(t *testing.T) {
	b, term, _, _ := up(t)
	_ = term.Open("t1", "node-1", 80, 24)
	ft := attached(t, b, "node-1")

	const writes = 2000
	for i := 0; i < writes; i++ {
		_, _ = ft.Write([]byte("0123456789\n"))
	}
	eventually(t, "all the output", func() bool { return len(output(b, "t1")) == writes*11 })
	if events := len(b.out.named("terminal:out:t1")); events > writes/4 {
		t.Errorf("%d events for %d writes: output is not batched", events, writes)
	}
	for _, d := range b.out.named("terminal:out:t1") {
		raw, _ := base64.StdEncoding.DecodeString(d.(string))
		if len(raw) > 2*outputChunk {
			t.Errorf("an event carried %d bytes, well over the %d chunk size", len(raw), outputChunk)
		}
	}
}

func TestResizeReachesTheShell(t *testing.T) {
	b, term, _, _ := up(t)
	_ = term.Open("t1", "node-1", 80, 24)
	if err := term.Resize("t1", 132, 43); err != nil {
		t.Fatal(err)
	}
	if cols, rows := attached(t, b, "node-1").Size(); cols != 132 || rows != 43 {
		t.Errorf("size = %dx%d", cols, rows)
	}
}

func TestCloseEndsTheTerminalQuietly(t *testing.T) {
	b, term, _, _ := up(t)
	_ = term.Open("t1", "node-1", 80, 24)
	term.Close("t1")
	term.Close("t1")
	term.Close("never-opened")

	eventually(t, "the session to end", func() bool { return term.Input("t1", "x") != nil })
	if x := exited(b, "t1"); x != nil {
		t.Errorf("a terminal the instructor closed announced an exit: %+v", x)
	}
	if err := term.Open("t1", "node-1", 80, 24); err != nil { // and its ID is free again
		t.Errorf("reopening: %v", err)
	}
}

func TestAShellThatExitsIsAnnounced(t *testing.T) {
	b, term, _, _ := up(t)
	_ = term.Open("t1", "node-1", 80, 24)
	_ = attached(t, b, "node-1").Close() // the shell ends by itself
	eventually(t, "the exit event", func() bool { return exited(b, "t1") != nil })
}

func TestTerminalsEndWhenTheRunEnds(t *testing.T) {
	b, term, runs, l := up(t)
	_ = term.Open("t1", "node-1", 80, 24)
	_ = term.Open("t2", "node-2", 80, 24)
	pumpCtx, stop := context.WithCancel(context.Background())
	defer stop()
	go pumpEvents(pumpCtx, b.eng, b.out.emit, l.runEnded)
	time.Sleep(20 * time.Millisecond)

	if _, err := runs.End(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "both terminals to close", func() bool {
		return term.Input("t1", "x") != nil && term.Input("t2", "x") != nil
	})
}

func TestOpenRefusesWhatItShould(t *testing.T) {
	b, term, runs, _ := up(t)
	if _, err := runs.NodeAction("node-2", "remove"); err != nil {
		t.Fatal(err)
	}
	_ = term.Open("taken", "node-1", 80, 24)

	for _, tt := range []struct {
		name       string
		id, node   string
		cols, rows int
		want       string
	}{
		{"a bad ID", "no spaces", "node-1", 80, 24, "valid terminal ID"},
		{"an unknown node", "x1", "node-9", 80, 24, "no node"},
		{"a removed node", "x2", "node-2", 80, 24, "removed"},
		{"an ID in use", "taken", "node-1", 80, 24, "already open"},
		{"no columns", "x3", "node-1", 0, 24, "size"},
		{"a huge size", "x4", "node-1", 80, 5000, "size"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := term.Open(tt.id, tt.node, tt.cols, tt.rows)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
	_ = b
}

func TestInputAndResizeNeedAnOpenTerminal(t *testing.T) {
	_, term, _, _ := up(t)
	if err := term.Input("nope", "x"); err == nil || !strings.Contains(err.Error(), "not open") {
		t.Errorf("Input: %v", err)
	}
	if err := term.Resize("nope", 80, 24); err == nil || !strings.Contains(err.Error(), "not open") {
		t.Errorf("Resize: %v", err)
	}
	_ = term.Open("t1", "node-1", 80, 24)
	if err := term.Input("t1", strings.Repeat("x", maxInput+1)); err == nil {
		t.Error("an oversized paste was accepted")
	}
	if err := term.Resize("t1", 0, 0); err == nil {
		t.Error("a zero size was accepted")
	}
}

func TestOnlyAFewTerminalsCanBeOpenAndNeedARun(t *testing.T) {
	b, term, _, _ := up(t)
	for i := 0; i < maxTerminals; i++ {
		if err := term.Open(fmt.Sprintf("t%d", i), "node-1", 80, 24); err != nil {
			t.Fatalf("terminal %d: %v", i, err)
		}
	}
	if err := term.Open("extra", "node-1", 80, 24); err == nil || !strings.Contains(err.Error(), "At most") {
		t.Errorf("err = %v", err)
	}

	idle := newBackend(t, nil)
	none, _, _, cancel := terminalsOf(t, idle)
	defer cancel()
	if err := none.Open("t1", "node-1", 80, 24); err == nil || !strings.Contains(err.Error(), "No scenario is running") {
		t.Errorf("with no run: %v", err)
	}
	_ = b
}

func TestTwoOpensOfOneIDAtOnceLeaveOneTerminal(t *testing.T) {
	_, term, _, _ := up(t)
	var wg sync.WaitGroup
	results := make([]error, 8)
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); results[i] = term.Open("same", "node-1", 80, 24) }()
	}
	wg.Wait()
	ok := 0
	for _, err := range results {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Errorf("%d opens of the same ID succeeded, want 1", ok)
	}
}
