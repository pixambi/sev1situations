---
name: define-scenario
description: Turn an idea for a training scenario into a well-defined issue. Use when asked to define, propose or plan a new Sev1 Situations scenario, before any scenario files are written.
---

# Define a scenario

A scenario starts as an issue, from `.github/ISSUE_TEMPLATE/new-scenario.yml` (R-CONTRIB-3). Do this before writing any files. This skill is plain Markdown and does not depend on a particular tool.

## Steps

1. Read `CONTRIBUTING.md`, the Scenario Standard (section 5.4 of `sev1situations-project-definition.md`) and two built-in scenarios under `scenarios/`, one of them for the same product.
2. Ask the person for the customer's report in their own words, and what the trainee should learn. Do not invent the failure.
3. Check that the failure can be made for real with the primitives that exist. If it needs behaviour the app does not have, say so: that work is a separate `feat/` branch that merges first.
4. Fill in every field of the template: name and ID, product and editions, difficulty, time, summary, objectives, initial state, failure created, symptoms with the exact error text, steps, the solution commands, pitfalls and references.
5. Make sure there is a verification that fails while the fault exists and passes once it is fixed.
6. Open the issue, or hand the person the text to open it. Do not open it without being asked.

## Rules

- The ID starts with the product and is kebab-case.
- No secret or password in the issue, even a made-up one.
- No emojis.
