# Druid CLI Refactor Context

## Repo State

- Repo: `/Users/marcschottstadt/Development/druid/druid-cli`
- Branch/worktree is very dirty from an active large refactor.
- Do not assume deleted files are accidental. Many legacy files were intentionally removed.
- Go toolchain used for verification: `GOTOOLCHAIN=go1.24.7`
- `make build` succeeds but still emits the existing OpenAPI 3.1 warning from `oapi-codegen`.

## Core Direction

- Split app binaries:
  - `apps/druid`: daemon + local OCI/validation tooling
  - `apps/druid`: daemon client CLI
  - `apps/druid-coldstarter`: standalone coldstarter
- Docker runtime is the local backend. Kubernetes runtime support works in-cluster or out-of-cluster with kubeconfig and lives under `internal/runtime/kubernetes`.
- Runtime concept is always `scrolls`; avoid `instances` terminology.
- Daemon is runtime control plane, not OCI artifact manager.
- CLI owns OCI actions: pull, push, login.
- Daemon should consume already-materialized scroll paths.
- Containers must not see daemon-owned scroll spec files like `scroll.yaml`.
- Containers only see explicit mounts sourced from runtime `data/`.

## Current Command Surface

`druid` now exposes daemon/local validation tooling only:

```text
druid serve
druid update [artifact] [dir]
druid validate [dir]
druid app_version
```

Removed from `druid`:

```text
druid pull <artifact> [dir]
druid push [artifact]
druid push category ...
druid login --host <host> -u <user> -p <password>
druid create
```

Runtime daemon and OCI interaction is through `druid`:

```text
druid login --host <host> -u <user> -p <password>
druid pull <artifact> [dir]
druid push [artifact] [dir]
druid push category ...
druid create <artifact-or-path> [name]
```

## OCI Ownership

- Flattened OCI commands now live on `druid`:
  - old `druid registry pull` -> `druid pull <artifact> [dir]`
  - old `druid registry push` -> `druid push [artifact] [dir]`
  - old `druid registry login` -> `druid login ...`
- `druid pull` keeps current behavior:
  - pulls into optional dir or current working directory
  - includes data by default
  - `--no-data` skips data files
- `druid create` first asks the daemon to materialize:
  - Kubernetes daemon creates the runtime PVC and runs a `druid worker pull` Job in-cluster.
  - Docker daemon uses a worker container for daemon-driven materialization.
- Kubernetes create path: daemon/controller creates PVCs, runs a `druid pull` Job, stores runtime scroll state in ConfigMaps, stores opaque `k8s://namespace/pvc` refs there, and runs procedures as Kubernetes Jobs or StatefulSets depending on run mode.
- Docker runtime state stays in local SQLite; Kubernetes runtime state must recover from ConfigMaps, not `state.db`.
- Kubernetes daemon auth prefers in-cluster config, then kubeconfig from `--k8s-kubeconfig`, `DRUID_K8S_KUBECONFIG`, `KUBECONFIG`, or `~/.kube/config`.

## Daemon/API

- `druid serve` starts the multi-scroll runtime daemon.
- Daemon listens on a Unix socket.
- OpenAPI is the REST route source.
- REST routes are registered via generated `api.RegisterHandlersWithOptions(...)`.
- Manual REST path registration was removed.
- `/health` is intentionally kept as a manual liveness alias.
- Generated `/api/v1/health` also exists.
- WebSocket attach remains manual:
  - `/ws/v1/scrolls/:id/consoles/:console`

Active OpenAPI REST endpoints now only cover:

```text
GET    /api/v1/health
GET    /api/v1/scrolls
POST   /api/v1/scrolls
GET    /api/v1/scrolls/{id}
DELETE /api/v1/scrolls/{id}
POST   /api/v1/scrolls/{id}/commands/{command}
GET    /api/v1/scrolls/{id}/ports
```

Legacy REST endpoints were removed from OpenAPI and code:

```text
/api/v1/command
/api/v1/procedure
/api/v1/logs
/api/v1/metrics
/api/v1/pstree
/api/v1/processes
/api/v1/queue
/api/v1/token
/api/v1/consoles
/api/v1/watch/*
/api/v1/daemon/stop
```

## Handler Layout

- HTTP handlers now live under `apps/druid/adapters/http/handlers`.
- Removed legacy `internal/handler` package and tests.
- Removed `apps/druid/adapters/http/server` and `apps/druid/adapters/http/middlewares`.
- Removed `internal/core/ports/handler_ports.go`.
- `apps/druid/adapters/cli/serve.go` is intentionally thin:
  - flags
  - dependency construction
  - route registration
  - socket/TCP listener startup

## Runtime State Layout

Runtime state root defaults to `~/.druid/runtime`, or `--state-dir`.

Paths:

```text
<state>/scrolls/<id>/scroll.yaml
<state>/scrolls/<id>/data
```

Domain:

- `RuntimeScroll.Root`: daemon-owned runtime root containing `scroll.yaml`, `data/`, and `.druid/`

SQLite store:

- `internal/core/services/runtime_state_store.go`
- Table: `scrolls`
- Runtime state stores a single `root`.

## Runtime Mount Model

- Removed implicit `/app/resources/deployment` mount.
- Removed `domain.ScrollMountPath`.
- Procedure mounts are explicit:

```yaml
mounts:
  - path: /server
    sub_path: minecraft
    read_only: false
```

- `sub_path` is optional and relative to runtime `data/`.
- Missing `sub_path` means mount whole `data/`.
- `read_only` is supported.
- Mount validation checks:
  - path required
  - path absolute
  - duplicate mount paths invalid
  - sub_path relative
  - sub_path cannot escape via `..`

Docker implementation maps:

```text
<Root>/data/<sub_path> -> <mount.path>
```

## Procedure Runtime Model

- Executable runtime fields live on procedures, not commands:
  - `type` (`container` default, `signal` explicit)
  - `image`
  - `command`
  - `working_dir`
  - `env`
  - `mounts`
  - `target`/`signal` for signal procedures
  - `tty`
  - `expectedPorts`
- Commands remain orchestration groups:
  - `procedures`
  - `needs`
  - `run`
- `ProcedureLauncher` no longer owns an OCI registry client.
- Unsupported `mode`, `wait`, and `data` procedure fields are rejected during validation.

## Expected Ports And Traffic

- Top-level `ports` define named port metadata.
- Procedure `expectedPorts` references top-level port names.
- `keepAliveTraffic` belongs under `expectedPorts`.
- Examples:
  - `10kb/5m`
  - `10b/1s`
- Docker backend binds expected ports by resolving named top-level ports.
- Docker traffic is container-level only; same RX/TX stats are copied to every expected port for that procedure/container.
- Port status API:
  - `GET /api/v1/scrolls/{id}/ports`

## InitScroll And Templates

- `InitScroll` was removed fully.
- `.scroll_template` rendering support was removed fully.
- `scroll-config.yml` and `scroll-config.yml.scroll_template` artifact support was removed.
- Sprig dependency was removed.
- Rationale: stay lean and avoid unclear daemon/data/Kubernetes semantics. Reintroduce explicit pattern later if needed, probably via external tool like `gomplate` or backend-specific init Job.

## Removed Legacy Areas

- Removed Nix/dependency-resolution support.
- Removed local runtime backend.
- Removed old single-scroll serve mode.
- Removed old `druid runtime`, `druid runtime serve`, and single-scroll `druid stop` flows.
- Removed old port monitor/watch-port flow.
- Removed plugin system files.
- Removed legacy handlers/server/middlewares.

## Key Files

- `apps/druid/adapters/cli/root.go`: current `druid` command surface.
- `apps/druid/adapters/cli/serve.go`: daemon startup/listener setup.
- `apps/druid/adapters/http/handlers/routes.go`: generated REST registration plus manual `/health` and websocket route.
- `apps/druid/adapters/http/handlers/scroll_handler.go`: generated OpenAPI server handler methods for runtime scrolls.
- `apps/druid/core/services/runtime_supervisor.go`: daemon coordinator for persisted runtime truth and sessions.
- `apps/druid/core/services/runtime_session.go`: in-memory scroll execution session.
- `apps/druid/core/services/runtime_materialization.go`: daemon materialization path.
- `apps/druid/adapters/cli/create.go`: REST-backed create command.
- `apps/druid/adapters/cli/pull.go`: client-owned OCI pull.
- `apps/druid/adapters/cli/push.go`: client-owned OCI push.
- `apps/druid/adapters/cli/login.go`: client-owned registry login.
- `apps/druid/adapters/daemonclient/openapi_client.go`: generated OpenAPI client adapter.
- `internal/core/services/runtime_scroll_manager.go`: `RuntimeScrollManager` and `MaterializeScrollArtifact`.
- `internal/core/services/runtime_state_store.go`: SQLite state store.
- `internal/runtime/docker/backend.go`: Docker runtime backend.
- `api/openapi.yaml`: OpenAPI source of truth for REST API.
- `internal/api/generated.go`: generated OpenAPI code.
- `examples/{jobs,static-web,mysql,minecraft}/scroll.yaml`: current examples.

## Verification Already Run

These commands passed after the latest changes:

```sh
GOTOOLCHAIN=go1.24.7 make generate-api
GOTOOLCHAIN=go1.24.7 make mock
GOTOOLCHAIN=go1.24.7 go test ./...
./scripts/validate_all_scrolls.sh
GOTOOLCHAIN=go1.24.7 make build
jq empty .vscode/launch.json
```

Also passed local smoke:

```text
./bin/druid serve --socket <tmp>/runtime.sock --state-dir <tmp>/state
./bin/druid --daemon-socket <tmp>/runtime.sock create smoke examples/static-web
verified:
  <state>/scrolls/smoke/scroll.yaml exists
  <state>/scrolls/smoke/data exists
  druid describe smoke works
```

## Known Warning

`make build` and `make generate-api` emit:

```text
WARNING: You are using an OpenAPI 3.1.x specification, which is not yet supported by oapi-codegen...
```

This is known and currently non-blocking.

## Important Follow-Ups

- Daemon resume hydrates per-scroll sessions from persisted `RuntimeScroll` state; this still needs more end-to-end coverage around long-running commands.
- DB command statuses are persisted through the queue status observer.
- `scroll-lock.json` still exists in services and queue behavior; DB should become authoritative later.
- Docs generated under `docs_md` are stale/incomplete after command flattening; deleted stale registry/runtime command pages but did not regenerate docs.

## Current Mental Model

Docker/local create:

```text
druid serve --runtime docker
druid create <artifact-or-path> [name]
  -> client POSTs generated OpenAPI CreateScrollRequest
  -> daemon materializes OCI/local artifact into one runtime root
  -> daemon reads scroll.yaml from the runtime root
  -> daemon caches scroll.yaml in SQLite

druid run <id> <command>
  -> daemon launches Docker procedure containers using explicit data mounts
```

Runtime is daemon-only: `druid create/list/describe` do not send, store, or display a per-scroll runtime.

Future Kubernetes create:

```text
daemon creates final PVC/root storage
Kubernetes Job runs: druid worker pull --artifact <artifact> --root /scroll
worker reports scroll.yaml to daemon callback
daemon validates and persists RuntimeScroll
backend creates Jobs/Deployments/StatefulSets from scroll procedures
```
