# W05 backend review follow-up

Verdict: the two material P2 findings from `/tmp/week05-backend-review.md` are resolved in the current source. No new material issue was found in the reviewed fixes. This closes the scoped backend follow-up; it is not a declaration that the final whole-branch acceptance run passed.

## Fix verification

1. **Process deadlines — resolved.** `internal/knowledge/service.go:196–213,224–254` now recovers deadline/cancellation from the operation context after claim, source open, extraction, and failed finalization, and uses a consistent failure-code mapper. Successful extraction returning after its deadline is also caught. Repository error sanitization no longer masks the public processing timeout. The independent failure context remains intact. `TestProcessPreservesDeadlineAcrossDatabasePhases` covers claim, late successful extraction, and finalization; the supplied RED log shows the original failures and the GREEN log shows the affected package passing.

2. **Incomplete cleanup journals — resolved.** `internal/platform/storage/local.go:186–226` writes a newly generated private journal temporary file, syncs and closes it, checks cancellation, atomically renames it to the final journal name, then syncs the containing directory. It no longer reports success merely because a final record exists. Replacement safely repairs empty, truncated, mismatched-key, and symlink entries without following a symlink target. Tests cover these four cases plus failed publication followed by retry. The evidence demonstrates reproduction of the prior invalid-record failures and subsequent package success. Direct power-loss or low-level write/fsync fault injection was not demonstrated; the publication ordering itself was reviewed.

3. **Temporary upload cleanup — addressed.** `local.go:172–184` syncs successful unlink and records `temporary_delete_failed` on unlink or directory-sync failure. If recording also fails, it explicitly logs that combined failure. The replacement-reader fixture forces a temporary-name removal failure and verifies the durable cleanup record. This does not create a blanket guarantee against crashes before a journal can be published; orphan temporary-file reconciliation remains an operational concern.

4. **Multipart byte handling — addressed.** `internal/knowledge/handler.go:125,174–182` uses NextRawPart and rejects the presence of Content-Transfer-Encoding, including an empty header value, before calling the service. This removes transparent quoted-printable decoding and makes the accepted upload path preserve the file-part bytes. The single-part EOF/trailer checks remain in place.

5. **Public-root ancestor symlink — addressed for the configured startup path.** `local.go:41–53` compares canonical storage and excluded public-directory paths in addition to the existing lexical checks. The new test exercises an alias pointing into web/private; supplied evidence shows RED then GREEN. Existing 0700 root checks, direct-symlink rejection, and rooted operations remain intact. This is not a claim that an arbitrary hostile same-UID process swapping startup paths is isolated by these checks.

## Uncertain commit behavior remains conservative

- Upload metadata `Create` still propagates `ErrUncertain` from the commit helper. `Service.Upload` retains the object and records `metadata_outcome_unknown`; the processing timeout mapper does not affect this path. It does not delete a possibly referenced object to make the public error look cleaner.
- Finalization can report timeout after an uncertain commit, but the independent `FailProcess` update remains conditional on the organization, visible parent, and PROCESSING state. A committed READY row cannot be overwritten as FAILED; soft-deleted rows cannot be revived. No new destructive object cleanup was introduced in processing.
- A claim whose server outcome is uncertain can still leave PROCESSING, consistent with the explicitly documented recovery limitation. The repair correctly does not blindly mark an unconfirmed claim FAILED.
- The new journal replacement has no current production call path that downgrades an unknown-commit record to an instruction to delete the object. Journal consumers must continue to reconcile database references rather than treat the presence of any journal as deletion authorization.

## Evidence inspected, not executed by this reviewer

- `docs/weekly/evidence/week05-review-red.log`: original deadline and invalid-journal regression cases fail.
- `docs/weekly/evidence/week05-review-green.log`: knowledge, storage, config packages report success.
- `docs/weekly/evidence/week05-storage-review-red.log`: ancestor-symlink root case fails before repair.
- `docs/weekly/evidence/week05-storage-review-green.log`: storage and knowledge packages report success after repair.
- `/tmp/week05-integration-smoke-final.log`: the four Content-Transfer-Encoding cases and the real PostgreSQL claim-lock / text-INSERT deadline cases report PASS. Inspection of their current test source confirms they assert HTTP 504 / PROCESS_TIMEOUT and no unintended text publication; the finalization case also checks the persisted timeout failure code. That same log contains a failing ancestor-symlink storage test and ends in FAIL, so it is **not** evidence that the overall run passed. The supplied later storage GREEN evidence covers that fix independently; a clean final whole run remains the root agent's acceptance task.

This follow-up was read-only: current source, the updated review diff, relevant test source and supplied logs were inspected. No tests were rerun, no database/browser session was exercised, no source was edited, and no real `.env`, credentials or private upload contents were read. Only this requested report was written.
