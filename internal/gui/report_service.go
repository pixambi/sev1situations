package gui

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pixambi/sev1situations/internal/atomicfile"
	"github.com/pixambi/sev1situations/internal/report"
)

// SaveDialog asks the instructor where to save a file, in a native dialog opened
// from Go (6.7). It returns the chosen path, or "" if they cancelled.
type SaveDialog func(suggestedName string) (string, error)

// ReportService lists, shows, deletes and exports reports (3.4). It needs neither
// Podman nor a run, so the Reports tab works whatever state the machine is in.
type ReportService struct {
	store    *report.Store
	renderer report.Renderer
	save     SaveDialog
}

// NewReportService returns a service over the store. renderer makes the HTML, and
// save opens the dialog an export asks where to write.
func NewReportService(store *report.Store, renderer report.Renderer, save SaveDialog) *ReportService {
	return &ReportService{store: store, renderer: renderer, save: save}
}

// ReportList is every saved report, newest first, and a description of each file
// that could not be read as one.
type ReportList struct {
	Reports  []report.Summary
	Problems []string
}

// ExportResult says where an export went.
type ExportResult struct {
	// Path is the file that was written. It is empty if the instructor cancelled.
	Path string
	// Cancelled is true when the instructor closed the dialog without saving.
	Cancelled bool
}

// List returns the saved reports.
func (s *ReportService) List() (ReportList, error) {
	list, problems, err := s.store.List()
	if err != nil {
		return ReportList{}, err
	}
	out := ReportList{Reports: list, Problems: []string{}}
	for _, p := range problems {
		out.Problems = append(out.Problems, fmt.Sprintf("%s: %v", p.File, p.Err))
	}
	return out, nil
}

// Page returns the address of a report's page, for the Reports tab to show in a
// sandboxed frame (R-REPORT-9). It checks that the report can be read, so the frame
// is never pointed at a report that cannot be shown.
func (s *ReportService) Page(id string) (string, error) {
	if _, err := s.store.Get(id); err != nil {
		return "", reportError(err)
	}
	return reportRoutePrefix + id + ".html", nil
}

// Delete removes a report.
func (s *ReportService) Delete(id string) error {
	return reportError(s.store.Delete(id))
}

// Export saves a report to a file the instructor chooses. format is "html", the
// self-contained page, or "data", the raw report (R-REPORT-8).
func (s *ReportService) Export(id, format string) (ExportResult, error) {
	var (
		data []byte
		ext  string
	)
	r, err := s.store.Get(id)
	if err != nil {
		return ExportResult{}, reportError(err)
	}
	switch format {
	case "html":
		if data, err = s.renderer.Render(r); err != nil {
			return ExportResult{}, fmt.Errorf("render the report: %w", err)
		}
		ext = s.renderer.Extension()
	case "data":
		if data, err = s.store.Raw(id); err != nil {
			return ExportResult{}, reportError(err)
		}
		ext = ".json"
	default:
		return ExportResult{}, notice("Choose to export the page or the raw data.")
	}

	path, err := s.save(exportName(r, ext))
	if err != nil {
		return ExportResult{}, fmt.Errorf("ask where to save: %w", err)
	}
	if path == "" {
		return ExportResult{Cancelled: true}, nil
	}
	if err := atomicfile.Write(path, data, 0o644); err != nil {
		return ExportResult{}, fmt.Errorf("save the report to %s: %w", filepath.Base(path), err)
	}
	return ExportResult{Path: path}, nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._ -]+`)

// exportName suggests a file name: the scenario, the trainee and the day, with
// nothing in it a file system dislikes.
func exportName(r *report.Report, ext string) string {
	parts := []string{r.Scenario.Name, r.Trainee, r.StartedAt.UTC().Format("2006-01-02")}
	name := strings.Join(parts, " - ")
	name = strings.TrimSpace(unsafeName.ReplaceAllString(name, ""))
	name = strings.TrimLeft(name, ".- ")
	if name == "" || name == r.StartedAt.UTC().Format("2006-01-02") {
		name = "report " + r.ID
	}
	return name + ext
}

// reportError turns the store's errors into sentences for the instructor.
func reportError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, report.ErrNotFound):
		return notice("That report no longer exists. Refresh the list.")
	case errors.Is(err, report.ErrInvalidID):
		return notice("That is not a report.")
	case errors.Is(err, report.ErrTooNew):
		return notice("That report was written by a newer version of the application, which this one cannot read.")
	}
	return fmt.Errorf("the report could not be read: %w", err)
}
