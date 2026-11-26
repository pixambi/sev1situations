import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, waitFor } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";

// Order matters here: the page must be listening before it opens the terminal.
const calls = vi.hoisted(() => [] as string[]);
const bus = vi.hoisted(() => {
  const handlers = new Map<string, (e: { data: unknown }) => void>();
  return {
    handlers,
    on: (name: string, fn: (e: { data: unknown }) => void) => {
      calls.push(`on ${name}`);
      handlers.set(name, fn);
      return () => handlers.delete(name);
    },
    emit: (name: string, data: unknown) => handlers.get(name)?.({ data }),
  };
});
const svc = vi.hoisted(() => ({ Open: vi.fn(), Input: vi.fn(), Resize: vi.fn(), Close: vi.fn() }));
const emulator = vi.hoisted(() => ({
  writes: [] as string[],
  options: undefined as unknown,
  onData: undefined as ((d: string) => void) | undefined,
  onResize: undefined as ((e: { cols: number; rows: number }) => void) | undefined,
  throwNext: 0,
  keyHandler: undefined as ((e: KeyboardEvent) => boolean) | undefined,
  instances: 0,
  disposed: 0,
  cols: 100,
  rows: 30,
}));

vi.mock("@wailsio/runtime", () => ({ Events: { On: bus.on } }));
vi.mock("../../bindings/github.com/pixambi/sev1situations/internal/gui/terminalservice.js", () => svc);
vi.mock("ghostty-web/ghostty-vt.wasm?url", () => ({ default: "/assets/ghostty-vt.wasm" }));
vi.mock("ghostty-web", () => ({
  Ghostty: { load: vi.fn(async (url: string) => ({ url })) },
  FitAddon: class {
    fit() {
      emulator.onResize?.({ cols: emulator.cols, rows: emulator.rows });
    }
    observeResize() {}
    dispose() {}
  },
  Terminal: class {
    cols = emulator.cols;
    rows = emulator.rows;
    constructor(options: unknown) {
      emulator.options = options;
      emulator.instances++;
    }
    keyHandler: ((e: KeyboardEvent) => boolean) | undefined;
    attachCustomKeyEventHandler(h: (e: KeyboardEvent) => boolean) {
      this.keyHandler = h;
      emulator.keyHandler = h;
    }
    loadAddon() {}
    open() {}
    focus() {}
    dispose() {
      emulator.disposed++;
    }
    write(d: Uint8Array) {
      if (emulator.throwNext > 0) {
        emulator.throwNext--;
        throw new Error("Out of bounds memory access");
      }
      emulator.writes.push(new TextDecoder().decode(d));
    }
    onData(cb: (d: string) => void) {
      emulator.onData = cb;
    }
    onResize(cb: (e: { cols: number; rows: number }) => void) {
      emulator.onResize = cb;
    }
  },
}));

import TerminalPane from "./TerminalPane.svelte";

const b64 = (s: string) => btoa(String.fromCharCode(...new TextEncoder().encode(s)));

beforeEach(() => {
  calls.length = 0;
  Object.assign(emulator, { writes: [], throwNext: 0, instances: 0, disposed: 0, cols: 100, rows: 30, onData: undefined, onResize: undefined });
  svc.Open.mockImplementation(async () => void calls.push("Open"));
  svc.Input.mockResolvedValue(undefined);
  svc.Resize.mockResolvedValue(undefined);
  svc.Close.mockResolvedValue(undefined);
  Object.defineProperty(document, "fonts", { value: { load: vi.fn().mockResolvedValue([]) }, configurable: true });
});
afterEach(() => {
  cleanup();
  bus.handlers.clear();
  vi.resetAllMocks();
});

function setup(props: Partial<{ visible: boolean }> = {}) {
  const onclose = vi.fn();
  const r = render(TerminalPane, { id: "T1", node: "node-1", visible: true, onclose, ...props });
  return { ...r, onclose };
}

describe("TerminalPane", () => {
  it("listens for output before it opens the session, and opens it at the fitted size", async () => {
    setup();
    await waitFor(() => expect(svc.Open).toHaveBeenCalledOnce());
    expect(svc.Open).toHaveBeenCalledWith("T1", "node-1", 100, 30);
    expect(calls.indexOf("on terminal:out:T1")).toBeLessThan(calls.indexOf("Open"));
    expect(calls.indexOf("on terminal:exit:T1")).toBeLessThan(calls.indexOf("Open"));
  });

  it("loads the terminal's WebAssembly from the application's assets, not from a data: URL", async () => {
    setup();
    await waitFor(() => expect(svc.Open).toHaveBeenCalled());
    const { Ghostty } = await import("ghostty-web");
    expect(Ghostty.load).toHaveBeenCalledWith("/assets/ghostty-vt.wasm");
  });

  it("writes the shell's output into the terminal, decoding the base64", async () => {
    setup();
    await waitFor(() => expect(svc.Open).toHaveBeenCalled());
    bus.emit("terminal:out:T1", b64("vault status\r\n日本語"));
    await waitFor(() => expect(emulator.writes.join("")).toBe("vault status\r\n日本語"));
  });

  it("sends what is typed to the shell", async () => {
    setup();
    await waitFor(() => expect(svc.Open).toHaveBeenCalled());
    emulator.onData?.("ls\r");
    expect(svc.Input).toHaveBeenCalledWith("T1", "ls\r");
  });

  it("sends keystrokes in order, one call at a time, so /dev cannot become /dve", async () => {
    setup();
    await waitFor(() => expect(svc.Open).toHaveBeenCalled());
    const got: string[] = [];
    let calls = 0;
    svc.Input.mockImplementation(async (_id: string, d: string) => {
      await new Promise((r) => setTimeout(r, calls++ === 0 ? 20 : 1)); // the first call is the slowest
      got.push(d);
    });
    for (const ch of "/dev/urandom") emulator.onData?.(ch);
    await waitFor(() => expect(got.join("")).toBe("/dev/urandom"));
  });

  it("tells the shell the new size once it settles, not at every step of a drag", async () => {
    setup();
    await waitFor(() => expect(svc.Open).toHaveBeenCalled());
    svc.Resize.mockClear();
    for (const [c, r] of [[90, 20], [95, 22], [120, 40]]) emulator.onResize?.({ cols: c, rows: r });
    await waitFor(() => expect(svc.Resize).toHaveBeenCalledTimes(1));
    expect(svc.Resize).toHaveBeenCalledWith("T1", 120, 40);
  });

  it("rebuilds the terminal after the library traps and keeps the output", async () => {
    setup();
    await waitFor(() => expect(svc.Open).toHaveBeenCalled());
    emulator.throwNext = 1;
    bus.emit("terminal:out:T1", b64("binary soup"));
    await waitFor(() => expect(emulator.instances).toBe(2));
    await waitFor(() => expect(emulator.writes).toContain("binary soup"));
    expect(emulator.disposed).toBe(1);
    expect(await screen.findByText("The screen was cleared")).toBeInTheDocument();
  });

  it("says when the session ends and lets the instructor close the terminal", async () => {
    const { onclose } = setup();
    await waitFor(() => expect(svc.Open).toHaveBeenCalled());
    bus.emit("terminal:exit:T1", { Reason: "" });
    expect(await screen.findByText("The session has ended")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Close this terminal" }));
    expect(onclose).toHaveBeenCalledOnce();
  });

  it("says why when the session could not be opened", async () => {
    svc.Open.mockRejectedValueOnce(new Error("node-1 has been removed, so it has no terminal."));
    setup();
    expect(await screen.findByText(/has been removed/)).toBeInTheDocument();
  });

  it("lets a keyboard user out with Shift+Escape, and passes every other key to the shell", async () => {
    setup();
    await waitFor(() => expect(svc.Open).toHaveBeenCalled());
    const leave = screen.getByRole("button", { name: "Back into the terminal" });
    expect(leave).not.toHaveFocus();

    const press = (init: KeyboardEventInit) => emulator.keyHandler!(new KeyboardEvent("keydown", { ...init, cancelable: true }));
    expect(press({ key: "Escape" })).toBe(false); // an ordinary Escape belongs to the shell
    expect(press({ key: "Tab" })).toBe(false);
    expect(press({ key: "a", shiftKey: true })).toBe(false);
    expect(leave).not.toHaveFocus();

    expect(press({ key: "Escape", shiftKey: true })).toBe(true);
    expect(leave).toHaveFocus();
  });

  it("closes the session and stops listening when the pane goes away", async () => {
    const { unmount } = setup();
    await waitFor(() => expect(svc.Open).toHaveBeenCalled());
    unmount();
    expect(svc.Close).toHaveBeenCalledWith("T1");
    expect(bus.handlers.has("terminal:out:T1")).toBe(false);
    expect(emulator.disposed).toBe(1);
  });
});
