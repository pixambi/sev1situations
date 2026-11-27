# Agent instructions

This file is for any coding agent. Project rules are in [`sev1situations-project-definition.md`](sev1situations-project-definition.md) (Section 1, binding) and [`CONTRIBUTING.md`](CONTRIBUTING.md). Tasks are in [`TASKS.md`](TASKS.md).

## Skills

Skills are plain Markdown instructions in `.agents/skills/<name>/SKILL.md`, each with a `name` and `description` in its front matter. Read the description to decide whether a skill applies, then follow the file. If your tool looks for skills elsewhere, point it at this folder (a symlink works) instead of copying them.

| Skill | Use it for |
|---|---|
| [`define-scenario`](.agents/skills/define-scenario/SKILL.md) | Turning an idea for a scenario into a well-defined issue. |
| [`scaffold-scenario`](.agents/skills/scaffold-scenario/SKILL.md) | Creating the branch and folder for a scenario from its issue. |
| [`build-scenario`](.agents/skills/build-scenario/SKILL.md) | Writing steps and guides, proving them on real Podman, generating the docs page. |
| [`git-conventional-commits`](.agents/skills/git-conventional-commits/SKILL.md) | Commits, branches, pull requests and release tags, using Conventional Commits. |

## Always

- Commit, push or tag only when the user asks.
- Run `task test` before committing code.
- No `Co-Authored-By:` trailers or other attribution lines in commits or pull requests.
- No emojis in code, comments, docs or commit messages (R-CONTRIB-10).
