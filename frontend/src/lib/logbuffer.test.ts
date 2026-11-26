import { describe, it, expect } from "vitest";
import { LogBuffer, maxLines } from "./logbuffer";

const numbered = (from: number, to: number) => Array.from({ length: to - from + 1 }, (_, i) => `line ${from + i}`);

describe("LogBuffer", () => {
  it("keeps lines in order", () => {
    const b = new LogBuffer(10);
    b.push(["a", "b"]);
    b.push(["c"]);
    expect([...b.lines]).toEqual(["a", "b", "c"]);
  });

  it("caps the lines, drops the oldest and counts them", () => {
    const b = new LogBuffer(5);
    b.push(numbered(1, 3));
    b.push(numbered(4, 9));
    expect([...b.lines]).toEqual(numbered(5, 9));
    expect(b.dropped).toBe(4);
  });

  it("copes with a single batch larger than the cap", () => {
    const b = new LogBuffer(3);
    b.push(numbered(1, 100));
    expect([...b.lines]).toEqual(numbered(98, 100));
    expect(b.dropped).toBe(97);
  });

  it("holds new lines aside while paused, and shows them on resume", () => {
    const b = new LogBuffer(10);
    b.push(["a"]);
    b.pause();
    b.push(["b", "c"]);
    expect([...b.lines]).toEqual(["a"]);
    expect(b.pending).toBe(2);
    expect(b.paused).toBe(true);

    b.resume();
    expect([...b.lines]).toEqual(["a", "b", "c"]);
    expect(b.pending).toBe(0);
    expect(b.paused).toBe(false);
  });

  it("caps the lines waiting while paused as well", () => {
    const b = new LogBuffer(4);
    b.pause();
    b.push(numbered(1, 10));
    expect(b.pending).toBe(4);
    b.resume();
    expect([...b.lines]).toEqual(numbered(7, 10));
    expect(b.dropped).toBe(6);
  });

  it("clears everything", () => {
    const b = new LogBuffer(2);
    b.push(numbered(1, 5));
    b.pause();
    b.push(["x"]);
    b.clear();
    expect([...b.lines]).toEqual([]);
    expect(b.pending).toBe(0);
    expect(b.dropped).toBe(0);
  });

  it("defaults to a cap that keeps memory bounded", () => {
    const b = new LogBuffer();
    b.push(numbered(1, maxLines + 500));
    expect(b.lines).toHaveLength(maxLines);
    expect(b.dropped).toBe(500);
  });
});
