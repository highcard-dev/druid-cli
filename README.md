# Druid CLI

This repository contains the Druid runtime tools for packaging Scrolls as OCI artifacts, serving the local runtime daemon, and controlling daemon-managed Scrolls.

A good use case is to let it run inside of a docker container. It will give additional insights and management abilities.

The current runtime backends are Docker for local development and Kubernetes for in-cluster or kubeconfig-backed cluster operation.

## Installation

We publish [releases on Github](https://github.com/highcard-dev/druid-cli/releases).

You can easlily install druid-cli on Linux by running:

```bash
curl -L -o druid "https://github.com/highcard-dev/druid-cli/releases/latest/download/druid" && sudo install -c -m 0755 druid /usr/local/bin
```

Also consider our installation documentation: [https://docs.druid.gg/cli/introduction](https://docs.druid.gg/cli/introduction)

## Scroll OCI manifest

The Druid CLI uses a **so called Scroll** to describe container-backed commands.
A scroll can also include files.
A Scroll is an OCI Artifact, so it is easy to distribute with registries like Dockerhub.

## Features

### Binaries

This repository builds two runtime binaries:

- `apps/druid` -> `bin/druid`: daemon, REST-backed CLI, OCI commands, and internal worker mode.
- `apps/druid-coldstarter` -> `bin/druid-coldstarter`: coldstart gate binary included in the runtime image.

Build all binaries with:

```bash
make build
```

Common local flow:

```bash
druid daemon --runtime docker
druid login --host <host> -u <user> -p <password>
druid pull <artifact> [dir]
druid push [artifact] [dir]
druid create <artifact-or-path> [name]
druid run <id> <command>
druid describe <id>
```

For examples, omit `[name]` so each scroll derives its own id from `scroll.yaml`.

### Dependency based command runner

The way commands are handled is described in the `scroll.yaml` and is similar to how Github Actions work, with support for long-running container commands.
Commands can also depend on each other.

### Web Server

There is a web server included, so you can control daemon-managed containers remotely.
There is also websocket support for stdout. TTY is also supported.

### Runtime backend

Runtime selection is daemon-only: start the daemon with `druid daemon --runtime docker`, then use `druid` to create, run, and inspect scrolls without passing a runtime. Docker runtime state stays in SQLite under the runtime state directory. Scroll specs and runtime data live together in one runtime root.

Agent tools are exposed through `/:id/mcp` and normal REST resource routes.
Existing queue, command-run/removal and UI-publication routes are registered once
and accept either their existing owner authentication or scoped workload credentials.
Other owner REST endpoints are unchanged.

REST paths below are relative to `/:id/api/v1`. Tool arguments are JSON bodies;
GET limits use `tail_lines` or `timeout_seconds` query parameters. Workload
responses use the same result objects as MCP. Existing owner handlers preserve
their previous response shapes and status codes.

| MCP tool | REST operation |
| --- | --- |
| `list_commands` | `GET /commands` |
| `create_command` | `POST /commands` |
| `remove_command` | `DELETE /scroll/commands/:command` |
| `run_command` | `POST /command` |
| `wait_for_command` | `GET /commands/:command/wait` |
| `get_queue` | `GET /queue` |
| `read_logs` | `GET /consoles/:console/logs` |
| `search_logs` | `POST /consoles/:console/logs/search` |
| `send_stdin` | `POST /consoles/:console/stdin` |
| `get_deployment_status` | `GET /status` |
| `publish_ui` | `POST /ui/packages/:scope/publish` |

`remove_command` requires `commands:remove`, stops the command and removes its
definition, queue/status and runtime containers. Its console/log access is also
removed; deployment data and other commands are retained. New OpenHands commands
request this permission; existing Kubernetes agents require explicit recreation
to gain it.

Kubernetes authorizes each tool request when it is accepted. An accepted command
wait may finish after its caller's credential is revoked (at most 30 seconds);
subsequent requests must pass authentication again.

Commands have no execution IDs. `run_command` acknowledges the command name and
`wait_for_command` observes its current status. Restarted/replaced runs are not
distinguished, and delayed status events from older containers may update the
current command status.

**Docker agent-tool caveat:** HTTP/MCP tool endpoints on the Docker runtime are
unauthenticated and ignore command permissions. Anyone who can reach them can
invoke every tool for any deployment, including command creation/execution,
stdin, log access and UI publication. Docker creates no workload credentials or
identity registry. Use it only for trusted, single-user local development; keep
the daemon proxy bound to loopback and do not expose it to untrusted networks or
multi-tenant workloads. Container workloads may also reach these endpoints.
Existing authentication on unrelated dashboard, management and worker routes is
unchanged. Kubernetes retains Pod-bound authentication and per-tool permissions.

Kubernetes runtime support is available with `druid daemon --runtime kubernetes` for in-cluster daemons or out-of-cluster daemons using kubeconfig. It stores daemon scroll state in ConfigMaps, materializes OCI artifacts through `druid worker pull` Jobs, and uses kubelet pod stats for procedure-level traffic checks. See `docs/kubernetes_runtime.md` for kubeconfig, RBAC, and PVC setup.

## Documentation

Read more at https://docs.druid.gg/cli
