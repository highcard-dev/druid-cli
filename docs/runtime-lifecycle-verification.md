# Runtime lifecycle acceptance

Verified locally on 2026-09-28. This is runtime acceptance, not complete product acceptance.

## Contract implemented

- Updates require an explicitly accepted `repository@sha256:<64 hex>` reference. Empty references and tags fail before stopping the workload.
- Reconciliation no longer changes an installed release. Explicit updates and restores own those transitions.
- The installed release endpoint reads the actual volume's `manifest.json` and canonical Scroll name through a read-only worker. It does not resolve the deployment tag or add another persisted baseline.
- Updates stage the whole candidate, preserve protected paths, and remove obsolete unprotected files. Both installed and candidate `skip_update` declarations protect the current transition, including nested declarations removed by the candidate.
- Protection longevity is unresolved: if a release removes a protected declaration, this transition retains its data, but indefinite retention across later releases is not guaranteed. No hidden persisted protection list or finalized-metadata rewrite was introduced.
- Failed rollback retains recovery files and keeps the workload stopped. Recovery locations are included in the error.
- Backup mode snapshots all runtime data, including files outside explicit release chunk selections, and preserves the installed descriptor bytes.
- Command admission shares the maintenance lock. Waiting for a command does not hold that lock, and waiters do not overwrite state after a release transition.
- Protected-path copying preserves symlinks without dereferencing them and rejects paths traversing symlink parents.

## Evidence

- Full `go test ./... -timeout=180s`: passed.
- Regressions were observed failing before fixes for removed protected chunks, rollback-file retention, omitted runtime-created backup files, command admission during maintenance, and symlink traversal.
- `TestKubernetesBackendCLIComplexLifecycle`, with rebuilt daemon/worker binaries and image, passed three times (100.42s, 103.78s, 104.95s). The final run includes command-admission, complete-backup, and symlink-hardening changes.
- Focused command-admission/maintenance tests passed with Go's race detector enabled.
- The Kubernetes test uses a unique namespace, PVC, registry, management ports, and daemon socket. It does not replace the shared daemon/operator or consume canonical ReservedPorts.
- Verified real workload start and command execution; stopped backup; installed-descriptor read; acceptance of v2 followed by moving the tag to v3; installation of v2; preservation of protected runtime data; restoration of v1, runtime-created data outside declared chunks, and byte-identical installed descriptor.
- Test harness failures were diagnosed separately: registry:2 needs an OCI Accept header, and explicit fixture chunks must include the version marker.
- Recovery-restart tests use persistent workload fixtures. The previous finite `true` command legitimately stopped immediately and was not evidence of a failed restart.

## Remaining product work

- Server/UI check-and-apply wiring, shared Team publication, and shared-stack browser acceptance remain outside this runtime milestone.
- The shared browser fixture is blocked by all canonical local game ports being allocated. No existing deployment was retired.
- This changes the update API/CLI contract; callers must send the accepted immutable reference. Deploy the matching callers with this runtime revision.
