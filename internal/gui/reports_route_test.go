package gui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pixambi/sev1situations/internal/report"
	htmlreport "github.com/pixambi/sev1situations/internal/report/html"
)

func savedReport(t *testing.T) (*report.Store, string) {
	t.Helper()
	store, id, _ := savedReportIn(t)
	return store, id
}

// savedReportIn also returns the folder the store keeps its files in.
func savedReportIn(t *testing.T) (*report.Store, string, string) {
	t.Helper()
	dir := t.TempDir()
	store := report.NewStore(dir)
	started := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	r := &report.Report{
		ID: report.NewID("abc123", started), Scenario: report.Scenario{Name: "Lost Quorum", Product: "vault"},
		Instructor: "Ada", Trainee: "Grace <b>Hopper</b>", Params: map[string]any{},
		StartedAt: started, EndedAt: started.Add(time.Hour), Result: report.Completed,
		Steps: []report.Step{}, Events: []report.Event{},
	}
	if err := store.Save(r); err != nil {
		t.Fatal(err)
	}
	return store, r.ID, dir
}

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

var rest = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("the application")) })

func TestAReportIsServedAsAPageWithItsOwnStrictPolicy(t *testing.T) {
	store, id := savedReport(t)
	h := reportRoute(store, htmlreport.Renderer{})(rest)

	rec := get(h, http.MethodGet, reportRoutePrefix+id+".html")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Lost Quorum") || strings.Contains(rec.Body.String(), "<b>Hopper</b>") {
		t.Error("the page is not the escaped report")
	}
	hd := rec.Header()
	if hd.Get("Content-Type") != "text/html; charset=utf-8" || hd.Get("Content-Security-Policy") != reportPolicy || hd.Get("Cache-Control") != "no-store" || hd.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers = %v", hd)
	}
	for _, bad := range []string{"script-src", "connect-src", "img-src", "unsafe-eval", "http"} {
		if strings.Contains(reportPolicy, bad) {
			t.Errorf("the report policy allows %q", bad)
		}
	}
}

func TestOtherRequestsGoOnToTheApplication(t *testing.T) {
	store, _ := savedReport(t)
	h := reportRoute(store, htmlreport.Renderer{})(rest)
	if rec := get(h, http.MethodGet, "/index.html"); rec.Body.String() != "the application" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestTheRouteRefusesWhatItShould(t *testing.T) {
	store, id := savedReport(t)
	h := reportRoute(store, htmlreport.Renderer{})(rest)
	for _, tt := range []struct {
		name, method, path string
		want               int
	}{
		{"an unknown report", http.MethodGet, reportRoutePrefix + "nope.html", http.StatusNotFound},
		{"a path that tries to leave the folder", http.MethodGet, reportRoutePrefix + "../settings.html", http.StatusBadRequest},
		{"an encoded path that tries to leave it", http.MethodGet, reportRoutePrefix + "..%2Fsettings.html", http.StatusBadRequest},
		{"an empty ID", http.MethodGet, reportRoutePrefix, http.StatusBadRequest},
		{"a POST", http.MethodPost, reportRoutePrefix + id + ".html", http.StatusMethodNotAllowed},
		{"a DELETE", http.MethodDelete, reportRoutePrefix + id + ".html", http.StatusMethodNotAllowed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, "/", nil)
			req.URL.Path = strings.ReplaceAll(tt.path, "%2F", "/")
			h.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
	if rec := get(h, http.MethodHead, reportRoutePrefix+id+".html"); rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("HEAD: %d with %d bytes", rec.Code, rec.Body.Len())
	}
}

func TestTheReportPolicyWinsOverTheApplicationsWhenChained(t *testing.T) {
	store, id := savedReport(t)
	h := chain(withSecurityHeaders, reportRoute(store, htmlreport.Renderer{}))(rest)

	page := get(h, http.MethodGet, reportRoutePrefix+id+".html")
	if page.Header().Get("Content-Security-Policy") != reportPolicy {
		t.Errorf("the report was served under %q", page.Header().Get("Content-Security-Policy"))
	}
	app := get(h, http.MethodGet, "/")
	if app.Header().Get("Content-Security-Policy") != contentSecurityPolicy {
		t.Errorf("the application was served under %q", app.Header().Get("Content-Security-Policy"))
	}
}

func TestChainSkipsNilAndKeepsTheOrder(t *testing.T) {
	var order []string
	mark := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { order = append(order, name); next.ServeHTTP(w, r) })
		}
	}
	h := chain(mark("outer"), nil, mark("inner"))(rest)
	get(h, http.MethodGet, "/")
	if strings.Join(order, ",") != "outer,inner" {
		t.Errorf("order = %v", order)
	}
}
