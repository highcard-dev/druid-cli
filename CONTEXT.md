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
  - `apps/druid-client`: daemon client CLI
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

Runtime daemon and OCI interaction is through `druid-client`:

```text
druid-client login --host <host> -u <user> -p <password>
druid-client pull <artifact> [dir]
druid-client push [artifact] [dir]
druid-client push category ...
druid-client create <artifact-or-path> [name]
druid-client register [dir] [name]
```

## OCI Ownership

- Flattened OCI commands now live on `druid-client`:
  - old `druid registry pull` -> `druid-client pull <artifact> [dir]`
  - old `druid registry push` -> `druid-client push [artifact] [dir]`
  - old `druid registry login` -> `druid-client login ...`
- `druid-client pull` keeps current behavior:
  - pulls into optional dir or current working directory
  - includes data by default
  - `--no-data` skips data files
- `druid-client create` first asks the daemon to materialize:
  - Kubernetes daemon creates PVCs and runs a `druid-client pull` Job in-cluster.
  - Docker daemon returns materialization unsupported, then client falls back to local materialization into `state/scrolls/<id>/spec` and `state/data/<id>/data`.
  - explicit `--scroll-root`/`--data-root` still materializes directly into those daemon-visible paths.
- `druid-client register [dir] [name]` reports an already checked-out scroll directory without OCI checkout/copying.
- Kubernetes create path: daemon/controller creates PVCs, runs a `druid-client pull` Job, stores runtime scroll state in ConfigMaps, stores opaque `k8s://namespace/pvc` refs there, and runs procedures as Kubernetes Jobs or StatefulSets depending on run mode.
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
/api/v1/procedures
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
<state>/scrolls/<id>/spec     # daemon-owned scroll spec root; contains scroll.yaml
<state>/data/<id>/data        # runtime data directory; mounted into containers by explicit mounts
```

Domain:

- `RuntimeScroll.ScrollRoot`: daemon-owned spec root
- `RuntimeScroll.DataRoot`: runtime data root parent
- Runtime config generated at `<DataRoot>/data/.druid/runtime.json`

SQLite store:

- `internal/core/services/runtime_state_store.go`
- Table: `scrolls`
- `data_root` migration exists.

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
<DataRoot>/data/<sub_path> -> <mount.path>
```

## Runtime Config

- Generated by daemon before running commands.
- Location: `<DataRoot>/data/.druid/runtime.json`
- Includes:
  - scroll id/name/artifact
  - runtime backend and generated time
  - top-level ports
  - expected ports by procedure
- Coldstarter now supports `--runtime-config` and should prefer it over reading `scroll.yaml`.

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
- Legacy `mode`, `wait`, and `data` procedures are rejected during validation.

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
- Removed `druid daemon`, `druid runtime`, `druid runtime serve`, `druid stop`.
- Removed old port monitor/watch-port flow.
- Removed plugin system files.
- Removed legacy handlers/server/middlewares.

## Key Files

- `apps/druid/adapters/cli/root.go`: current `druid` command surface.
- `apps/druid/adapters/cli/serve.go`: daemon startup/listener setup.
- `apps/druid/adapters/http/handlers/routes.go`: generated REST registration plus manual `/health` and websocket route.
- `apps/druid/adapters/http/handlers/scroll_handler.go`: generated OpenAPI server handler methods for runtime scrolls.
- `apps/druid/core/services/runtime_controller.go`: run command, write runtime config, port status.
- `apps/druid-client/adapters/cli/create.go`: local materialization then daemon registration.
- `apps/druid-client/adapters/cli/pull.go`: client-owned OCI pull.
- `apps/druid-client/adapters/cli/push.go`: client-owned OCI push.
- `apps/druid-client/adapters/cli/login.go`: client-owned registry login.
- `apps/druid-client/adapters/daemon/openapi_client.go`: generated OpenAPI client adapter.
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
./bin/druid-client --daemon-socket <tmp>/runtime.sock create smoke examples/static-web --state-dir <tmp>/state
verified:
  <state>/scrolls/smoke/spec/scroll.yaml exists
  <state>/data/smoke/data exists
  <state>/data/smoke/data/scroll.yaml does not exist
  druid-client describe smoke works
```

## Known Warning

`make build` and `make generate-api` emit:

```text
WARNING: You are using an OpenAPI 3.1.x specification, which is not yet supported by oapi-codegen...
```

This is known and currently non-blocking.

## Important Follow-Ups

- DB-first daemon resume is still conceptual, not implemented:
  - daemon startup does not yet restore runners/sessions from `RuntimeScroll.Status` and `RuntimeScroll.Commands`.
  - `RunRuntimeScrollCommand` still creates queue machinery per command invocation.
  - Need a daemon-owned per-scroll session/controller eventually.
- DB command statuses are not yet persisted on every queue transition.
- `scroll-lock.json` still exists in services and queue behavior; DB should become authoritative later.
- `runtime_instance_manager.go` filename still says instance; consider renaming to match `RuntimeScrollManager`.
- `druid-client create` local materialization assumes shared filesystem with daemon unless explicit `--scroll-root` and `--data-root` are passed.
- Kubernetes design still needs proper backend refs instead of local filesystem paths.
- Docs generated under `docs_md` are stale/incomplete after command flattening; deleted stale registry/runtime command pages but did not regenerate docs.

## Current Mental Model

Docker/local create:

```text
druid serve --runtime docker
druid-client create <artifact-or-path> [name]
  -> client materializes OCI/local artifact into runtime state
  -> client POSTs generated OpenAPI CreateScrollRequest with scroll_root/data_root
  -> daemon reads scroll.yaml through its configured runtime backend
  -> daemon caches scroll.yaml in SQLite

druid-client register [dir] [name]
  -> client reports already checked-out dir
  -> daemon reads scroll.yaml through its configured runtime backend
  -> daemon caches scroll.yaml in SQLite

druid-client run <id> <command>
  -> daemon writes runtime config
  -> daemon launches Docker procedure containers using explicit data mounts
```

Runtime is daemon-only: `druid-client create/register/list/describe` do not send, store, or display a per-scroll runtime.

Future Kubernetes create:

```text
controller/daemon creates PVC/spec volume
Kubernetes Job runs: druid-client pull <artifact> [mounted-dir]
daemon/controller registers materialized scroll
backend creates Jobs/Deployments/StatefulSets from scroll procedures
```
