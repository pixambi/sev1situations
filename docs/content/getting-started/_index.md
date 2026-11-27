---
title: Getting started
description: Install the app, start Podman and run your first scenario.
weight: 1
---

## Prerequisites

- A Mac with Apple silicon or Intel, and [Podman](https://podman.io/) installed. `brew install podman` works.
- A running Podman machine.

{{< cmd >}}
podman machine init
podman machine start
{{< /cmd >}}

On first launch the app checks Podman and tells you what is missing.

## Run the Vault Sandbox

{{< steps >}}
1. Open the app and choose **Vault Sandbox** in the scenario list.
2. Enter your name and the trainee's name, and press **Start**. The first deployment downloads the Vault binary and image, so it takes longer.
3. Open the **Cluster** tab for the addresses and the root token, and the **Terminals** tab to run commands on a node.
4. Press **End** when you are done. The containers and network are removed.
{{< /steps >}}

{{< callout tip >}}
A sandbox has no fault. Use it to learn the run page before you run a scenario that breaks something.
{{< /callout >}}
