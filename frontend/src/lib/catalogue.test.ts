import { describe, it, expect } from "vitest";
import type { ScenarioSummary } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";
import { distinct, groupByProduct, matches, noFilters, productLabel } from "./catalogue";

const scenario = (id: string, over: Partial<ScenarioSummary> = {}): ScenarioSummary => ({
  ID: id, Name: id, Summary: "", Product: "vault", Editions: ["community"], Difficulty: "beginner",
  EstimatedMinutes: 10, Tags: [], Source: "built-in", NeedsLicense: false, ...over,
});

describe("matches", () => {
  const s = scenario("a", { Product: "vault", Difficulty: "advanced", Editions: ["community", "enterprise"] });

  it("matches everything with no filters", () => expect(matches(s, noFilters)).toBe(true));

  it.each([
    [{ product: "vault" }, true],
    [{ product: "boundary" }, false],
    [{ difficulty: "advanced" }, true],
    [{ difficulty: "beginner" }, false],
    [{ edition: "enterprise" }, true],
    [{ edition: "community" }, true],
    [{ product: "vault", difficulty: "advanced", edition: "enterprise" }, true],
    [{ product: "vault", difficulty: "beginner" }, false],
  ])("filters %j", (f, want) => expect(matches(s, { ...noFilters, ...f })).toBe(want));

  it("an enterprise-only scenario does not match the community edition", () => {
    expect(matches(scenario("e", { Editions: ["enterprise"] }), { ...noFilters, edition: "community" })).toBe(false);
  });
});

describe("groupByProduct", () => {
  it("groups in the order the catalogue gives, and keeps each group's order", () => {
    const groups = groupByProduct([scenario("b1", { Product: "boundary" }), scenario("v1"), scenario("b2", { Product: "boundary" }), scenario("v2")]);
    expect(groups.map((g) => [g.product, g.scenarios.map((s) => s.ID)])).toEqual([
      ["boundary", ["b1", "b2"]],
      ["vault", ["v1", "v2"]],
    ]);
  });

  it("gives no groups for no scenarios", () => expect(groupByProduct([])).toEqual([]));
});

describe("distinct and productLabel", () => {
  it("lists each value once, sorted, flattening lists", () => {
    const list = [scenario("a", { Editions: ["enterprise", "community"] }), scenario("b", { Editions: ["community"] })];
    expect(distinct(list, (s) => s.Editions)).toEqual(["community", "enterprise"]);
  });
  it("capitalises", () => expect(productLabel("vault")).toBe("Vault"));
});
