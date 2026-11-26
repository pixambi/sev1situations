package gui

import "net/http"

// contentSecurityPolicy is the policy every page and asset is served with in a
// production build (R-QUAL-5, R-TERM-6). The webview loads nothing from outside the
// application: no remote scripts, styles, fonts, images or connections.
//
//   - 'wasm-unsafe-eval' lets the terminal's WebAssembly compile. It does not allow eval.
//   - There is no 'unsafe-inline' for scripts or styles. Spike W2 showed the terminal needs none.
//   - connect-src 'self' is enough because the terminal's WASM is served as an asset and
//     not embedded as a data: URL (which would need connect-src data:).
//   - img-src allows data: for the small images Carbon's stylesheets embed.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' 'wasm-unsafe-eval'; " +
	"style-src 'self'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'none'; " +
	"frame-ancestors 'none'"

// withSecurityHeaders adds the policy and two related headers to every response,
// including errors.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
