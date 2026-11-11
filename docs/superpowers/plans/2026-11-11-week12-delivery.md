# Week12 MVP Completion and Delivery Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans inline, with one independent final reviewer. User already authorizes direct completion in original main; no approval pause/worktree/commit/push.

**Goal:** Complete existing API UI workflows and a reproducible, verified 12-week local portfolio delivery.
**Architecture:** Existing modular Go API/Worker and Vue frontend; standard-library local setup/archive tools; synthetic isolated verification.
**Tech Stack:** Existing Go1.26.1/Vue/TS/Node24/Python3.8+/PG17/Redis; no dependency upgrades.
**Spec:** docs/superpowers/specs/2026-11-11-week12-delivery-design.md

## Global Constraints
- Original /media/yjz/pzp/Go/projects/NexusDesk only, preserve earlier dirty files/evidence; no worktree, commit, push, migration edit, new dependencies or cloud calls.
- Keep39 businessAPI/14migrations; frontend10views after members. Existing real.env/storage/backups untouched; tests synthetic ownedresources only.
- Authorization remains server-owned; lossless IDs, in-memoryToken, cancellation/scope, manualAIconfirmation retained.
- Publication/user ability claims require actual authority/evidence; localpackage/demo do not mean GitHub published or personalindependentexperience.

## Review Focus
- Late org/session/route responses cannot restore members or navigate afterdelete (Task1 mountedtests+browser).
- 409/conflictingversion/403/404 must clear stale management intent/content without retryingwrites (Task1).
- Memberlist pagination/disabledmembers/bigint IDs cannot cause wrongassignee or rounded user_id (Task1).
- Init cannot overwriteexistingconfig/symlink; migration cannot targetexternal DB or exposeDSN, packagecannottraverse/symlink/copyprivatefiles (Task2 tests).
- Fullgate mustnot replace skips with pass, archive must containrunnablecode, video must be actual5–8min and useonlysyntheticdata (Task3).

### Task 1: Existing API management UI
**Files:** web/src/composables/{members,ticket-management}.ts; views/OrganizationMembersView.vue; components/TicketManagementPanel.vue; lib/__tests__/management.spec.ts; modify types/router/App/DeskView/TicketDetailView/desk/client/CSS as needed.
**Interfaces:** useDesk returnsDesk; Member DTO user_id/display_name/role/status/created_at; existing Client.request and Scope. PATCH expected_version string(encodedJSONint), DELETE no body; no newbackend.
- [x] Write mounted RED tests foradminadd/currentorg/losslessid,memberreadonly,lateorg/permissionclear; OPENedit/adminassign/explicitdeletecancelandconfirm/version409/staleresponse.
- [x] Implement focusedcomposables/views/panel, canonicalpositiveIDvalidation, pageractiveassignees, permission/intent clearing; fixdeskonlyloadticketpaths and appropriateprotecteddata clearing.
- [x] Run focusedthenfullfrontendunit/build/lint/format; inspectresults andlogTDD. No commit.

### Task 2: Safe setup and source package
**Files:** scripts/local_setup.py, scripts/package_release.py, scripts/test_local_setup.py, scripts/test_package_release.py; .gitignore evidenceallowlist only.
**Interfaces:** local_setup.init_env(root), migration_env(values,parent) -> scopedenv; CLI init/status/up. package_release.build(root,output,version)->manifest; outputunder.local/releases, nooverwrite.
- [x] RED filesystemtests init0600/randomsecret/nooverwrite/symlink, scopedDSN/loopback/errors; packageallowlist/symlink/privatefiles/overwrite/pathvalidation.
- [x] Implement usingstdlib; Goose env=none; init/migrations onlyuserinvoked, not automaticdailywrites; sourcearchive deterministicmanifest/checksum, credentialscan namedpathonly.
- [x] Run Python tests; actualinit+Up14+status on ownedfixture; sourcepackage unpackbuild andnpmci/build fromcontainedlocks (Task3 afterdocsready).

### Task 3: Final gates, demo, documentation and handoff
**Files:** scripts/week12-{smoke.py,checks.py,browser.mjs}; docs/testing/evidence/week12; currentdocs/release/*,docs/demo/week12-demo.md,docs/weekly/week12{,-answers}.md,docs/项目最终交付介绍_第十二周.md; manual15_NexusDesk_第十二周详细开发手册.md; README/AGENTS/currentrequirements/architecture links.
**Interfaces:** ReuseW11ownedruntime/model; W12outputonly; CLI --backend-only/--web-only/--demo (demo5–8minvideo+chapters); nooldartifactchanges.
- [x] Adapt full smoke/checks+browser managementflows, extra403/409/staleversion assertions; recordrealUI/screens/5–8mincaptionedvideo separately.
- [x] Write7daymanual, conciseREADME/currentarchitecture/APIindex/maintenance, API-UIacceptancematrix/knownlimits, projectresume(nofabricatedexperience)+10deepquestions, demo script/subtitles, localreleasechecklist.
- [x] RunfullrealPG/Redisrace0Skip,frontend/Python/staticgates,browser,sourcearchiveGo/npmbuild; savefailuresandactualcounts; verifyold994filespreservation,dependencies/migrations/privateignore/sourcehashes.
- [x] Independentwholechangereview (mostcapablefreshcontext) explicitlyagainstspec; onefixpassifneeded, meaningfulRED→GREEN andaffectedregression.
- [x] VerifyownedmainUI/API/Workerreadiness; no dailyDB/dependencyrestartneededunlesssourcechange. Finalizeweekly/stage/evidence/packagelinks, keepmainuncommitted.
