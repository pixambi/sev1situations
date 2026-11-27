---
title: Adding a product
description: Support another HashiCorp product.
weight: 6
---

A product is a Go package that knows how to run one thing. Vault and Boundary are the two that exist, and Boundary is a good one to read, because it builds a whole deployment of several parts.

## What a product provides

| Method | What it does |
|---|---|
| `Name`, `Editions` | Its name, such as `boundary`, and the editions it has. |
| `BaseImage`, `Versions` | The image it runs in, and the versions an instructor can choose. |
| `Deploy` | Builds the whole deployment and returns the nodes and the credentials. |
| `Describe` | The Cluster tab: facts and, for each node, its details. Credentials go here and nowhere else. |
| `LogStreams` | The logs each node offers. |
| `NodeActions`, `Revive` | What the instructor can do to a node, and how a removed node comes back. |
| `Primitives` | The verbs its scenarios use. |
| `SettingsFields` | The settings it needs, such as a license field. The settings page picks these up. |

## Rules

- Everything is built through the runtime, never by calling a container tool directly.
- Wait for the real thing to be ready, not for a timer. Readiness has to be a question the product can answer.
- A secret is generated per run, kept in the deployment's credentials, shown only on the Cluster tab, and never written to a log or a report.
- A deployment may have parts that are not the product itself, such as a database or a load balancer. Build them from their own images inside the product, and name them so the Cluster tab can tell them apart.
- Describe must never fail the tab. A node that is stopped or removed is shown as such.
- Add the product to `DefaultProducts` so the app ships it.

## Proving it

Write unit tests against a simulator, and write an integration test that deploys it on real Podman and plays a scenario through the engine. A product is not done until that has passed on a machine.
