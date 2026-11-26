import { expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

const rules = ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa", "best-practice"];

/** Fails with a readable list of problems if the page breaks an accessibility rule. */
export async function audit(page: Page, what: string, options: { exclude?: string[] } = {}) {
  // Colours are measured, so the page must have stopped changing: Carbon's buttons fade between states.
  await expect(page.getByText("Checking that Podman is ready")).toHaveCount(0);
  await page.waitForTimeout(400);
  // A sandboxed frame forbids scripts, so axe cannot enter it and would wait out its timeout. Such a frame is
  // audited as a page of its own, by its own address.
  let builder = new AxeBuilder({ page }).withTags(rules);
  for (const selector of options.exclude ?? []) builder = builder.exclude(selector);
  const { violations } = await builder.analyze();
  const summary = violations.map(
    (v) => `${v.id} (${v.impact}): ${v.help}\n${v.nodes.slice(0, 3).map((n) => "    " + n.html.slice(0, 140) + "\n    -> " + (n.failureSummary ?? "").split("\n").slice(1, 3).join(" ")).join("\n")}`,
  );
  expect(summary, `${what} has accessibility problems:\n${summary.join("\n")}`).toEqual([]);
}
