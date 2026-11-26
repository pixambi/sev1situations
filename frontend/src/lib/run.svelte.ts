import { Events } from "@wailsio/runtime";
import { Current } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/runservice.js";
import type { Snapshot } from "../../bindings/github.com/pixambi/sev1situations/internal/engine/models.js";

/**
 * The run as Go last reported it. The frontend keeps no state of its own about the
 * run (R-UI-5): this holds the newest snapshot, and everything shown is derived from it.
 */
export const run: { snapshot: Snapshot | null } = $state({ snapshot: null });

/** True while a run exists, whether it is deploying, in progress or has failed. */
export function runActive(s: Snapshot | null): boolean {
  return s !== null && s.Status !== "idle";
}

/**
 * Loads the current run and then follows run:state. It returns a function that stops
 * following. A snapshot that arrives before the first load finishes wins, because it is newer.
 */
export function followRun(): () => void {
  let live = false;
  const stop = Events.On("run:state", (e) => {
    live = true;
    run.snapshot = e.data as Snapshot;
  });
  Current()
    .then((s) => {
      if (!live) run.snapshot = s;
    })
    .catch(() => {
      // Nothing is connected yet. The first run:state or the next load fills it in.
    });
  return stop;
}
