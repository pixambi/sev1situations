package gui

import (
	"errors"
	"net/http"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/pixambi/sev1situations/internal/report"
)

// reportRoutePrefix is where the page serves a report as HTML, for the Reports tab to
// show in a frame (R-REPORT-9). It is a route of the application's own asset server,
// not a network listener (R-APP-3).
const reportRoutePrefix = "/_reports/"

// reportPolicy is the Content-Security-Policy of a report page. A report carries its
// style inline, so it needs that and nothing else: no script, no image, no font, no
// connection. It may be framed by the application and by nothing else.
const reportPolicy = "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'self'"

// reportRoute serves /_reports/<id>.html, a report rendered by rd. Any other request
// goes on to the next handler. The page sets its own, stricter policy over the
// application's.
func reportRoute(store *report.Store, rd report.Renderer) application.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if !strings.HasPrefix(req.URL.Path, reportRoutePrefix) {
				next.ServeHTTP(w, req)
				return
			}
			if req.Method != http.MethodGet && req.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			id := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, reportRoutePrefix), ".html")
			r, err := store.Get(id)
			switch {
			case errors.Is(err, report.ErrInvalidID):
				http.Error(w, "not a report", http.StatusBadRequest)
				return
			case errors.Is(err, report.ErrNotFound):
				http.Error(w, "no such report", http.StatusNotFound)
				return
			case err != nil:
				http.Error(w, "the report could not be read", http.StatusInternalServerError)
				return
			}
			page, err := rd.Render(r)
			if err != nil {
				http.Error(w, "the report could not be shown", http.StatusInternalServerError)
				return
			}

			h := w.Header()
			h.Set("Content-Type", rd.ContentType())
			h.Set("Content-Security-Policy", reportPolicy)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cache-Control", "no-store") // a report names a trainee, so nothing keeps a copy
			if req.Method == http.MethodGet {
				_, _ = w.Write(page)
			}
		})
	}
}

// chain applies the middleware in order, outermost first. A nil one is skipped.
func chain(ms ...application.Middleware) application.Middleware {
	return func(next http.Handler) http.Handler {
		for i := len(ms) - 1; i >= 0; i-- {
			if ms[i] != nil {
				next = ms[i](next)
			}
		}
		return next
	}
}
