# Retro: Azure Plugin Typed-IaC Conformance Migration

**PR:** #5 — feat: typed-IaC strict-contracts migration — Azure plugin v1.0.0
**Merged:** 2026-05-13
**Branch:** feat/typed-iac-azure-v1
**Design:** docs/plans/2026-05-13-plugin-azure-typed-iac-design.md
**Plan:** docs/plans/2026-05-13-plugin-azure-typed-iac-conformance.md
**Related ADRs:** none

## Adversarial-review findings, scored

The design doc records `c552ac3` ("address adversarial review findings in Azure typed-IaC design") and the plan went through `alignment-check` before scope-lock. Adversarial review reports were not committed as standalone files (the findings were iterated inline in the design/plan docs), so scoring is based on the design doc's stated "alternatives considered / assumptions / open questions" sections.

| Phase | Finding | Severity | Outcome |
|---|---|---|---|
| design | Use correct test name `TestWorkflowHostConformance_LoadsTypedIaCPlugin` (AWS script has stale `LoadsLegacyIaCModulePlugin` that causes silent skip) | Important | Resolved upfront — plan spec'd the correct test name; CI gate ran and passed |
| design | JSON bytes only on the wire — no structpb.Struct / Any.UnmarshalTo | Critical | Resolved upfront — iacserver.go uses `config_json`/`outputs_json` throughout |
| design | Set `minEngineVersion` to `0.51.2` not `0.51.0` (ServeIaCPlugin predates 0.51.0) | Important | Resolved upfront — plugin.json ships `0.51.2`; conformance test validated this |
| design | Branch from `fix/bootstrap-state-backend-stub` not `main` (BootstrapStateBackend stubs required) | Important | Resolved upfront — branch strategy handled; both commits landed via PR history |
| plan | Task 1 (go.mod bump) must precede writing server code to surface API breaks | Minor | Resolved upfront — task ordering followed; no API breaks found |

## Gate misses

| Issue | Gate that missed | Why it slipped | Fix idea (optional) |
|---|---|---|---|
| `Scale` method passed negative replicas straight to driver without validation | adversarial-design-review (plan) | The plan listed all 9 ResourceDriver RPCs but did not enumerate input validation requirements per method; adversarial review checked structural coverage, not per-RPC invariants | Add "input validation completeness" to the IaC plugin bug-class checklist in `workflow-plugin-reviewer` |
| `iacserver_test.go` header comment claimed Apply/Destroy/Import/Status lived in `provider_test.go` — they don't | requesting-code-review | Comment was written when tests were first drafted and not re-verified against actual `provider_test.go` content before PR | Cross-check file-level doc comments against actual test function lists in `verification-before-completion` |
| Branch diverged from main mid-PR (main got `ce06592` draft-release workflow while PR was in flight), requiring a merge commit to unblock squash | none (expected race condition) | main advanced during the ~17-minute review window; not a gate miss, just a timing artifact | Consider `--auto` flag for future monitor sessions if branch protection requires approval |

## Missed skill activations

| Gate | Fired? | Notes |
|---|---|---|
| brainstorming | yes | Confirmed via design doc date and c552ac3 adversarial fix commit |
| adversarial-design-review (design) | yes | c552ac3 records design revisions |
| writing-plans | yes | Plan committed as docs/plans/2026-05-13-plugin-azure-typed-iac-conformance.md |
| adversarial-design-review (plan) | yes | scope-lock file present (.scope-lock), indicating alignment-check + adversarial plan phase |
| alignment-check | yes | scope-lock committed (2026-05-13T00:00:00Z) |
| executing-plans / subagent-driven-development | yes | 8 tasks executed, all 8 commits visible in PR history |
| finishing-a-development-branch | yes | PR created with scope manifest and runtime validation results |
| pr-monitoring | yes | this session |
| post-merge-retrospective | yes | this document |

No missed activations.

## What worked

- **Adversarial review caught all structural risks upfront.** The three design-phase findings (test name, structpb wire invariant, minEngineVersion) were all resolved before the first commit; zero CI failures across 3 CI runs.
- **AWS v1.0.0 as a direct mirror reference made execution mechanical.** Having a tested prior art reduced ambiguity in every task; the plan could cite exact file structures and copy marshalling helpers verbatim.
- **Host conformance test as runtime launch validation.** The test builds the binary, loads it via the real external plugin manager, and makes live gRPC calls — catching integration failures that unit tests would miss. It passed on first run.
- **Compile-time interface guards.** The three `var _ pb.*Server = (*azureIaCServer)(nil)` lines caught any missing method implementations at build time, preventing CI failures from incomplete interface satisfaction.

## What didn't

- **Per-RPC input validation was not in scope for adversarial review.** The `Scale` replicas validation gap is a class of bug (missing boundary checks on numeric gRPC inputs) that adversarial review didn't look for — it checked interface coverage, not value-range invariants. Copilot caught it; adversarial review did not.
- **File-level doc comments were not re-verified before PR.** The `iacserver_test.go` comment was stale from initial drafting. `verification-before-completion` should include a grep-and-verify pass on file header comments that cite other files.
- **Branch divergence from main required a reactive merge commit.** The monitoring session had to resolve a go.mod conflict (main bumped to v0.19.2 while branch had v0.51.7). This was an expected race but added one extra CI cycle.

## Plugin-level follow-ups

**New bug class for `workflow-plugin-reviewer`:** Add "ResourceDriver input validation" to the IaC pattern checklist. For every `Scale`, `Create`, `Update` RPC: verify numeric inputs have non-negative / in-range guards before driver dispatch. The `Scale` miss here is the same class of bug that could affect AKS (`int32` overflow) or AppService (zero replica count). Pattern: `req.GetReplicas() < 0` → `codes.InvalidArgument`. One prior retro (this one) — watch for recurrence across GCP/DO plugins before promoting to a permanent checklist item.
