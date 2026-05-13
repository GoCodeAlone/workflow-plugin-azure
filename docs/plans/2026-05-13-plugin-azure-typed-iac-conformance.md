# Azure Plugin Typed-IaC Conformance Migration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Migrate workflow-plugin-azure from the legacy `sdk.Serve` string-dispatch entrypoint to the typed-IaC gRPC pattern (`sdk.ServeIaCPlugin`), bumping to workflow v0.51.7 and releasing as v1.0.0.

**Architecture:** Single-PR force-cutover mirroring workflow-plugin-aws v1.0.0 (PR #11). A new `azureIaCServer` struct wraps `*AzureProvider` and implements every required typed gRPC interface. The legacy `sdk.Serve(internal.New(version))` entrypoint and the `sdk.PluginProvider` surface (`Manifest()` method) are removed. The SDK auto-registers all typed services via Go type-assertion at startup.

**Tech Stack:** Go 1.26, workflow v0.51.7, azure-sdk-for-go Track 2, google.golang.org/protobuf v1.36.11, gRPC

**Base branch:** `fix/bootstrap-state-backend-stub` (2 commits ahead of main; includes BootstrapStateBackend + SupportedCanonicalKeys stubs required by interfaces.IaCProvider)

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 8
**Estimated Lines of Change:** ~1200 (new iacserver.go ~760, resourcedriver_server.go ~240, tests ~200, CI scripts ~80, metadata updates ~50, deletions ~70)

**Out of scope:**
- Implementing `EnumerateAll`/`EnumerateByTag` (no Azure tag-query support yet)
- Implementing `ValidatePlan` (no cross-resource validator)
- Implementing `RevokeProviderCredential` or `RepairDirtyMigration`
- Any changes to Azure SDK drivers themselves
- Populating `plugin.contracts.json` with a FileDescriptorSet (the engine only validates FileDescriptorSet if present over gRPC, not from disk JSON)

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat: typed-IaC strict-contracts migration — Azure plugin v1.0.0 | Task 1, Task 2, Task 3, Task 4, Task 5, Task 6, Task 7, Task 8 | feat/typed-iac-azure-v1 |

**Status:** Locked 2026-05-13T00:00:00Z

---

## Context: Branch Setup

Before starting Task 1, create the feature branch from the current HEAD of `fix/bootstrap-state-backend-stub`:

```bash
cd /Users/jon/workspace/workflow-plugin-azure
git checkout fix/bootstrap-state-backend-stub
git checkout -b feat/typed-iac-azure-v1
```

Verify the branch includes the bootstrap fixes:
```
git log --oneline -5
# Should show: f9071c4 fix(provider): polish — Azure-specific doc + underscore params
#              c27d0ba fix(provider): implement IaCProvider.{BootstrapStateBackend,SupportedCanonicalKeys}...
#              37b861b fix(drivers): implement ResourceDriver.SensitiveKeys...
```

---

### Task 1: go.mod version bump + verify build

**WHY FIRST:** The v0.19.2 → v0.51.7 jump (32 minor versions) may break `interfaces.*` or `plugin/external/proto/*` APIs. Surface any breaks before writing new code.

**Files:**
- Modify: `go.mod`
- Modify: `go.sum` (via `go mod tidy`)

**Step 1: Update go.mod to workflow v0.51.7**

Edit `go.mod`, change:
```
github.com/GoCodeAlone/workflow v0.19.2
```
to:
```
github.com/GoCodeAlone/workflow v0.51.7
```

**Step 2: Run `go mod tidy`**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" go mod tidy
```

Expected: exits 0, `go.sum` updated. If there are "module not found" errors, ensure git is configured for private module access:
```bash
git config --global url."https://${GITHUB_TOKEN}@github.com/".insteadOf "https://github.com/"
```

**Step 3: Verify the codebase compiles**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" go build ./...
```

Expected: exits 0. If there are compilation errors:
- If `interfaces.IaCProvider` has changed methods → update `internal/provider.go` to match new interface
- If `sdk.PluginProvider` has changed → note but don't fix (Task 2 removes it entirely)
- If driver interfaces changed → fix in the affected `internal/driver/*.go` files

**Step 4: Run existing tests**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" go test ./... -count=1
```

Expected: all tests pass (or the same tests that pass today still pass — `integration_test.go` uses wftest mocks and may have a wftest API change under v0.51.7; note any new failures). The `internal/provider_test.go` `TestAzureProvider_Manifest` test will still pass at this stage since `Manifest()` is not yet removed.

**Step 5: Commit**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
git add go.mod go.sum
git commit -m "chore: bump workflow v0.19.2 → v0.51.7 for strict-contracts migration"
```

**Rollback:** `git revert HEAD` then `go mod tidy` to restore go.sum to the v0.19.2 state.

---

### Task 2: Proto contracts — `azure.proto` + `azure.pb.go`

The `plugin.contracts.json` needs to reference a fully-qualified proto message type name `workflow.plugins.azure.v1.AzureProviderConfig`. We need a slim proto (just the config message, mirroring `internal/contracts/aws.proto`).

**Files:**
- Create: `internal/contracts/azure.proto`
- Create: `internal/contracts/azure.pb.go`

**Step 1: Create `internal/contracts/azure.proto`**

```bash
mkdir -p /Users/jon/workspace/workflow-plugin-azure/internal/contracts
```

Write `/Users/jon/workspace/workflow-plugin-azure/internal/contracts/azure.proto`:

```proto
syntax = "proto3";

package workflow.plugins.azure.v1;

option go_package = "github.com/GoCodeAlone/workflow-plugin-azure/internal/contracts;contracts";

// AzureProviderConfig is the typed configuration for the iac.provider module
// provided by workflow-plugin-azure. All fields correspond to the map keys
// accepted by the Initialize(ctx, map[string]any) path.
message AzureProviderConfig {
  // subscription_id is the Azure subscription ID (required).
  string subscription_id = 1;
  // resource_group is the default Azure resource group (required).
  string resource_group = 2;
  // location is the Azure region (default: eastus).
  string location = 3;
  // storage_account is the Azure Storage account name for Blob operations (optional).
  string storage_account = 4;
}
```

**Step 2: Generate `azure.pb.go`**

Option A (if protoc is available):
```bash
cd /Users/jon/workspace/workflow-plugin-azure
protoc --go_out=. --go_opt=paths=source_relative internal/contracts/azure.proto
```

Option B (if protoc is NOT available — write the pb.go manually by adapting aws.pb.go):

Write `/Users/jon/workspace/workflow-plugin-azure/internal/contracts/azure.pb.go` as a hand-ported version of the AWS `aws.pb.go` with:
- Package: `contracts`
- Type name: `AzureProviderConfig` (not `AWSProviderConfig`)
- Fields: `SubscriptionId` (1), `ResourceGroup` (2), `Location` (3), `StorageAccount` (4)
- Proto source reference: `internal/contracts/azure.proto`
- Package path: `github.com/GoCodeAlone/workflow-plugin-azure/internal/contracts`
- Proto package: `workflow.plugins.azure.v1`

The easiest approach is to generate it. If protoc is not available, use `protoc` via Docker:
```bash
docker run --rm -v $(pwd):/workspace -w /workspace \
  ghcr.io/grpc-ecosystem/grpc-gateway/protoc:latest \
  --go_out=. --go_opt=paths=source_relative \
  internal/contracts/azure.proto
```

**Step 3: Verify the generated file compiles**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" go build ./internal/contracts/...
```

Expected: exits 0, no errors.

**Step 4: Commit**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
git add internal/contracts/azure.proto internal/contracts/azure.pb.go
git commit -m "feat: add AzureProviderConfig proto contract for plugin.contracts.json"
```

---

### Task 3: Metadata updates — `plugin.json`, `plugin.contracts.json`, `internal/provider.go`, `.goreleaser.yaml`

**Files:**
- Modify: `plugin.json`
- Create: `plugin.contracts.json`
- Modify: `internal/provider.go`
- Modify: `.goreleaser.yaml`

**Step 1: Update `plugin.json`**

Replace the contents of `plugin.json` with:

```json
{
  "name": "workflow-plugin-azure",
  "version": "1.0.0",
  "author": "GoCodeAlone",
  "description": "Microsoft Azure infrastructure provider: ACI, AKS, SQL, Redis, VNet, LB, DNS, ACR, APIM, NSG, MSI, Blob Storage, App Service Certificates",
  "license": "MIT",
  "type": "external",
  "tier": "community",
  "minEngineVersion": "0.51.0",
  "keywords": ["azure", "iac", "infrastructure", "cloud", "aci", "aks", "sql", "redis", "vnet"],
  "homepage": "https://github.com/GoCodeAlone/workflow-plugin-azure",
  "repository": "https://github.com/GoCodeAlone/workflow-plugin-azure",
  "capabilities": {
    "configProvider": false,
    "moduleTypes": [
      "iac.provider"
    ],
    "stepTypes": [],
    "triggerTypes": []
  }
}
```

Note: `iacProvider: true` and `resourceTypes` are removed; `moduleTypes: ["iac.provider"]` is added. The `downloads` section will be populated by GoReleaser at release time — no need to include it manually.

**Step 2: Create `plugin.contracts.json`**

Write `/Users/jon/workspace/workflow-plugin-azure/plugin.contracts.json`:

```json
{
  "version": "v1",
  "contracts": [
    {
      "kind": "module",
      "type": "iac.provider",
      "mode": "strict",
      "config": "workflow.plugins.azure.v1.AzureProviderConfig"
    }
  ]
}
```

**Step 3: Update `internal/provider.go`**

Remove:
1. The `Manifest()` method (entire function, lines ~39-46 in current provider.go)
2. The `var _ sdk.PluginProvider = (*AzureProvider)(nil)` compile guard
3. The `sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"` import (if it's only used by `Manifest()` and the compile guard)

Add a package-level variable for GoReleaser version injection (mirrors AWS `provider.ProviderVersion`):

```go
// ProviderVersion is the current plugin version. It is overridden at build time
// by GoReleaser via -X github.com/GoCodeAlone/workflow-plugin-azure/internal.ProviderVersion=...
var ProviderVersion = "1.0.0"
```

After removal, the `AzureProvider` still satisfies `interfaces.IaCProvider` (which does NOT require `Manifest()`). The SDK import may still be needed if `sdk.PluginManifest` type is used elsewhere — if not, remove the import.

**Step 4: Verify the updated provider still compiles**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" go build ./...
```

Expected: exits 0.

Note: `internal/provider_test.go` has `TestAzureProvider_Manifest` — this test will now fail to compile since `Manifest()` is removed. Remove that test in this step too.

**Step 5: Remove `TestAzureProvider_Manifest` from `internal/provider_test.go`**

Delete the test function `TestAzureProvider_Manifest` (and its `Manifest().Name` assertion) from `internal/provider_test.go`.

**Step 6: Run tests**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" go test ./... -count=1
```

Expected: all remaining tests pass.

**Step 6a: Update `.goreleaser.yaml` ldflags to inject version into `internal.ProviderVersion`**

In `.goreleaser.yaml`, change the `ldflags` line from:
```yaml
      - -s -w -X main.version={{.Version}}
```
to:
```yaml
      - -s -w -X github.com/GoCodeAlone/workflow-plugin-azure/internal.ProviderVersion={{.Version}}
```

This ensures GoReleaser injects the correct version at build time (v1.0.1, v1.1.0, etc.) into `ProviderVersion` rather than targeting the now-removed `var version` in `main.go`. Without this fix, all future releases after v1.0.0 would report version `"1.0.0"` regardless of the git tag.

**Step 7: Commit**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
git add plugin.json plugin.contracts.json internal/provider.go internal/provider_test.go .goreleaser.yaml
git commit -m "feat: update plugin metadata for v1.0.0 typed-IaC — plugin.json, plugin.contracts.json, remove Manifest(), add ProviderVersion var, fix goreleaser ldflags"
```

---

### Task 4: `internal/iacserver.go` — azureIaCServer typed gRPC layer

This is the main new file. It mirrors `internal/iacserver.go` in workflow-plugin-aws v1.0.0 exactly, substituting `azure` for `aws` throughout.

**Files:**
- Create: `internal/iacserver.go`

**Step 1: Write `internal/iacserver.go`**

The file structure matches AWS v1.0.0 (`/Users/jon/workspace/workflow-plugin-aws/internal/iacserver.go`) with these substitutions:
- Package comment: `azureIaCServer` not `awsIaCServer`
- No provider import needed — `iacserver.go` is `package internal`, same package as `AzureProvider`. All provider types and `ProviderVersion` are accessed directly (same-package, no import). The `New(version string) *AzureProvider` constructor is called as `New(ProviderVersion)` within `NewIaCServer()`.
- Struct name: `azureIaCServer`
- Constructor: `NewIaCServer()` returns `*azureIaCServer`, creates `New("1.0.0")` provider
- All error messages: `"azure iacserver: ..."` prefix

Key implementation note: The `AzureProvider` is in the `internal` package itself (not a sub-package). `iacserver.go` is also in the `internal` package (`package internal`). So the struct references `*AzureProvider` directly (same package, no import needed).

The `newAzureIaCServer(p *AzureProvider)` helper takes `*AzureProvider`, and `NewIaCServer()` calls `newAzureIaCServer(New(ProviderVersion))` — using the package-level `ProviderVersion` var added in Task 3 so GoReleaser ldflags injection flows through correctly.

The struct definition:

```go
type azureIaCServer struct {
    pb.UnimplementedIaCProviderRequiredServer
    pb.UnimplementedIaCProviderEnumeratorServer
    pb.UnimplementedIaCProviderDriftDetectorServer
    pb.UnimplementedIaCProviderCredentialRevokerServer
    pb.UnimplementedIaCProviderMigrationRepairerServer
    pb.UnimplementedIaCProviderValidatorServer
    pb.UnimplementedIaCProviderDriftConfigDetectorServer
    pb.UnimplementedResourceDriverServer

    provider *AzureProvider
}
```

Compile-time guards (place after struct definition):

```go
var (
    _ pb.IaCProviderRequiredServer      = (*azureIaCServer)(nil)
    _ pb.IaCProviderDriftDetectorServer = (*azureIaCServer)(nil)
    _ pb.ResourceDriverServer           = (*azureIaCServer)(nil)
)
```

Required methods to implement (delegate to `s.provider.*`):
- `Initialize` — `s.provider.Initialize(ctx, cfg)` where cfg is from `unmarshalJSONMap(req.GetConfigJson())`
- `Name` — `s.provider.Name()`
- `Version` — `s.provider.Version()`
- `Capabilities` — `s.provider.Capabilities()`
- `Plan` — `s.provider.Plan(ctx, desired, current)`
- `Apply` — `s.provider.Apply(ctx, plan)`
- `Destroy` — `s.provider.Destroy(ctx, refs)`
- `Status` — `s.provider.Status(ctx, refs)`
- `Import` — `s.provider.Import(ctx, req.GetProviderId(), req.GetResourceType())`
- `ResolveSizing` — `s.provider.ResolveSizing(resourceType, size, hints)`
- `BootstrapStateBackend` — `s.provider.BootstrapStateBackend(ctx, cfg)`

Optional methods:
- `DetectDrift` — `s.provider.DetectDrift(ctx, refs)` (real impl)
- `DetectDriftWithSpecs` — delegates to `s.provider.DetectDrift(ctx, refs)` (ignores specs map)

All marshalling helpers (`unmarshalJSONMap`, `marshalJSONMap`, `marshalJSONAny`, `unmarshalJSONAny`, `refToPB`, `refFromPB`, `refsToPB`, `refsFromPB`, `hintsToPB`, `hintsFromPB`, `specToPB`, `specFromPB`, `specsFromPB`, `stateToPB`, `stateFromPB`, `statesFromPB`, `outputToPB`, `statusesToPB`, `driftClassToPB`, `driftsToPB`, `planActionToPB`, `planActionFromPB`, `changesToPB`, `changesFromPB`, `planToPB`, `planFromPB`, `applyResultToPB`, `destroyResultToPB`, `bootstrapResultToPB`, `sizingToPB`, `timeToPB`, `timeFromPB`, `copyStringMap`) are copied verbatim from AWS v1.0.0 — the only change is the error message prefix `"azure iacserver:"`.

**Step 2: Verify compile-time guards pass**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" go build ./internal/...
```

Expected: exits 0. If a method is missing, the compile error will point directly at the `var _ pb.IaCProvider*Server = (*azureIaCServer)(nil)` line.

**Step 3: Commit**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
git add internal/iacserver.go
git commit -m "feat: add azureIaCServer typed IaC gRPC layer (Required + DriftDetector + marshalling)"
```

---

### Task 5: `internal/resourcedriver_server.go` — ResourceDriver CRUD dispatch

**Files:**
- Create: `internal/resourcedriver_server.go`

**Step 1: Write `internal/resourcedriver_server.go`**

Mirrors `internal/resourcedriver_server.go` from workflow-plugin-aws v1.0.0 (`/Users/jon/workspace/workflow-plugin-aws/internal/resourcedriver_server.go`) with substitutions:
- All `"aws ResourceDriver(..."` error prefixes → `"azure ResourceDriver(..."`
- `s.provider.ResourceDriver(resourceType)` is the same method name on `*AzureProvider`

The `resolveResourceDriver` helper and all 9 CRUD methods (`Create`, `Read`, `Update`, `Delete`, `Diff`, `Scale`, `HealthCheck`, `SensitiveKeys`, `Troubleshoot`) are copied verbatim — the methods are on `*azureIaCServer` which is the same struct.

The additional marshalling helpers (`diffResultToPB`, `healthResultToPB`, `outputFromPB`) at the bottom are also copied verbatim.

**Step 2: Verify compile + run existing tests**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" go build ./... && \
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" go test ./... -count=1
```

Expected: exits 0, all tests pass.

**Step 3: Commit**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
git add internal/resourcedriver_server.go
git commit -m "feat: add ResourceDriverServer CRUD dispatch to azureIaCServer"
```

---

### Task 6: Entrypoint cutover — `cmd/workflow-plugin-azure/main.go`

**Files:**
- Modify: `cmd/workflow-plugin-azure/main.go`

**Step 1: Replace main.go**

Replace the current content of `cmd/workflow-plugin-azure/main.go` with:

```go
// Command workflow-plugin-azure is a workflow engine external plugin that
// provides Microsoft Azure infrastructure provisioning via the typed IaC gRPC
// contract. It runs as a subprocess and communicates with the host (wfctl) via
// the go-plugin protocol.
//
// As of the strict-contracts force-cutover (workflow v0.51.0+), the plugin is
// served via sdk.ServeIaCPlugin which auto-registers every typed
// pb.IaCProvider*Server interface the underlying *AzureProvider satisfies.
// The legacy sdk.Serve / PluginService InvokeService string-dispatch surface
// has been removed entirely — there is no fallback path.
package main

import (
	"github.com/GoCodeAlone/workflow-plugin-azure/internal"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

func main() {
	sdk.ServeIaCPlugin(internal.NewIaCServer(), sdk.IaCServeOptions{})
}
```

Note: `var version = "dev"` is removed. `NewIaCServer()` calls `New(ProviderVersion)` using the package-level `ProviderVersion` var from `internal/provider.go`. GoReleaser injects the correct version at build time via `-X github.com/GoCodeAlone/workflow-plugin-azure/internal.ProviderVersion={{.Version}}`.

**Step 2: Build the plugin binary**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" go build -o /tmp/workflow-plugin-azure ./cmd/workflow-plugin-azure
```

Expected: exits 0, binary produced at `/tmp/workflow-plugin-azure`.

**Step 3: Verify the binary starts and responds to Name RPC**

The binary must start without errors. Since `sdk.ServeIaCPlugin` blocks waiting for the host, we just verify it builds and passes basic checks via the conformance test in Task 7.

**Step 4: Commit**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
git add cmd/workflow-plugin-azure/main.go
git commit -m "feat: cutover entrypoint from sdk.Serve to sdk.ServeIaCPlugin (typed-IaC)"
```

**Rollback:** `git revert HEAD`. Old engine tags (pre-v0.50.0) are permanently incompatible after this commit.

---

### Task 7: Tests — `internal/iacserver_test.go` + `internal/host_conformance_test.go`

**Files:**
- Create: `internal/iacserver_test.go`
- Create: `internal/host_conformance_test.go`

**Step 1: Write `internal/iacserver_test.go`**

Mirrors `internal/iacserver_test.go` from workflow-plugin-aws v1.0.0 (`/Users/jon/workspace/workflow-plugin-aws/internal/iacserver_test.go`) with substitutions:
- Package: `package internal` (same as Azure — iacserver is in the `internal` package directly)
- Provider name assertion: `"azure"` not `"aws"`
- `NewIaCServer()` constructor (same name)
- Compile-time guards assertions: `(*azureIaCServer)(nil)` not `(*awsIaCServer)(nil)`
- `TestIaCServer_Initialize_EmptyConfig`: empty `{}` config will fail with `"azure: subscription_id is required"` not with AWS credential error — the expected behavior comment should note this

Tests to include:
1. `TestNewIaCServer_NotNil`
2. `TestIaCServer_Name` — asserts `resp.GetName() == "azure"`
3. `TestIaCServer_Version` — asserts version is non-empty
4. `TestIaCServer_Capabilities` — asserts `infra.container_service` is present
5. `TestIaCServer_Initialize_EmptyConfig` — empty `{}` config should return an error (azure subscription_id required)
6. `TestIaCServer_CompileTimeGuards` — inline `var _ pb.*Server = (*azureIaCServer)(nil)` assertions
7. `TestIaCServer_DetectDrift_Uninitialized` — **Azure-specific behavior**: `AzureProvider.DetectDrift` does NOT return an error for an uninitialized provider; it swallows driver lookup failures and returns a non-drifted result with `nil` error. This differs from the AWS pattern. The test MUST assert `err == nil` and `len(resp.GetDrifts()) == 1` and `!resp.GetDrifts()[0].GetDrifted()`. Do NOT copy the AWS test assertion that expects `err != nil`.
8. `TestIaCServer_DetectDriftWithSpecs_DelegatesToDetectDrift` — same Azure-specific behavior: assert `err == nil` and `len(resp.GetDrifts()) == 1`.

**Step 2: Write `internal/host_conformance_test.go`**

Mirrors `internal/host_conformance_test.go` from workflow-plugin-aws v1.0.0 (`/Users/jon/workspace/workflow-plugin-aws/internal/host_conformance_test.go`) with substitutions:
- Build command: `./cmd/workflow-plugin-azure`
- Plugin name: `"workflow-plugin-azure"` (read from `plugin.json`)
- Provider name assertion: `name.GetName() != "azure"`
- Helper functions: same names (`hostConformanceRepoRoot`, `hostConformancePluginName`, `hostConformanceCopyFile`, `registryHasService`, `capabilitiesHasResource`)
- Test function name: `TestWorkflowHostConformance_LoadsTypedIaCPlugin` (NOT `LoadsLegacyIaCModulePlugin`)

The `plugin.json` name field is `"workflow-plugin-azure"` (updated in Task 3).

**Step 3: Run unit tests (non-conformance)**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" go test ./internal -run 'TestNewIaCServer|TestIaCServer|TestAzureProvider' -v -count=1
```

Expected: all tests pass including new `TestIaCServer_*` tests.

**Step 4: Run host conformance test**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
WORKFLOW_IAC_HOST_CONFORMANCE=1 \
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" \
go test ./internal -run TestWorkflowHostConformance_LoadsTypedIaCPlugin -v -count=1
```

Expected output:
```
=== RUN   TestWorkflowHostConformance_LoadsTypedIaCPlugin
--- PASS: TestWorkflowHostConformance_LoadsTypedIaCPlugin (Xs)
PASS
```

If the test fails with a `"contract registry missing required service"` error, verify:
1. `sdk.ServeIaCPlugin` is used in `main.go` (not `sdk.Serve`)
2. The `azureIaCServer` compile-time guards pass
3. `NewIaCServer()` returns a non-nil `*azureIaCServer`

**Step 5: Run full test suite**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" go test ./... -v -race -count=1
```

Expected: all tests pass. Note: `integration_test.go` uses wftest mocks; if wftest API changed under v0.51.7, fix the test to use the new API (only if compilation fails — if it compiles and runs, no change needed).

**Step 6: Commit**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
git add internal/iacserver_test.go internal/host_conformance_test.go
git commit -m "test: add iacserver unit tests and host conformance test (typed-IaC)"
```

---

### Task 8: CI — conformance script + workflow

**Files:**
- Create: `scripts/workflow-iac-host-conformance.sh`
- Create: `.github/workflows/iac-host-conformance.yml`
- Modify: `.github/workflows/ci.yml` (verify it runs `go test ./...` — no change expected)

**Step 1: Create `scripts/workflow-iac-host-conformance.sh`**

Write `/Users/jon/workspace/workflow-plugin-azure/scripts/workflow-iac-host-conformance.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

engine_version="${1:?usage: workflow-iac-host-conformance.sh <workflow-engine-version> [label]}"
label="${2:-${engine_version}}"

case "${engine_version}" in
  v*) ;;
  *) engine_version="v${engine_version}" ;;
esac

repo_root="$(git rev-parse --show-toplevel)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

work_dir="${tmp_dir}/repo"
mkdir -p "${work_dir}"

rsync -a \
  --exclude '.git' \
  --exclude '.worktrees' \
  --exclude '_worktrees' \
  --exclude 'data' \
  "${repo_root}/" "${work_dir}/"

cd "${work_dir}"

echo "==> workflow IaC host conformance (${label}): github.com/GoCodeAlone/workflow@${engine_version}"
go mod edit -require "github.com/GoCodeAlone/workflow@${engine_version}"
GOWORK=off go mod tidy
WORKFLOW_IAC_HOST_CONFORMANCE=1 GOWORK=off go test ./internal -run TestWorkflowHostConformance_LoadsTypedIaCPlugin -count=1 -v
```

Make it executable:
```bash
chmod +x /Users/jon/workspace/workflow-plugin-azure/scripts/workflow-iac-host-conformance.sh
```

**IMPORTANT:** The `-run` flag is `TestWorkflowHostConformance_LoadsTypedIaCPlugin` — NOT `LoadsLegacyIaCModulePlugin` (the AWS script has a stale name that silently skips; Azure uses the correct typed-IaC name).

**Step 2: Create `.github/workflows/iac-host-conformance.yml`**

Write `/Users/jon/workspace/workflow-plugin-azure/.github/workflows/iac-host-conformance.yml`:

```yaml
name: IaC Host Conformance
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

env:
  GOPRIVATE: github.com/GoCodeAlone/*

jobs:
  typed-iac-engine-range:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Configure Git for private repos
        env:
          RELEASES_TOKEN: ${{ secrets.RELEASES_TOKEN }}
        run: |
          if [ -n "${RELEASES_TOKEN}" ]; then
            git config --global url."https://x-access-token:${RELEASES_TOKEN}@github.com/".insteadOf "https://github.com/"
          fi

      - name: Determine Workflow engine versions
        id: versions
        env:
          GH_TOKEN: ${{ secrets.RELEASES_TOKEN || github.token }}
          WORKFLOW_CURRENT_VERSION: ${{ vars.WORKFLOW_CURRENT_VERSION }}
        run: |
          set -euo pipefail
          min="$(jq -r '.minEngineVersion // empty' plugin.json)"
          if [ -z "${min}" ]; then
            echo "::error::plugin.json must declare minEngineVersion"
            exit 1
          fi
          case "${min}" in
            v*) min_version="${min}" ;;
            *) min_version="v${min}" ;;
          esac
          current="${WORKFLOW_CURRENT_VERSION}"
          if [ -z "${current}" ]; then
            current="$(gh release view --repo GoCodeAlone/workflow --json tagName --jq '.tagName')"
          fi
          if [ -z "${current}" ]; then
            echo "::error::could not determine current Workflow engine release"
            exit 1
          fi
          first="$(printf '%s\n%s\n' "${current}" "${min_version}" | sort -V | head -n1)"
          if [ "${first}" = "${current}" ] && [ "${current}" != "${min_version}" ]; then
            echo "::notice::current Workflow release ${current} is older than declared minimum ${min_version}; testing minimum as current"
            current="${min_version}"
          fi
          echo "min=${min_version}" >> "${GITHUB_OUTPUT}"
          echo "current=${current}" >> "${GITHUB_OUTPUT}"
          echo "Declared min engine: ${min_version}"
          echo "Current engine release: ${current}"

      - name: Conformance against declared minimum engine
        run: ./scripts/workflow-iac-host-conformance.sh "${{ steps.versions.outputs.min }}" min

      - name: Conformance against current engine release
        if: steps.versions.outputs.current != steps.versions.outputs.min
        run: ./scripts/workflow-iac-host-conformance.sh "${{ steps.versions.outputs.current }}" current

      - name: Remove private repo Git credential rewrite
        if: always()
        env:
          RELEASES_TOKEN: ${{ secrets.RELEASES_TOKEN }}
        run: |
          if [ -n "${RELEASES_TOKEN}" ]; then
            git config --global --unset-all url."https://x-access-token:${RELEASES_TOKEN}@github.com/".insteadOf || true
          fi
```

**Step 3: Verify the CI workflow is valid YAML**

```bash
python3 -c "import yaml; yaml.safe_load(open('/Users/jon/workspace/workflow-plugin-azure/.github/workflows/iac-host-conformance.yml'))" && echo "valid YAML"
```

Expected: `valid YAML`

**Step 4: Run the conformance script locally against `v0.51.0` (minimum engine)**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" \
  ./scripts/workflow-iac-host-conformance.sh v0.51.0 min
```

Expected (last lines):
```
==> workflow IaC host conformance (min): github.com/GoCodeAlone/workflow@v0.51.0
...
--- PASS: TestWorkflowHostConformance_LoadsTypedIaCPlugin (Xs)
PASS
```

**Step 5: Commit**

```bash
cd /Users/jon/workspace/workflow-plugin-azure
git add scripts/workflow-iac-host-conformance.sh .github/workflows/iac-host-conformance.yml
git commit -m "ci: add IaC host conformance script and workflow (typed-IaC)"
```

---

## Post-implementation verification

After all 8 tasks complete, run the full local validation gate:

```bash
cd /Users/jon/workspace/workflow-plugin-azure

# 1. Full test suite with race detector
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" \
  go test ./... -v -race -count=1

# 2. Host conformance (typed-IaC gate)
WORKFLOW_IAC_HOST_CONFORMANCE=1 \
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" \
  go test ./internal -run TestWorkflowHostConformance_LoadsTypedIaCPlugin -v -count=1

# 3. Verify plugin binary builds
GONOSUMDB="github.com/GoCodeAlone/*" GOPRIVATE="github.com/GoCodeAlone/*" \
  go build -o /tmp/workflow-plugin-azure ./cmd/workflow-plugin-azure && echo "binary OK"
```

Expected: all pass, binary built.

## PR creation

```bash
cd /Users/jon/workspace/workflow-plugin-azure
git push -u origin feat/typed-iac-azure-v1
gh pr create \
  --title "feat: typed-IaC strict-contracts migration — Azure plugin v1.0.0" \
  --body "$(cat <<'EOF'
## Summary
- Migrates workflow-plugin-azure from legacy `sdk.Serve` string-dispatch to `sdk.ServeIaCPlugin` typed-IaC gRPC pattern
- Bumps workflow dependency v0.19.2 → v0.51.7; releases as v1.0.0
- Adds `azureIaCServer` implementing all Required + DriftDetector + ResourceDriver typed interfaces
- Adds `plugin.contracts.json` with `AzureProviderConfig` proto descriptor
- Adds IaC host conformance CI gate (`iac-host-conformance.yml`)

## Test plan
- [ ] `go test ./... -race -count=1` passes
- [ ] `WORKFLOW_IAC_HOST_CONFORMANCE=1 go test ./internal -run TestWorkflowHostConformance_LoadsTypedIaCPlugin -v` passes
- [ ] `go build -o /tmp/workflow-plugin-azure ./cmd/workflow-plugin-azure` succeeds
- [ ] All compile-time interface guards (`var _ pb.IaCProvider*Server = (*azureIaCServer)(nil)`) pass

## Breaking changes
Old workflow engine tags (pre-v0.50.0) are permanently incompatible. Rollback: `git revert` the typed-IaC commit + retag v0.1.0.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)" \
  --base main \
  --head feat/typed-iac-azure-v1 \
  --reviewer "@copilot"
```

## Post-merge: tag v1.0.0

After PR merges to `main`:

```bash
cd /Users/jon/workspace/workflow-plugin-azure
git checkout main && git pull origin main
git tag v1.0.0
git push origin v1.0.0
```

The GoReleaser `.github/workflows/release.yml` will trigger and produce cross-platform binaries.
