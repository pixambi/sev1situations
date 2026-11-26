package primitive

import (
	"sort"
	"strings"
)

// Redacted replaces a secret in text.
const Redacted = "[redacted]"

// minSecretLen is the shortest secret that is redacted. Shorter strings would
// match ordinary words and make output unreadable.
const minSecretLen = 4

// Redactor removes known credentials from text before it is shown or stored
// (R-QUAL-3).
type Redactor struct {
	secrets []string
}

// NewRedactor returns a Redactor for the given secrets. Empty and very short
// values are ignored. Longer secrets are replaced first, so one that contains
// another is removed whole.
func NewRedactor(secrets ...string) *Redactor {
	var keep []string
	seen := map[string]bool{}
	for _, s := range secrets {
		if len(s) >= minSecretLen && !seen[s] {
			seen[s] = true
			keep = append(keep, s)
		}
	}
	sort.Slice(keep, func(i, j int) bool { return len(keep[i]) > len(keep[j]) })
	return &Redactor{secrets: keep}
}

// Redact returns text with every known secret replaced. A nil Redactor
// returns the text unchanged.
func (r *Redactor) Redact(text string) string {
	if r == nil {
		return text
	}
	for _, s := range r.secrets {
		text = strings.ReplaceAll(text, s, Redacted)
	}
	return text
}
