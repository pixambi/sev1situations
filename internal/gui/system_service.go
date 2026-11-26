package gui

import "context"

// PrereqStatus says whether the container runtime is ready.
type PrereqStatus struct {
	// Ready is true when scenarios can run.
	Ready bool
	// Message says what is wrong and what to do about it. It is empty when Ready.
	Message string
}

// PrereqCheck inspects the container runtime.
type PrereqCheck func(ctx context.Context) PrereqStatus

// SystemService reports the state of what the application depends on (3.4).
type SystemService struct {
	check PrereqCheck
	ctx   context.Context
	emit  Emitter
}

// NewSystemService returns a service over a check. ctx ends when the application does.
func NewSystemService(ctx context.Context, check PrereqCheck, emit Emitter) *SystemService {
	return &SystemService{check: check, ctx: ctx, emit: emit}
}

// EventSystemPrereq carries a PrereqStatus.
const EventSystemPrereq = "system:prereq"

// Prereq checks the container runtime now and publishes the answer.
func (s *SystemService) Prereq() PrereqStatus {
	st := s.check(s.ctx)
	s.emit(EventSystemPrereq, st)
	return st
}
