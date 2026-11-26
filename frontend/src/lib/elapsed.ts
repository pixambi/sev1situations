/** Formats a length of time as m:ss, or h:mm:ss from an hour on. Negative times show as zero. */
export function formatElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
}

/** Go's zero time, which a field holds until it is set. */
const zeroTime = "0001-01-01T00:00:00Z";

/** How long a run has lasted at `now`: up to when it ended, if it has. */
export function runElapsed(startedAt: string, endedAt: string, now: number): number {
  const start = Date.parse(startedAt);
  if (!startedAt || startedAt === zeroTime || Number.isNaN(start)) return 0;
  const end = endedAt && endedAt !== zeroTime ? Date.parse(endedAt) : now;
  return end - start;
}
