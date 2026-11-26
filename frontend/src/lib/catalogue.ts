import type { ScenarioSummary } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";

/** What the catalogue is filtered by. An empty value matches everything. */
export interface Filters {
  product: string;
  difficulty: string;
  edition: string;
}

export const noFilters: Filters = { product: "", difficulty: "", edition: "" };

/** The order difficulties are listed in, easiest first. */
export const difficulties = ["beginner", "intermediate", "advanced"];

export function matches(s: ScenarioSummary, f: Filters): boolean {
  return (
    (f.product === "" || s.Product === f.product) &&
    (f.difficulty === "" || s.Difficulty === f.difficulty) &&
    (f.edition === "" || (s.Editions ?? []).includes(f.edition))
  );
}

/** Groups scenarios by product, keeping the order they came in, which is the order of the catalogue. */
export function groupByProduct(list: ScenarioSummary[]): { product: string; scenarios: ScenarioSummary[] }[] {
  const groups = new Map<string, ScenarioSummary[]>();
  for (const s of list) {
    groups.set(s.Product, [...(groups.get(s.Product) ?? []), s]);
  }
  return [...groups].map(([product, scenarios]) => ({ product, scenarios }));
}

/** The distinct values of a field, sorted, for a filter's choices. */
export function distinct(list: ScenarioSummary[], pick: (s: ScenarioSummary) => string | string[] | null): string[] {
  return [...new Set(list.flatMap((s) => pick(s) ?? []))].sort();
}

/** "vault" becomes "Vault". Product names are shown with a capital. */
export function productLabel(name: string): string {
  return name.charAt(0).toUpperCase() + name.slice(1);
}
