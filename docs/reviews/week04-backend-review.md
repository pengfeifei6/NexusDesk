# W04 backend review

Reviewed uncommitted backend changes against baseline 7f1d7d2, the supplied diff, AGENTS.md, claim API and ADR-0004, with W03 authorization/transaction context. No repository edits, secrets, migrations, dependency changes, or test reruns. Existing `docs/weekly/evidence/week04-postgres.log` was inspected; its lines 997–1034 show the 2-request round, twenty 20-request rounds, 18 observed concurrent database lock waiters, rollback injections and cancellation passing. This is review of that evidence, not a new execution claim.

## Verdict

Spec compliance: PASS for reviewed backend. Quality: suitable to proceed, with one minor test-cleanup improvement. No Critical or Important findings. Frontend/browser and final documentation reconciliation remain for whole-delivery review.

## Strengths

- `internal/ticket/repository.go:163`: one conditional UPDATE includes org/id, OPEN, NULL assignee, nondeleted parent, exact version and int64 upper bound. Actor is a bound parameter from trusted middleware/service identity; no new migration or lock service is needed.
- `internal/ticket/service.go:178`: membership is checked within the write transaction; zero-row updates become a scoped visibility lookup, so missing/wrong-org/deleted objects return 404, visible conflicts return the dedicated 409, lookup errors stay failures. Same-user repeats also conflict. Existing locking lookup does not precede the claim UPDATE.
- `internal/ticket/service.go:72` and `:196`: mutation and one TICKET_ASSIGNED event share the transaction; result is discarded on event/commit errors. Rollback has an independent bounded context; cancellation is propagated. Audit before/after and actor match the actual assignment, and success leaves status OPEN.
- `internal/ticket/handler.go:333`: shared strict request decoder, trusted scope, complete existing DTO, no-store and static safe errors are reused rather than duplicated.
- `tests/integration/ticket_claim_test.go:196`: parallel test is substantial: 20 distinct PostgreSQL connection PIDs, synchronized HTTP handlers, a deliberately held row, observed concurrent UPDATE lock waiters, 20 rounds plus two-user competition, and winner/row/version/event assertions. Real insert-trigger and deferred-commit failures verify rollback. Service unit tests cover missing/disabled membership, visibility, invalid versions, lookup failure, same-user repeat and cancellation.
- Smoke claim flag correctly enables W03 and W02 prerequisite gates and uses the existing isolated-resource runner.

## Findings

Critical: none.

Important: none.

Minor — failure-path fixture cleanup can hide the original test failure (`tests/integration/ticket_claim_test.go:205`, also initial connection warming at lines 43–52).

After `pool.Begin`, the competition test registers no deferred rollback before the row-lock query and activity observation. If either query errors and calls `t.Fatal` (lines 211 or 243), the checked-out transaction is never released. The parent test's deferred `pool.Close()` waits for checked-out connections, so a useful immediate error can instead become the smoke runner's 240-second timeout. Similarly, a failure acquiring the twentieth warmup connection leaves earlier leases unreleased. Reproduction by fault injection: make the activity query fail after acquiring the lock, or make a later warmup acquisition fail. This is a test robustness issue, not a product atomicity defect. Register bounded independent-context rollback immediately after Begin, and arrange release cleanup for already-acquired leases before later acquisitions can fail. No fault injection was executed in this read-only review.

## Considered boundaries / limits

- Concurrent SQL membership revocation and indeterminate network failure during COMMIT were considered, not silently dismissed: ADR explicitly preserves current non-linearizable revocation semantics and disclaims request-level exactly-once outcomes. No public revoke endpoint exists; database errors never produce a success response. These do not block this backend contract.
- Concurrent existing admin patch/delete/transition operations use the same row and version discipline; no new lost-update path was found. A successful claim can later be changed by an authorized admin, consistent with existing product rules.
- No fairness or fixed latency guarantee is inferred from the contention test. Two active users generating 20 requests is sufficient to prove this contract; 20 distinct users are not required.
- Frontend behavior after 409, browser flow, final old-document status updates, and public deployment readiness were not judged in this backend-only review. The old W03 documents still describe claim as future work; reconcile them during the already-planned delivery closeout.

## Root follow-up

Added cleanup for already-acquired warmup connections and bounded independent-context rollback for the contention fixture before fallible queries. Final W04 real-DB run passed after this change; no product semantics changed. Reviewer did not rerun or re-review.
