---
title: Writing a scenario
description: Add a scenario by hand, with no special tools.
weight: 3
---

This takes you from nothing to a scenario you can run. Use the [Scenario Standard]({{< relref "scenario-standard" >}}) as the list of what to fill in.

{{< steps >}}
1. **Define it first.** Open an issue from the new-scenario template. Say what is broken, what the trainee should see, and what fixed means.
2. **Create the folder.** The command writes a valid starting point:

   ```sh
   s1s scenario new vault-my-scenario --product vault
   ```

   A scenario lives in a folder with a `scenario.yaml` and a `steps/` folder of Markdown guides. Your own scenarios go in the application data directory, and a built-in one goes under `scenarios/<product>/<name>/` in the repository.
3. **Fill in `scenario.yaml`.** Every field of the Standard, then the `steps`.
4. **Write each step's guide.** A guide has a `## Briefing`, `## Hints`, `## Solution` and `## References` section.
5. **Validate it.** The errors name the file and the field:

   ```sh
   s1s scenario validate path/to/your-scenario
   ```
6. **Run it for real** on Podman, and play it as an instructor would. This is required before a scenario merges.
{{< /steps >}}

## The shape of a scenario

{{< cmd >}}
id: vault-my-scenario
name: My Scenario
summary: One sentence for the catalogue.
product: vault
editions: [community]
difficulty: beginner
estimatedMinutes: 15
tags: [vault]
objectives:
  - What the trainee gains.
prerequisites:
  - What they should already know.
parameters:
  - id: version
    type: version
    product: vault
    label: Vault version
    help: The version to run. It defaults to the newest.
topology:
  clusters:
    - name: primary
      product: vault
      nodes: 3
      storage: raft
initialState: What exists before step one.
failureCreated: What is broken, and that it can be recovered.
successCriteria: What fixed means.
pitfalls:
  - A common mistake.
references:
  - title: A documentation page
    url: https://developer.hashicorp.com/vault/docs
steps:
  - id: break-it
    kind: breakage
    title: Break it
    guide: steps/break-it.md
    resetPoint: true
    action:
      - primitive: container.stop
        args: { node: node-2 }
    verify:
      - primitive: vault.status
        args: { node: node-2, reachable: false }
  - id: fixed
    kind: verification
    title: Fixed
    guide: steps/fixed.md
    verify:
      - primitive: wait.until
        args:
          interval: 3s
          check:
            primitive: vault.status
            args: { node: node-2, sealed: false }
{{< /cmd >}}

## Steps

- A **breakage** step has an `action` that makes the fault and a `verify` that proves the fault is there. The instructor cannot move on until it passes.
- A **verification** step has only a `verify`. It must fail while the fault exists and pass once it is fixed. Check both.
- `resetPoint: true` lets the instructor roll back to the start of that step.
- A scenario with no steps is an open-ended sandbox.

## Writing checks that do not lie

- Check the thing the customer would notice, not a side effect. Wait for it with `wait.until`, which retries a check until it passes.
- Make every check safe to run again. The instructor will press **Verify** many times.
- Read credentials through the product's primitives. If you must use one in an argument, use a template such as `{{ .Creds.primary.rootToken }}`, and never write a secret into the scenario.
- Do not put a password in the YAML, even a made-up one. Use a value from `.Creds`.
- Remember that the cluster needs a few seconds to settle after a restart. A check that races it is a flaky scenario.

## Primitives you can use

Run `s1s scenario validate` and the errors list what exists. The built-in ones are:

| Group | Primitives |
|---|---|
| Containers | `container.exec`, `container.stop`, `container.start`, `container.remove`, `container.kill-process`, `container.copy-in` |
| Files | `file.write`, `file.render` |
| Checks | `assert.equals`, `assert.contains`, `assert.matches`, `assert.json-path`, `http.request` |
| Waiting | `wait.until`, `wait.sleep` |
| Vault | `vault.cli`, `vault.status`, `vault.init`, `vault.unseal`, `vault.raft-peers`, `vault.expect-no-leader`, `vault.kv-put`, `vault.kv-get` |
| Boundary | `boundary.cli`, `boundary.lookup`, `boundary.authorize-session`, `boundary.target-info`, `boundary.session-info`, `boundary.recordings-list` |

The generated [scenario pages]({{< relref "/scenarios" >}}) show real examples of all of these in use.
