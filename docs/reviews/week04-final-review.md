# W04 whole-change review

Read-only review against HEAD 7f1d7d2 and `/tmp/week04-whole-review.diff`, with actual frozen frontend/API/runner files, AGENTS.md, implementation plan, claim contract, ADR-0004, frontend contract and existing backend review/evidence. No tests rerun, repository modifications, secrets or Git mutations. Final docs and browser execution are still in progress and are not treated as missing implementation.

## Decision

Changes requested: **2 Important, 1 Minor; no Critical**. Backend atomicity/scoped-error implementation is satisfactory on inspected code and existing PostgreSQL evidence. Frontend completion should wait for the two recovery/session fixes and final browser evidence.

## Findings

### Important — failed initial organization loading disappears and cannot be retried

`web/src/composables/desk.ts:275–276`, `:123–144`; `web/src/App.vue:38`.

After successful login, make GET `/organizations?page=1&page_size=20` fail with 500 or a network error. `loadOrgs()` records the error, but the route watcher unconditionally follows it with `load()`, which clears the error and performs no request on `/desk`. The user sees an empty organization chooser as if no memberships exist, with no error/recovery. Even when an organization pagination failure leaves an alert, its global Retry calls only `load()`, so it never retries the organization request. This blocks the principal login→organization→work path after an ordinary transient failure; logout/login is currently the recovery.

Return/retain organization-load outcome and wire retry to the failed operation (or explicitly provide organization retry). Regression: mounted login into `/desk`, fail first organizations call, assert visible error; restore response, click retry, assert a second organizations request and populated options. Also cover organization-pagination failure.

### Important — login through browser history can retain the previous account's organizations

`web/src/composables/desk.ts:48–70`, `web/src/stores/session.ts:9–13`, `web/src/router/index.ts:8`.

Login as A, then browser Back to `/login` (login uses push to `/desk`, and authenticated `/login` is allowed). Login as B without pressing Logout. Successful login assigns `session.user` directly without advancing `session.epoch`; only `clear()` advances it and triggers org-cache reset. Hold or fail B's organizations response: A's organization names and roles remain in the header under B's identity. Failed re-login also clears the client token but leaves A's user in the store, creating inconsistent authenticated UI. Existing logout/new-login test explicitly calls Logout, so does not cover this reachable path.

Either redirect authenticated users away from `/login`, or use an explicit session-replacement boundary that clears old account state and preserves consistent failure behavior. Regression: authenticated A → history navigation `/login` → B login with deferred organizations; assert A's org options never appear in B's session (or assert the authenticated login screen cannot be reached). Cover invalid second credentials as well.

### Minor — response-body transport failures are mislabeled as malformed JSON

`web/src/lib/client.ts:96–111`.

`await response.text()` is inside the JSON/decode catch. If headers arrive but the body stalls until the 15-second timer, is canceled, or fails during download, this throws `ApiError('响应格式异常')`; the outer timeout/network/cancel classification is bypassed. Requests terminate and controls recover, so this is diagnostic accuracy rather than data corruption. Read body outside the decode-only catch or rethrow transport failures. Regression: successful headers with `text()` rejecting on abort/network failure, then assert timeout/cancel/network messages respectively; retain the malformed-JSON test.

## Verified by inspection / existing evidence

- Claim condition includes org/id, OPEN, unassigned, nondeleted, exact version and int64 bound; scoped lookup distinguishes invisible 404 from visible dedicated 409, propagating storage errors. Trusted actor, one event, independent bounded rollback and commit-before-success remain intact.
- Existing real PostgreSQL evidence and backend review cover 20×20 plus two-user contention, lock waiting, event/commit rollback and cancellation. Fixture cleanup correction is present per current diff/context; no new execution claim here.
- Normal logout/session expiry and org/route changes cancel and scope requests; same-token authentication generation protects a newer login against an older 401. Sequence guards protect filters/pagination against late results. The history-login boundary above is the missing account-change path.
- Int64 identifiers/versions use lossless JSON strings and emit expected_version as JSON integer; numeric priority is correctly decoded. Vue interpolation escapes supplied title/body/comment/event content; no raw HTML rendering found.
- Login/me/org/list/filter/page/create/detail/claim/transition/comment/events are wired to real API paths. Claim conflict triggers GET without automatic write retry. Forms preserve content on request errors and clear on scope changes.
- Browser runner uses real API fixtures and forwards requests instead of fabricating responses; credential values stay in memory, passwords render as password inputs, and no HAR/traces/request-body logs are generated. Isolation wrapper owns process group and disposable database cleanup.

## Explicitly not judged / final acceptance limits

- Final screenshots/video/results were not available during this review (browser evidence directory was empty). Do not equate runner inspection with executed browser acceptance or visual credential audit. Root must inspect completed artifacts and safe logs.
- Browser script does not exercise list filters or pagination; implementation was inspected, but successful real-browser coverage of those controls is not inferred. Its delayed-org check asserts immediately after releasing the old request, so unit scope tests provide the stronger late-response evidence; await request settlement before claiming that browser assertion itself proves late-response isolation.
- Final README/week report/link reconciliation and remote CI are pending root work; no findings inferred from draft omissions. Fresh-machine dependency/browser installation was not verified.
- No public deployment, W05 functionality, concurrent external-SQL membership revocation linearizability, commit-outcome idempotency, fairness, or expanded management UI is required by this review. ADR explicitly bounds these behaviors.


## Root remediation and acceptance (after reviewer report)

Both Important findings and the body-transport classification finding were reproduced with six additional failing tests, then fixed; unit suite 22→28 passing. Evidence: ../weekly/evidence/week04-web-review-red.log and week04-web-review-green.log. The root independently reran build/lint/format/unit and real browser tests. Initial org network failure→Retry, authenticated browser Back, 401/409/offline, switch after old request settlement, real 21-row pagination/filters all passed. Final screenshots and video were produced and inspected by root, not the reviewer. Raw screenshots led to moving feedback above long detail content; root repeated frontend and browser checks after this presentation change.

The reviewer did not re-review the fixes or run those checks. Original findings/verdict above are retained as the historical review. Concurrency revocation/idempotency/fairness/public deployment remain explicitly bounded by ADR, not newly implemented guarantees. Fresh temporary web install reused host tools/cache; it is not a fresh-machine browser installation. Remote CI is configured but unrun. Developer-tool npm audit findings are documented in week04, not declared fixed.
