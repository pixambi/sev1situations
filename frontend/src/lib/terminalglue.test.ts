import { describe, it, expect } from "vitest";
import { GuardedTerminal, InputQueue, decodeBase64, maxInputChunk, type TermCore } from "./terminalglue";

/** A fake emulator that traps on a chosen write, and from then on on every write, as ghostty-web does. */
class FakeCore implements TermCore {
  cols = 80;
  rows = 24;
  written: string[] = [];
  disposed = false;
  dead = false;
  private dataCb: (d: string) => void = () => {};
  private resizeCb: (c: number, r: number) => void = () => {};
  constructor(private readonly trapAt: number | null = null) {}

  write(data: Uint8Array) {
    if (this.dead || (this.trapAt !== null && this.written.length + 1 === this.trapAt)) {
      this.dead = true;
      throw new Error("Out of bounds memory access");
    }
    this.written.push(new TextDecoder().decode(data));
  }
  fit() {
    this.resizeCb(100, 40);
  }
  focus() {}
  dispose() {
    this.disposed = true;
  }
  onData(cb: (d: string) => void) {
    this.dataCb = cb;
  }
  onResize(cb: (c: number, r: number) => void) {
    this.resizeCb = cb;
  }
  type(s: string) {
    this.dataCb(s);
  }
}

const bytes = (s: string) => new TextEncoder().encode(s);
const settle = () => new Promise((r) => setTimeout(r, 0));

function make(traps: (number | null)[] = [null]) {
  const cores: FakeCore[] = [];
  const events = { data: [] as string[], sizes: [] as string[], rebuilt: 0 };
  const term = new GuardedTerminal(
    async () => {
      const c = new FakeCore(traps[Math.min(cores.length, traps.length - 1)]);
      cores.push(c);
      return c;
    },
    { ondata: (d) => events.data.push(d), onresize: (c, r) => events.sizes.push(`${c}x${r}`), onrebuild: () => events.rebuilt++ },
  );
  return { term, cores, events };
}

describe("GuardedTerminal", () => {
  it("writes in order and passes input and size changes on", async () => {
    const { term, cores, events } = make();
    await term.init();
    term.write(bytes("one "));
    term.write(bytes("two"));
    await settle();
    expect(cores[0].written).toEqual(["one ", "two"]);

    cores[0].type("ls\r");
    term.fit();
    expect(events.data).toEqual(["ls\r"]);
    expect(events.sizes).toEqual(["100x40"]);
    expect(term.rebuilds).toBe(0);
  });

  it("rebuilds a terminal that traps and writes the chunk again, losing nothing", async () => {
    const { term, cores, events } = make([3, null]); // the first terminal traps on its third write
    await term.init();
    for (const s of ["a", "b", "c", "d", "e"]) term.write(bytes(s));
    await settle();
    await settle();

    expect(cores).toHaveLength(2);
    expect(cores[0].disposed).toBe(true);
    expect(cores[0].written).toEqual(["a", "b"]);
    expect(cores[1].written).toEqual(["c", "d", "e"]); // the chunk that trapped is the first one the new terminal gets
    expect(term.rebuilds).toBe(1);
    expect(term.lost).toBe(0);
    expect(events.rebuilt).toBe(1);
    expect(events.sizes).toEqual(["100x40"]); // the shell is told the real size again
  });

  it("keeps the order of chunks that arrive while it is rebuilding", async () => {
    const { term, cores } = make([1, null]);
    await term.init();
    term.write(bytes("first"));
    term.write(bytes("second"));
    term.write(bytes("third"));
    await settle();
    await settle();
    expect(cores[1].written).toEqual(["first", "second", "third"]);
  });

  it("survives repeated defects", async () => {
    const { term, cores } = make([2, 2, 2, null]);
    await term.init();
    for (let i = 0; i < 6; i++) term.write(bytes(`chunk${i}`));
    for (let i = 0; i < 8; i++) await settle();
    expect(term.rebuilds).toBeGreaterThanOrEqual(3);
    const all = cores.flatMap((c) => c.written);
    for (let i = 0; i < 6; i++) expect(all).toContain(`chunk${i}`);
    expect(term.lost).toBe(0);
  });

  it("counts a chunk that even a fresh terminal rejects, and carries on", async () => {
    const { term, cores } = make([1, 1, null]); // the first two terminals trap on the first write they get
    await term.init();
    term.write(bytes("poison"));
    term.write(bytes("fine"));
    for (let i = 0; i < 5; i++) await settle();
    expect(term.lost).toBe(1);
    expect(cores.flatMap((c) => c.written)).toContain("fine");
  });

  it("does nothing after it is disposed, and disposes a terminal built too late", async () => {
    const { term, cores } = make([1, null]);
    await term.init();
    term.write(bytes("x")); // will trap and start a rebuild
    term.dispose();
    for (let i = 0; i < 4; i++) await settle();
    expect(cores.every((c) => c.disposed)).toBe(true);
    expect(term.rebuilds).toBe(0);

    term.write(bytes("after"));
    await settle();
    expect(cores.flatMap((c) => c.written)).not.toContain("after");
  });
});

describe("decodeBase64", () => {
  it("returns the original bytes, including ones that are not valid text on their own", () => {
    const original = new Uint8Array([0x41, 0xe6, 0x97, 0xa5, 0xe6, 0x9c, 0xac, 0xff, 0x00]);
    const b64 = btoa(String.fromCharCode(...original));
    expect([...decodeBase64(b64)]).toEqual([...original]);
  });
  it("decodes the empty string", () => expect(decodeBase64("")).toHaveLength(0));
});

describe("InputQueue", () => {
  const typeAll = (q: InputQueue, s: string) => [...s].forEach((ch) => q.push(ch));

  it("delivers keystrokes in the order they were typed even when the calls take different times", async () => {
    const received: string[] = [];
    // Early calls are slow and later ones fast, so unqueued they would overtake each other.
    let calls = 0;
    const send = async (d: string) => {
      const delay = calls++ === 0 ? 30 : 1;
      await new Promise((r) => setTimeout(r, delay));
      received.push(d);
    };
    const q = new InputQueue(send, () => {});
    typeAll(q, "/dev/urandom");
    await new Promise((r) => setTimeout(r, 120));
    expect(received.join("")).toBe("/dev/urandom");
  });

  it("has only one call in flight, and sends what was typed meanwhile together", async () => {
    let inFlight = 0;
    let maxInFlight = 0;
    const received: string[] = [];
    const q = new InputQueue(async (d) => {
      inFlight++;
      maxInFlight = Math.max(maxInFlight, inFlight);
      await new Promise((r) => setTimeout(r, 5));
      received.push(d);
      inFlight--;
    }, () => {});
    typeAll(q, "abcdefghij");
    await new Promise((r) => setTimeout(r, 50));
    expect(maxInFlight).toBe(1);
    expect(received.join("")).toBe("abcdefghij");
    expect(received.length).toBeLessThan(10); // not one call per character
  });

  it("sends a large paste in pieces no bigger than Go accepts, in order", async () => {
    const pieces: string[] = [];
    const q = new InputQueue(async (d) => void pieces.push(d), () => {});
    const paste = "x".repeat(maxInputChunk * 2 + 5);
    q.push(paste);
    await new Promise((r) => setTimeout(r, 10));
    expect(pieces.map((p) => p.length)).toEqual([maxInputChunk, maxInputChunk, 5]);
    expect(pieces.join("")).toBe(paste);
  });

  it("reports a failed call and still sends what comes after it", async () => {
    const received: string[] = [];
    const errors: unknown[] = [];
    let n = 0;
    const q = new InputQueue(async (d) => {
      if (n++ === 0) throw new Error("not open");
      received.push(d);
    }, (e) => errors.push(e));
    q.push("a");
    await new Promise((r) => setTimeout(r, 5));
    q.push("b");
    await new Promise((r) => setTimeout(r, 5));
    expect(errors).toHaveLength(1);
    expect(received).toEqual(["b"]);
  });
});
