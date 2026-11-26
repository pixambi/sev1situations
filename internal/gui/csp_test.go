package gui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, status int) http.Header {
	t.Helper()
	h := withSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Header()
}

func TestEveryResponseCarriesThePolicy(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusInternalServerError} {
		h := serve(t, status)
		if h.Get("Content-Security-Policy") != contentSecurityPolicy {
			t.Errorf("status %d: Content-Security-Policy = %q", status, h.Get("Content-Security-Policy"))
		}
		if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("status %d: headers = %v", status, h)
		}
	}
}

func TestThePolicyAllowsOnlyTheApplicationItself(t *testing.T) {
	directives := map[string]string{}
	for _, d := range strings.Split(contentSecurityPolicy, ";") {
		name, value, _ := strings.Cut(strings.TrimSpace(d), " ")
		directives[name] = value
	}

	// Nothing may come from the network or from anywhere but the app (R-UI-7, R-QUAL-5).
	for name, value := range directives {
		for _, bad := range []string{"http:", "https:", "ws:", "wss:", "*", "'unsafe-eval'", "'unsafe-inline'"} {
			if strings.Contains(value, bad) {
				t.Errorf("%s allows %s: %q", name, bad, value)
			}
		}
	}

	want := map[string]string{
		"default-src":     "'self'",
		"script-src":      "'self' 'wasm-unsafe-eval'", // the terminal's WebAssembly, and nothing else (R-TERM-6)
		"style-src":       "'self'",
		"connect-src":     "'self'", // events and services only, no data: (spike W2)
		"font-src":        "'self'",
		"object-src":      "'none'",
		"frame-ancestors": "'none'",
	}
	for name, value := range want {
		if directives[name] != value {
			t.Errorf("%s = %q, want %q", name, directives[name], value)
		}
	}
}
