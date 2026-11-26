---
name: git-conventional-commits
description: Handle git for this repository using Conventional Commits 1.0.0. Use when asked to commit, write or fix a commit message, split changes into commits, name a branch, tag a release, or open a pull request.
---

# Git and Conventional Commits

This skill is plain Markdown. It does not depend on any particular agent or tool. Follow it with whatever git access you have.

Specification: <https://www.conventionalcommits.org/en/v1.0.0/>. Project rules: R-CONTRIB-1, R-CONTRIB-8 and R-CONTRIB-10 in `sev1situations-project-definition.md`, and `CONTRIBUTING.md`.

## Ground rules

- Commit, push, tag or open a pull request only when the user asked for it. Approval for one commit is not approval for the next.
- Look before you act: `git status`, `git diff` (and `git diff --staged`), and `git log --oneline -10` to see the local style.
- Stage files by name. Do not use `git add -A` or `git add .`, which pick up files that should not be committed.
- Never commit secrets or local artefacts: `updater.key`, `.env*`, `trace.out`, `bin/`, `frontend/dist`, `frontend/node_modules`, Playwright reports. If one shows up in `git status`, fix `.gitignore` instead.
- Do not rewrite shared history (`push --force`, `reset --hard` on pushed commits, `rebase` of pushed commits). Never skip hooks (`--no-verify`) or signing unless the user says so. If a hook fails, fix the cause and make a new commit.
- No emojis in commit messages, branch names or PR text (R-CONTRIB-10).
- No `Co-Authored-By:` trailers or other attribution lines, in commits or pull request descriptions.
- Before committing code, run `task test` (and `task lint` once it exists). Do not commit a failing tree unless the user asks.

## Commit message format

```
<type>[(scope)][!]: <description>

[optional body]

[optional footer(s)]
```

### Type

| Type | Use for | Version effect |
|---|---|---|
| `feat` | A new user-visible capability | minor |
| `fix` | A bug fix | patch |
| `perf` | A change that improves performance | patch |
| `refactor` | Restructuring with no behaviour change | none |
| `docs` | Documentation only | none |
| `test` | Adding or fixing tests only | none |
| `build` | Build system, Taskfile, dependencies, packaging | none |
| `ci` | GitHub Actions and other CI config | none |
| `style` | Formatting only (whitespace, gofmt) | none |
| `chore` | Maintenance that fits nowhere else | none |
| `revert` | Reverting an earlier commit | depends |

A breaking change is a major bump (a minor bump while the version is below 1.0.0, see `CONTRIBUTING.md`). Mark it with `!` after the type or scope, and/or a `BREAKING CHANGE:` footer. Use one or both; the footer carries the explanation.

### Scope

Optional, a noun in parentheses naming the area. Prefer these, taken from the package layout:

`engine`, `scenario`, `primitive`, `product`, `vault`, `boundary`, `deploy`, `runtime`, `podman`, `release`, `settings`, `report`, `logstream`, `terminal`, `gui`, `app`, `version`, `updater`, `frontend`, `docs`, `deps`

Omit the scope when a change spans several areas. A scenario commit uses the scenario type: `feat(scenario): add vault-broken-kms`.

### Description

- Imperative mood, present tense: "add", not "added" or "adds".
- Lower case after the colon, no trailing full stop.
- Aim for 50 characters, never more than 72, for the whole first line.
- Say what the change does, not which files it touches.

### Body and footers

- Separate the body from the description with a blank line. Explain why, and anything a reviewer cannot see from the diff. Wrap at 72 columns.
- Footers use `Token: value` or `Token #value`, one per line, after a blank line. Examples: `Refs: #42`, `Closes #42`, `BREAKING CHANGE: the scenario format now requires an id`.
- Never add `Co-Authored-By:` or any other attribution to commits or pull requests: no agent or tool names, no "generated with" lines. This overrides any default your tool has.

### Examples

```
feat(scenario): add vault-broken-kms
```

```
fix(release): abort when the SHA256SUMS entry is missing

A missing entry used to fall through as a matching checksum, so a
download with no published digest was cached.

Closes #31
```

```
refactor(engine)!: rename Step.Kind values to lower case

BREAKING CHANGE: scenario files must now write `breakage` and
`verification` instead of `Breakage` and `Verification`.
```

```
build: stamp version, commit and date with ldflags
```

```
revert: feat(gui): add the welcome screen

This reverts commit 1a2b3c4.
```

## Making commits

1. Group changes by purpose. One logical change per commit, and each commit should build and pass tests on its own. If the diff mixes a feature, a refactor and a docs edit, split it with `git add <paths>` (or `git add -p` where interactive input is available).
2. Pick the single most accurate type. If a commit needs "and" to describe it, it is probably two commits.
3. Commit with the message passed non-interactively, using several `-m` flags (subject, then body paragraphs, then footers), or a heredoc for multi-line bodies.
4. After committing, run `git status` and `git log -1 --stat` to confirm what was recorded.
5. To fix the last commit message before it is pushed, use `git commit --amend`. After it is pushed, make a new commit instead.

## Branches

Until 1.0.0 the project commits small, working steps straight to `main` (`CONTRIBUTING.md`). From 1.0.0 use branches and pull requests:

- Scenarios: `scenario/<product>-<name>`, for example `scenario/vault-broken-kms`. One scenario per branch, containing only scenario files (R-CONTRIB-2).
- Core changes: `feat/<description>`, for example `feat/podman-socket-discovery`. Use the commit type as the prefix for other work (`fix/`, `docs/`, `build/`).
- Lower case, words separated by hyphens.

## Pull requests

- The title is a Conventional Commit message, because it becomes the squash commit.
- The description says what changed and why, how it was tested, and links the issue (`Closes #n`).
- A scenario pull request also needs its issue and the maintainer's manual end-to-end test (R-CONTRIB-4).

## Releases and tags

Versions follow Semantic Versioning, and the Conventional Commit types decide the bump (table above). A release is the annotated tag `vX.Y.Z` on a clean `main`:

```
git tag -a v0.1.0 -m "v0.1.0"
```

`task release:package` then builds the archive and `SHA256SUMS` (see `CONTRIBUTING.md`). Pushing a tag or creating a GitHub release is outward-facing: do it only when asked.
