# W05 backend review

Scope: `/tmp/week05-backend-review.diff` against the recorded W04 working-tree baseline (HEAD remains `7f1d7d2`), current `internal/platform/storage`, `internal/knowledge`, migrations 00007/00008, storage config, and API injection. Requirements read: `docs/api/knowledge-documents.md` and Global Constraints / Review Focus / Tasks 1–2 of `docs/superpowers/plans/2026-11-10-week05.md`.

Verdict: the main design follows the contract, but two bounded correctness/durability gaps need correction before declaring full compliance. No confirmed cross-organization disclosure, raw internal-error response, unsafe overwrite, or process/delete text-publication race was found. Findings below are source-confirmed paths with proposed targeted reproductions, not claims that those reproductions or real DB/browser tests were executed.

## Material findings

### P2 — Process deadlines are lost on database paths

Locations: `internal/knowledge/repository.go:24–25`, `:99–106`; `internal/knowledge/service.go:205–207`, `:234–241`.

`Process` gives claim/extraction/finalization the processing deadline, but repository scan and Begin failures collapse context deadline errors into `ErrStorage`. If the deadline expires while waiting to claim a locked row, or while waiting to finalize after extraction, `Process` returns that generic error. The HTTP handler consequently emits 500 rather than the contracted 504 `PROCESS_TIMEOUT`. When finalization times out, the failure cleanup also stores `PROCESS_FAILED` rather than `PROCESS_TIMEOUT`. A separate deadline between successful extraction and line 234 is incorrectly saved as `PROCESS_CANCELED` even though the returned error is DeadlineExceeded.

Concrete trigger: hold the target document row lock in a second transaction beyond `Limits.ProcessTimeout`, then call process; or pause an extractor after it has read the source, acquire the document row lock, let extraction return, and hold the lock past the finalization deadline. For the second case, release the lock while the independent failure context is still live so the final failure code can be checked.

Impact: clients receive the wrong machine-readable error and persisted operational diagnosis, contrary to the API contract. This is not a claim of text or tenant data corruption.

Targeted validation: a short-timeout repository/API test covering claim and finalization lock waits must return 504/PROCESS_TIMEOUT; successful independent cleanup should persist PROCESS_TIMEOUT. Add a service test whose extractor returns successfully after the deadline to cover the line 234 path. Keep uncertain commit handling conservative: mapping the public timeout must not authorize deleting a potentially committed object or overwrite READY.

### P2 — Cleanup records can be published incomplete and later accepted as durable

Location: `internal/platform/storage/local.go:172–187`.

`RecordCleanup` creates the final `<key>.cleanup.json` name before writing/syncing its contents. A short/failed write, or a crash between creation and completion, can leave an empty or truncated record. On the next call for that same key, the `os.ErrExist` branch immediately returns nil without reading, validating, or replacing the incomplete record. This can report successful reconciliation journaling even though the reason is missing/unparseable. This matters especially for `metadata_outcome_unknown`, where reconciliation must not assume the object is safe to delete.

Concrete trigger: in an owned temporary storage root, create an empty or partial `<valid-key>.cleanup.json` (the exact state left by interrupted creation), then call `RecordCleanup(ctx,key,"metadata_outcome_unknown")`. Source shows it returns nil and leaves invalid JSON unchanged. An injected write/Sync failure can exercise the creation path directly.

Impact: the promised durable pending-cleanup information can be unusable, with false success on retry. The service currently logs a direct RecordCleanup error, but that does not repair this existing-file success path.

Targeted validation: fault-inject journal write/sync failure and verify subsequent retry yields a complete validated record or an explicit error. Test existing empty, truncated, mismatched-key, and symlink journal paths. Publish the journal from a synced temporary file atomically, and verify any existing final record before claiming success. Preserve no-follow/no-overwrite properties and avoid destructive reconciliation for unknown outcomes.

## Non-blocking observations / limits

- `local.go:69–73,101–107`: temporary unlink happens after the directory fsync, and unlink errors only produce a log. A successful upload can therefore leave a `.upload-*` link after a crash or failed unlink. There is no durable pending-temp-cleanup record or documented startup scan in the reviewed scope. Add a safe temp-object reconciliation policy or durable cleanup handling; do not claim every failed temporary cleanup is journaled. This is distinct from deleting a potentially referenced published object.
- `handler.go:174` uses `multipart.Reader.NextPart`. Local Go standard-library source confirms that it transparently decodes `Content-Transfer-Encoding: quoted-printable` and hides that header. An explicitly encoded part containing `=41=42` is consequently stored as `AB`. The contract does not explicitly settle transfer-encoding support; this is a behavior to decide/document or reject using NextRawPart, not a demonstrated security bypass. Add a raw-byte/hash HTTP case if exact part-body preservation is intended.
- Context checks surround regular-file reads; they cannot forcibly interrupt an arbitrary blocking `io.Reader.Read`. Current production storage is a local regular file and extraction is byte-bounded. No claim of a hard cancellation guarantee for future remote storage implementations is warranted.

## Positive compliance observations

- Raw Content-Disposition filename parameters are validated before storage; handler does not use the basename-normalizing Part.FileName helper. Random hex keys, 0700 root/0600 files, rooted operations, symlink checks, and atomic hard-link publication prevent user-path selection and overwrite collisions.
- Upload validation consumes the entire stream, enforces UTF-8/control-character constraints, checks relevant signatures, and makes the sole-part reader verify subsequent parts before EOF. Whole-request MaxBytesReader remains in place through trailer draining. DTOs exclude storage_key, deleted_at, and internal paths; errors use fixed public strings.
- PDF signature-checked upload/download plus 422 PDF_UNSUPPORTED processing matches the expressly selected scope. Absence of a PDF parser is not a finding.
- Every repository read/write carries org_id and visible-parent filtering where applicable. Composite FKs bind uploader/document-text records to their organization. Soft deletion preserves source/text while normal reads hide them.
- Process claiming is a conditional update. Finalization locks the visible parent before inserting text and updating READY in one transaction, so deletion winning the row race prevents new text publication; concurrent process claims cannot both win. The code uses short database transactions around finalization rather than holding a transaction during extraction.
- Definite metadata insert failures compensate the object; uncertain commit retains it and records reconciliation. Conditional FAILED cleanup cannot overwrite READY or revive a deleted row. Membership and role are server-resolved rather than trusted from the request.
- Config defaults and upper bounds align with the contract; startup injects the storage/extractor and retains the existing auth middleware. Downloads set attachment, octet-stream, nosniff, no-store and stream a bounded stored length.

## Verification boundary

This was a read-only source/diff review. No repository files, migrations, databases, private uploads, secrets, `.env`, unrelated `--data`/`-H` files, or application state were modified/read. Only this requested report was written. Parent's completed unit/race checks were not rerun. No real PostgreSQL tests, browser tests, fault-injection tests, or extra runtime reproductions were executed by this reviewer. The proposed cases above are targeted validation work for the implementing agent.
