import { describe, it, expect } from "vitest";
import { formatElapsed, runElapsed } from "./elapsed";

describe("formatElapsed", () => {
  it.each([
    [0, "0:00"],
    [999, "0:00"],
    [1000, "0:01"],
    [61_000, "1:01"],
    [599_000, "9:59"],
    [3_600_000, "1:00:00"],
    [3_725_000, "1:02:05"],
    [-5000, "0:00"],
  ])("%d ms is %s", (ms, want) => expect(formatElapsed(ms)).toBe(want));
});

describe("runElapsed", () => {
  const start = "2026-11-26T09:00:00Z";
  const at = (s: string) => Date.parse(s);

  it("counts up to now while the run is going", () => {
    expect(runElapsed(start, "0001-01-01T00:00:00Z", at("2026-11-26T09:01:30Z"))).toBe(90_000);
  });
  it("stops at the end time once the run has ended", () => {
    expect(runElapsed(start, "2026-11-26T09:10:00Z", at("2026-11-26T12:00:00Z"))).toBe(600_000);
  });
  it("is zero before there is a start time", () => {
    expect(runElapsed("", "", Date.now())).toBe(0);
    expect(runElapsed("0001-01-01T00:00:00Z", "", Date.now())).toBe(0);
    expect(runElapsed("not a time", "", Date.now())).toBe(0);
  });
});
