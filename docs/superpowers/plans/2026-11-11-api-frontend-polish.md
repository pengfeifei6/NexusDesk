# API Reference and Frontend Polish Implementation Plan

> **For agentic workers:** Use scoped parallel delegation for independent API documentation and root frontend work, then one fresh final review. User authorizes direct execution in original main; no approval menu, worktree or Git writes.

**Goal:** Deliver complete accurate API reference and a visibly refined responsive frontend.
**Architecture:** Existing Go backend read-only; Chinese Markdown + OpenAPI3.1 JSON. Existing Vue components, code-native SVG and CSS tokens; no new dependency.
**Tech Stack:** Vue3/TypeScript, Playwright, Python standard library, existing Node24 toolchain.
**Spec:** docs/superpowers/specs/2026-11-11-api-frontend-polish-design.md

## Global Constraints
- Original NexusDesk main only; no Go/backend/migration/.env/dependency-lock changes.
- Preserve39businessAPI contracts, in-memoryToken/losslessIDs/scope/permissions/409/humanAIconfirmation.
- Evidence only docs/testing/evidence/frontend-polish/{before,after}; own random fixtures, no daily DB/private data/cloud calls.
- No fabricated globalstatistics or nonfunctional decorative controls. No commit/push/deployment.

## Review Focus
- OpenAPI defaults/statuses/envelopes/nullable/bigint/multipart/SSE reflect actual code, not old drafts.
- Longcontent/IDs/membernames do not overflow; actions and confirmations remain reachable on smallscreens.
- Currentpage metrics must not misrepresent totalorganization counts.
- New visuals preserve keyboardfocus, meaningfullabels, errors, disabled/loading states and reducedmotion.
- Historicalartifacts, backend and locks stay byteidentical.

### Task1: Complete API documentation (scoped independent agent)
**Files:** docs/api/NexusDesk_API完整文档.md, docs/api/openapi.json, docs/api/README.md, scripts/check_api_reference.py; only these.
**Interfaces:** 39 canonical businessoperations documented from actual code; optionalhealthz. Markdown and OpenAPI operation/schema mapping. Agent returnsfiles/counts/checkresults.
- [x] Read domain contracts and implementation; generate all endpoint/input/response/error/security/SSE details and losslessbigint notes.
- [x] Validate all39 routes, refs, parameter locations and examples; no backend mutation.

### Task2: Refine application visuals (root)
**Files:** App.vue, views/*.vue, components/UiIcon.vue, assets/main.css and assets/workspace-theme.css; focused presentation helper/tests only ifbehavioradded.
**Interfaces:** Existingdesk/knowledge/AI APIs unchanged. Page-onlyloadedticket summary, keyboard-reachable in-page creation link.
- [x] Capturebefore via new frontend-polish-smoke.py/browser.mjs.
- [x] Add focused behavior tests first for anynew currentpage summary/shortcut behavior.
- [x] Update shell/auth/overview/list/forms/detail/knowledge/RAG/AI/member visuals with sharedtokens and nofakecontrols.
- [x] Run fullfrontendunit/build/lint/format and realbrowserafter; inspect screenshots, mobileoverflow/focus/reducedmotion.

### Task3: Integrate documentation and verify delivery (root)
**Files:** README, docs/frontend-polish.md, newevidence and reviewrecord.
- [x] Validate agentdocs and outputs, updatecurrentREADME pointers.
- [x] Independent wholechange review, fiximportantfindings with regression, no repeatedreview.
- [x] Preservation/hash/link/secret-pattern checks, mainfrontendreadiness; provide docandvisualartifactlinks.
