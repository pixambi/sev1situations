# S1: Podman REST API on macOS

**Result: go.** Go can drive Podman with `net/http` alone over the machine's unix socket, with no Podman client library.

Tested 2026-11-26 on macOS (Apple Silicon) with Podman client 6.1.4 and machine engine 5.8.2 (applehv), Go 1.27.4. The program is [`s1-podman/main.go`](s1-podman/main.go) (`go run spikes/s1-podman/main.go`, standard library only). It cleans up after itself and leaves no containers or networks.

## What was proved

Every call is against `http://podman/v5.0.0/libpod/...` with a custom `DialContext` that dials the socket. The host part of the URL is ignored.

| Need | Call | Result |
|---|---|---|
| Find the socket | `podman machine inspect --format '{{.ConnectionInfo.PodmanSocket.Path}}'` | Prints the host-side unix socket, e.g. `$TMPDIR/podman/podman-machine-default-api.sock`. |
| Reach the API | `GET /version` | Engine version in `.Version`. |
| Create a network, labelled | `POST /networks/create` `{"name","labels"}` | Works. |
| Create a container | `POST /containers/create` with `name`, `image`, `command`, `labels`, `netns`, `networks`, `portmappings` | Returns `{"Id"}`. |
| Publish a port on loopback only | `portmappings: [{"host_ip":"127.0.0.1","host_port":19000,"container_port":8080,"protocol":"tcp"}]` | Reachable from the host at `127.0.0.1:19000`. |
| Start | `POST /containers/{id}/start` | 204. |
| Exec, capture output and exit code | `POST /containers/{id}/exec` (`Cmd`, `AttachStdout`, `AttachStderr`), then `POST /exec/{id}/start` `{"Detach":false}`, then `GET /exec/{id}/json` | Stdout and stderr arrive separately. `.ExitCode` was 3 for `exit 3`. |
| Copy a file in | `PUT /containers/{id}/archive?path=/tmp` with a tar body | Works on a **created** container (before start) and on a **running** one. |
| Stream logs | `GET /containers/{id}/logs?follow=true&stdout=true&stderr=true` | Streams until the request context is cancelled. |
| List by label | `GET /containers/json?all=true&filters={"label":["s1s.run=spike"]}` | Returns only labelled containers. |
| Remove | `DELETE /containers/{id}?force=true`, `DELETE /networks/{name}?force=true` | Works. |

Exec and log streams use the Docker multiplexed format: repeated frames of an 8-byte header (byte 0 is the stream, 1 for stdout and 2 for stderr, bytes 4 to 7 are the big-endian payload length) followed by the payload. About 15 lines of Go demultiplex it. This holds because the exec has no TTY. A TTY exec (the terminal, R-TERM) is a raw stream and needs the hijacked connection, which this spike did not test (see W2).

Errors come back as JSON `{"cause","message","response"}` with a useful message, for example `crun: executable file 'httpd' not found in $PATH`. The runtime should surface `message` and `cause`.

## Findings that change the definition

- **Socket discovery rule (T-012):** run `podman machine inspect` for the socket path. Do not hard-code a path: it lives under `$TMPDIR`, which differs per user and session. If `podman machine inspect` fails or prints nothing, treat the machine as missing or stopped and say so (this is the `s1s prereq` wording).
- **Pin the API path:** use `/v5.0.0/libpod/`. The client (6.1.4) is newer than the machine engine (5.8.2), so build the path from our own constant, not from the client version.
- **A stopped machine was not tested.** I did not stop the machine because it is in use. T-012 must check what the socket does when the machine is stopped (the file may remain with nothing listening) and map that to the "machine is stopped" error.
- **Images:** this spike used a local `docker.io/library/alpine:3`. Pulling (`POST /images/pull`) was not tried, so the missing-image path stays open for T-013.
- **The Alpine base image has no `httpd` applet** (BusyBox 1.37.0). It has `nc`, `wget` and `sh`. That matters for S2 and for any scenario that wants a throwaway server.

## Decision and the rule or section it updates

No change to the definition. R-LANG-5 (standard library `net/http` for the Podman socket) holds. The Podman REST client in `internal/runtime/podman` needs no third-party module.
