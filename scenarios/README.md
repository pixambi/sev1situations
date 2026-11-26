# Built-in scenarios

Every folder under this one that contains a `scenario.yaml` is a built-in scenario. The whole tree is embedded in the executable, and it is discovered at start-up, so adding a scenario means adding a folder and changing no Go file (R-SCN-16).

```
scenarios/<product>/<name>/
  scenario.yaml      metadata, parameters, topology and steps
  steps/             Markdown guidance for each step
  templates/         config templates
  fixtures/          files copied into containers
```

A scenario is self-contained and never reads another scenario's files (R-SCN-15). `s1s scenario validate` checks a folder against the Scenario Standard. See section 3.6 and section 5.4 of the project definition.

No scenario has been written yet. The first, the Vault sandbox, arrives with milestone M4.
