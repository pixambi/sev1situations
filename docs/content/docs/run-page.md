---
title: The run page
description: Steps, hints, verification and rollback.
weight: 1
---

A run is one scenario played once. The page shows the steps in order, and you cannot skip ahead.

## Steps

Each step is one of three kinds.

| Kind | What happens |
|---|---|
| Start | The deployment is created and checked healthy. |
| Breakage | **Apply breakage** makes the fault. The step checks that the fault is there before you can continue. |
| Verification | **Verify** checks that the trainee fixed it. You can press it as often as you like. |

A step opens its guidance when it is reached. The **Briefing** says what to tell the trainee and what they should see.
**Hints** are for you to give a little at a time. The **Solution** is for you, not the trainee. A later step's
solution is not shown until the step is reached.

## Notes and ratings

Each step has a notes box and a rating from 1 to 5. Both are saved in the report.

## Rollback

A breakage step can be a reset point. **Roll back** returns the cluster to the start of that step so the trainee can try again,
without a new deployment. Rolling back removes what the step changed and makes the fault again.

## Ending

**End** stops the run, removes every container and network the run created, and saves a report, even if the run was not finished.
