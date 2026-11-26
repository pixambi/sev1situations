import { describe, it, expect } from "vitest";
import { actionLabel, nodeTagType } from "./cluster";

describe("cluster helpers", () => {
  it.each([["stop", "Stop"], ["start", "Start"], ["remove", "Remove"], ["revive", "Revive"], ["pause", "Pause"]])("%s is %s", (a, want) =>
    expect(actionLabel(a)).toBe(want));
  it.each([["running", "green"], ["stopped", "gray"], ["removed", "red"], ["whatever", "red"]])("%s is %s", (s, want) =>
    expect(nodeTagType(s)).toBe(want));
});
