# Container base image

The root `Dockerfile` builds a versioned base image for running one herdr-soho job per container. It holds the `herdr-soho` CLI and skill resources, the `claude`, `codex`, `grok` and `pi` agent runner CLIs, and the toolchain a job needs to build and test this repository: Go, a C compiler for the race detector, Git and Node.js. Downstream images extend it with `FROM`; the base itself contains no credentials, no runner login and no project checkout.

Herdr stays on the host. A host-side Herdr pane runs `docker exec -it` into the job container, so a person can watch and type into the in-container runner like any other pane. The image does not ship Herdr, and in-container `herdr-soho` cannot control host panes (see [Herdr and in-container herdr-soho](#herdr-and-in-container-herdr-soho)).

## Contents

| Component | Version input | Source and verification |
|---|---|---|
| Base OS | `DEBIAN_IMAGE` (`debian:trixie-slim` by digest) | Docker Official Image, pinned by index digest |
| Debian packages | `DEBIAN_SNAPSHOT` | `snapshot.debian.org` archive at a fixed timestamp; apt verifies the archive signatures with the base image's keyring |
| Go | `GO_VERSION`, `GO_SHA256_*` | `go.dev/dl` archive, SHA-256 from the official download index |
| Node.js | `NODE_VERSION`, `NODE_SHA256_*` | `nodejs.org/dist` archive, SHA-256 from the release's `SHASUMS256.txt` |
| Claude Code | `CLAUDE_CODE_VERSION`, `CLAUDE_CODE_SHA256_*` | Native build from `downloads.claude.ai/claude-code-releases/<version>/linux-<arch>/claude`, SHA-256 from that release's `manifest.json` (the source the official installer uses) |
| Grok CLI | `GROK_VERSION`, `GROK_SHA256_*` | Release artifact `x.ai/cli/grok-<version>-linux-<arch>` (the official installer's source); upstream publishes no checksum, so the digest is recorded when the version is pinned |
| Codex CLI | `CODEX_VERSION`, `container/runners/package-lock.json` | npm package `@openai/codex` and its platform package, installed with `npm ci` from the lockfile |
| Pi | `PI_VERSION`, `container/runners/package-lock.json` | npm package `@earendil-works/pi-coding-agent`, installed with `npm ci --ignore-scripts` as its documentation recommends; the `protobufjs` override mirrors the official Pi installer's lockfile root |
| herdr-soho | `HERDR_SOHO_VERSION` | Built from the curated source in the build context with the release flags (`-trimpath -buildvcs=false -ldflags "-s -w -X …version=…"`) |
| Dockerfile frontend | `# syntax=` line | `docker/dockerfile` pinned by digest |

Debian packages installed explicitly: `ca-certificates`, `curl`, `fd-find` (also linked as `fd`), `gcc`, `git`, `less`, `libc6-dev`, `openssh-client`, `patch`, `procps`, `ripgrep`, `tini` and `xz-utils`. Their dependencies come from the same snapshot.

The npm lockfile records the exact version, registry URL and `sha512` integrity of every transitive package; `npm ci` refuses a tree that does not match it. Lifecycle scripts are disabled, so no package runs code at install time.

Runtime layout:

| Path | Content |
|---|---|
| `/usr/local/bin/herdr-soho` | herdr-soho CLI |
| `/usr/local/share/herdr-soho/skills/herdr-soho` | Skill resources (`SKILL.md`, roles, references, templates); `HERDR_SOHO_SKILL_DIR` points here |
| `/usr/local/bin/claude`, `/usr/local/bin/grok` | Native runner binaries |
| `/opt/herdr-soho/runners` | npm tree for `codex` and `pi` (linked from `/usr/local/bin`) |
| `/usr/local/go`, `/opt/node` | Go and Node.js toolchains |
| `/workspace` | Working directory for the job checkout (owned by `agent`) |
| `/home/agent` | Home of the non-root `agent` user (UID and GID 1000) |

The image runs as `agent`, with `tini` as the entrypoint and `sleep infinity` as the default command: the container idles, and runner sessions start with `docker exec`. The environment sets `GOTOOLCHAIN=local` (the pinned Go never downloads another toolchain), `DISABLE_AUTOUPDATER=1`, `GROK_DISABLE_AUTOUPDATER=1` and `PI_SKIP_VERSION_CHECK=1`, so runners do not replace the pinned binaries at run time. To use the skill with a runner's skill loader, link the resource directory into that runner's skill directory inside the container, for example `ln -s "$HERDR_SOHO_SKILL_DIR" ~/.claude/skills/herdr-soho`.

## Build

The build reads only an allowlisted context: `Dockerfile.dockerignore` denies everything and allows `go.mod`, the `herdr-soho` command, `internal/`, the skill resources and the runner lockfile, then excludes tests, test data and dot files. Version control data, local state, agent configuration, documentation and anything else in the checkout never reach the builder.

Build with a dedicated BuildKit builder pinned by digest. The builder version is part of the reproducibility boundary, and the exporter of Docker's default `docker` driver on the classic image store does not apply `rewrite-timestamp`, which leaves build-time file timestamps in the layers.

```sh
docker buildx create --name herdr-soho-base --driver docker-container \
  --driver-opt image=moby/buildkit:v0.33.1@sha256:cec9f139f45e93c5c69c60f8b07cfad9f43f4ef6b6a6cd917527fea5ff2e3dea
docker buildx build --builder herdr-soho-base \
  --platform linux/arm64 \
  --build-arg SOURCE_DATE_EPOCH="$(git log -1 --format=%ct)" \
  --build-arg HERDR_SOHO_VERSION="$(git describe --tags --always)" \
  --provenance=false --sbom=false \
  --output type=docker,name=herdr-soho-base:0.1.0,dest=herdr-soho-base-0.1.0.tar,rewrite-timestamp=true \
  .
docker load -i herdr-soho-base-0.1.0.tar
```

Use `--platform linux/amd64` on an x86-64 host. Run each platform's image on a host of that architecture: under QEMU emulation of `linux/amd64` on an ARM host, the native Claude Code binary aborts at start (its Bun runtime crashes), while the other tools run. The tag carries `BASE_IMAGE_VERSION` (also the `org.opencontainers.image.version` label); bump both together when an input changes. Provenance and SBOM attestations are disabled because they record builder and context details; the inputs are documented here instead. `docker buildx rm herdr-soho-base` removes the builder when it is no longer needed.

To inspect the exact context the build receives, export the `build-context` stage:

```sh
docker buildx build --target build-context --output type=local,dest=/tmp/herdr-soho-context .
```

## Using the base downstream

`container/examples/downstream/Dockerfile` is a generic downstream image. Two references to the base exist, and they are different digests:

- **Local tag.** After `docker load`, the base exists only on this Docker engine as `herdr-soho-base:0.1.0`. `docker build` resolves local images, so `FROM herdr-soho-base:0.1.0` works: `docker build -t my-job-image:dev container/examples/downstream`. Check that the tag still points at the expected build with `docker image inspect --format '{{.Id}}' herdr-soho-base:0.1.0`; the image ID is the digest of the image config. A `docker-container` builder (such as the pinned builder above) does not see the engine's local images, so build downstream images with the engine's own builder.
- **Registry digest.** A digest after `@` in `FROM` is a manifest digest, which only a registry resolves. The local image ID is not one: `FROM herdr-soho-base:0.1.0@sha256:<image ID>` fails. Once the base is pushed to a registry you control, pin downstream images to the digest the push reports (`docker image inspect --format '{{json .RepoDigests}}'`): `FROM <registry>/herdr-soho-base:0.1.0@sha256:<repo digest>`. The example's `BASE_IMAGE` build argument takes either form. This repository does not publish the image.

Downstream images should keep `USER agent`, the `/workspace` working directory and the entrypoint unless they have a reason to change them, and must keep credentials out of their builds as well.

## Reproducibility

What is pinned: the frontend, the base image and every download by digest; Debian packages by snapshot timestamp; npm packages by lockfile integrity; the herdr-soho source by the checked-in tree. `SOURCE_DATE_EPOCH` fixes the image creation time and history timestamps, and `rewrite-timestamp=true` clamps the modification time of every file in the layers to that value.

Verify a rebuild by building twice without the cache, with the same builder, platform, build arguments and image name, and comparing the exported archives:

```sh
# build as above into a.tar, then again with --no-cache into b.tar
sha256sum a.tar b.tar
go run ./container/imagecheck compare a.tar b.tar
```

Identical archive digests mean a bit-for-bit identical image: the same config (image ID), manifest and compressed layers. `compare` also works on two `docker save` archives and on builds exported under different names (the name is recorded in the archive's `index.json` and `manifest.json`, so their archive digests differ). It prints both image IDs and, when they differ, every differing config field, layer and file attribute (`mode`, `uid`, `mtime`, `content`, …); it exits 0 only when the configs, and so every layer digest, are identical.

The boundary of the claim:

- The bit-for-bit result is verified for two builds on the same machine with the pinned builder and the same platform. Another BuildKit version, another platform or another exporter may produce different layer bytes; the inputs stay the same, the bytes are not promised. Each platform is its own image: `linux/amd64` and `linux/arm64` builds have different digests.
- The inputs are immutable only while their sources serve them: a removed snapshot, release artifact or npm version stops the build rather than changing it, because every download is checked against its pinned digest.
- The Grok digest is a first-use pin: it proves later builds receive the bytes recorded at pin time, not that those bytes are authentic beyond the HTTPS download from the official host.
- apt and npm can write caches, logs and timestamps; the Dockerfile removes the known ones. Any remaining difference is listed by `compare` rather than hidden.

## Inventory and smoke test

`container/smoke.json` lists the commands that prove each tool is present and runs without a login: `--version` and `--help` for every runner, the toolchain versions, the skill resources and the runtime user. `imagecheck smoke` runs each one in a fresh container with `--network none` and checks the reported version against the `ARG` defaults in the `Dockerfile`:

```sh
go run ./container/imagecheck smoke --image herdr-soho-base:0.1.0 --spec container/smoke.json \
  --build-arg HERDR_SOHO_VERSION="$(git describe --tags --always)"
```

## Secret and independence scans

`imagecheck` scans both ends of the build:

- `imagecheck context <dir> --allow <prefix>…` scans an exported build context: every file must sit under an allowed prefix, no path component may be version control, local state, agent configuration or a credential file, and no file may contain a private key or a recognizable access token.
- `imagecheck image <docker-save.tar>` scans every layer and the image config: no credential file in a home directory, no `.git` directory, no secret-looking environment variable with a value, no host home path in the config or history, and the same token patterns in every file.

Both take `--deny-file <path>`: a private file, kept outside the repository, with one literal per line (a canary, a host or user name, a private path, any name that must never ship). A match reports only the line number, never the literal. `--accept <path>=<rule>` and `--accept-file <path>` record reviewed false positives explicitly; the output still lists them as `accepted`.

Third-party binaries and documentation in the image contain strings that look like secrets without being one: PEM headers used as format strings, upstream test keys shipped with the Go distribution, adjacent prefix strings in compiled runners, base64 data. `container/image-scan-accept.txt` lists each reviewed match for the `linux/arm64` and `linux/amd64` images, with the reason. Review a new match before adding it there; paths of vendored binaries differ per platform and version, so the list changes with the pins.

```sh
go run ./container/imagecheck image herdr-soho-base-0.1.0.tar \
  --accept-file container/image-scan-accept.txt --deny-file /path/outside/repo/deny.txt
```

A useful negative test is a canary: put a file holding a unique literal in the checkout outside the allowlist, export the context, and confirm the scan with that literal in the deny file finds nothing; then add the literal to an allowed file in a copy of the exported context and confirm the scan reports it.

## Running a job

One container per job; one Compose project per job keeps containers, networks and volumes apart. `container/examples/compose.yaml` is the reference, with `macos.env.example` and `windows.env.example`:

```sh
docker compose -p job-42 --env-file /path/to/job-42.env -f container/examples/compose.yaml up -d
docker compose -p job-42 -f container/examples/compose.yaml exec agent claude
docker compose -p job-42 -f container/examples/compose.yaml down       # keep runner state
docker compose -p job-42 -f container/examples/compose.yaml down -v    # remove runner state
```

Without Compose:

```sh
docker run -d --name job-42 \
  --mount type=bind,source=/path/to/jobs/42/repo,target=/workspace \
  --mount type=volume,source=job-42-claude,target=/home/agent/.claude \
  herdr-soho-base:0.1.0
docker exec -it job-42 claude
docker rm -f job-42          # the job-42-* volumes stay until docker volume rm
```

- **Workspace.** The job checkout is bind-mounted read-write at `/workspace`. On Windows hosts with Docker Desktop (WSL 2 backend), use forward-slash paths in the env file (`C:/jobs/42/repo`); keeping the checkout inside the WSL file system is faster than a Windows drive. Clone with `core.autocrlf=false` so files keep LF line endings.
- **Runner state.** Each runner keeps logins, sessions and settings under its home directory (`~/.claude`, `~/.codex`, `~/.grok`, `~/.pi`). The example gives each job its own named volumes: they survive `down` and container restarts and are deleted with `down -v` or `docker volume rm`. Retain them while a job may resume; delete them when the job closes.
- **Lifecycle.** `up -d` starts the idle container; each `exec` starts one runner session; stopping the container ends every session; `down` removes the container and network.
- **Resources.** The examples set no CPU or memory limits.

### Credentials

The image holds no credential, and nothing in the build accepts one: no build argument or `ENV` carries a key, and runner login files exist only in runtime volumes. Choose per runner:

- **Interactive login.** Run the runner in the container (`docker exec -it job-42 claude`) and log in once; the login lands in that job's state volume, never in an image layer.
- **Key files.** Mount each key read-only as a file. The Compose example declares `secrets:` entries, mounted at `/run/secrets/<name>`, with placeholder sources; point `ANTHROPIC_API_KEY_FILE`, `OPENAI_API_KEY_FILE` and `XAI_API_KEY_FILE` at files outside any checkout. Export a key only into the runner process: `docker exec -it job-42 sh -c 'ANTHROPIC_API_KEY="$(cat /run/secrets/anthropic_api_key)" exec claude'`.
- **Other services.** The image does not include clients or configuration for additional services, such as a Hermes or Cinzel endpoint or an ai-memory server. A downstream image or the job's runtime adds the client; its endpoint, tokens and any runner MCP entry that references them are injected at run time only, the same way: a read-only secret file (the Compose example declares the placeholders `hermes_credentials`, `cinzel_credentials` and `ai_memory_credentials`, from `HERMES_CREDENTIALS_FILE`, `CINZEL_CREDENTIALS_FILE` and `AI_MEMORY_CREDENTIALS_FILE`) and configuration kept in the job's runner state volume. What each client expects in that file is defined by the client, not by this image.

Avoid `environment:`, `docker run -e` and `--env-file` for keys: those values are stored in the container configuration and anyone who can run `docker inspect` (or `kubectl get pod -o yaml`) reads them. A key exported inside one `exec` is visible only through that process's environment. Never bind-mount a host runner configuration directory read-write into a job: it would share the host's sessions and let the job change them. Do not pass keys as build arguments; they would be recorded in the image history.

### Herdr panes

Herdr runs on the host. A host pane attaches to the container with an interactive TTY:

```sh
docker exec -it job-42 claude
```

`-i` keeps stdin open and `-t` allocates a terminal, so keys typed into the pane (or sent with `herdr pane send-keys`) reach the runner, and resizing the pane resizes the runner's terminal. When the session ends, the pane returns to the host shell; the container keeps running.

## Herdr and in-container herdr-soho

The `herdr-soho` binary in the image runs the commands that need no Herdr server, such as `--version`, `--help`, `env` (which reports the skill directory and the installed runner versions) and `lint` for briefs, and it serves the skill resources. Commands that drive Herdr (`init`, `spawn`, `dispatch`, `wait`, `roster`, …) need the Herdr server's control socket and `HERDR_ENV`. Herdr does not run in the container, and no bridge forwards its socket into one, so those commands are not available inside the container.

Orchestration therefore stays on the host: the orchestrating agent and `herdr-soho` run in Herdr on the host, and a pane runs a containerized runner with `docker exec -it`. `herdr-soho spawn` starts runner CLIs directly on the host; it has no option to start them inside a container, so a containerized worker pane is opened and driven by hand (or by a host tool) rather than by `spawn`. Report files a containerized runner writes under `/workspace` appear in the host checkout through the bind mount.

## Native-only checks

The container covers Linux builds and tests of the Go code, the runners' CLI surface and the job workflow inside one container. It does not replace native testing on macOS, Linux and Windows. These stay on native hosts:

- Herdr itself, its panes, tray and window integration, and the optional plugin's terminal interface.
- Windows behavior: Credential Manager, Windows paths and process handling, console and terminal semantics.
- macOS behavior: process-tree inspection and ownership checks, and any macOS-specific path or permission handling.
- Every OS-native test of process, locking, installation and terminal behavior that the project's native CI matrix runs.

## Updating pins

Change one input at a time, rebuild, rerun `smoke`, the scans and `compare`, then bump `BASE_IMAGE_VERSION`.

- **Base image and frontend:** `docker buildx imagetools inspect debian:trixie-slim` (or `docker/dockerfile:<version>`) and copy the index digest.
- **Debian packages:** set `DEBIAN_SNAPSHOT` to a `YYYYMMDDTHHMMSSZ` timestamp listed on `snapshot.debian.org`, usually the one in the new base image's `/etc/apt/sources.list.d/debian.sources` comment.
- **Go:** take the `sha256` of the two `linux-amd64` and `linux-arm64` archives from `https://go.dev/dl/?mode=json&include=all`.
- **Node.js:** take the two `linux-x64` and `linux-arm64` `.tar.xz` lines from `https://nodejs.org/dist/v<version>/SHASUMS256.txt`.
- **Claude Code:** take `platforms["linux-x64"].checksum` and `platforms["linux-arm64"].checksum` from `https://downloads.claude.ai/claude-code-releases/<version>/manifest.json`.
- **Grok CLI:** download `https://x.ai/cli/grok-<version>-linux-x86_64` and `-linux-aarch64`, compare each against the same file from the installer's fallback host (`storage.googleapis.com/grok-build-public-artifacts/cli/`), and record the SHA-256.
- **Codex and Pi:** edit the versions in `container/runners/package.json` (and `CODEX_VERSION`/`PI_VERSION` in the `Dockerfile`), then regenerate the lockfile with the pinned Node.js release, for example `docker run --rm -v "$PWD/container/runners:/w" -w /w node:<NODE_VERSION>-trixie-slim npm install --package-lock-only --ignore-scripts`. Review the lockfile diff: every `resolved` URL must be on `registry.npmjs.org` and every entry must keep an `integrity` value.
