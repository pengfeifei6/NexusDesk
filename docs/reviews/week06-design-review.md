# W06 read-only architecture/security builder review

2026-11-11. AI-prepared review; not human acceptance or real-model evaluation. Scope: W06 design/API, migrations 00009/00010, W05 knowledge service/repository/handler, RAG model/error interfaces and config. No production edits, database/runtime calls, private uploads, secrets, or historical whole-project review. Source was actively being implemented; findings describe the files read, not later changes.

## Confirmed contract gaps and contradictions

### P2 — Successful generation does not explicitly require any citation

Evidence: `docs/superpowers/specs/2026-11-11-week06-design.md:36`, `docs/api/rag.md:28`, `internal/rag/models.go:36`.

Trigger: model returns `{answer:"unsupported assertion", citation_ids:[], insufficient_evidence:false}` after nonempty retrieval. The proposed subset/unknown-ID rule accepts an empty set; no written rule rejects an empty/whitespace answer either.

Impact: a nominally grounded success can have no evidence references, despite the contract's intention to return supported answers. This is a confirmed specification omission, not a claim that the unfinished service currently accepts it.

Fix: explicitly require a nonempty trimmed bounded answer and at least one distinct selected citation whenever `insufficient_evidence=false`; otherwise return `MODEL_INVALID_RESPONSE`. When insufficient, use the fixed refusal and empty sources. Apply this at the service boundary as well as strict adapter parsing, so an alternative/test generator cannot bypass it. Test empty citations, whitespace answers, unknown IDs, duplicate IDs, and the valid insufficiency path.

### P2 — Rune limits do not establish the configured model context budget

Evidence: design lines 30 and 40 allow query 1000 runes/4000 bytes, evidence 5000 runes, `num_ctx=8192`, and `num_predict=512`; there is no budget over the complete serialized model input.

Trigger: ten 500-rune chunks containing multibyte or heavily escaped text, plus query, source names, JSON fields, system prompt, chat template, and structured-output overhead. Rune count is not token count, so the current bounds do not prove this input fits.

Impact: allowed input may be rejected or truncated upstream; citations then may refer to chunks the model did not receive intact. Actual truncation behavior remains unverified and is not asserted here.

Fix: select complete chunks against a deterministic budget that covers the entire serialized prompt, template overhead, and output reserve; use a documented conservative bound or verified tokenizer. Never silently truncate chunk excerpts while retaining their original full-span metadata. Keep the model citation allowlist limited to the selected prompt chunks. Test adversarial Unicode/escaping and maximum source metadata without calling the real model.

### P2 — Search frontend timeout is shorter than its supported server budget

Evidence: design line 40 gives Search 25 seconds; line 42 keeps everything except Index/Ask at 15 seconds. API line 46 says Search *may* use 30 seconds.

Trigger: a valid 16–25 second search.

Impact: the design-default browser cancels a request that is within the documented server budget, making the advertised Search timeout unreliable for the UI.

Fix: make Search's explicit 30-second client timeout required, retain Index/Ask 70 seconds and ordinary W05 requests 15 seconds, and align both documents. Verify the option is scoped to these routes and inherited abort signals still cancel it.

### P3 — Disabled-RAG behavior differs between design and API

Evidence: design line 42 says disabling leaves APIs at 503; API line 48 explicitly keeps GET index status readable.

Trigger: `RAG_ENABLED=false` with an existing document/index.

Impact: a blanket feature gate may unnecessarily hide existing index status, contrary to the API and useful W05 degradation behavior.

Fix: define the API behavior as authoritative: authorize first; GET index status remains readable, while POST model operations return safe 503. State whether an index POST that could reuse READY is still gated (consistent simple choice: all POST index operations gated). Test all four routes under disabled mode and nonmember/disabled-member requests.

## Implementation assumptions requiring explicit checks (not confirmed bugs)

### P1 if violated — Revalidate all evidence shown to the generator, not only cited output sources

Evidence: design line 36 and API line 30 require final current-source checks but use “sources” without distinguishing selected prompt evidence from the response citation subset. `rag.Source` intentionally has no internal source hash/index version/org fields.

Trigger: generator sees A and B; B is deleted or reindexed during generation; answer uses information from B but cites only A. If final checks iterate response citations only, B's stale text can remain in the answer.

Fix/rationale: retain an internal retrieval snapshot containing trusted org/document/chunk IDs, source hash, index version, and model/chunker identity. After slow generation, reauthorize current membership and validate **every chunk supplied to the generator**, even when the model omits it from citations or declares insufficiency. Build public sources separately. Do not infer current index identity merely from public DTO fields. Test uncited-source deletion/version change and membership revocation, plus empty-retrieval/insufficiency branches so they cannot bypass final membership checks.

### Index publication/transaction invariants to preserve

The schema and prose are mutually compatible: 500 runes fit the 2000-byte chunk limit; vector dimension and fixed model are aligned; composite parent FKs prevent cross-org child attachment; version/hash checks can hide obsolete chunks. No demonstrated schema contradiction or cross-org bypass was found.

Implementation must still lock parent → text → index consistently at claim/finish, compare the captured source hash and run version, and replace chunks plus READY in one short transaction. Capture the source snapshot inside claim rather than relying on a preceding W05 `Text` read. Reuse must include dimension/model/chunker/current hash, and failure cleanup must be conditional on the same INDEXING version and visible parent. A failed/uncertain finalize must never overwrite a possibly committed READY result. Use independent bounded rollback/failure contexts and per-query/transaction caps rather than giving locks the full 60-second model budget.

### Authorization order and safe error translation

W05 `internal/knowledge/handler.go:75` sets `Cache-Control: no-store`, resolves actor and org, authorizes membership, then parses document ID/body. Preserve that order for RAG, repeat authorization in service methods, and gate disabled-model operations only after authorization. W05 membership errors must map to their existing 401/403/404 semantics; they must not fall through to generic RAG 500. Every SQL read/write must include org and visible parent predicates, including final checks and failure updates.

### Fixed-model and evaluation limits

The inspected constants/config/migrations all fix BGE-M3 at 1024 dimensions. Model-name matching by itself is not proof of immutable model contents; record the actual local model artifact identity in evaluation evidence and treat manual replacement under the same tag as requiring reindex. This is an operational assumption, not an observed mismatch.

All 20 inspected fixture labels explicitly say `AI-prepared exact source spans; human review pending`; this satisfies the requested provenance language. No human-gold or real-model-result claim was made by this review. Official pgvector 17.11/0.8.7 image availability and the ongoing model download are parent-provided status, not runtime findings verified here. Real model quality, SQL execution plans, transaction races, and HTTP behavior remain for the authorized implementation/integration checks.

## Parent implementation decisions received after review

Parent accepted the citation requirement, with adapter and service validation. Parent selected `num_ctx=16384`, a 12000-byte cap over complete `json.Marshal(messages)` including system/user/evidence metadata, `num_predict=512`, removal of lowest-ranked whole chunks, and an allowlist restricted to actual prompt sources. If no whole chunk fits, fail with `MODEL_INVALID_RESPONSE`. These decisions address the identified omissions; implementation and the tokenizer/template overhead assumption remain to be verified by the owning agent. They are not test results from this review. No additional confirmed blocking schema/auth contradiction was found.
