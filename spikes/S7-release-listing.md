# S7: Release listing and verification

**Result: go.** The version list, the download and the SHA-256 check all work with the standard library against `releases.hashicorp.com`.

Tested 2026-11-26 with Go 1.27.4 and the live site. The program is [`s7-releases/main.go`](s7-releases/main.go) (`go run spikes/s7-releases/main.go`, standard library only).

## What was proved

**Listing.** `GET https://releases.hashicorp.com/<product>/index.json` returns one JSON document: `{"name", "versions": {"<version>": {...}}}`. Each version has `shasums` (the name of its SHA256SUMS file) and `builds`, a list of `{os, arch, filename, url}`. The Vault index is 2.7 MB with 1,404 versions, so fetch it once and cache it for the session.

**Editions are encoded in the version string** after a `+`:

| Suffix | Edition |
|---|---|
| none | community |
| `+ent` | Enterprise |
| `+ent.hsm` | Enterprise with HSM (PKCS#11) |
| `+ent.fips1403`, `+ent.hsm.fips1403` | Enterprise FIPS 140-3 builds |
| `+ent.fips1402`, `+ent.hsm.fips1402` | Enterprise FIPS 140-2 builds (older, last seen at 1.19.3) |

Pre-releases carry a `-` in the core version (`1.21.0-rc1`, `1.21.0-rc1+ent`). Filter them out of the default list.

**Newest stable per edition on the test day:**

| Product | Edition | Newest |
|---|---|---|
| vault | community | 2.2.2 |
| vault | `+ent`, `+ent.hsm`, `+ent.fips1403`, `+ent.hsm.fips1403` | 2.2.2 |
| vault | `+ent.fips1402`, `+ent.hsm.fips1402` | 1.19.3 |
| boundary | community | 0.21.3 |
| boundary | `+ent` | 1.1.1 |

Every listed version has both `linux/amd64` and `linux/arm64` builds, named `<product>_<version>_<os>_<arch>.zip`, for example `vault_2.2.2+ent.hsm_linux_arm64.zip`. The `+` is used as is in the URL path.

**Download and verify.** Downloaded `boundary_0.21.3_linux_arm64.zip` (172,997,803 bytes) from the build's `url`. Fetched `https://releases.hashicorp.com/boundary/<version>/<shasums>` and found the line `<sha256>  <filename>` (two spaces). The computed SHA-256 matched. After changing one byte of the archive, the match was `false`, so a corrupted download is caught.

## Findings that change the definition

- **Versions are not comparable across editions.** Boundary community is at 0.21.3 while Boundary Enterprise is at 1.1.1+ent. The Start form (R-SCN-11) must list versions per product **and edition**, and must not assume the same number exists in both. D1 ("newest available at deploy time") means newest per edition.
- **Archives are large** (173 MB for Boundary on arm64), so R-RUN-8 (the on-disk cache) matters. The cache key must be product, edition, version and architecture, and the download needs progress and cancellation.
- **The container architecture follows the Podman machine** (arm64 on Apple Silicon, amd64 on Intel). Select the build by the machine's architecture, not the host's. The index also carries the other architecture, so a wrong choice would download fine and then fail to run.
- **FIPS builds** (`+ent.fips1402`, `+ent.fips1403`) exist beside the editions the definition names. Decide whether the app lists them. The default should hide them (R-SCOPE and the Start form name only community, Enterprise and HSM). This is a decision for the project owner.
- **No signature check, as decided in D7.** Only the SHA-256 from the `shasums` file is used. The signature files (`.sig`) are listed in the index and are ignored.

## Decision and the rule or section it updates

No rule changes. Section 7 (runtime) and the `release` package (T-018, T-019) follow these findings: list from `index.json`, key the cache by product, edition, version and architecture, verify against the SHA256SUMS line for the file name.
