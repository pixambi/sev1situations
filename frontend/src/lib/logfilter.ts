/** How serious a log line is, as far as its text says. Anything unmarked is "info". */
export type Level = "error" | "warn" | "info";

/** Which lines the view keeps by level. */
export type LevelFilter = "all" | "warnings" | "errors";

const errorWord = /\b(?:ERROR|ERR|FATAL|PANIC|CRITICAL)\b|"level"\s*:\s*"(?:error|fatal|panic)"/i;
const warnWord = /\b(?:WARN|WARNING)\b|"level"\s*:\s*"warn(?:ing)?"/i;

/** Reads a line's level from the common spellings: [ERROR], level=warn, "level":"error". */
export function levelOf(line: string): Level {
  if (errorWord.test(line)) return "error";
  if (warnWord.test(line)) return "warn";
  return "info";
}

/** The longest pattern accepted, which keeps a mistyped expression from being enormous. */
export const maxPatternLength = 200;

export interface Query {
  text: string;
  /** Treat the text as a regular expression. */
  regex: boolean;
  level: LevelFilter;
}

export const noQuery: Query = { text: "", regex: false, level: "all" };

/** A compiled query: whether a line passes, and where it matches. */
export interface Matcher {
  /** Why the query cannot be used, for a bad regular expression. Empty when fine. */
  error: string;
  keeps(line: string): boolean;
  /** The parts of the line, in order, each saying whether it is a match. */
  parts(line: string): { text: string; match: boolean }[];
}

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/** Compiles a query. Plain text matches without regard to case. A bad expression gives an error and keeps everything. */
export function compile(q: Query): Matcher {
  let re: RegExp | null = null;
  let error = "";
  if (q.text !== "") {
    if (q.regex && q.text.length > maxPatternLength) {
      error = `The expression can be at most ${maxPatternLength} characters.`;
    } else {
      try {
        re = new RegExp(q.regex ? q.text : escape(q.text), "gi");
      } catch (e) {
        error = `That is not a valid regular expression: ${(e as Error).message.replace(/^Invalid regular expression: /, "")}`;
      }
    }
  }

  const levelOk = (line: string) => {
    if (q.level === "all") return true;
    const lv = levelOf(line);
    return q.level === "errors" ? lv === "error" : lv !== "info";
  };

  return {
    error,
    keeps(line) {
      if (!levelOk(line)) return false;
      if (!re) return true;
      re.lastIndex = 0;
      return re.test(line);
    },
    parts(line) {
      if (!re) return [{ text: line, match: false }];
      const out: { text: string; match: boolean }[] = [];
      let at = 0;
      re.lastIndex = 0;
      for (let m = re.exec(line); m !== null; m = re.exec(line)) {
        if (m[0] === "") {
          re.lastIndex++; // an empty match would loop for ever
          continue;
        }
        if (m.index > at) out.push({ text: line.slice(at, m.index), match: false });
        out.push({ text: m[0], match: true });
        at = m.index + m[0].length;
      }
      if (at < line.length) out.push({ text: line.slice(at), match: false });
      return out.length > 0 ? out : [{ text: line, match: false }];
    },
  };
}
