# Design: Azure Plugin Typed-IaC Conformance Migration (v1.0.0)

**Date:** 2026-05-13
**Author:** Claude Code (autonomous pipeline)
**Status:** Approved

## Context

workflow-plugin-azure v0.1.0 still uses the legacy `sdk.Serve(internal.New(version))`
string-dispatch surface. The workflow engine's strict-contracts force-cutover (v0.50.0+)
removed this path from the host side. This migration adopts the same typed-IaC gRPC
pattern that workflow-plugin-aws v1.0.0 (PR #11) and workflow-plugin-digitalocean
v1.0.1 shipped under.

The existing `strict-contract` branch in this repo uses `module_instance.go` +
`internal/contracts/` (the older `InvokeTypedMethod` string-dispatch approach, predating
force-cutover) and MUST NOT be merged. The new approach mirrors AWS v1.0.0 exactly.

## Precedents

- workflow-plugin-aws v1.0.0 (PR #11) — direct mirror reference
- workflow-plugin-digitalocean v1.0.1 — original force-cutover reference
- Both pin workflow v0.51.2/v0.51.7 and use `sdk.ServeIaCPlugin`

## Approach

**Single-PR force-cutover mirroring AWS v1.0.0.**

No compat shim. The legacy `sdk.Serve(internal.New(version))` entrypoint in
`cmd/workflow-plugin-azure/main.go` is replaced with
`sdk.ServeIaCPlugin(internal.NewIaCServer(), sdk.IaCServeOptions{})`.
The `Manifest()` method on `AzureProvider` (used only by the legacy `sdk.PluginProvider`
interface) and `var version = "dev"` in main.go are removed. The SDK auto-registers every
typed gRPC service the server satisfies via Go type-assertion at plugin startup.

Alternatives considered:
- **Keep `sdk.Serve` + add `InvokeTypedMethod` bridge** — rejected: incompatible with
  engine v0.50.0+.
- **Merge from `strict-contract` branch** — rejected: that branch uses the old
  `module_instance.go` + `contracts/` string-dispatch approach, not `sdk.ServeIaCPlugin`.
- **Two-PR approach** — rejected per `feedback_force_strict_contracts_no_compat`.

## Scope

### Phase 1 — Typed server layer (new files)

| File | Action |
|------|--------|
| `internal/iacserver.go` | NEW: `azureIaCServer` struct with Required + DriftDetector RPC methods |
| `internal/resourcedriver_server.go` | NEW: ResourceDriver CRUD dispatch (9 methods) |

`azureIaCServer` embeds (same pattern as AWS v1.0.0):
```
pb.UnimplementedIaCProviderRequiredServer
pb.UnimplementedIaCProviderEnumeratorServer        // forward-compat only
pb.UnimplementedIaCProviderDriftDetectorServer
pb.UnimplementedIaCProviderCredentialRevokerServer // forward-compat only
pb.UnimplementedIaCProviderMigrationRepairerServer // forward-compat only
pb.UnimplementedIaCProviderValidatorServer         // forward-compat only
pb.UnimplementedIaCProviderDriftConfigDetectorServer
pb.UnimplementedResourceDriverServer
```

**What gets implemented:**
- All `IaCProviderRequiredServer` methods: `Initialize`, `Name`, `Version`,
  `Capabilities`, `Plan`, `Apply`, `Destroy`, `Status`, `Import`, `ResolveSizing`,
  `BootstrapStateBackend`
- `IaCProviderDriftDetectorServer`: both `DetectDrift` (real impl) and
  `DetectDriftWithSpecs` (thin delegator to `DetectDrift`, ignores specs map)
- `ResourceDriverServer`: 9 CRUD methods via `AzureProvider.ResourceDriver(type)`

**What remains Unimplemented** (embed only, not auto-registered):
- `EnumerateAll`, `EnumerateByTag` — no Azure tag-query implementation
- `RevokeProviderCredential` — no credential rotation
- `RepairDirtyMigration` — no migration repair
- `ValidatePlan` — no cross-resource plan validator
- `DetectDriftConfig` — DriftConfigDetector is a separate service

Marshalling helpers are copied verbatim from AWS v1.0.0 `internal/iacserver.go`.
All config/outputs cross as `config_json`/`outputs_json` (JSON bytes) — no structpb.

### Phase 2 — Entrypoint cutover

| File | Action |
|------|--------|
| `cmd/workflow-plugin-azure/main.go` | Replace `sdk.Serve(internal.New(version))` with `sdk.ServeIaCPlugin(internal.NewIaCServer(), sdk.IaCServeOptions{})` |

Note: `var version = "dev"` and the `Manifest()` method are no longer needed
after removing `sdk.PluginProvider`. `Manifest()` can be removed from
`internal/provider.go` (it satisfies `sdk.PluginProvider`, not `interfaces.IaCProvider`).
The `var _ sdk.PluginProvider = (*AzureProvider)(nil)` compile guard is also removed.

### Phase 3 — Version/metadata updates

| File | Action |
|------|--------|
| `go.mod` | Bump `workflow v0.19.2` → `v0.51.7`; run `go mod tidy` first |
| `plugin.json` | Bump `version` to `1.0.0`, `minEngineVersion` to `0.51.0`; update manifest shape to match AWS v1.0.0 (add `moduleTypes: ["iac.provider"]`, remove `iacProvider: true` + `resourceTypes`) |
| `plugin.contracts.json` | NEW: `{"version":"v1","contracts":[{"kind":"module","type":"iac.provider","mode":"strict","config":"workflow.plugins.azure.v1.AzureProviderConfig"}]}`; requires `internal/contracts/azure.proto` + generated `azure.pb.go` (extract from `strict-contract` branch, strip `module_instance.go` integration) |
| `internal/contracts/azure.proto` | NEW (extracted from strict-contract branch): proto message `AzureProviderConfig` with fields: `subscription_id`, `resource_group`, `location`, `storage_account` |
| `internal/contracts/azure.pb.go` | NEW: generated from `azure.proto` via protoc |
| `internal/provider.go` | Remove `Manifest()`, `var _ sdk.PluginProvider`, update version constant |

**plugin.json shape after:** mirrors AWS v1.0.0 (capabilities with `moduleTypes: ["iac.provider"]`,
no `iacProvider` or `resourceTypes` at capability level).

### Phase 4 — Tests

| File | Action |
|------|--------|
| `internal/iacserver_test.go` | NEW: unit tests for all server methods (same pattern as AWS) |
| `internal/host_conformance_test.go` | NEW: mirrors AWS v1.0.0 `host_conformance_test.go` with `azure` substituted for `aws` |
| `internal/provider_test.go` | Remove test for `Manifest()` (method deleted) |
| `integration_test.go` | Leave as-is (uses wftest mocks, unaffected by gRPC cutover); verify it compiles and passes under workflow v0.51.7 after go.mod bump as part of plan task |

**`host_conformance_test.go` spec** (mirrors AWS v1.0.0 exactly):
1. Build plugin binary via `go build -o <tmpdir>/workflow-plugin-azure ./cmd/workflow-plugin-azure`
2. Load via `external.NewExternalPluginManager` + `LoadPlugin`
3. Assert `adapter.ContractRegistry()` contains a service-kind contract
   with `pb.IaCProviderRequired_ServiceDesc.ServiceName`
4. Make live `pb.NewIaCProviderRequiredClient(adapter.Conn()).Name()` RPC
5. Assert name = `"azure"` and `infra.container_service` in Capabilities

### Phase 5 — CI

Add `scripts/workflow-iac-host-conformance.sh` (mirroring AWS v1.0.0) with the
`-run` flag set to `TestWorkflowHostConformance_LoadsTypedIaCPlugin` (NOT
`LoadsLegacyIaCModulePlugin` — the AWS script has a stale name that causes silent
skip; Azure must use the correct typed-IaC test name from the start).

Add `.github/workflows/iac-host-conformance.yml` mirroring AWS v1.0.0.

The existing `ci.yml` runs `go test ./...` which covers all non-conformance tests.
The conformance workflow provides the typed-IaC load path gate.

## Compile-time guards

```go
var (
    _ pb.IaCProviderRequiredServer      = (*azureIaCServer)(nil)
    _ pb.IaCProviderDriftDetectorServer = (*azureIaCServer)(nil)
    _ pb.ResourceDriverServer           = (*azureIaCServer)(nil)
)
```

## Wire invariants (strict-contracts hard invariants)

- NO `structpb.Struct` on the wire
- NO `Any.UnmarshalTo` for config/outputs — use `config_json` / `outputs_json` (JSON bytes)
- Outputs that are `map[string]any` are marshalled to JSON, never via `structpb.NewStruct`
- Typed slices (`[]string`, `[]X`) are safe because `pb.ResourceOutput.outputs_json` is `bytes`

## Ordering note

Phase 3 (go.mod bump) MUST happen before writing new server code. The v0.19.2 → v0.51.7
jump (32 minor versions) may introduce transitive dependency changes. Run
`go mod tidy && go build ./...` to surface any API breaks before writing iacserver.go.

## Rollback

Revert the commit and retag v0.1.0. No database migrations, no state mutations.
After reverting, run `go mod tidy` to restore go.sum to the v0.19.2-pinned state.
Old workflow engine tags (pre-v0.50.0) are permanently incompatible after this PR.

## Assumptions

1. `sdk.ServeIaCPlugin` and `sdk.RegisterAllIaCProviderServices` are present and stable
   in workflow v0.51.7 (confirmed: AWS v1.0.0 used v0.51.7 with this API).
2. `pb.Unimplemented*Server` embeds for optional services cause those services NOT to be
   auto-registered (type-assertion fails) — callers get "service not registered".
3. `host_conformance_test.go` can validate typed-IaC load without a live Azure credential
   (plugin starts and responds to `Name()` and `Capabilities()` without an initialized
   Azure session — same pattern as AWS and DO).
4. `workflow v0.51.7` is the latest stable tag.
5. `*AzureProvider.DetectDrift` implements existence-check only. `DetectDriftWithSpecs`
   delegates to `DetectDrift`.
6. The `fix/bootstrap-state-backend-stub` branch's provider additions (`BootstrapStateBackend`,
   `SupportedCanonicalKeys`) are correct and already merged to `main` (they are — git log
   confirms `fix/bootstrap-state-backend-stub` is 2 commits ahead of `main`; migration
   PR will rebase from `main` so those fixes must be merged first OR included in this PR).
7. `Manifest()` on `AzureProvider` satisfies `sdk.PluginProvider` only (not
   `interfaces.IaCProvider`). Removing it after the `sdk.Serve` cutover is safe.
8. The `integration_test.go` uses `wftest` mocks against step types (`step.azure_deploy`,
   `step.azure_provision`) that don't exist in this plugin — these tests already pass
   because they mock everything. No change needed.

## Open questions resolved

- Q: Start from `main` or `fix/bootstrap-state-backend-stub`?
  A: Branch from `fix/bootstrap-state-backend-stub` HEAD (commit `f9071c4`). That branch
  is 2 commits ahead of `main` with `BootstrapStateBackend` and `SupportedCanonicalKeys`
  stubs that are required by `interfaces.IaCProvider`. Those changes have not yet merged to
  `main`. The migration PR will include them as part of the typed-IaC cutover commit history.
  The base branch for the PR is `main` (the PR merges bootstrap fixes + typed-IaC together).
- Q: Single PR vs multiple? A: Single-PR force-cutover per mandate.
- Q: Keep `internal/contracts/` from the old strict-contract branch? A: No. The old
  `module_instance.go` approach is discarded. The new approach uses
  `sdk.ServeIaCPlugin` + typed pb servers, not module factories.
