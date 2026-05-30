---
title: "Kubernetes keepAliveTraffic"
sidebar_label: Kubernetes keepAliveTraffic
---

## Kubernetes keepAliveTraffic

Kubernetes runtimes use Hubble Relay to evaluate `keepAliveTraffic` on expected ports.

When a running job procedure has an expected port with `keepAliveTraffic`, druid checks for matching Hubble flows over the configured window. If the full window has elapsed and no flow is observed, druid deletes that procedure job and records it as a clean stop. The command run mode is not changed; `restart` and `persistent` scheduling decide what runs next.

Coldstarter procedures are not stopped by this rule. For Minecraft restart-mode scrolls, put `keepAliveTraffic` on the real runtime procedure's `main` expected port, not on the coldstarter procedure.

The current Hubble integration tracks flow presence. Use a minimum such as `1b/60m` to mean "at least one observed flow in the last 60 minutes".

Required daemon configuration:

```
DRUID_HUBBLE_RELAY_ADDR=hubble-relay.kube-system.svc.cluster.local:80
```

Validation commands:

```
kubectl -n kube-system get svc hubble-relay
kubectl -n kube-system rollout status deployment/hubble-relay
kubectl -n druid-system get deploy druid-cli -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="DRUID_HUBBLE_RELAY_ADDR")].value}{"\n"}'
```

If Hubble Relay is disabled or unavailable, druid does not stop any procedure for missing traffic and reports `hubble-relay-unavailable` in port status/logs.
