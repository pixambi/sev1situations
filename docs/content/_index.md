---
title: Sev1 Situations
tagline: Practise severity-1 incident calls against real HashiCorp Vault and Boundary, on your own Mac.
---

Sev1 Situations is a desktop app for instructors. It starts a real Vault or Boundary deployment in Podman containers, breaks it
in a way you chose, and guides you through the call while a trainee finds and fixes the fault. When it ends it writes a report.

## How it works

{{< steps >}}
1. Choose a scenario and name the trainee. The app deploys real software in containers on your machine.
2. Press **Apply breakage**. The fault is real: nodes are stopped, tokens revoked, grants removed.
3. Coach the trainee with the hints. Open a terminal on any node from the app.
4. Press **Verify** when they say it is fixed. The app checks the cluster, not the trainee's word.
5. End the run. Everything is removed and a report is saved.
{{< /steps >}}

## Who it is for

Support engineers and trainers who want a trainee to practise on a real cluster that is really broken, without a shared
environment to clean up afterwards.
