---
title: Architecture
description: The layers and what each one may do.
weight: 2
---

The app is a Go backend and a Svelte frontend in one Wails window. Everything that matters is plain Go, so it can be tested without a window or a container.

## Layers

| Layer | Packages | Job |
|---|---|---|
| Runtime | `internal/runtime` | Starts, stops and removes containers, networks and volumes, runs commands in them. Podman is one implementation and a fake is another. |
| Products | `internal/product/...` | Knows one HashiCorp product: how to deploy it, how to describe it for the Cluster tab, its logs, its node actions and its primitives. |
| Primitives | `internal/primitive` | Named building blocks a scenario uses, such as `container.stop` or `vault.status`. |
| Scenarios | `internal/scenario` | Loads and validates a scenario folder. |
| Deploy | `internal/deploy` | Turns a scenario's topology into a running deployment, through the products. |
| Engine | `internal/engine` | Runs one run: the step state machine, verification, rollback, teardown and the report. |
| Reports | `internal/report` | The saved report and its single-page HTML renderer. |
| Shell | `internal/gui` | The Wails services and events. It holds no logic of its own. |
| Frontend | `frontend/` | Shows the latest state and sends the instructor's actions. It keeps no state beyond that. |

The rule that keeps this honest: a layer calls only the layers below it. The shell calls the engine, the engine calls deploy and primitives, and nothing below the shell knows Wails exists.

## One run

1. The shell asks the engine to start a scenario.
2. Deploy builds the network and every cluster in the scenario's topology, using each product's `Deploy`.
3. The engine opens the first step. Pressing **Apply breakage** runs the step's `action` primitives. **Verify** runs its `verify` primitives.
4. The shell sends a snapshot to the frontend after every change, as an event.
5. **End** removes everything the run created, then saves the report.

## Why scenarios are data

A scenario names primitives and gives them arguments. It never contains code. That is what lets you add one without touching the Go core, and what lets `s1s scenario validate` check every reference before anything runs.

Credentials are not in a scenario either. A primitive finds the cluster's credentials itself, and a template can read them as `{{ .Creds.<cluster>.<key> }}`.
