// Package report defines the report data and its store (R-REPORT-1).
//
// A report is structured data with a versioned schema, and it is the source of
// truth. Presentation is derived from it by renderers, which are pure functions
// from a Report to output (R-REPORT-2, R-REPORT-3), so a new format needs no change
// here. A report holds no secret (R-QUAL-3): what goes into it is redacted before it
// is built, and the HTML renderer's tests check the output for known secrets.
package report
