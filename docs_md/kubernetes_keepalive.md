---
title: "Kubernetes keepAliveTraffic"
sidebar_label: Kubernetes keepAliveTraffic
---

## Kubernetes keepAliveTraffic

Kubernetes runtimes use kubelet pod network stats to evaluate `keepAliveTraffic` on running procedures.

When a running job procedure has an expected port with `keepAliveTraffic`, druid samples that procedure pod's RX/TX bytes from `/api/v1/nodes/<node>/proxy/stats/summary`. If the full configured window has elapsed and the RX-byte delta is below every configured threshold, druid deletes that procedure job and records it as a clean stop. The command run mode is not changed; `restart` and `persistent` scheduling decide what runs next.

Coldstarter procedures are not stopped by this rule. For Minecraft restart-mode scrolls, put `keepAliveTraffic` on the real runtime procedure's `main` expected port, not on the coldstarter procedure.

Use values such as `10kb/5m` to mean "at least 10 KiB of pod RX traffic in the last 5 minutes". The metric is procedure-level: a single procedure pod can satisfy any of its configured keepalive expected ports.

Required Kubernetes RBAC:

```
apiGroups: [""]
resources: ["nodes/proxy"]
verbs: ["get"]
```

Validation commands:

```
kubectl auth can-i get nodes/proxy --as=system:serviceaccount:druid-system:druid-cli
kubectl get --raw '/api/v1/nodes/<node>/proxy/stats/summary' | head
```

After daemon restart, druid fails open until enough pod-stat samples exist to cover the configured window. If pod stats are unavailable or the active pod cannot be resolved, druid does not stop the procedure for missing traffic and reports `kubernetes-pod-stats-unavailable` in port status/logs.
