---
title: The Scenario Standard
description: What every scenario defines.
weight: 4
---

Every scenario, built-in or your own, defines the following. `s1s scenario validate` checks it.

| Field | What it is |
|---|---|
| `id` | Stable, unique and kebab-case. Start it with the product, for example `vault-broken-kms`. |
| `name`, `summary` | Shown in the catalogue. The summary is one sentence. |
| `product`, `editions` | The main product, and whether it runs on community, Enterprise or both. |
| `difficulty` | `beginner`, `intermediate` or `advanced`. |
| `estimatedMinutes` | How long a session takes. |
| `tags` | For grouping. |
| `objectives` | What the trainee should gain. |
| `prerequisites` | Knowledge and tools needed. |
| `parameters` | What the Start form lets the instructor choose. |
| `initialState` | What the deployment provides before step one. |
| `failureCreated` | What is broken, the symptoms, and whether it is recoverable. |
| `steps` | Ordered, each with a guide. |
| `successCriteria` | What fixed means. |
| `pitfalls` | Common mistakes and their symptoms. |
| `references` | Links to product documentation. |

Each step's guide has a briefing for the instructor, hints given a little at a time, a solution, and references.

A scenario is also held to a quality bar by a test that every built-in scenario must pass, so a thin one is caught before it is merged.
