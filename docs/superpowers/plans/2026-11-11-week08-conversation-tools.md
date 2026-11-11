# W08 执行计划

Spec: `docs/superpowers/specs/2026-11-11-week08-conversation-tools.md`

## Global Constraints

- Preserve existing uncommitted work, old migrations, private data and credentials. No commits/push/deployment. Work in current feature checkout so user's local app receives changes.
- Actor identity is server-owned; current active membership and conversation ownership at every entry. Model cannot perform real writes. Confirm creates ticket, audit and action transition in one transaction.
- All SQL/model/stream work bounded and cancelable. Only fixed approved model endpoints. No sensitive log/error bodies. Tests use synthetic data and isolated database.
- Use exact internal and HTTP contracts in spec. Read AGENTS.md; newest task authorization supersedes instructional readonly examples.

### Task 1: Persistence and atomic approval

Read spec Internal contract first. Own only `internal/ai/{types,errors,store,approval}*.go`, migration `00012_create_ai_conversations_messages_actions.sql`, ticket transaction-aware creation additions/refactor and `tests/integration/ai_store_test.go`. Do not own AI runner/model/handler or frontend. Implement Store API verbatim. Normalize/validate strings UTF8 NUL and lengths title200/body10000, priority1..3. Conversation title1..100, user content1..4000 runes/max16KiB. UUID canonical. Transaction5s/query3s/rollback3s. Recheck owner and current active membership; lock active membership during confirmation to serialize concurrent revocation. Query only org scoped. Message uniqueness user+assistant roles per request, one live stream per conversation, stale120s interrupted; action maxone per assistant, 15min expiry, canonical digest, rowlocked repeat confirmation returns same ticket; confirmation/rejection works after streaming finished unless FAILED/INTERRUPTED safety ruling documented. Prefer allow valid proposal after stop because explicit saved preview remains; it remains just a proposal. Enforce same org composite FK and action-assistant role, status checks. Audit links proposal ID without creating new event type. No ticket SQL duplication. Write meaningful tests first (unit normalization plus real PG integration under `NEXUSDESK_TEST_W08_AI=1` with week06Fixture/own prefix safeguards); include concurrency/dedupe/expiry/rejection/revocation/digest failure/triggered rollback, schema constraints and interrupted recovery. No agents/commits. Report file supplied by controller; note commands and red/green evidence. Controller will run gated PG harness.

### Task 2: Streaming model, runner, tools and HTTP integration

Controller owns `internal/ai/model`, `internal/ai/{runner,tools,chat,handler,sse}*.go`, RAG source validation wrapper, main wiring. Implement bounded vendor-neutral DeepSeek/Ollama stream, accumulate tool fragments till completed, safe error codes, strict arguments, finite loop; reuse Store from Task1. SSE writer supports Flush, bounded deadlines, cancellation and heartbeat. Authentication remains existing protected router. Tests first for adapter chunks/truncation/unknown tools/spoofed actor/budgets/cancel/EOF and HTTP behavior. Existing RAG Ask unchanged. Full Go checks and real PG handler tests.

### Task 3: Frontend integration

Own web files only. Use existing design, session, client and organization route patterns. Add AI conversations nav, list/new/history/messages, streaming parser with UTF8/chunk buffering, stop/error handling, citations and persisted proposal cards. Exact API/SSE contract supplied. Confirmation sends no params. Preview org/creator/expiry plus ticket title/body/priority; link created ticket. No v-html, no persistent tokens, no stale response crossover. Add robust parser and UI tests (malformed stream, split multi-byte, old session401, scope reset, confirm/reject/errors). Run unit/type/build/lint/format. No agents/commits.

### Task 4: Evidence, actual runtime and documentation

Controller owns scripts, docs, README/AGENTS, local setup. Isolated PG/Redis migrations Up/Down test, W08 dedicated integration and W01–W07 regression, full Go race/vet/build; frontend checks; browser actual live DeepSeek+BGE synthetic conversation search/ticketlookup/proposal/no-write/confirm/reject/stop/crossorg. Browser proxy latency and screenshots +3–5minute demo recording; cap real requests. Back up daily PG opaque0600, apply only Up12, restart owned local services safely, status/health. Write API, four AI design docs, ADR0008, week08 test plan/results T01–T35 with evidence/limits, daily completion weekly.md, answers10+12, demo/requests. Actual learning review remains user's task. Review SSE/cancel, tools/security, confirmation/transactions in three readonly passes. Fix important findings then final verification and concise delivery.
