---
title: Logs and terminals
description: Read a node's logs and run commands on it.
weight: 3
---

## Logs

The Logs tab streams a node's log files live. A node can have more than one stream, for example Boundary's load balancer and database
have their own.

## Terminals

The Terminals tab opens a shell inside a node's container, so you type exactly what the trainee would. You can open several.

Inside the containers:

| Product | Set this first |
|---|---|
| Vault | `export VAULT_ADDR=http://127.0.0.1:8200` |
| Boundary | `export BOUNDARY_ADDR=http://127.0.0.1:9200` |

Credentials are on the Cluster tab.
