---
title: The Cluster tab
description: Addresses, credentials and node actions.
weight: 2
---

The Cluster tab shows what is running, and what you need to reach it.

## Facts

Each product lists what an instructor needs: addresses, versions, and credentials. Credentials are shown masked and can be revealed or copied.
For Vault this includes the root token and, with a transit seal, the seal token and the recovery keys. For Boundary it includes the
admin login and password, and the IDs of the org and project scopes, the host set and the target.

## Nodes

Every node has its own details, such as its role, its state and the credentials it uses.

## Node actions

| Action | What it does |
|---|---|
| Stop and Start | Stops or starts the node's container. Data stays. |
| Remove | Deletes the container and its data, as if the machine were lost. |
| Revive | Creates a fresh node with the same name in its place. |

A Boundary database cannot be removed or revived, because its data would be gone and Boundary cannot work without it.
Reviving a Boundary controller, worker or load balancer also updates what depends on it.
