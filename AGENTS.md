# Druid CLI Agent Guide

## Local daemon command selection

Keep the normal Linux and macOS development path on the existing command:

```bash
make watch
```

That command uses `http://host.k3d.internal:8083` for Kubernetes worker
callbacks and must remain free of Windows or WSL auto-detection.

When Docker Desktop runs on Windows and the repository is executed inside
WSL2, use the explicit Windows command instead:

```bash
make watch-windows
```

The Windows command derives the current WSL source address and passes its
callback URL to the unchanged `make watch` implementation. Set
`DRUID_HOST_SERVICES_IP` to override address discovery, or set
`DRUID_WORKER_CALLBACK_URL` to override the complete callback URL.

Do not use `make watch-windows` on native Linux or macOS. Keep future
platform-specific setup additive instead of adding OS detection to `make
watch`.
