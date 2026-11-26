# Sev1 Situations

A local training tool for role-played severity-1 incident calls. An instructor runs real HashiCorp products (first Vault and Boundary) in Podman containers on their Mac and plays a customer whose environment has a problem. A trainee joins a call and helps fix it.

It is a native macOS app built with [Wails v3](https://v3.wails.io): a Go backend and a Svelte and TypeScript frontend using the Carbon Design System.

The project is in planning. The specification is in [`sev1situations-project-definition.md`](sev1situations-project-definition.md), the build plan is in [`TASKS.md`](TASKS.md) and the dependency register is in [`DEPENDENCIES.md`](DEPENDENCIES.md). The user and contributor documentation will be a Hugo site in `docs/`, published to GitHub Pages.

## Requirements

- macOS (Apple Silicon or Intel)
- [Podman](https://podman.io/docs/installation) with a running machine: `podman machine init && podman machine start`

## Install (planned)

Download the archive from the GitHub releases page (access to the repository is needed), unzip it and move `Sev1 Situations.app` to Applications. Releases are not signed at first, so macOS asks you to clear the quarantine attribute once: `xattr -cr "/Applications/Sev1 Situations.app"`.

## Use (planned)

Open the app. The window shows the scenario catalogue. The executable also has headless commands:

```sh
s1s prereq                         # check that Podman is installed and its machine is running
s1s scenario list                  # list built-in and your own scenarios
s1s scenario new <id> --product vault   # create a scenario folder to fill in
s1s scenario validate [path]       # check scenario folders
s1s version                        # print version information
```

## Develop (planned)

Needs Go, Node, [Task](https://taskfile.dev) and the Wails v3 CLI.

```sh
task dev      # run the app with hot reload
task build    # build Sev1 Situations.app
task test     # Go and frontend unit tests
task lint     # go vet, golangci-lint and svelte-check
```
