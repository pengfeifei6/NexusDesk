# Week 05 real PostgreSQL and filesystem integration report

Owned change: `tests/integration/knowledge_documents_test.go` only. No production, runner, migration, dependency, credential, or frontend edits; no commits.

## Isolation and implementation

- Both top-level entries (`TestWeek05Schema`, `TestWeek05Knowledge`) first require `NEXUSDESK_TEST_W05_KNOWLEDGE=1`, then call the existing `ticketDB(t)` fixture. Its W03 opt-in, literal loopback host, and dedicated `nexusdesk_w02d2_test_` database-name guard remain mandatory.
- Real pgx/PostgreSQL repository, real organization membership and user repositories, real token manager, `RequireUser`, root HTTP router, knowledge handlers/service, and real private local filesystem storage.
- Storage is a `t.TempDir()` explicitly chmod 0700; real object bytes, SHA-256, and 0600 mode are verified. Fixture identities contain only synthetic `example.test` emails and the literal non-credential password placeholder. Synthetic bearer tokens stay in memory.
- Successful process races wrap the real UTF-8 extractor with channel barriers. The barrier is reached after real SQL `ClaimProcess`, not after manually assigning PROCESSING. No `t.Parallel`, random sleeps, mocked database, or test assertions removed.
- SQL faults are actual BEFORE INSERT and deferred constraint triggers. Cleanup removes the fixture trigger/function. Assertions report expected SQLSTATE or a fixed description, never raw database errors, token text, password data, file paths, or hash values.

## Acceptance matrix

| ID | Real assertion | Outcome in runs 2–3 |
| --- | --- | --- |
| B01 | Active member TXT upload 201; actor/org/UPLOADED metadata and private original bytes | PASS |
| B02 | Markdown upload 201; inferred media, size and stored SHA-256 match original bytes | PASS |
| B03 | Default/page/page_size/status filtering; organization scope; created_at/id descending tie order; invalid/overflow query 400; empty array page | PASS |
| B04 | Member detail/list 200; exact 12-field DTO; no storage key, disk path, body, deleted_at, or password field | PASS |
| B05 | Detail/download/text/process/delete against other organization's ID return 404 for both source org and actor org paths; absent source cannot override scope rejection | PASS |
| B06 | Existing token immediately loses all five document operations, upload and list after real membership disabled update | PASS |
| B07 | Active unrelated member delete 403; uploader delete 204; admin delete 204 | PASS |
| B08 | All document operations after deletion 404; list hides deleted row; metadata/text/file retained | PASS |
| S01 | Exact default 5,242,880-byte valid TXT stores successfully with matching hash | PASS |
| S02 | Default maximum +1 byte gives 413 with unchanged row and private-directory entry counts | PASS |
| S03 | `.exe` and `.html` give stable 415 with no row/file residue | PASS |
| S04 | ZIP signature/NUL binary disguised as MD rejected 415, including file cleanup | PASS |
| S05 | Text/plain multipart claims cannot admit disguised PDF/PNG; safe UTF-8 accepted with untrusted octet-stream part MIME. Run 3 additionally rejects quoted-printable/base64/binary/empty-present Content-Transfer-Encoding with 400 and no row/file | PASS |
| S06 | Traversal and backslash filenames give 400 without row/file residue | PASS |
| S07 | Empty file, missing file field, additional multipart part give 400; wrong request media 415 | PASS |
| S08 | Real document BEFORE INSERT and deferred COMMIT faults give safe 500, no metadata row, and compensation removes new object | PASS |
| P01 | MD processing returns READY and normalized BOM/CRLF/CR text; SHA, Unicode chars, version and timestamp verified; source unchanged | PASS |
| P02 | Immediate and late invalid UTF-8 upload 415; no metadata/object publication | PASS |
| P03 | Whitespace document upload succeeds, extraction returns 422 EMPTY_TEXT and independent FAILED, no text row | PASS |
| P04 | 1 MiB+1 extracted output returns 422 TEXT_TOO_LARGE, FAILED, no text; original preserved | PASS |
| P05 | First real claimed process blocks in wrapper; second HTTP process returns 409; released first returns 200 with one text row | PASS |
| P06 | READY repeat process returns 409; text count, hash and publication timestamp remain unchanged | PASS |
| P07 | Real text BEFORE INSERT and deferred COMMIT faults return 500, FAILED, no READY/text; original preserved and retry succeeds after removing fault | PASS |
| P08 | Extractor timeout HTTP 504/PROCESS_TIMEOUT and cancellation after real claim independently persist FAILED with proper fixed code; no text and original retained. Run 3 adds real locked ClaimProcess timeout 504 and actual text INSERT deadline 504 with independent PROCESS_TIMEOUT/FAILED and no READY/text | PASS |

Additional passing coverage in run 2: process/delete race never publishes READY/text after deletion; missing original and same-length tamper produce safe process 500 with SOURCE_UNAVAILABLE/SOURCE_CHANGED; changed-size download gives safe 500; authorized original-byte download verifies attachment filename, octet-stream, nosniff, no-store, byte count and hash; all document operations reject unauthenticated requests, unknown query, extra request body, invalid/missing IDs. PDF upload remains a controlled download object and processing returns 422 PDF_UNSUPPORTED.

Schema coverage passes: table presence/defaults; filename byte length, storage key/hash form, media enum, file size, status/failure/extracted-char checks; organization/uploader and composite text-parent foreign keys; unique storage key and one text row per document; both organization query indexes include descending timestamp/id ordering and non-deleted partial predicate. Constraint assertions inspect SQLSTATE 23514/23503/23505 only.

## Executed evidence

- `/tmp/week05-integration-gate.log`: `go test ./tests/integration -run 'TestWeek05' -count=1` exits 0 with gates disabled. This verifies compilation and opt-in behavior only, not real DB acceptance.
- `/tmp/week05-integration-vet.log`: `go vet ./tests/integration` exits 0.
- `/tmp/week05-integration-smoke-1.log`: `python3 scripts/week05-smoke.py --backend-only` exits 0. Includes full enabled real database `go test -race -count=1 -v ./...`, migration 6→8→6→8 with existing organization retained, historical API process regression, and cleanup of only this run's containers/labeled volume.
- `/tmp/week05-integration-smoke-2.log`: same command exits 0 after strengthening paging/default/error envelope/input/source checks. `TestWeek05Schema` PASS (0.13s), `TestWeek05Knowledge` PASS (1.93s); all enabled real database gates and full race suite PASS. Runner cleanup completed.
- `/tmp/week05-integration-smoke-final.log` (run 3): same command exits 1 overall because root concurrently introduced the planned RED storage unit `TestLocalRejectsRootWithinPublicDirectoryViaAncestorSymlink` (`local_test.go:305`, "public root accepted through ancestor symlink"). All Week 05 real schema/knowledge integration tests PASS (`TestWeek05Schema` 0.13s, `TestWeek05Knowledge` 2.72s), including the new four transfer-encoding cases and real database claim/finalization deadlines. The full integration package PASS under race (9.624s); isolated runner cleanup completed before reporting the failed package. This is not counted as an overall passing smoke run.
- `/tmp/week05-integration-vet-final.log`: scoped vet exits 0 after the final test additions.
- `/tmp/week05-integration-gate-final.log`: verbose default-gate test confirms both Week 05 entries explicitly SKIP without opt-in. These skips are not counted as database acceptance.
- The new file was gofmt formatted; `git diff --no-index --check /dev/null tests/integration/knowledge_documents_test.go` emitted no whitespace errors (its difference exit status is 1 for this untracked file).

## Final status and remaining ownership

After run 2, root review identified deadline normalization around repository claim/finalization errors and raw multipart transfer decoding. Root applied production fixes; run 3 verified both through real handlers and PostgreSQL. `P08_real_PostgreSQL_claim_lock_deadline_HTTP_504_no_publication` holds the parent row lock and expects a 300ms deadline response; it asserts no READY/text and preserved original, allowing unknown canceled-claim outcomes as specified by root. `P08_real_PostgreSQL_text_INSERT_deadline_rolls_back_READY_and_text` uses an actual PostgreSQL BEFORE INSERT trigger with a 2-second `pg_sleep` canceled by the 300ms service deadline, and verifies independent PROCESS_TIMEOUT/FAILED and original retention.

Root confirmed the run 3 storage unit failure landed in its intentional RED window and was subsequently fixed and verified under race (`docs/weekly/evidence/week05-storage-review-green.log`). That fix is root-owned and its final combined backend/browser acceptance remains root's responsibility; root explicitly asked not to launch another full backend runner. This report does not reinterpret run 3 as a passing full smoke run or claim browser acceptance.

All requested integration coverage is implemented and has passed with real PostgreSQL/private files under race. No unresolved integration defect was found. No production code, script assertion, migration, frontend file, dependency, or private configuration was modified by this task, and no commits were made.
