@AGENTS.md

## Claude Code

- Skills live in `.agents/skills/` and are exposed to Claude Code through the symlink `.claude/skills`. Add new skills under `.agents/skills/`, not `.claude/skills/`.
- Run `task test` before committing, and `task dev` to try the app (the updater is off in `dev` builds).
