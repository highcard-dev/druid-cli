# Runtime Examples

These examples illustrate the container-first, runtime-backend scroll model.

They intentionally keep commands as orchestration groups and put executable runtime fields on `procedures`.

Each example declares the container paths it needs with `mounts`. Mounts are sourced from the runtime `data/` directory only. If `sub_path` is omitted, the whole `data/` directory is mounted; otherwise `sub_path` is relative to `data/`.

## Examples

- `minecraft`: finite install and coldstart procedures plus a restarting game server procedure.
- `mysql`: restarting database procedure with a persistent data subpath plus a finite backup procedure.
- `static-web`: build-once procedure served by a restarting web procedure.
- `jobs`: finite job-only pipeline that prepares data, transforms it, reports output, and exits.
- `container-lab`: container-only integration example with setup jobs, persistent web/cache services, ports, mounts, env, smoke checks, reports, and signal cleanup.

Use `druid serve --runtime docker` for container execution. The daemon listens on a Unix socket, and `druid-client` connects to that socket with `--daemon-socket`. The client owns OCI work: `druid-client pull` downloads artifacts, while `druid-client create <artifact-or-path> [name]` materializes a scroll and registers it with the daemon. For already checked-out examples, use `druid-client register [dir]` and omit `[name]` so ids are derived from each example's `scroll.yaml`. Run commands with `druid-client run <id> <command>` and inspect state with `druid-client describe <id>`.

Runtime procedures use `image`, `command`, `working_dir`, `env`, `ports`, `mounts`, `signal`, and `tty` directly on each procedure.

The coldstart gate is a normal command that runs the standalone `druid-coldstarter` binary/image. Build the local image with `make build-coldstarter-image` before running the Minecraft example. Custom coldstart handlers belong under `data/coldstart/` inside the canonical scroll volume.

The `container-lab` example intentionally avoids coldstarter so it can be used as a broad runtime smoke test for Docker and Kubernetes:

```bash
druid-client register examples/container-lab
druid-client describe container-lab
druid-client ports container-lab
druid-client run container-lab verify
druid-client run container-lab report
druid-client run container-lab stop
```
