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

Use `druid serve --runtime docker` for container execution. The daemon listens on a Unix socket, and `druid` connects to that socket with `--daemon-socket`. `druid pull` downloads artifacts, while `druid create <artifact-or-path> [name]` materializes a scroll and registers it with the daemon. For already checked-out examples, pass the local directory to `druid create`. Run commands with `druid run <id> <command>` and inspect state with `druid describe <id>`.

Runtime procedures use `image`, `command`, `working_dir`, `env`, `ports`, `mounts`, `signal`, and `tty` directly on each procedure.

The coldstart gate is a normal command that runs `druid-coldstarter` from the same runtime image as other Druid workers. It is configured only through env, with `DRUID_ROOT` pointing at the mounted runtime root. Custom coldstart handlers belong in the scroll root, for example `packet_handler/minecraft.lua`.

The `container-lab` example intentionally avoids coldstarter so it can be used as a broad runtime smoke test for Docker and Kubernetes:

```bash
druid create examples/container-lab container-lab
druid describe container-lab
druid ports container-lab
druid run container-lab verify
druid run container-lab report
druid run container-lab stop
```
