// Package html renders a report as a single self-contained HTML page (R-REPORT-2,
// R-REPORT-4).
//
// The page has its CSS inline and loads nothing: no script, no stylesheet, no image
// and no font from anywhere, so it can be opened or sent as one file, offline. Every
// value is escaped by html/template, so a note or a name containing markup is shown
// as text and never runs. The renderer is a pure function of the report.
package html
