# Runtime lifecycle acceptance

## Current scope (2026-09-29, local only)

This feature is evaluated against master `bc17ea075910c3390402e3323fec243975f5ee2b`.
Only Unified Scroll Lifecycle changes and problems introduced by those changes
belong in this diff. Pre-existing general bugs are deferred, not claimed fixed.

The approved scope pruning is complete locally. Broad operation/attempt IDs,
durable admission/recovery stores, worker-completion/cleanup protocol changes,
snapshot ownership metadata, generic local materialization-copy changes, and
UI-package persistence repairs have been removed. Ordinary Stop/requeue and
platform-wide Docker host-gateway changes have also been excluded from the PR
delta. Late semantic validation, failed-stop Update retry, and queue-drain timeout
repairs remain deferred because those problems already exist on master.

## Retained contract

- Explicit updates require an accepted immutable digest, with an optional
  expected-installed-digest precondition checked under the maintenance lock.
- Reconciliation does not silently update an installed release. The read-only
  installed-release endpoint reads the volume's actual descriptor.
- Updates stage the candidate, remove obsolete unprotected files, and preserve
  paths protected by either installed or candidate declarations. Globs expand
  against both trees. Protected-copy modes, numeric owners and symlinks survive
  the new staging path; Docker/Kubernetes update helpers do not recursively
  overwrite those owners afterward.
- Removed protection declarations protect this transition, not indefinitely
  across future releases. No hidden persisted protection list was introduced.
- Backups include runtime-created files and preserve the installed descriptor.
  Successful maintenance can restart the workload. Failed backup/restore leaves
  it stopped with an error; error text is not proof that restarting is safe.
- Command admission shares the maintenance lock, but waiting does not hold it.
- Explicit source timestamps and stable layer ordering support reproducible
  Team publication. The semantic-validator binary supports Core lifecycle APIs.

## Current verification

After pruning:

- `go test ./... -timeout=180s`: passes.
- Rebuilt-image `TestDockerUpdatePreservesProtectedOwner`: passes (15.77s),
  including custom-owner workload access, symlinks, permission preservation,
  new-data default ownership and failure before replacement when chown fails.
- Focused protected-copy and failure-restart tests: three race-enabled runs pass.
- Rebuilt-image `TestKubernetesBackendCLIComplexLifecycle`: passes (102.77s)
  after pruning. Exercises a real workload in a disposable namespace, accepted
  digest despite tag movement, stale-baseline rejection, protected data, complete
  backup restoration and byte-identical installed descriptor. It does not run
  the browser/Core/engine/operator chain.
- Earlier broader crash/recovery evidence does not apply to the narrowed code.

## Remaining acceptance limits

This is not a claim of full product merge readiness. Team workflow cutover,
full browser-to-workload acceptance and CI on the final local changes are still
outstanding. No shared services, production deployments, existing workloads,
remote branches or PRs were changed by this pruning pass.

## Workspace recovery (2026-10-03)

The temporary worktree and its temporary backup archives were lost. The retained
September 29 source/test state was reconstructed from successful session file
changes and complete file reads, on top of commit `a9f5dd9`. It now lives in
`druid-local/.worktrees/scroll-lifecycle/druid-cli` on the local branch
`recovery/scroll-lifecycle-20261003`. Reconstruction tooling and the recovered
schema are under `druid-local/.recovery/lifecycle-20261003`.

The recovered full Go suite passes. Docker/Kubernetes results above are
historical September 29 evidence, not fresh acceptance of this recovered tree.
The temporary pre-pruning archives are unavailable; do not rely on their old
paths as backups. Deferred behavior remains excluded as described above.
