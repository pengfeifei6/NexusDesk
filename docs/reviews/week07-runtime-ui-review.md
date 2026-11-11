# Task 2 spec and quality review

Spec verdict: **CHANGES REQUIRED** for two UI lifecycle requirements below. Runtime/configuration/transport wiring matches the reviewed contract, subject to the separately pending real-process acceptance.

Quality verdict: **CHANGES REQUIRED**. Two reproducible P2 correctness gaps remain; no P0/P1 issue was established in this review.

## Ranked findings

1. **P2 — First observed successful job does not refresh stale document/index data.** `web/src/components/AsyncJobPanel.vue:86` emits `completed` only when the previous local job state was active. On initial entry, `job` is null. A document read can return UPLOADED/PROCESSING and the index read ABSENT/INDEXING, then the worker can finish before the first `/jobs` response returns SUCCEEDED. The panel displays completion, emits no refresh, and stops polling because the job is terminal. The surrounding detail and index therefore remain stale until manual intervention. The same gap can occur after a hidden initial load is canceled before establishing a local active state. Track successful job identity plus run version at an owner that survives the detail panel's refresh/unmount, and refresh each newly observed successful run once, including the first response. Do not simply emit on every initial SUCCEEDED: `loadDetail()` clears detail and remounts this panel, which would produce a refresh loop. Add a focused test with stale initial document/index responses and an immediately successful first job response; assert one refresh and no remount loop.

2. **P2 — Permission denial clears preview without invalidating its pending request.** `web/src/components/AsyncJobPanel.vue:52`–58 clears the knowledge refs on 403/404, but only the panel owns/cancels its request scope. The knowledge composable's current generation remains valid. A text request already started by `web/src/composables/knowledge.ts:62`–65 can resolve afterward and write protected content back into `preview`. This is reachable because `loadDetail()` publishes a READY document before awaiting `/text`, so the new panel can fetch jobs concurrently with that text request. The detail stays null, so this specific reproduction does **not** establish visible text disclosure; it does establish failure of the binding protected-preview clearing contract and retention of revoked content in shared reactive state. Centralize permission invalidation in the knowledge owner: abort/invalidate outstanding document/text work, clear protected refs, and allow later explicit refresh to start a fresh generation. Add a deferred-text test for both 403 and 404 that resolves the earlier response after denial and asserts the signal is aborted and both refs remain null.

## Focused verification

Read the supplied brief, report, snapshot diff, and binding W07 design. The initial combined tool output truncated the middle of the diff; only those missing sections were subsequently retrieved. Follow-up source reads were limited to the named completion/permission races, startup error redaction, and cancellation propagation. No git commands, broad source crawl, actual environment/credential/private upload/backup access, daily database access, or broad test rerun was performed.

Executed a read-only Node check that transpiled the current `load`, `fail`, and knowledge `loadText` function bodies, using synthetic in-memory API responses and scope stubs. Results:

```text
first response already succeeded: jobStatus=SUCCEEDED, completionRefreshes=0, willPoll=false
text resolves after permission denial: detailCleared=true, previewCleared=false
```

These are focused function checks, not new Vue/browser or process acceptance claims. The supplied report's real Go/race integration PASS, 93 UI tests PASS, and build/lint/format PASS are acknowledged as existing evidence; their suites were not repeated. Existing completion coverage observes PENDING before SUCCEEDED and therefore misses finding 1. Existing denial coverage has no outstanding text response and therefore misses finding 2.

## Contract checks with no additional finding

- API adds the job routes through authenticated routing without constructing or pinging a queue client. Worker configuration independently loads DB/storage/model/async settings without requiring HTTP/JWT settings. Defaults and validation match loopback Redis 6380, DB 6, concurrency 2 bounded 1–4; Redis password is omitted from JSON.
- Queue identity/type, strict minimal payload parsing, task/run/dispatch identity, MaxRetry 2, 5/30-second retry function, one-hour retention, 180-second task timeout, and worker 150-second index budget match the snapshot contract. No existing module version upgrade appears in the dependency diff.
- Worker passes its canceled context into execution and dispatch, stops intake, and invokes Asynq shutdown configured for 10 seconds. Queue logs discard upstream messages, and inspected DB/model startup errors are sanitized. Actual bounded SIGTERM behavior remains a process-acceptance item, not proven by this static review.
- Upload then submit preserves the uploaded document on ordinary submission failure and exposes a submit-again path. IDs/requested_by/run_version remain lossless strings. Owner/admin controls, escaped template rendering, no browser persistence, scoped polling/nonoverlap, visibility pause, and route/session cleanup are present. No W08 feature expansion was observed.

The reported Task 3 actual-process attempt failed before its cases because of a Python 3.8 script incompatibility. Its repair and real Worker/Redis/model/browser acceptance remain separate outstanding evidence; no process pass is inferred here.


---

# Task 2 scoped re-review — UI fix round 1

Spec verdict: **PASS for the two reviewed corrections**.

Quality verdict: **PASS for the scoped fix diff**. Both original P2 findings are addressed. No new actionable defect was established in the five-file correction diff. This is not a browser or Worker process acceptance verdict.

## Original findings

1. **ADDRESSED — First observed SUCCEEDED job must refresh stale document/index data without a remount loop.** `web/src/components/AsyncJobPanel.vue:83` and `:107` now emit the successful job payload for initial reads and mutations. `web/src/views/KnowledgeDetailView.vue:71` delegates to the knowledge owner's `refreshCompletedJob` (`web/src/composables/knowledge.ts:94`). The owner checks status/session/org/document, constructs the identity from org/document/job/run strings, and records it before triggering the detail reload. Its Set lives outside the detail panel and survives that panel's clear/remount; route/org/session changes clear the Set at `:251`. This closes the first-response gap while preventing repeated success observations from creating a refresh loop. Regression coverage includes stale UPLOADED and PROCESSING detail plus INDEXING metadata, hidden initial request cancellation, manual detail remount, and adjacent int64 run versions that must remain distinct.

2. **ADDRESSED — Permission denial must invalidate pending document/text work before clearing protected refs.** `web/src/composables/knowledge.ts:38` centralizes cancellation of the owner's scope, list sequence invalidation, protected list/detail/preview clearing, busy reset, and denial messaging. Both `AsyncJobPanel.vue:55` and `RAGIndexPanel.vue:24` call it, as does the composable's own denial handler. An earlier text callback retains its canceled generation and therefore cannot repopulate preview. Explicit list/detail refresh obtains a fresh generation at `knowledge.ts:52` / `:76`, avoiding a permanently canceled retry path. Four regression cases cover jobs and index denials, each with 403 and 404, and explicitly check the original signal is aborted, protected refs stay null after the late response, and an explicit retry receives a fresh signal and authorized text.

## Evidence and limits

Reviewed only the original two findings, appended Task 2 fix report, `task-2-fix1.patch`, and exact changed-file line references. Inspected the five named evidence log tails: RED shows eight new failures with ten original passes; GREEN shows 101/101 tests passing across six files; build completes; lint reports no errors; formatting reports all matched files conform. The new tests retain the original assertions and exercise the actual Vue App lifecycle with synthetic API responses, including shared owner refs, rather than only mirroring helper implementation.

No full suite was rerun during re-review. No implementation changes, git commands, unrelated source crawl, private data/environment reads, external resources, or sub-agents were used. Only this review artifact was written.

The root reports production Go validation at 961 PASS / 0 skipped. Actual-process acceptance remains separate while its HTTP-name-shadowing test-harness defect is repaired; that issue neither reopens these UI findings nor supplies a process pass. Final browser/process acceptance is outside this scoped approval.
