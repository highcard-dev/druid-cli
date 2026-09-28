# Runtime lifecycle acceptance

Verified locally on 2026-09-28. This is runtime acceptance, not complete product acceptance.

## Contract implemented

- Updates require an explicitly accepted `repository@sha256:<64 hex>` reference. Empty references and tags fail before stopping the workload.
- Reconciliation no longer changes an installed release. Explicit updates and restores own those transitions.
- The installed release endpoint reads the actual volume's `manifest.json` and canonical Scroll name through a read-only worker. It does not resolve the deployment tag or add another persisted baseline.
- Callers can send `expected_installed_digest` with an accepted update. The runtime checks the actual installed descriptor under its maintenance lock and rejects a stale approval with HTTP 409 before stopping or changing the workload.
- The owner-authenticated public listener exposes installed-release reads and explicit updates, using the same runtime handlers as management. Tests reject missing and cross-owner authorization before either operation.
- Updates stage the whole candidate, preserve protected paths, and remove obsolete unprotected files. Both installed and candidate `skip_update` declarations protect the current transition, including nested declarations removed by the candidate.
- Protection longevity is unresolved: if a release removes a protected declaration, this transition retains its data, but indefinite retention across later releases is not guaranteed. No hidden persisted protection list or finalized-metadata rewrite was introduced.
- Failed rollback retains recovery files and keeps the workload stopped. Recovery locations are included in the error.
- Backup mode snapshots all runtime data, including files outside explicit release chunk selections, and preserves the installed descriptor bytes.
- Command admission shares the maintenance lock. Waiting for a command does not hold that lock, and waiters do not overwrite state after a release transition.
- Protected-path copying preserves symlinks without dereferencing them and rejects paths traversing symlink parents.
- CLI authored pushes honor explicit `SOURCE_DATE_EPOCH` for the OCI creation annotation. Without it ORAS stamps current time; identical CI rebuilds then produce different digests. Normal pushes retain existing timestamp behavior. Invalid explicit timestamps are rejected.
- Root file layers are sorted before packing; Go map iteration must not change an identical release's manifest digest.
- `druid-scroll-validator` exposes the existing runtime semantic rules as a bounded, read-only stdin/JSON protocol for Core. It does not expand host environment variables, initialize CLI config, extract archives or run commands. `make build` and `make install` include this helper.

## Evidence

- Full `go test ./... -timeout=180s`: passed.
- Regressions were observed failing before fixes for removed protected chunks, rollback-file retention, omitted runtime-created backup files, command admission during maintenance, and symlink traversal.
- `TestKubernetesBackendCLIComplexLifecycle`, with rebuilt daemon/worker binaries and image, passed three times (100.42s, 103.78s, 104.95s). The final run includes command-admission, complete-backup, and symlink-hardening changes.
- Focused command-admission/maintenance tests passed with Go's race detector enabled.
- The installed-digest precondition passed focused runtime/HTTP/client tests and a further rebuilt-image Kubernetes lifecycle run (102.55s), including successful v2 acceptance, rejection of the stale original baseline with HTTP 409, unchanged v2 contents, and backup restoration.
- The Kubernetes test uses a unique namespace, PVC, registry, management ports, and daemon socket. It does not replace the shared daemon/operator or consume canonical ReservedPorts.
- Verified real workload start and command execution; stopped backup; installed-descriptor read; acceptance of v2 followed by moving the tag to v3; installation of v2; preservation of protected runtime data; restoration of v1, runtime-created data outside declared chunks, and byte-identical installed descriptor.
- Test harness failures were diagnosed separately: registry:2 needs an OCI Accept header, and explicit fixture chunks must include the version marker.
- Recovery-restart tests use persistent workload fixtures. The previous finite `true` command legitimately stopped immediately and was not evidence of a failed restart.
- Authenticated local publisher acceptance demonstrated the timestamp problem with identical layers and different creation times, then passed with identical finalized digests after rebuilding with a fixed source date (7.10s, including fixture cleanup). Focused CLI timestamp tests passed.
- A later run exposed a second reproducibility issue: `.meta` and `scroll.yaml` layers changed order. A local OCI regression failed in 0.08s with identical layer digests in different orders. Sorting root paths fixed it; three 32-push regression runs and a race-enabled run passed.
- After both reproducibility fixes, normal dedicated-account sign-in through the gateway, private import, identical rebuild/retry, shared publication and anonymous visibility passed three consecutive times (8.53s, 6.25s, 6.16s), including fixture cleanup.
- Validator negative cases cover empty procedures, missing images, invalid versions, missing descriptions, unsafe mounts, unknown expected ports, duplicate IDs, invalid signal procedures, legacy fields, malformed/oversized input and literal environment placeholders. Focused race tests and the full CLI Go suite pass.
- Core reuses this validator for create/import/promote and repository-wide publication. Its Core-only image builds the validator from pinned CLI commit `9490beb01fd32716ea00191f9b97d6e4d14da374`; the actual image passes offline semantic/fail-closed smoke tests, including execution in a read-only container with all capabilities dropped. The live local publisher passes again with validation enabled (8.29s).

## Remaining product work

- Server/UI check-and-apply wiring is implemented in the separate monorepo worktree but shared-stack integration, Team publication, and browser acceptance remain outside this runtime milestone.
- The shared browser fixture is blocked by all canonical local game ports being allocated. No existing deployment was retired.
- This changes the update API/CLI contract; callers must send the accepted immutable reference. Deploy the matching callers with this runtime revision.
