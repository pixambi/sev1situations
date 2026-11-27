---
title: The command line
description: The s1s commands.
weight: 6
---

The `s1s` program opens the app with no arguments. These commands are available:

| Command | What it does |
|---|---|
| `s1s version` | Prints the version, commit and build date. |
| `s1s prereq` | Checks that Podman is installed and its machine is running. |
| `s1s scenario list` | Lists the built-in scenarios and your own. |
| `s1s scenario validate [path]` | Checks scenario folders, or all of them. |
| `s1s scenario new <id> --product <name>` | Creates a scenario folder to fill in. |

Your own scenarios live in the application data directory, which is `~/Library/Application Support/Sev1Situations`.
Setting `S1S_DATA_DIR` uses a different one, which is handy for trying things without touching your real data.
