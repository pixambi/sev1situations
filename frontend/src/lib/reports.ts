import type { Summary } from "../../bindings/github.com/pixambi/sev1situations/internal/report/models.js";

/** A date and time for a list, in the instructor's own zone. */
export function whenShort(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "unknown";
  return d.toLocaleString(undefined, { day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" });
}

/** Names a report in words, for a confirmation that must say which one is meant (R-REPORT-8). */
export function describeReport(r: Pick<Summary, "scenarioName" | "trainee" | "startedAt">): string {
  return `the report of ${r.trainee} on ${r.scenarioName}, ${whenShort(r.startedAt)}`;
}
