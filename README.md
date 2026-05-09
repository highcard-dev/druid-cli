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

This repository builds three isolated binaries:

- `apps/druid` -> `bin/druid`: daemon plus local validation/update tooling.
- `apps/druid-client` -> `bin/druid-client`: client-only CLI for daemon API and OCI commands.
- `apps/druid-coldstarter` -> `bin/druid-coldstarter`: standalone coldstart gate binary/image.

Build all binaries with:

```bash
make build
```

Common local flow:

```bash
druid serve --runtime docker
druid-client login --host <host> -u <user> -p <password>
druid-client pull <artifact> [dir]
druid-client push [artifact] [dir]
druid-client create <artifact-or-path> [name]
druid-client register [dir] [name]
druid-client run <id> <command>
druid-client describe <id>
```

For examples, omit `[name]` so each scroll derives its own id from `scroll.yaml`.

### Dependency based command runner

The way commands are handled is described in the `scroll.yaml` and is similar to how Github Actions work, with support for long-running container commands.
Commands can also depend on each other.

### Web Server

There is a web server included, so you can control daemon-managed containers remotely.
There is also websocket support for stdout. TTY is also supported.

### Runtime backend

Runtime selection is daemon-only: start the daemon with `druid serve --runtime docker`, then use `druid-client` to create, register, run, and inspect scrolls without passing a runtime. Docker runtime state stays in SQLite under the runtime state directory. Scroll specs and runtime data are materialized separately so containers only receive explicit mounts from runtime `data/`.

Kubernetes runtime support is available with `druid serve --runtime kubernetes` for in-cluster daemons or out-of-cluster daemons using kubeconfig. It stores daemon scroll state in ConfigMaps, materializes OCI artifacts through cluster Jobs, and uses Cilium/Hubble Relay for port traffic presence. See `docs/kubernetes_runtime.md` for kubeconfig, RBAC, PVC, and Hubble setup.

## Documentation

Read more at https://docs.druid.gg/cli
