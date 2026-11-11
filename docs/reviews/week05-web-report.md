# W05 frontend handoff

Scope: implemented Task 3 only, editing `web/src/**`. No package/dependency/config/backend/docs/script changes, no commits or subagents. Build output and compiler caches are generated artifacts from required checks. Existing W04 source was preserved in place. Parent owns browser/real-API acceptance and independent review.

## Delivered behavior

- `/register` uses the existing three-field `/auth/register` endpoint. Success returns to `/login?registered=1` with a fixed Chinese success notice; clears password; does not log in or create an organization. Login page links to registration.
- `/desk` always offers create organization, including zero-organization users. Only `{name}` is sent. Routes into the server-returned organization ticket list; role is loaded from the server membership results. Selected organizations outside the visible organization page retain a separate selector option.
- Selected-organization navigation links 工单/知识库/创建组织. Organization switches preserve ticket versus knowledge area and reset document/ticket state.
- `/organizations/:orgID/knowledge`: list, status filter, 20-item pagination, refresh, TXT/MD/PDF upload. File inputs reset across navigation/session changes. Client guard rejects >5242880 bytes; browser/server remain authoritative for multipart and content validation.
- `/organizations/:orgID/knowledge/:documentID`: metadata, synchronous processing, escaped plain-text preview, authorized Blob download, uploader/admin delete, refresh. Processing exposed to active admin/member when UPLOADED/FAILED. PDF explicitly explains unsupported extraction while retaining download. Known failure codes show fixed Chinese messages.
- On successful deletion, detail and text clear before returning to list. Current 403/404 failures clear detail and preview. Process 409/422/504 refreshes current metadata before retaining the safe action error.
- Dedicated knowledge `Scope` invalidates immediately on route/session epoch changes; late upload/process/detail/text/filter/download success or failure cannot affect a replacement context. Same-list sequence guards also protect filter/pagination races. All requests use AbortSignal.
- Downloads use authorized client fetch, a Blob ObjectURL and a temporary download anchor; URL is revoked and anchor removed in finally. URLs do not contain tokens; no static storage URL is exposed.
- Unified Client now handles FormData unchanged without manually setting Content-Type; supports Blob response path while keeping shared HTTP/auth/error/cancel/15-second timeout behavior. Business error allowlists are status-specific and require own properties. Raw server messages/HTML never display.
- Explicit lossless `uploaded_by` decoding added alongside existing ID/version keys. `size_bytes` and `extracted_chars` remain safe numeric values. Memory-only session retained; no storage or cookie API added.

## Meaningful RED -> GREEN evidence

1. `/tmp/week05-web-client-red.log`: missing multipart/Blob/uploader/error-map behavior caused 8 failures (20 passed). `/tmp/week05-web-client-green.log`: 28/28 passed after implementation. The first binary-byte assertion used Blob.arrayBuffer unsupported by jsdom; the test now reads its real Blob with FileReader and asserts literal bytes [0,1,255].
2. `/tmp/week05-web-ui-red.log`: all 16 initial onboarding/knowledge mounted tests failed before implementation; `/tmp/week05-web-ui-green.log`: full then-current suite 54/54 passed.
3. `/tmp/week05-web-cancel-red.log`: HTTP error-body cancellation incorrectly became status422; outer error-body reader was swallowing AbortError. Re-throwing aborted reads restored shared cancellation classification. Binary-body cancel and timeout tests also pass.
4. `/tmp/week05-web-login-red.log`: final review exposed typed login password becoming empty while resetting session state. Capture the intended credentials before clearing memory/form state. `/tmp/week05-web-login-green.log`: 63/63 passed after the fix.
5. `/tmp/week05-web-allowlist-red.log`: 4 failures demonstrated inherited `constructor`, `__proto__`, `toString` properties bypassing direct object lookup allowlists. Own-property checks fix both Client business messages and detail failure labels. Final suite 67/67 passes.

Additional regression tests cover late upload/process/detail/filter/text; forbidden delete clearing; failed extraction metadata refresh; PDF explanation; download authorization/errors; ObjectURL cleanup. Some of these strengthening tests passed against already-implemented behavior and are not claimed as separate RED cycles.

## Exact stale/failed-action tests requested by parent

`web/src/lib/__tests__/knowledge.spec.ts`:

- `does not save a late download after %s change` (organization/logout/navigation)
- `releases a successful download ObjectURL and uses its safe original filename`
- `does not navigate or display late %s results after leaving knowledge` (upload/process)
- `does not resurrect a late document detail after changing organization`
- `ignores late detail and text from the previous organization and preserves knowledge on organization switch`
- `ignores a late same-organization filter response`
- `clears READY detail and text after a forbidden delete`
- `clears old detail and preview when refresh returns 404`
- `refreshes FAILED metadata after extraction rejection and shows only the safe message`

`web/src/lib/__tests__/client.spec.ts`:

- `decodes uploader IDs losslessly while preserving safe measurements`
- `expires the current session on binary 401 but ignores a stale binary 401`
- `classifies %s during a Blob body read` (cancel/timeout)
- `keeps cancellation classification while reading an HTTP error body`

## Final checks, all exit 0

Commands ran with project-local Node24 prepended:

```sh
PATH="$PWD/.local/week04-tools/node_modules/node/bin:$PATH" npm --prefix web run test:unit -- --run
PATH="$PWD/.local/week04-tools/node_modules/node/bin:$PATH" npm --prefix web run build
PATH="$PWD/.local/week04-tools/node_modules/node/bin:$PATH" npm --prefix web run lint
PATH="$PWD/.local/week04-tools/node_modules/node/bin:$PATH" npm --prefix web run format:check
```

- `/tmp/week05-web-unit.log`: 4 files, 67 tests passed, zero failures.
- `/tmp/week05-web-build.log`: vue-tsc + Vite production build passed, 54 modules.
- `/tmp/week05-web-lint.log`: ESLint passed.
- `/tmp/week05-web-format.log`: all checked files passed Prettier.
- `/tmp/week05-web-format-write.log`: source-only formatting invocation.
- `/tmp/week05-web-diff.log`: diff versus parent-provided W04 source baseline; newly added files are reported as Only in by diff -ru and must also be reviewed directly.

Two intermediate build failures were resolved: a test DOM element needed an HTMLInputElement cast; Object.hasOwn was newer than the project's configured TS library and was replaced with existing-compatible Object.prototype.hasOwnProperty.call. Final build above confirms both fixed. No tests were deleted or assertions weakened.

## Review and limits

Self-review completed for API paths/payloads, identity strings, no raw HTML, authorization-dependent buttons, Scope/AbortSignal lifecycles, status errors, ObjectURL release and preserving ticket routes. No unresolved frontend test/build/lint/format failures. No real API/database/browser run was performed by this frontend worker; those are parent-owned acceptance gates and cannot be claimed from mounted tests. Client accept/size hints do not replace server validation. PDF extraction and crash recovery limits remain exactly the W05 contract. No material deviations from API contract were needed.
