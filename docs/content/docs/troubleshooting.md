---
title: Troubleshooting
description: Fixes for the problems people meet first.
weight: 7
---

## The app says Podman is missing or not running

Run `s1s prereq`. Install Podman with `brew install podman`, then `podman machine init` and `podman machine start`.

## A deployment failed

The run stays so you can read why. Use **Retry** to tear it down and deploy again, or **End** to clean up. The message names the node and shows the end of its log.

## Containers are left over

**End** removes everything a run created. If the app was killed, it removes what is left over the next time it starts. To look yourself,
everything the app creates carries the label `s1s.app=sev1situations`:

{{< cmd >}}
podman ps -a --filter label=s1s.app=sev1situations
{{< /cmd >}}

## A Verify takes a minute to fail

A check that waits for the cluster to change waits up to 60 seconds before it gives up. That is expected when the fault is still there.

## A Boundary session works only some seconds after a change

Boundary controllers and workers take a few seconds to agree after a node is restarted or revived. Verification retries for this reason.
