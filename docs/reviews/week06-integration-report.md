# Task 4: real PostgreSQL/pgvector integration

Implemented only `tests/integration/knowledge_index_test.go` and `tests/integration/rag_search_test.go`, with this report. No production source edits, dependency changes, commits, browser runs, semantic evaluation, real environment/private upload reads, or daily database changes.

All fixtures require `NEXUSDESK_TEST_W06_RAG=1`, then the existing W05 and W03 gates; `ticketDB` enforces explicit `127.0.0.1` and `nexusdesk_w02d2_test_` database prefix. All five W06 top-level tests check these environment gates before starting subtests, so ordinary Go runs explicitly show five SKIP results rather than parent PASS with skipped children. Documents pass through real W05 upload/process using synthetic text and temporary private storage. Only model boundary implementations are test fakes; authorization, indexing, failure writes, vector SQL, and source validation are real PostgreSQL operations. Fakes are never used for semantic metrics.

## Cases

- Schema: vector(1024), wrong dimension, nonzero norm, composite organization/document FKs on both tables, chunk ordering uniqueness, model/chunker/hash/offset/version checks, index status/dimension/ready-count/timestamp/failure invariants. Fixed-model/chunker CHECKs reject alternate persisted model data without weakening the schema.
- Index: ABSENT, successful index and exact rune slices/hash/version/dimension, same-hash reuse without additional model calls, failure and successful retry with incremented versions. Count/dimension/zero/NaN/Inf outputs persist FAILED with allowed codes and no chunks.
- Real HTTP state/limit audit: newly UPLOADED and whitespace-extraction FAILED sources return 409 INDEX_STATE_CONFLICT; soft-deleted READY source returns 404. None claims an index, writes chunks, or calls the embedder. An 84081-rune READY document requires 201 windows and returns 422 INDEX_TOO_MANY_CHUNKS; version 1 FAILED, count 0, null indexed_at, zero chunks/model calls are checked in the real database.
- Parallel index: channel-controlled embedder barrier; a second claim conflicts without another embedding. Independent transaction acquires parent/text/index row locks with NOWAIT while the model is blocked, proving claim releases its locks before model work.
- Source race: changed hash or soft-deletion while embedding prevents READY/chunks. Changed source records FAILED; deletion leaves the prior INDEXING row untouched by conditional failure handling. Explicit cancellation persists PROCESS_CANCELED using the independent cleanup context.
- Atomic publication: owned per-document PostgreSQL trigger raises at second replacement INSERT. Assert the trigger error was actually reached, original chunk identity/content/version remains, new chunks disappear, index becomes FAILED, old chunks are not searchable. Cleanup removes only this test's trigger/function.
- Real cosine SQL: best pair tie broken by chunk ID, score 1 and approximately 0.6, min-score excludes orthogonal vector, top_k limits results, exact source metadata. EXPLAIN (ANALYZE, BUFFERS) of the repository-equivalent scoped SQL is in the run log; small synthetic fixture only, no ANN or scale/performance claim.
- Retrieval excludes foreign org, changed text/chunk/index hashes, mismatched chunk/index versions, deleted/FAILED parent, FAILED/INDEXING index. HTTP unauthenticated401, foreign/disabled404, cross-org document404; rejected requests make no embedding calls. Membership revoked during query embedding cannot receive results.
- Ask: no evidence fixed refusal skips generator; unknown/missing citations reject output, duplicates collapse to stored evidence, model insufficiency uses fixed refusal. Channel barriers mutate cited or uncited source deletion/hash, uncited version, or member status after generation starts; final validation blocks output for every case.
- Repository deadlines: ACCESS EXCLUSIVE index-table lock tests status three-second DB budget; parent row lock tests five-second claim budget and INDEX_TIMEOUT with no model call. External five/eight-second guards detect the original missing deadlines. Timers bound waits, while synchronization uses channels/row locks rather than sleeps.

## Verification so far

- `go test ./tests/integration -run TestWeek06 -count=1`: compile passed; default DB gates skipped. Not DB evidence.
- `go vet ./tests/integration`: exit0.
- `git diff --check -- tests/integration/knowledge_index_test.go tests/integration/rag_search_test.go`: exit0.
- First `python3 scripts/week06-smoke.py --backend-only`: **RED**, exit1. `/tmp/week06-integration-first.log`. Actual disposable PostgreSQL17/pgvector0.8.7 migrations passed, then full `go test -race -count=1 -v ./...` failed only the two newly added deadlines. Status waited 5.001249832s; claim waited 8.007202558s, demonstrating dependence on external guards instead of intended three/five-second repository limits. All other W06 schema/index/search/Ask cases passed. Integration package elapsed24.228s. Root notified for production fix.
- Runner reported cleanup, and follow-up owned project container/volume listings were empty. First project: `nexusdesk-w02d2-60d5809f935b`.

## Final green verification

After root fixed production repository deadlines and the final matrix audit added the two state/chunk-limit subtests (this agent did not edit production sources), `python3 scripts/week06-smoke.py --backend-only` exited **0**. Durable logs: `docs/weekly/evidence/week06-integration-red.log` and `docs/weekly/evidence/week06-integration-green.log` (RED copied from `/tmp/week06-integration-first.log`; final GREEN from `/tmp/week06-integration-final-green.log`, superseding the prior 52-subtest green evidence).

- Full `go test -race -count=1 -v ./...`: 12 test-bearing packages, **153 top-level tests + 713 subtests = 866 passing nodes**, zero failures, zero skips, zero race warnings in this recorded run. Counts include root's new `TestRollbackUsesLiveBoundedIndependentContext` and `TestLocalDatabaseDeadlineMapsWithoutOuterDeadline` tests (present in the final green log).
- W06: **5 top-level tests and 54 subtests**: schema24, index13, search4, Ask11, database-deadline2. Every gate actually ran against disposable PostgreSQL17/pgvector0.8.7.
- Integration package: 20.638s. Deadline subtests: status3.10s and claim5.10s, now both explicitly assert INDEX_TIMEOUT.
- Recorded tiny-fixture EXPLAIN: planning0.838ms / execution0.122ms. This only proves the exact scoped SQL executed successfully; not production-scale latency or semantic quality.
- W06 repeated Up/reverse Down8/restore10 and retained old-data sentinel passed. W01–W05 migrations and real-gate regressions passed. W02/W03 TCP scenario demos passed. The updated runner also passed model-outage probes for both RAG_ENABLED=true/false: API startup and identity/tickets/knowledge200, search503, clean query/credential logs. Runner printed owned-container/volume cleanup and exited normally. The remaining live disposable project was confirmed by root to belong to root's browser run and was untouched.
- Fresh `go vet ./tests/integration` and scoped `git diff --check` exited0 after the final test edit. No unverified semantic/model/browser claim is made by this report.

The two repository context-helper tests were also rerun with `go test -race -count=1 -v ./internal/rag -run 'TestRollbackUsesLiveBoundedIndependentContext|TestLocalDatabaseDeadlineMapsWithoutOuterDeadline'`: exit0, package1.036s (`/tmp/week06-integration-deadline-helper.log`).

No remaining integration blocker. Root owns broader browser/evaluation/final documentation checks.
