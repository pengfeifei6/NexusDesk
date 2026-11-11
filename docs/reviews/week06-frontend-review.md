# Task 3 independent frontend review

Reviewed 2026-11-11. Verdict: **changes requested for one P1 finding**. Production code and tests were not modified.

## Finding

### P1 — Honor authorization failures from the post-index metadata refresh

Location: `web/src/components/RAGIndexPanel.vue:58-64` (also the delayed handling at `:56-58`).

After an index POST fails, the component refreshes metadata but discards every error from that GET. It then calls `fail` with only the original POST error. If the POST fails with MODEL_UNAVAILABLE/503, INDEX_SOURCE_CHANGED/409, or a browser timeout, and the refresh returns 403/404 because membership was revoked or the document was deleted, the existing document detail, full text preview, and previous index metadata stay visible. The GET has positively established that the protected resource is inaccessible, but its response never reaches the existing protected-data clearing in `fail`. This also leaves a stale READY/ABSENT status visible when the refresh fails.

Related ordering: if the POST itself returns 403/404, clearing is delayed until the GET finishes, potentially another default 15-second timeout. Access-denied responses should clear the protected document and preview immediately, independently of a best-effort status refresh.

Suggested fix: process a current POST 403/404 before starting refresh; preserve the current-scope check when handling a refresh failure, and propagate its 403/404 into protected-data clearing. Invalidate unconfirmed index metadata on refresh failure. Preserve the original safe model/timeout error where appropriate, but do not let it override the access-denied state. Ensure old-route/old-session refresh errors cannot clear a newly loaded document.

Required focused regression cases: load a READY document and visible preview; return POST 503/409/timeout followed by GET 403/404; assert detail, preview, and old index data are cleared and a safe access error is shown. Also defer the refresh after POST 404 and assert clearing occurs before it settles. Resolve/reject an old refresh after navigation and assert the new document is unaffected. The existing `rag.spec.ts` metadata-refresh case tests only POST 503 followed by a successful FAILED metadata response and therefore misses this branch.

## Reviewed behavior without another actionable defect

- Search/ask scope cancellation and synchronous route/session/query/top-k watchers discard old responses and clear prior answers/sources. The tests exercise late ask responses after organization, session, and navigation changes, plus clearing completed results when the question changes.
- Index reads/writes use scope guards and unmount cancellation. Endpoint construction keeps current organization and document IDs, and search source links use the current organization.
- Source excerpts, document names, and generated answers are Vue text interpolation; there is no HTML rendering or external model-generated link execution. Tests check script/img-shaped strings remain text.
- `chunk_id` and the newly added `index_version` remain decimal strings through lossless decoding. Fractional finite scores and timing metrics are handled separately from integer measurements. The RAG numeric test checks max-int64 IDs and fractional values while retaining integer-only size validation.
- Index/ask explicitly request 70 seconds, search 30 seconds, and normal client calls retain 15 seconds; transport cancellation is separate from timeout. The new timeout option is bounded to 1–70000 integer milliseconds.
- Search route precedes the document detail route; the knowledge composable no longer requests a document list on the search route. Organization switching preserves the search view.

## Evidence and limits

Read applicable `AGENTS.md`, `docs/api/rag.md`, the W06 design, relevant architecture/requirements/database context, new RAG Vue components/view/tests, client and types, router/App/detail integration, knowledge/desk composables, session store, and related client timeout tests. Compared modified W05 files against the supplied safe snapshot at `/tmp/nexusdesk-w06-baseline-8nhk57sz`; Git HEAD cannot represent the uncommitted W05 baseline.

Inspected existing logs: `docs/weekly/evidence/week06-web-green.log` reports 5 files / 80 tests passed; `/tmp/week06-web-build.log` reports vue-tsc/Vite completion; `/tmp/week06-web-lint.log` records ESLint without an error. These are supplied execution evidence, not checks independently rerun by this reviewer. The 70/30-second override timing itself, invalid metric branches, late index responses, query changes while a request is pending, and the failing-refresh access-denial branch are not specifically covered by the new RAG unit cases. Prior client tests cover default timeout/body-read cancellation.

This review is static. No suites, browsers, runtime services, model endpoints, real environment files, credentials, or private uploads were accessed. Only this requested review report was written.

## Follow-up disposition — 2026-11-11

**The P1 finding is resolved; no blocking finding remains in the reviewed frontend scope.** Re-read the updated `RAGIndexPanel.vue`, the three added `rag.spec.ts` regression cases, and the supplied red/green evidence. A current POST 403/404 now clears protected state immediately and returns before attempting a refresh. A refresh 403/404 is processed only after checking `run.current()` and clears index metadata, document detail, and preview while surfacing the access error. A late denial from an obsolete scope cannot clear the new document.

The new cases exercise POST 503 followed by refresh 404, immediate POST 404 with a hypothetical refresh left pending, and a late refresh denial after organization navigation. `week06-web-review-red.log` records the two protected-content assertions failing against the previous implementation (81 passed, 2 failed); `week06-web-review-green.log` records all 83 tests passing after the fix. These targeted cases cover both observed failure modes and the cancellation guard around the fix. The 403 path shares the same explicit branch as 404; 409/timeout POST failures share the refresh path tested with 503.

The earlier suggestion to invalidate metadata after every non-authorization refresh error was broader than the P1 fix: such failures still retain last-known metadata beside the original safe error. This does not leave a confirmed access denial unhandled and is not a blocker for closing this finding. No suite or runtime operation was rerun during this follow-up, and production files were not changed by the reviewer.
