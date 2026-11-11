# W05 independent final code review

## Verdict

**Ready for the W05 code handoff: yes.** No confirmed Critical/P1 or Important/P2 defect was found in the reviewed frontend, acceptance runners, integration assertions, or cross-layer authorization paths. One minor input-validation mismatch is recorded below. The root agent's final documentation reconciliation remains outside this code verdict.

Review baseline: `/tmp/nexusdesk-w05-baseline-1hswmkzs` and `/tmp/week05-final-review.diff`, not a diff against W03 HEAD `7f1d7d2`. The existing W04 working tree was treated as inherited work.

## Strengths and source findings

- Registration posts exactly the existing three-field contract, creates no organization/session, clears the password, and returns to login. Create-first-organization posts only `name`, follows the server-returned organization ID, and obtains the current role from the real organization list (`web/src/composables/desk.ts:53–94,136–184`). The change preserves typed credentials before synchronous session reset, avoiding an empty-password login regression (`:100–110`).
- Knowledge metadata, preview, selection, and loading state are reset synchronously on route/session changes. Async completion paths check their captured request scope before assigning data, navigating, or saving a file. Same-route list/filter requests also have a sequence guard (`web/src/composables/knowledge.ts:43–82,92–195,204–223`). The download guard runs before `URL.createObjectURL`, and successful downloads remove the anchor and revoke the URL. I found no path that saves a completed old-scope download after a route, organization, or session change.
- Document previews use Vue text interpolation, including inside `pre`; no raw HTML or Markdown renderer is introduced (`web/src/views/KnowledgeDetailView.vue:63–66`). Access errors clear the stored detail and preview (`web/src/composables/knowledge.ts:36–40`). Error/failure messages use own-property checks on allowlists, including hostile inherited-property names.
- Multipart requests preserve the browser's FormData boundary; Blob requests retain Authorization and omit credentials/caching. IDs, including `uploaded_by`, remain decimal strings through lossless decoding. Token generations prevent late 401 responses from clearing a replacement session, even if token text is identical (`web/src/lib/client.ts:2–15,40–86,132–155`). Session credentials remain memory-only.
- The service checks current database membership, rather than token claims or frontend role visibility. Organization IDs are carried through metadata, list, text, mutation, and process-finalization SQL. Downloads resolve authorized metadata before opening the stored key. Finalization and deletion serialize on the parent row; text and READY commit together (`internal/knowledge/service.go`, `repository.go`, `handler.go`). No new evidence reopens the two backend P2 findings closed by `/tmp/week05-backend-review-followup.md`.
- The shared runner creates a random owned Compose project/database, rejects preexisting project/volume collisions, disables Compose/goose dotenv reads, supplies fresh fixture credentials, and verifies the project label before removing its volume. API storage is rooted in its owned temporary directory. Browser subprocesses have a separate process group for timeout cleanup. W05 runs the W04 browser checks into a separate evidence directory before the new browser checks (`scripts/week02-day2-smoke.py:71–108,135–145,358–399,401–491`; `scripts/week04-browser.mjs:15–18`).
- All 893 lines of the new integration test were reviewed, including schema constraints, DTO leakage assertions, exact upload limits, rejected-upload compensation, cross-organization and disabled-member checks, byte/hash downloads, soft deletion, real concurrent claims, process/delete ordering, INSERT/deferred-COMMIT failures, independent cancellation cleanup, and real PostgreSQL deadline tests. The blocking extractor controls scheduling while successful extraction still executes production code. The browser runner exercises real registration, organization/ticket creation, upload/process/preview/download/delete, paging/filtering, authorization, memory-only session state, and ten original fixtures.

## Issues

### Minor / P3 — Native input length constraints count differently from the API

Locations: `web/src/views/RegisterView.vue:16,25–26`; `web/src/views/DeskView.vue:15`. Backend reference: `internal/auth/service.go:57`, `internal/auth/password.go:23–29`, and the Unicode-code-point limits in `docs/api/auth-organization.md`.

The HTML `minlength`/`maxlength` attributes count UTF-16 code units, while the API validates Unicode code points. A password containing eight supplementary-plane characters is sixteen UTF-16 units and can pass the form's minimum while the API rejects its eight code points. Conversely, a valid 65-character supplementary-plane password requires 130 UTF-16 units and cannot be entered in full under `maxlength=128`. The same mismatch prematurely limits display and organization names containing these characters. The generic 400 message does not explain the counting mismatch.

Impact: a bounded international-input usability inconsistency; no authentication or authorization bypass. This is source-confirmed by the native attributes versus Go rune-count rules; no new browser reproduction was executed during this read-only review.

Suggested follow-up: validate the advertised code-point counts explicitly (for example, `Array.from(value).length`), retain the server as the authority, and use a sufficiently generous native maximum or omit the conflicting maximum. A focused browser check should cover below-minimum and valid above-native-limit supplementary-plane passwords, without logging passwords.

## Evidence inspected

- `docs/weekly/evidence/week05-final-smoke.log`: W05 migration 6→8, repeat Up, reverse Down to6 and restore8; all enabled real PostgreSQL/race suites pass; W04 browser 11 scenarios and W05 browser 9 scenarios pass; ends with clean API shutdown and owned-resource cleanup.
- `docs/weekly/evidence/week05-web-unit.log`: 4 test files, 67 tests passed.
- `docs/weekly/evidence/week05-web-build.log`: vue-tsc and production Vite build complete.
- `docs/weekly/evidence/week05-web-lint.log` and `week05-web-format.log`: supplied lint/format runs, format confirms all matched files comply. Successful command exit statuses were supplied by the coordinating agent.
- `docs/weekly/evidence/week05-go-quality.log`: supplied quality-run artifact (`all modules verified`); the coordinating agent supplied the zero exit status for its go-quality command. I did not independently execute vet/build.

The code checks are supported by the supplied fresh acceptance artifacts; this reviewer did not rerun the suite or independently observe the browser/database execution. Existing RED logs were not mistaken for final acceptance failures.

## Declined to judge

- PDF text extraction: explicitly outside the selected P1 scope; signature-checked upload/download and 422 `PDF_UNSUPPORTED` match the contract.
- Automatic recovery of crashed PROCESSING jobs and automatic orphan deletion: deferred to W07/operational reconciliation by the design; no recovery guarantee was inferred.
- Instant revocation of bytes already authorized and streaming: explicitly excluded by the design's concurrency contract; new requests still recheck current membership.
- Public-internet deployment and reliable abuse/rate limiting: inherited local-loopback deployment boundary, not a new W05 promise.
- Final documentation completeness and any later documentation edits: the root agent was still completing those files and requested a substantive code review.

## Review limits

Actual current frontend production files, frontend tests, changed runner files, all new knowledge integration assertions, relevant auth validation, current knowledge service/repository/handler/extractor/upload paths, the plan/API/design, and prior backend reports were read. Storage internals, configuration, migrations, and their unit tests rely principally on the dedicated completed backend review rather than a second exhaustive audit here. No power-loss, browser-engine portability, or adversarial same-UID filesystem experiment was performed.

No source/index/branch/commit/database/application state was modified, no runtime service was started, and no subagents were dispatched. No real `.env`, credentials, private uploads, or unrelated root `--data`/`-H` files were read. Only this requested report was written.

## Follow-up: Unicode validation and initial registration route — resolved

This addendum supersedes the outstanding P3 and the previous frontend/browser counts above. **No open issue remains from this review; the code-handoff verdict remains yes.**

- `web/src/composables/desk.ts:53–64,84–90` now measures display names, passwords, and organization names with `Array.from(...).length`, rejects out-of-range input before sending it, and provides a specific local message. It preserves the original password in the request. The conflicting UTF-16 `minlength`/`maxlength` attributes were removed from `RegisterView.vue` and `DeskView.vue`, leaving server validation authoritative. These changes resolve the supplementary-plane input mismatch identified in the P3.
- The three added tests at `web/src/lib/__tests__/knowledge.spec.ts:473` exercise rejection of an eight-code-point supplementary-plane password without an API call, preservation of a valid 65-code-point password and 80-code-point display name, and rejection/acceptance at 101/100 organization-name code points. `week05-web-unicode-red.log` records all three failing before the fix; the current `week05-web-unit.log` records all 70 tests passing afterward.
- The new browser scenario exposed an initial direct `/register` navigation problem which the earlier tests, opening a ready memory router or clicking from `/login`, did not cover. `week05-web-direct-register-red.log` records failure waiting for the registration name field. In the current `web/src/main.ts:9–13`, installation starts router navigation and `router.isReady()` completes before mounting App and executing the desk's immediate authentication watcher. That ordering addresses the actual initial-route race without changing authenticated-route checks or session persistence.
- `scripts/week05-browser.mjs:45–54,78–105` now opens `/register` directly in a new browser context, verifies that the short password produces a local error without a registration request, then checks real 201 registration with the full supplementary-plane name, real login with the full password, and real 201 organization creation with its full name. The Unicode page is closed before the screenshot-producing workflow; its password is included in failure redaction. No token URL or persistent credential storage was added.

Fresh evidence inspected after the follow-up: `docs/weekly/evidence/week05-web-review-smoke.log` now records PASS for `unicode-onboarding`, all previous W05 scenarios (10 W05 scenarios total), all 11 W04 browser scenarios, clean API shutdown, and owned-container/volume cleanup. This was the coordinator's **web-only** rerun, not a second whole Go/race run; the unchanged backend retains the earlier full-suite evidence. Current frontend build, lint, and format artifacts were reread, with successful build output and the format-compliance result.

This follow-up was again read-only except for this report append. No tests or services were launched by the reviewer, and no backend code, database, branch, credentials, or private content was touched. No new finding was identified in these scoped changes.

Coordinator terminal confirmation received after the artifact inspection: the final web-only smoke process exited 0; final unit/build/lint/format commands also each exited 0. These statuses are coordinator-supplied execution evidence, not commands rerun by this reviewer.
