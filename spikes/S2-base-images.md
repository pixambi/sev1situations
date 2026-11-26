# S2: Binaries on the base image

**Result: go with changes.** Four of the five builds run on Alpine. The HSM build needs a glibc image, as suspected, and every Enterprise build needs a license to start.

Tested 2026-11-26 on a Podman machine (Podman engine 5.8.2, arm64) with `alpine:3` (BusyBox 1.37.0) and `debian:stable-slim` (Debian 13 "trixie"). Binaries were the newest stable linux/arm64 builds from `releases.hashicorp.com`, each checked against SHA256SUMS. The program is [`s2-binaries/main.go`](s2-binaries/main.go) (`go run spikes/s2-binaries/main.go`, standard library plus the `podman` CLI).

## What was proved

`<binary> version` run inside each image:

| Product | Edition | Version | alpine:3 | debian:stable-slim |
|---|---|---|---|---|
| vault | community | 2.2.2 | runs | runs |
| vault | `+ent` | 2.2.2 | runs | runs |
| vault | `+ent.hsm` | 2.2.2 | **fails**: `exec container process (missing dynamic library?)` | runs (reports `(cgo)`) |
| boundary | community | 0.21.3 | runs | runs |
| boundary | `+ent` | 1.1.1 | runs | runs |

- The HSM build is a cgo binary linked to `libresolv`, `libdl`, `libpthread` and `libc` (glibc). Alpine uses musl, so it cannot start. The other four are static.
- A community Vault dev server (`vault server -dev`) starts on Alpine and answers `/v1/sys/health` with `initialized: true, sealed: false`. The log says `Mlock: supported: true, enabled: false`.
- A Vault Enterprise dev server on Alpine **refuses to start without a license**: `Error initializing core: license check failed: no autoloaded license provided.`
- SoftHSM works on Debian: `apt-get install softhsm2` gives SoftHSM 2.6.1, `softhsm2-util --init-token` creates a token, and the PKCS#11 library is at `/usr/lib/aarch64-linux-gnu/softhsm/libsofthsm2.so`.

What each image has by default:

| Tool | alpine:3 | debian:stable-slim |
|---|---|---|
| `sh` | yes | yes |
| `bash` | no | yes |
| `wget` | yes (BusyBox) | no |
| `curl` | no | no |
| `nc` | yes (BusyBox) | no |
| `tar`, `unzip` | yes | `tar` only |
| `jq`, `openssl` | no | no |
| CA certificates | yes | no (`ca-certificates` needed for TLS to outside hosts) |

## Findings that change the definition

- **Use two base images (R-RUN-10 confirmed):** `alpine:3` for everything except the HSM build, and `debian:stable-slim` for `+ent.hsm`. Decide per product, edition and scenario, and state the choice in one place.
- **Enterprise needs a license for every start.** The license comes from Settings (R-SET-1). A scenario that uses Enterprise must fail early, with a clear message, when no license is set, rather than let the container exit with a log line.
- **Tools the scenarios can rely on:** on Alpine `sh`, `wget`, `nc` and `tar`. Anything else (`curl`, `jq`, `openssl`, `bash`) must be installed into the image or avoided in exec primitives. Prefer the Vault and Boundary CLIs and HTTP calls made from the app over shell pipelines inside containers.
- **Debian images need `ca-certificates`** if a container fetches from outside. Installing packages at run time needs network from the container (it worked here).
- **mlock:** `disable_mlock` is not needed on this machine (`Mlock: supported: true`), but the runtime should set `IPC_LOCK` or `disable_mlock` deliberately. Decide in T-051.

## Not tested

- **Vault Enterprise HSM with SoftHSM end to end** (a `pkcs11` seal). It needs a license. SoftHSM and the HSM binary both run, so the remaining step is the seal configuration and `vault operator init`, to be done when a license is available (M7, the HSM scenario).
- **Boundary Enterprise start-up** and a Boundary dev run (needs Postgres, T-100).
- **amd64.** Only the arm64 builds were run. The Intel build path follows the machine's architecture (S7).

## Decision and the rule or section it updates

R-RUN-10 is confirmed and needs no wording change. Add the license-required behaviour to Section 7 when the Enterprise path is built.
