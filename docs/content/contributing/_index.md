---
title: Contributing
description: How to add a scenario, a primitive or a product, and what done means.
weight: 1
---

Most contributions are scenarios, and a scenario is data: a YAML file and some Markdown. You do not need to read the Go code to write one.

| I want to | Read |
|---|---|
| Understand how the app is built | [Architecture]({{< relref "architecture" >}}) |
| Add a scenario | [Writing a scenario]({{< relref "writing-a-scenario" >}}) |
| Know what every scenario must define | [The Scenario Standard]({{< relref "scenario-standard" >}}) |
| Add a check or an action a scenario can use | [Adding a primitive]({{< relref "adding-a-primitive" >}}) |
| Support another HashiCorp product | [Adding a product]({{< relref "adding-a-product" >}}) |
| Know when my work is finished | [The done checklist]({{< relref "done-checklist" >}}) |

## Rules that apply to everything

- Commits follow [Conventional Commits](https://www.conventionalcommits.org/). Branches are `scenario/<product>-<name>` for a scenario and `feat/<description>` for anything else.
- A scenario branch holds only scenario files. If a scenario needs new behaviour in the app, that work merges first, through its own `feat/` branch.
- Every scenario starts as an issue that defines it, from the new-scenario issue template.
- No emojis in code, comments, documentation or commit messages.
- Run `task test` before you commit.

## Working with an AI assistant

The repository ships three skills in `.agents/skills/`: `define-scenario`, `scaffold-scenario` and `build-scenario`. They are plain Markdown and work with any
assistant that can read files. They follow the same rules as people do, and they never commit or open a pull request unless you ask.
