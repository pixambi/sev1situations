---
name: scaffold-scenario
description: Create the branch and the folder for a new Sev1 Situations scenario from its issue. Use after a scenario is defined and the person wants the files started.
---

# Scaffold a scenario

Follow `define-scenario` first. This skill is plain Markdown and does not depend on a particular tool.

## Steps

1. Work from the scenario's issue. If there is none, stop and use `define-scenario`.
2. Branch from `main` as `scenario/<product>-<name>` (R-CONTRIB-1). One scenario per branch, and only scenario files on it (R-CONTRIB-2). Use the `git-conventional-commits` skill for git.
3. Create the folder:

   ```sh
   s1s scenario new <id> --product <product> --dir scenarios/<product>
   ```

   For a built-in scenario the folder is `scenarios/<product>/<name>/`.
4. Fill `scenario.yaml` from the issue, every field of the Scenario Standard.
5. Run `s1s scenario validate scenarios/<product>/<name>` and fix every error. The errors name the file and the field.

## Rules

- Never commit, push or open a pull request unless asked.
- No secret or password in the YAML. Read credentials from `.Creds`.
