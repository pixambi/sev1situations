import { describe, it, expect } from "vitest";
import { compile, levelOf, maxPatternLength, noQuery } from "./logfilter";

describe("levelOf", () => {
  it.each([
    ["2026-11-26T09:00:00.000Z [ERROR] core: failed to unseal", "error"],
    ["2026-11-26T09:00:00.000Z [WARN]  core: no seal", "warn"],
    ["2026-11-26T09:00:00.000Z [INFO]  core: ok", "info"],
    ['{"time":"x","level":"error","msg":"boom"}', "error"],
    ['{"level":"warn"}', "warn"],
    ["level=fatal msg=bye", "error"],
    ["panic: runtime error", "error"],
    ["WARNING: low disk", "warn"],
    ["a perfectly ordinary line", "info"],
    ["terror, errors and warnings are only parts of words here", "info"],
  ])("%s -> %s", (line, want) => expect(levelOf(line)).toBe(want));
});

describe("compile: plain text", () => {
  it("keeps everything with no query", () => {
    expect(compile(noQuery).keeps("anything")).toBe(true);
    expect(compile(noQuery).parts("anything")).toEqual([{ text: "anything", match: false }]);
  });

  it("matches without regard to case and treats punctuation as text", () => {
    const m = compile({ ...noQuery, text: "a.b" });
    expect(m.keeps("xx A.B yy")).toBe(true);
    expect(m.keeps("xx axb yy")).toBe(false); // the dot is not a wildcard
  });

  it("splits a line around each match for highlighting", () => {
    expect(compile({ ...noQuery, text: "err" }).parts("an Error and an err")).toEqual([
      { text: "an ", match: false }, { text: "Err", match: true }, { text: "or and an ", match: false }, { text: "err", match: true },
    ]);
  });
});

describe("compile: regular expressions", () => {
  it("matches the expression", () => {
    const m = compile({ text: "peer-\\d+", regex: true, level: "all" });
    expect(m.keeps("joined peer-12")).toBe(true);
    expect(m.keeps("joined peer-x")).toBe(false);
    expect(m.parts("a peer-1 b").map((p) => p.text)).toEqual(["a ", "peer-1", " b"]);
  });

  it("says why a bad expression cannot be used, and keeps every line meanwhile", () => {
    const m = compile({ text: "(unclosed", regex: true, level: "all" });
    expect(m.error).toMatch(/not a valid regular expression/);
    expect(m.keeps("whatever")).toBe(true);
  });

  it("refuses a very long expression", () => {
    expect(compile({ text: "a".repeat(maxPatternLength + 1), regex: true, level: "all" }).error).toMatch(/at most/);
  });

  it("does not loop on a pattern that can match nothing", () => {
    const m = compile({ text: "x*", regex: true, level: "all" });
    expect(m.parts("abxxc").map((p) => p.text).join("")).toBe("abxxc");
  });

  it("gives the same answer when asked twice (no state is left in the expression)", () => {
    const m = compile({ ...noQuery, text: "ok" });
    expect([m.keeps("ok"), m.keeps("ok"), m.keeps("ok")]).toEqual([true, true, true]);
  });
});

describe("compile: level", () => {
  const lines = ["[INFO] fine", "[WARN] hmm", "[ERROR] bad"];
  const kept = (level: "all" | "warnings" | "errors") => lines.filter(compile({ ...noQuery, level }).keeps);

  it("keeps warnings and errors, or only errors", () => {
    expect(kept("all")).toHaveLength(3);
    expect(kept("warnings")).toEqual(["[WARN] hmm", "[ERROR] bad"]);
    expect(kept("errors")).toEqual(["[ERROR] bad"]);
  });

  it("combines with the text", () => {
    const m = compile({ text: "bad", regex: false, level: "errors" });
    expect(m.keeps("[ERROR] bad")).toBe(true);
    expect(m.keeps("[ERROR] other")).toBe(false);
    expect(m.keeps("[INFO] bad")).toBe(false);
  });
});
