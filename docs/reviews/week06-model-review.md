# W06 Task 1 independent review

Date: 2026-11-11. Verdict: **APPROVE for the scoped adapter implementation.** No actionable correctness, security, or specification finding was identified in this review. This does not approve the unfinished whole-feature or real-model acceptance work.

## Scope and method

Reviewed `task-1-brief.md`, `task-1-report.md`, `task-1-review.diff`, the repository collaboration rules, and all nine files represented in the review diff: `internal/rag/{models,errors}.go` plus the three implementation and four test files in `internal/platform/llm/`. A read-only comparison confirmed that all nine current files exactly match the supplied diff. Other backend/frontend work was outside scope.

This was static review plus inspection of existing verification evidence. No passing tests were rerun, no production code was edited, and no real model/API/database operations, credential/private-upload reads, environment reads, or subagents were used. Only this requested review report was written. An initial read-only comparison helper used a Python string method unavailable on the installed interpreter; the compatible replacement completed and reported MATCH for every file.

## Specification and quality assessment

- Interfaces, numeric int64 source IDs, snake_case source fields, fixed `bge-m3:567m`/1024 and `qwen3.5:35b` identifiers match the contract. No production mock, API-key dependency, model pull, or startup network request exists in these files.
- The constructor and dial callback independently restrict connections to literal loopback IPs. Userinfo, non-HTTP schemes, hostnames, paths, queries, fragments, invalid ports, and scoped addresses are rejected. Proxy use and redirects are disabled. The adapter supplies no authorization header or tool definitions.
- Both wrappers share the same two-slot admission channel. Admission fails immediately with `RAG_BUSY`; acquired slots and response bodies are released by defer. Requests inherit cancellation/earlier deadlines and add 20-second embedding or 35-second chat limits. Existing tests exercise mixed embedding/chat occupancy, cancellation, slot reuse, and actual request deadlines.
- Embedding inputs are bounded to eight nonblank valid UTF-8 texts, 4000 bytes each and 32000 bytes total. `truncate:false` is explicit. Responses are capped at 2 MiB. Returned model, count, dimension, null scalars, malformed JSON, nonfinite values, and zero vectors are checked. Float64 norm accumulation and subsequent normalization avoid float32 overflow/underflow in downstream cosine calculations. No indexing panic or zero-norm division path was found for validated responses.
- Chat retains the finalized `num_ctx=16384`, `num_predict=512`, temperature zero, `think:false`, and `stream:false`. Query/source limits are checked. The exact `json.Marshal(messages)` length is limited to 12000 UTF-8 bytes, including the fixed instructions and escaped JSON data. Whole sources are removed from the ranked tail; neither question nor chunk text is truncated. A prompt with no fitting evidence is rejected before HTTP.
- Query, source names, and excerpts are contained in a separate JSON user message under fixed system instructions. The citation allowlist is built after source selection, so removed-source IDs are rejected. Returned citation strings must be canonical positive int64 decimals; duplicates are left for the service's specified deduplication. Normal answers require nonblank text and at least one allowed citation.
- Chat responses are capped at 64 KiB. The parser rejects malformed/trailing JSON, exact duplicate object keys, invalid UTF-8/surrogates, missing/null answer fields, unknown inner fields, invalid citations, oversized answers, unfinished/truncated responses, actual tool calls, wrong model/role, and error-plus-success envelopes. Recursive JSON validation has a depth bound. No unsafe slice access was found in the budget or Unicode scanners.
- Public errors use fixed allowlisted messages and unknown codes are sanitized by `NewError`. Transport errors, upstream bodies, query/source content, and model output are not logged or wrapped into public errors in the scoped implementation.

## Evidence and remaining acceptance

Inspected the existing `/tmp/week06-model-race.log`, `week06-model-whole-go.log`, `week06-model-vet.log`, and `week06-model-build.log`; each records `EXIT_CODE=0`. The scoped test sources contain the reported 24 top-level tests. These are prior implementation checks, not fresh review executions or evidence that concurrently changing files pass now.

Real Ollama model availability, literal response model names, structured-output behavior, hardware latency, and end-to-end database/API acceptance remain pending outside this review. The semaphore limits calls sharing one Client; composition must reuse that Client. Membership/source revalidation, no-evidence refusal, deduplication, and safe text rendering remain service/frontend responsibilities outside this scope. Prompt separation and selected-source citation checks constrain syntax and provenance IDs; they do not prove semantic grounding against adversarial source text.
