# Contributing

The rules for this project are in [`sev1situations-project-definition.md`](sev1situations-project-definition.md), Section 1. They are binding. Cite them by ID (for example `R-DEP-1`) in reviews.

## Until 1.0.0

Commit small, working steps straight to `main` using [Conventional Commits](https://www.conventionalcommits.org/). The branch and pull request rules in the definition (R-CONTRIB-1 and R-CONTRIB-2) apply from 1.0.0.

## Before you commit

- `task test` and `task lint` pass.
- Every new Go package has a doc comment, and no emojis appear anywhere (R-QUAL-6, R-CONTRIB-10).
- Any new Go module or npm package has a row in `DEPENDENCIES.md` (R-DEP-1).
- The core (`internal/` except `internal/gui`) does not import Wails and builds with `CGO_ENABLED=0` (R-LANG-2).
- The frontend holds no business logic (R-UI-5). Regenerate the Wails bindings if a service changed (3.8).
- Behaviour that users can see is documented (R-DOC-4).

## Versions and releases

Versions follow [Semantic Versioning](https://semver.org). Before 1.0.0 a minor bump may break things; from 1.0.0 only a major bump may. A release is a git tag `vX.Y.Z` and the version in the app is `X.Y.Z` (no `v`).

- The version, commit and build date are stamped into `internal/version` by `task build`. A checkout that is not exactly on a tag builds as `dev`. `VERSION=1.2.3 task build` overrides this.
- The app updates itself from GitHub releases (`updater.go`). `dev` builds never check for updates, so `task dev` is unaffected. Run it with a stamped build to try it: `task build VERSION=0.0.1`.
- The repository is access-controlled, so the updater needs a GitHub token in `S1S_GITHUB_TOKEN` to see releases.
- To cut a release: commit, `git tag vX.Y.Z`, `task release:package`, then `gh release create vX.Y.Z bin/release/* --title vX.Y.Z --notes-file <notes>`. The zip name must keep `darwin` and the architecture in it, and `SHA256SUMS` must be published with the zip, because the updater checks the download against it.

## Picking up work

Tasks are listed in [`TASKS.md`](TASKS.md). Take them in order. Tick a task only when its acceptance line is true.

A full guide to writing scenarios, primitives and products is added with task T-113.
