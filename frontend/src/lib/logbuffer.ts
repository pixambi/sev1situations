/** What a log:<id> event carries. Go declares it, but only events use it, so the bindings do not. */
export interface LogBatch {
  Lines: string[] | null;
  Ended: boolean;
  Reason: string;
}

/** How many lines the view keeps. Older ones are dropped, so a busy log cannot use all the memory. */
export const maxLines = 5000;

/**
 * The lines of one log as the page holds them, capped at `cap`. While paused, new lines wait on the
 * side, also capped, so the instructor can read without the view moving, and catch up afterwards.
 */
export class LogBuffer {
  private shown: string[] = [];
  private waiting: string[] = [];
  private isPaused = false;
  /** How many lines have been dropped from the front because of the cap. */
  dropped = 0;

  constructor(private readonly cap: number = maxLines) {}

  get lines(): readonly string[] {
    return this.shown;
  }
  get paused(): boolean {
    return this.isPaused;
  }
  /** How many lines are waiting while paused. */
  get pending(): number {
    return this.waiting.length;
  }

  push(lines: readonly string[]): void {
    if (this.isPaused) {
      this.waiting = this.capped(this.waiting.concat(lines));
    } else {
      this.shown = this.capped(this.shown.concat(lines));
    }
  }

  pause(): void {
    this.isPaused = true;
  }

  /** Resumes, and shows what arrived while paused. */
  resume(): void {
    this.isPaused = false;
    if (this.waiting.length > 0) {
      const w = this.waiting;
      this.waiting = [];
      this.shown = this.capped(this.shown.concat(w));
    }
  }

  clear(): void {
    this.shown = [];
    this.waiting = [];
    this.dropped = 0;
  }

  private capped(all: string[]): string[] {
    if (all.length <= this.cap) return all;
    this.dropped += all.length - this.cap;
    return all.slice(all.length - this.cap);
  }
}
