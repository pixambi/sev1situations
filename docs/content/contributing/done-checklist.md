---
title: The done checklist
description: When a piece of work is finished.
weight: 7
---

Tick every line before you ask for a review. A piece of work is not done until it is documented and tested.

## Every change

- [ ] `task test` passes.
- [ ] Linting passes, and so does `go vet` with and without `-tags integration`.
- [ ] The behaviour is documented in `docs/`, and the page is right.
- [ ] Commits follow Conventional Commits and carry no attribution lines.
- [ ] No emojis anywhere.
- [ ] No secret is written to a log, an error, a report or a file.

## A scenario

- [ ] An issue defines it, from the template.
- [ ] `s1s scenario validate` is clean, and the quality test passes.
- [ ] It has been **played end to end on real Podman** by a maintainer, including the fix, and a verification that fails before the fix.
- [ ] The guide's solution commands are the ones that were typed during that test.
- [ ] `task docs:generate` has been run and the pages are committed. CI fails if they are out of date.
- [ ] The branch holds only scenario files.

## A product or a primitive

- [ ] Unit tests cover it, with a simulator or a fake.
- [ ] An integration test has deployed it for real.
- [ ] Teardown leaves nothing behind.
- [ ] Its Cluster tab shows what an instructor needs, and no secret appears anywhere else.
