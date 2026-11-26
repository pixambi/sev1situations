import { describe, it, expect } from "vitest";
import { describeReport, whenShort } from "./reports";

describe("whenShort", () => {
  it("formats a date and time", () => {
    const s = whenShort("2026-11-26T09:15:30Z");
    expect(s).toMatch(/2026/);
    expect(s).toMatch(/\d{1,2}:\d{2}|\d{1,2}\s?(AM|PM)/i);
  });
  it("says so when the date is unreadable", () => expect(whenShort("not a date")).toBe("unknown"));
});

describe("describeReport", () => {
  it("names the trainee, the scenario and the day, so a confirmation says which one is meant", () => {
    const text = describeReport({ trainee: "Grace Hopper", scenarioName: "Lost Quorum", startedAt: "2026-11-26T09:15:30Z" });
    expect(text).toContain("Grace Hopper");
    expect(text).toContain("Lost Quorum");
    expect(text).toContain("2026");
  });
});
