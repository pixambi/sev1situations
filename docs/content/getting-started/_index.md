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

## Install

{{< steps >}}
1. Download `sev1situations-<version>-darwin-arm64.zip` (Apple silicon) or `-amd64.zip` (Intel) from the project's GitHub releases. The repository is access-controlled, so you need access to it.
2. Check the download against `SHA256SUMS` from the same release:

   ```sh
   shasum -a 256 -c SHA256SUMS --ignore-missing
   ```
3. Unzip it and move `Sev1 Situations.app` to Applications.
4. Clear the quarantine attribute, because the first releases are not signed or notarised and macOS Gatekeeper blocks them:

   ```sh
   xattr -cr "/Applications/Sev1 Situations.app"
   ```
{{< /steps >}}

{{< callout note >}}
`go install` is not supported, because the build needs the frontend and cgo.
{{< /callout >}}

To use the command line, run the program inside the bundle, or alias it:

{{< cmd >}}
alias s1s='"/Applications/Sev1 Situations.app/Contents/MacOS/s1s"'
s1s prereq
{{< /cmd >}}

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
