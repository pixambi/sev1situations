package gui

import (
	"context"
	"testing"
)

func TestPrereqReportsAndPublishesTheState(t *testing.T) {
	out := &emitted{}
	want := PrereqStatus{Ready: false, Message: "The Podman machine is stopped. Run: podman machine start"}
	svc := NewSystemService(context.Background(), func(context.Context) PrereqStatus { return want }, out.emit)

	if got := svc.Prereq(); got != want {
		t.Errorf("Prereq = %+v, want %+v", got, want)
	}
	if got := out.named(EventSystemPrereq); len(got) != 1 || got[0] != want {
		t.Errorf("published %v", got)
	}
}
