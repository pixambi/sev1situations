/** The part of a terminal emulator the glue needs. The real one wraps ghostty-web. */
export interface TermCore {
  readonly cols: number;
  readonly rows: number;
  write(data: Uint8Array): void;
  /** Fits the terminal to its container, which reports the new size through onResize. */
  fit(): void;
  focus(): void;
  dispose(): void;
  onData(cb: (data: string) => void): void;
  onResize(cb: (cols: number, rows: number) => void): void;
}

/** Builds a new, empty terminal in the same container. */
export type CoreFactory = () => Promise<TermCore>;

export interface Handlers {
  /** What the instructor typed or pasted. */
  ondata: (data: string) => void;
  /** The size in cells after a fit, for the shell. */
  onresize: (cols: number, rows: number) => void;
  /** Called after the terminal was rebuilt, which clears its screen. */
  onrebuild?: () => void;
}

/**
 * A terminal that survives ghostty-web's defect (spike W2): certain output, such as random binary or
 * a flood of grapheme clusters, makes it trap, and after that it throws on every write and cannot be
 * reset. When a write throws, this disposes the dead terminal, builds a fresh one in its place and
 * writes the chunk again. The screen is cleared by that, but the shell in the container is not
 * affected. Writes are queued in order, because building a terminal is asynchronous.
 */
export class GuardedTerminal {
  private core: TermCore | null = null;
  private queue: Promise<void> = Promise.resolve();
  private disposed = false;

  /** How many times the terminal was rebuilt. */
  rebuilds = 0;
  /** How many chunks were dropped because even a fresh terminal rejected them. */
  lost = 0;

  constructor(
    private readonly factory: CoreFactory,
    private readonly handlers: Handlers,
  ) {}

  /** Builds the first terminal. */
  async init(): Promise<void> {
    this.core = await this.build();
  }

  /** Writes output from the shell. It never throws. */
  write(data: Uint8Array): void {
    this.queue = this.queue.then(() => this.writeNow(data));
  }

  fit(): void {
    this.core?.fit();
  }

  /** The terminal's size in cells, or the default before it exists. */
  size(): { cols: number; rows: number } {
    return { cols: this.core?.cols || 80, rows: this.core?.rows || 24 };
  }

  focus(): void {
    this.core?.focus();
  }

  dispose(): void {
    this.disposed = true;
    this.core?.dispose();
    this.core = null;
  }

  private async build(): Promise<TermCore> {
    const core = await this.factory();
    if (this.disposed) {
      core.dispose();
      return core;
    }
    core.onData((d) => this.handlers.ondata(d));
    core.onResize((c, r) => this.handlers.onresize(c, r));
    return core;
  }

  private async writeNow(data: Uint8Array): Promise<void> {
    if (this.disposed || !this.core) return;
    try {
      this.core.write(data);
      return;
    } catch {
      // The terminal is dead. Replace it and try the chunk once more.
    }
    try {
      this.core.dispose();
    } catch {
      // A dead terminal may fail to dispose too. It is being replaced either way.
    }
    this.core = await this.build();
    if (this.disposed) return;
    this.rebuilds++;
    this.core.fit(); // a new terminal starts at the default size, so the shell is told the real one
    try {
      this.core.write(data);
    } catch {
      this.lost++;
    }
    this.handlers.onrebuild?.();
  }
}

/** Decodes the base64 Go sends terminal output in. */
export function decodeBase64(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

/** The most one Input call carries. Go refuses more, and a paste larger than this is sent in pieces. */
export const maxInputChunk = 64 * 1024;

/**
 * Sends what the instructor types to the shell in the order it was typed. Each keystroke would otherwise
 * be its own call, and calls to Go can overtake one another, which turned `/dev` into `/dve` in a real
 * terminal. Only one call is in flight at a time, and what is typed meanwhile goes out together in the
 * next one, so a fast typist or a paste costs a few calls and not one per character.
 */
export class InputQueue {
  private pending = "";
  private flushing = false;

  constructor(
    private readonly send: (data: string) => Promise<void>,
    private readonly onerror: (err: unknown) => void,
  ) {}

  push(data: string): void {
    this.pending += data;
    if (!this.flushing) void this.flush();
  }

  private async flush(): Promise<void> {
    this.flushing = true;
    try {
      while (this.pending !== "") {
        const chunk = this.pending.slice(0, maxInputChunk);
        this.pending = this.pending.slice(chunk.length);
        try {
          await this.send(chunk);
        } catch (err) {
          this.onerror(err); // a failed call is reported, and what was typed after it still goes
        }
      }
    } finally {
      this.flushing = false;
    }
  }
}
