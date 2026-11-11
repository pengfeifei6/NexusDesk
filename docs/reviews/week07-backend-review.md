# W07 Task 1 spec and quality review

Reviewed the Task 1 brief, implementation report, supplied snapshot diff, binding design and accepted builder corrections. Source review only; no production edits, commits, private data access, database/runtime work, or broad test reruns. The supplied diff is against the W07 snapshot, not HEAD alone. Targeted source reads resolved the retry-classification and HTTP-helper risks.

**Spec verdict: correction required for deterministic model failures; the remaining reviewed implementation is consistent with the binding contracts, subject to the separately assigned real PostgreSQL acceptance.**

**Quality verdict: request changes for the P2 finding below.** No concrete parent/job lock-order, stale-token publication, ended-attempt cleanup, or historical-column-reference defect was found statically. Reported unit/race/vet/build results are implementer evidence, not independently rerun results from this review.

## Ranked finding

1. **P2 — Generic upstream errors also represent permanent input/authentication failures, but are always retried.** `internal/jobs/errors.go:71` includes `MODEL_UPSTREAM_ERROR` in the transient branch. The existing adapter's `internal/platform/llm/client.go:108`–117 maps every non-200 status other than 404/408/429/503/504 to this code; `internal/platform/llm/client_test.go:168` explicitly covers HTTP 400 as `MODEL_UPSTREAM_ERROR`. Thus a deterministic 400 invalid-input response (also 401/403/422) causes RETRYING and up to three actual executions, contrary to design line 18's permanent invalid-input/not-authorized rule. The existing safe code alone does not establish transience. Keep the public error safe while either treating this ambiguous code as permanent or preserving a narrow typed retryability distinction from the adapter so only known transient upstream failures retry. Do not inspect upstream message text. Add a focused adapter-to-job policy check proving 400 fails after one attempt while 429/503 and the chosen transient 5xx behavior still follow 5s/30s backoff. Any adapter change is outside Task 1's original allowed files and should be coordinated with the root implementer.

## Verified by inspection

- Public payload and DTO contracts are present; strict numeric payload parsing rejects duplicate/unknown/missing fields, oversize input and noncanonical IDs. All five routes use fixed safe errors, authentication, organization authorization before target parsing, no query/body, and numeric IDs. Status queries require a visible parent; retry/cancel authorize creator/admin and recheck membership under the transaction.
- Submit stores original document SHA, while index/recovery comparison uses current extracted-text SHA separately. Dedup returns terminal rows unchanged, and disabled mode gates Submit/Retry while permitting Cancel.
- `withJob` locks parent before job. Domain Claim/Complete/Fail and RAG Claim/Finish/Fail also take the parent first; job-only heartbeat and dispatch reservations never acquire a later parent lock. Scans are bounded and unlocked, with predicates rechecked per item.
- Live task guards verify exact identity/run/token, actor membership, source identity and an unexpired lease. Renewal cannot revive an expired lease. Publication is rechecked after text/chunk writes, with NULL ownership predicates for synchronous completion/failure.
- Ended reconciliation is separate from the live guard, takes parent/job locks, resets only matching-token PROCESSING/INDEXING rows, preserves READY rows and transitions job state in the same transaction. Ordinary domain cleanup retains context guard values.
- Recovery prioritizes deleted/revoked/changed source over success. Valid complete publication is checked before attempt exhaustion, including current extractor/text/model/dimension/chunker/index-version chunk count; attempt 3 can succeed without a fourth execution.
- PostgreSQL guards actual attempts and due time, and dispatch outcome writes include run_version plus dispatch_seq. Unknown DB outcomes return a fixed delivery retry error; known no-ops and committed outcomes acknowledge safely.
- Migration 11 is additive and constrains nullable lower-hex ownership tokens, job state/lease/completion relationships and compound parent/membership references. All newly added domain task_token SQL is selected only after schema detection; historical column lists stay unchanged.

## Requested focused acceptance evidence

The existing Task 1 unit tests exercise parser/policy/defaults/unauthenticated HTTP/DTO shape and context-value preservation. They do not execute the new SQL, so the fresh integration assignment must provide the following evidence before final acceptance:

1. Real migration 11 constraints and cross-org FKs; authenticated five-route body/query/status contracts; disabled/nonmember/deleted invisibility and creator/admin retry/cancel.
2. Concurrent duplicate and old-run deliveries; due-time no-op; attempts capped at three; manual retry retains total_attempts/requested_by while replacing actor and incrementing run_version; expired heartbeat fails.
3. Cancel/revoke/delete with model work blocked, followed by attempted publication and stale cleanup; preserve READY and newer ownership, and never reset NULL-token legacy PROCESSING/INDEXING.
4. Attempt-3 crash after READY publication, followed by expired recovery to SUCCEEDED without another model call; original upload hash and normalized text hash deliberately differ in the fixture. Mismatched chunk count/current version must not falsely reconcile success.
5. Latest-schema synchronous claims conflict with nonterminal jobs; migration 8 knowledge Claim/Complete/Fail and migration 10 RAG Claim/Finish/Fail use the old SQL successfully.
6. Delayed enqueue outcomes cannot overwrite a newer run/dispatch; enqueue failure and delivery loss remain repairable without bypassing PostgreSQL backoff.
7. The P2 regression above: deterministic model HTTP failure is permanent, with no second actual execution.

No other concrete source finding. These acceptance requests are evidence gaps, not claims that the inspected SQL has failed.


---

# Task 1 fix-round-1 re-review

**Original P2: ADDRESSED. Spec verdict: compliant with the accepted conservative retry ruling. Quality verdict: approve this scoped fix; no new fix-related finding.**

Reviewed only `task-1-fix1.patch`, the appended implementation report, and the original review/brief context. No source edits, runtime work, broad test reruns, or unrelated source crawl.

`internal/jobs/errors.go` removes `MODEL_UPSTREAM_ERROR` from the transient branch and explicitly classifies it as permanent. This resolves the deterministic 400/401/403/422 retry defect while preserving the safe public code. Ambiguous 500 errors are also permanent, as explicitly ruled by the root implementer. Known unavailable/rate/timeout/busy errors retain their transient classification. No adapter behavior or public error format changes.

The new `TestModelHTTPFailurePolicyKnownTransience` exercises the real HTTP adapter before classification, including permanent 400/401/403/422/500 and transient 429/503/408/504; it also checks that upstream response text stays hidden. The reported RED failure of all five ambiguous-code cases and subsequent focused race GREEN directly cover the original finding. These are implementer-reported results, not independently rerun results from this review.

The seven real-PostgreSQL cases in `TestWeek07ModelHTTPFailureRetryPolicy` meaningfully check persisted attempts/status, one model call for permanent failures, 5s/30s scheduling and due-time no-op for transient failures, exhaustion after three calls, no fourth call, no published chunks, and released index ownership. Test-only due-time advancement occurs after checking the stored delay. The fixture controls only the external HTTP model response and uses real application services and PostgreSQL; it does not claim semantic model quality.

The root reports the earlier PostgreSQL/Redis attempt-2 acceptance passed. This review does not repeat or independently certify that run. The newly added seven PostgreSQL cases were compile/gate checked but skipped in the implementer's environment; their execution remains assigned to the root's owned runner. A passing run of those cases will close the remaining fix-specific acceptance evidence. No further source correction requested.
