package gui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/product"
)

func logsOf(t *testing.T, b *backend) (*LogService, *RunService, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	l := newLink(ctx, func(context.Context) (*Connection, error) {
		return &Connection{
			Engine: b.eng, Runtime: b.rt,
			LogStreams: func(_, _ string) []product.LogStream {
				return []product.LogStream{{ID: "operational", Label: "Operational log"}, {ID: "audit", Label: "Audit log", File: "/vault/logs/audit.log"}}
			},
		}, nil
	}, b.out.emit)
	l.ensure()
	return NewLogService(ctx, l, b.out.emit), NewRunService(ctx, l.source, b.catalogue, b.out.emit), cancel
}

func started(t *testing.T) (*backend, *LogService, *RunService) {
	t.Helper()
	b := newBackend(t, map[string]string{"s": scenarioYAML("demo", "Demo", "vault", true)})
	logs, runs, cancel := logsOf(t, b)
	t.Cleanup(cancel)
	if _, err := runs.Start(form()); err != nil {
		t.Fatal(err)
	}
	return b, logs, runs
}

func linesOf(b *backend, id string) (lines []string, ended *LogBatch) {
	for _, d := range b.out.named("log:" + id) {
		batch := d.(LogBatch)
		lines = append(lines, batch.Lines...)
		if batch.Ended {
			x := batch
			ended = &x
		}
	}
	return lines, ended
}

func TestStreamsListsWhatTheProductOffers(t *testing.T) {
	_, logs, _ := started(t)
	got, err := logs.Streams("node-1")
	if err != nil || len(got) != 2 || got[0] != (StreamInfo{"operational", "Operational log"}) || got[1].ID != "audit" {
		t.Errorf("streams = %+v, %v", got, err)
	}
}

func TestAnOpenStreamSendsTheNodesLinesInBatches(t *testing.T) {
	b, logs, _ := started(t)
	id, _ := b.eng.ContainerOf("node-1")
	_ = b.rt.Feed(id, "[INFO] core: before")

	if err := logs.Open("s1", "node-1", "operational"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the existing line", func() bool { l, _ := linesOf(b, "s1"); return len(l) == 1 })

	_ = b.rt.Feed(id, "[ERROR] core: after")
	eventually(t, "the live line", func() bool { l, _ := linesOf(b, "s1"); return len(l) == 2 })
	lines, _ := linesOf(b, "s1")
	if lines[0] != "[INFO] core: before" || lines[1] != "[ERROR] core: after" {
		t.Errorf("lines = %q", lines)
	}
}

func TestClosingAStreamStopsItAndFreesItsSlot(t *testing.T) {
	b, logs, _ := started(t)
	id, _ := b.eng.ContainerOf("node-1")
	if err := logs.Open("s1", "node-1", "operational"); err != nil {
		t.Fatal(err)
	}
	logs.Close("s1")
	logs.Close("s1")   // closing it again is fine
	logs.Close("nope") // so is closing one that never existed

	eventually(t, "the slot to free", func() bool { return logs.Open("s1", "node-1", "operational") == nil })
	logs.Close("s1")
	_ = b.rt.Feed(id, "after close")
	if _, ended := linesOf(b, "s1"); ended != nil {
		t.Errorf("a stream the instructor closed announced an end: %+v", ended)
	}
}

func TestAStreamEndsWhenItsNodeIsRemoved(t *testing.T) {
	b, logs, runs := started(t)
	if err := logs.Open("s1", "node-2", "operational"); err != nil {
		t.Fatal(err)
	}
	if _, err := runs.NodeAction("node-2", "remove"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the end", func() bool { _, e := linesOf(b, "s1"); return e != nil })
}

func TestOpenRefusesWhatItCannotDo(t *testing.T) {
	b, logs, runs := started(t)
	if _, err := runs.NodeAction("node-2", "remove"); err != nil {
		t.Fatal(err)
	}
	if err := logs.Open("dup", "node-1", "operational"); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name             string
		id, node, stream string
		want             string
	}{
		{"a bad ID", "has space", "node-1", "operational", "valid stream ID"},
		{"an empty ID", "", "node-1", "operational", "valid stream ID"},
		{"a long ID", strings.Repeat("a", 65), "node-1", "operational", "valid stream ID"},
		{"an unknown node", "x1", "node-9", "operational", "no node"},
		{"a removed node", "x2", "node-2", "operational", "removed"},
		{"an unknown log", "x3", "node-1", "nope", `no log called "nope"`},
		{"an ID in use", "dup", "node-1", "operational", "already open"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := logs.Open(tt.id, tt.node, tt.stream)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
	_ = b
}

func TestOpenNeedsARunAndAConnection(t *testing.T) {
	b := newBackend(t, nil)
	logs, _, cancel := logsOf(t, b)
	defer cancel()
	if err := logs.Open("s1", "node-1", "operational"); err == nil || !strings.Contains(err.Error(), "No scenario is running") {
		t.Errorf("with no run: %v", err)
	}
	if _, err := logs.Streams("node-1"); err == nil {
		t.Error("Streams with no run succeeded")
	}

	down := NewLogService(context.Background(), newLink(context.Background(), func(context.Context) (*Connection, error) { return nil, context.Canceled }, b.out.emit), b.out.emit)
	if err := down.Open("s1", "node-1", "operational"); err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Errorf("with no connection: %v", err)
	}
}

func TestOnlyAFewLogsCanBeOpenAtOnce(t *testing.T) {
	_, logs, _ := started(t)
	for i := 0; i < maxOpenStreams; i++ {
		if err := logs.Open(fmt.Sprintf("s%d", i), "node-1", "operational"); err != nil {
			t.Fatalf("stream %d: %v", i, err)
		}
	}
	if err := logs.Open("one-too-many", "node-1", "operational"); err == nil || !strings.Contains(err.Error(), "At most") {
		t.Errorf("err = %v", err)
	}
}
