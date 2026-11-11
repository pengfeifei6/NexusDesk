# Week11 Observability, Performance and Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. User authorizes direct full completion in original main; no approval pause, worktree or commit.

**Goal:** Deliver measured API performance, diagnosable requests and verified dependency/worker recovery.
**Architecture:** Existing Go modular monolith, standard-library metrics/diagnostics, isolated synthetic PG/Redis experiments.
**Tech Stack:** Go1.26.1, Gin, pgx, Asynq, Redis; Python3.8+ harness; existing Vue/Playwright.
**Spec:** docs/superpowers/specs/2026-11-11-week11-observability-performance.md

## Global Constraints

- Only /media/yjz/pzp/Go/projects/NexusDesk main; no worktree/commit/push/reset/stash. Preserve previous dirty files.
- No new dependencies/migrations. Do not read private uploads/backups; no real.env modification or credential output. Tests never call paid cloud models.
- Preserve real-time authorization, source/caching version checks, SSE cancellation/flush, human confirmation, existing 39 business routes and nine views.
- Fault/seed/cleanup only randomly named script-owned resources. No daily PG/Redis data or service changes.
- Logs/metrics contain no headers, raw URL/query/body/SQL/args/secret/model content; metric label cardinality bounded.
- Preserve W10 and older evidence. Measure before optimizing; failures and all runs retained.

## Review Focus

1. Unknown paths/methods and malicious headers must not create unbounded labels or log data (Task1 tests).
2. Panic and SSE must preserve safe correlated errors and streaming cancellation (Task1 tests + Task3 full regression).
3. Cache decode optimization must reject foreign/null/corrupt payload and preserve authorized fallback (Task2 tests + Task3 gates).
4. Diagnostic listener cancellation/bind conflict and concurrent captures must not strand processes (Task1 tests, Task3 lifecycle).
5. Worker crash recovery must use real lease expiry and avoid duplicate current chunks; DB outage cannot authorize from cache (Task3 actual fault drills).

---

### Task 1: Integrated observability and local diagnostics

**Files:** Create internal/platform/observe/{registry,context,diagnostics,pgx}.go plus focused tests; internal/platform/httpserver/observe.go/tests; internal/platform/config/observability.go/tests. Modify cmd/api/main.go, cmd/worker/main.go, config/config.go and async.go, database/postgres.go, httpserver/router.go, seven domain handler error helpers, rag/service.go, scripts/dev.py/test_dev.py, .env.example. No cache implementation edits, no UI edits.

**Interfaces:** Produces observe.Registry and a diagnostics server returning its address/close lifecycle; NewRouterWithObserver registry wiring; database.Open variadic pgx.QueryTracer (old calls remain valid); observe.RequestID(context.Context) string. External commands use OBSERVABILITY_ADDR (API), WORKER_OBSERVABILITY_ADDR (Worker), dev.py api|worker --diagnostics-address 127.0.0.1:6060|6061. /metrics Prometheus0.0.4; /debug/pprof/{profile,heap,allocs,goroutine,trace}, seconds1..30 default5. Registry helpers and metric exact names chosen in this task and documented in report for Tasks2/3.

- [x] Write focused failing tests for server-generated32hex request ID/header/error JSON, panic privacy, unknown labels bounded, SQL noargs exposure, parallel registry/race, diagnostics disabled/loopback/GET/allowlist/seconds/concurrent capture/shutdown and launcher child-env isolation.
- [x] Implement spec in stated files; JSON slog in both binaries; HTTP request trace task and SQL regions, PG/cache/runtime metrics; Worker delivery-return/dispatch-return metrics and safe job correlation. Do not reinterpret Execute nil as business success. Keep original NewRouter helper for tests while applying request ID at real router.
- [x] Run focused tests then go test -race ./internal/platform/... ./internal/{auth,organization,ticket,knowledge,rag,jobs,ai} ./cmd/... (database gated skips explicitly acknowledged; controller later enables real gates), Python launcher tests, go vet/build, gofmt, diff check.
- [x] Write task-1-report.md with exact new interfaces/metrics, file list, TDD and results; freeze for independent task review, no commit.

### Task 2: API load experiment and one measured optimization

**Files:** scripts/week11-performance.py, scripts/week11_load.py and tests; cmd/loadtest/main.go and tests if Go client is used (standard library only); internal/platform/cache/cache.go and cache decode tests/benchmarks only after baseline evidence; docs/performance-report.md; docs/testing/evidence/week11/performance*.json, profile artifacts/top text. Depends Task1 diagnostics, controller's scripts/week11_runtime.py fixture contract.

**Interfaces:** Fixture exports Week11Lab, application(lab,directory,overrides=None,worker=False,web=False) yielding object with api/origin/env/diagnostic_url/worker_diagnostic_url, own api_process/worker lifecycle helpers and safe logs. Coordinate final signature with controller before writing runner. /metrics and pprof from Task1.

- [x] Implement finite closed-loop keepalive load generator with per-worker connection, strict timeout/bounded response, count successes/failures, nearest-rank percentiles with meaningful tests. Validate loopback/test-only guards and avoid persisting Token. Measure 1/8/16 concurrency, >=3seconds each,2rounds; endpoint scenarios ticket list/knowledge list/detail/create, exact parameters recorded.
- [x] Seed isolated two-org corpus (initial20000rows/table) and authenticate via realAPI; collect machine/runtime/data metadata, metrics, CPU/heap/trace from business load; save baseline before edits. Preserve baseline source file copy/hash.
- [x] Identify profile-supported CPU/allocation hotspot. Candidate is cache envelope doubleJSON decode; if confirmed, implement smallest semantics-preserving optimization with corruption/foreign/null/truncated/valid-empty tests and before/after benchmark. No hard performance assertion on noisy timings, no safety removal. If evidence contradicts candidate, report before modifying other packages.
- [x] Rerun same complete API matrix/benchmarks after optimization, retain all runs/errors/profiles and explain uncertainty, total allocatedbytes/allocs as well as latency. Verify pprof and trace parse. Report ordinaryAPI separately from LLM; do not call cloud models.
- [x] Run affected Go race tests and script tests, write docs/performance-report.md and task-2-report.md, freeze for task review. Root owns all-gate final checks and fault drills.

### Task 3: Owned recovery drills, regression and handoff (controller)

**Files:** scripts/week11_runtime.py, scripts/week11-faults.py, scripts/week11-smoke.py, scripts/week11-checks.py, scripts/week11-browser.mjs (reuse W10 workflow with W11 artifacts); requests/week11.http; docs/observability.md, docs/failure-drills.md, docs/weekly/week11.md and week11-answers.md, docs/demo/week11-demo.md, docs/testing/week11-test-plan.md, docs/项目阶段介绍_第十一周.md, 项目开发文件/每周开发手册/14_NexusDesk_第十一周详细开发手册.md; README/AGENTS currentphase. New .local/week11-* owned scratch, evidence/week11.

- [x] Build fixture subclass reusing existing owned namespace guards, safe env/proxy/credentials and cleanup; expose exact interface to Task2. Write meaningful safety/metric math tests when new logic needs them.
- [x] Run actual Redis stop/start: DB fallback200, AI preflight503 and message counts unchanged, recovery. Run actual PG stop/start: health200, protected500 withID (no cached authorization), original records remain after recovery.
- [x] Run graceful Worker signal/restart and kill own RUNNING worker blocked on localmodel, real30second lease recovery then finalSUCCEEDED and unique current index; record times/attempts/observations, no SQL lease shortening. All resource stops guarded by ownership.
- [x] Run complete realPG/Redis race allgates (0Skip), frontend149+/build/lint/format, Python tests, Go vet/build and adapted full browser. No extra cloud evaluation or W10 evidence rewrite.
- [x] Write seven-day manual, actual profile/load/fault evidence explanations, troubleshooting/run guide/answers; update README/AGENTS, record preservation and source hashes. Readiness/restart only owned main application cohort if needed, no daily dependency restart or schema changes.
- [x] Independent final whole-change review covers controller Task3, then one scoped fix wave if necessary. Leave original main uncommitted and report actual results/limitations/entry links.
