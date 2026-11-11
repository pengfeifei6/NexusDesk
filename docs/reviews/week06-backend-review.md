# Task 2 independent backend review

Reviewed 2026-11-11. Verdict: **changes requested for one P2 finding**. No production files changed.

## Finding

### P2 — Bound repository reads, transactions, and rollback separately from model budgets

Locations: `internal/rag/index_repository.go:47`, `:91`, `:131-137`; caller `internal/rag/service.go:156-160`.

Both transaction methods defer `tx.Rollback(context.Background())`. That cleanup has no deadline, so a database/network stall while rolling back an otherwise healthy transaction can keep the request and pooled connection alive beyond the index operation's timeout. The independent three-second failure context does not fix this: `Finish`/`Claim` must return through the deferred rollback before the service can report the error or record failure.

The status path has the same missing-boundary problem for ordinary SQL: `IndexStatus` passes the incoming HTTP context directly to `Status`, and neither supplies a deadline. The earlier `knowledge.Get` call's three-second child context ends inside that method and does not constrain this subsequent query. A locked index table, stalled query, or pool exhaustion after that check can therefore hold the status request indefinitely. `http.Server.WriteTimeout` limits socket writes; it is not a handler/database-context deadline. The database pool setup only sets connection establishment timeout, not query deadlines.

Use a bounded independent rollback helper (the existing W05 repository uses three seconds), a short context for status reads, and short child contexts for claim/finalize transactions while preserving the outer 60-second indexing budget. Ensure newly introduced local transaction timeouts still map to `INDEX_TIMEOUT`, including when the outer operation context has not yet expired. Add focused deadline propagation/cleanup tests and a disposable-database blocked-query test; the existing fake repository tests do not exercise these calls.

## Reviewed behavior without another actionable defect

- Claim and finish consistently lock parent, text, then index; claim serializes first insertion through the parent lock. Matching READY hash/model/dimension/chunker is reused, INDEXING conflicts, and actual retries increment versions.
- Finish checks source hash, claimed version, and INDEXING under locks before replacing chunks and publishing READY in one transaction. Model work occurs outside transactions. Conditional failure cannot overwrite READY or a different run version; uncertain claim outcomes do not fail an unowned run.
- Search and final validation join organization IDs throughout, visible READY parent/current text/READY index, matching source hashes, model/dimension/chunker, and index versions. Vector parameters are bound, finite/nonzero validation precedes casts, and ordering uses distance plus chunk ID.
- Search rechecks membership and all retrieved rows. Ask also rechecks after generation, including uncited evidence. Returned sources come from selected SQL rows; citation IDs are checked and deduplicated. The production generator additionally validates against only sources retained in its prompt budget. No-evidence and model-declared insufficiency use the fixed refusal.
- HTTP uses existing strict bounded JSON parsing, rejects unknown/duplicate fields and malformed Unicode, checks optional integer top_k, and authorizes before parsing body inputs. Errors use fixed public messages.
- Disabled RAG keeps index-status reads available; main wiring creates a local adapter without contacting or pulling models. The shared adapter instance preserves admission limits. Migration 9/10 constraints and organization-composite foreign keys align with the design; Down does not remove the shared extension.

## Evidence and limits

Read `AGENTS.md`, the W06 design/API/implementation plan, task-2 implementation report, relevant architecture/database/repository contracts, all new rag service/repository/handler files and their tests, migrations 9/10, main injection, and supporting W05 authorization/storage SQL, shared auth parser, database pool setup, and generator interface/selection validation. Existing `models.go`/`errors.go` were inspected for contracts, not re-reviewed as a separate deliverable.

This is static source review. Tests were inspected but **not run or rerun**, as assigned; the integration agent owns real SQL and concurrent execution evidence. No real environment, credentials, private uploads, runtime services, database state, or model endpoints were accessed. Consequently this report does not certify PostgreSQL lock behavior or migration execution empirically, semantic retrieval quality, or passing test results. No additional SQL isolation/citation defect was identified within this scope.

## Follow-up disposition — 2026-11-11

**The P2 finding is resolved in source; approved on static re-review, with the integration agent's GREEN result still pending at review time.**

`internal/rag/index_repository.go:16-24` now gives rollback its own three-second background context and maps expired local database contexts through named error results. Claim and Finish use five-second child contexts; Status, Fail, Search, and Validate use three-second child contexts. Child deadlines preserve earlier caller cancellation/deadlines. Deferred error mapping runs before cancellation of the local context and after transaction cleanup, and preserves successful commits by only replacing a non-nil error. Index operations map local expiry to `INDEX_TIMEOUT`; retrieval uses `MODEL_TIMEOUT`, consistent with the existing public code set. The SQL locking, version guards, and atomic publication remain intact.

Inspected the new `TestWeek06DatabaseDeadlines` cases: an exclusive index-table lock checks the status budget and a parent-row lock checks the claim budget, with longer caller deadlines to distinguish repository enforcement; claim also asserts no model call. The supplied RED log `/tmp/week06-integration-first.log` records the original status/claim failures. No suites were rerun by this reviewer. No further actionable concern was found in the fix. Network-stalled rollback itself is bounded by code inspection, rather than a new failure-injection test in the reviewed cases.
