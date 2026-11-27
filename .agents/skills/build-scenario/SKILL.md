---
name: build-scenario
description: Write the steps and guides of a Sev1 Situations scenario, prove them on real Podman, and generate its docs page. Use after a scenario has been scaffolded.
---

# Build a scenario

Follow `scaffold-scenario` first. This skill is plain Markdown and does not depend on a particular tool. The guide for people is `docs/content/contributing/writing-a-scenario.md`.

## Steps

1. Write the steps: a breakage with an `action` and a `verify` that proves the fault is there, then a verification that fails now and passes after the fix. Use `wait.until` for anything that takes time.
2. Write each guide with `## Briefing`, `## Hints`, `## Solution` and `## References`. The briefing says what to tell the trainee and what they should see. The solution lists commands that were actually typed.
3. Run `s1s scenario validate` and `go test ./internal/app`. The second runs the quality test that every built-in scenario must pass.
4. Play it for real: add an integration test beside the others in `internal/app` that applies the breakage, checks the symptom, shows a verify that fails, types the guide's solution into the containers, and shows the verify pass. Run it with `go test -tags integration -run <Name>` on a machine with Podman. A scenario is not done until this has passed (R-CONTRIB-4).
5. Run `task docs:generate` and include the generated pages. CI fails if they are out of date.
6. Run `task test`.

## Rules

- Make every check safe to run again.
- A check that expects a refusal must not pass because something was down.
- Say in the pull request that the real run passed, and what it showed. Do not say so if it did not.
- Never commit, push or open a pull request unless asked.
