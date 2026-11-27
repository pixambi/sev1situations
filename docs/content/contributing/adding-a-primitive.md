---
title: Adding a primitive
description: Add a check or an action that scenarios can use.
weight: 5
---

A primitive is a named, documented building block. Add one when several scenarios would otherwise repeat the same few steps, or when a check needs to understand a product's output.

Add a **generic** primitive in `internal/primitive`, and a **product** primitive in the product's package, for example `internal/product/boundary/primitives.go`. A product returns its primitives from `Primitives()`, and the registry adds them.

## What a primitive is

A primitive has two methods.

- `Spec()` returns its name, a one-sentence summary, its arguments, its timeout, and whether it changes anything.
- `Run(ctx, env, args)` does the work and returns a result with a status, a message, optional evidence and optional outputs.

The registry checks the arguments against the spec before `Run` is called, applies the timeout, and removes every known secret from what the primitive reports. You do not need to do any of that.

## Rules

1. **Name it** `<group>.<verb>`, in kebab-case, for example `boundary.authorize-session`.
2. **Say whether it is safe to run again.** A check always is. An action should say what happens the second time.
3. **Bound it.** Set a timeout that is no longer than it needs.
4. **Never put a secret on a command line.** Pass tokens and passwords in the environment.
5. **Tell a real answer from a failure to answer.** A check that expects a refusal must not pass because the server was down. `boundary.authorize-session` passes "denied" only on a 403 for this reason.
6. **Return evidence** the instructor can read, such as the command's output. It is redacted for you.
7. **Test it** against a fake runtime, and run it once against real containers.

## Adding it

{{< steps >}}
1. Write the type with its `Spec` and `Run`.
2. Return it from `Primitives()`.
3. Write the tests. Cover the passing case, the failing case, a bad argument and a secret that must not appear.
4. Use it in a scenario, and play that scenario for real.
5. Document it in the table on [Writing a scenario]({{< relref "writing-a-scenario" >}}).
{{< /steps >}}
