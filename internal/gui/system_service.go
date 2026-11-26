package gui

// PrereqStatus says whether the container runtime is ready.
type PrereqStatus struct {
	// Ready is true when scenarios can run.
	Ready bool
	// Message says what is wrong and what to do about it. It is empty when Ready.
	Message string
	// Notices describe what start-up cleanup did, for example that a run left by
	// an earlier launch was removed.
	Notices []string
}

// EventSystemPrereq carries a PrereqStatus.
const EventSystemPrereq = "system:prereq"

// SystemService reports the state of what the application depends on (3.4).
type SystemService struct {
	link *link
	emit Emitter
}

// NewSystemService returns a service over the connection.
func NewSystemService(l *link, emit Emitter) *SystemService {
	return &SystemService{link: l, emit: emit}
}

// Prereq checks the container runtime now, connecting to it if it has not been
// reached yet, and publishes the answer. The page calls it on load and on Retry.
func (s *SystemService) Prereq() PrereqStatus {
	st := s.link.ensure()
	s.emit(EventSystemPrereq, st)
	return st
}
