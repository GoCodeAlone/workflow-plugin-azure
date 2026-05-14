# Changelog

All notable changes to `workflow-plugin-azure` are documented here.

## v1.1.1

### Added

- **`IaCStateBackend.Configure` RPC handler.** The `azure_blob` backend now
  constructs its store from host-supplied config (closes the Phase-A
  config-plumbing gap). `azureIaCServer.Configure` decodes the iac.state
  module config delivered by the engine and lazily builds the
  `AzureBlobIaCStateStore` — previously the store was left `nil` and the
  state RPCs returned `FailedPrecondition`.

### Migration note

**Must be co-deployed with `workflow` core that includes PR 1** — a
post-PR-1 engine calls `IaCStateBackend.Configure` during
`IaCModule.Init()`; `v1.1.0` returns `Unimplemented` and causes a loud
startup failure (better than the prior silent `FailedPrecondition`, but a
co-deploy requirement).

## v1.1.0

### Added

- **`azure_blob` IaC state backend.** The plugin now serves the typed
  `IaCStateBackend` gRPC contract: `azureIaCServer` implements
  `pb.IaCStateBackendServer` (the 6 state RPCs plus `ListBackendNames`),
  backed by an `AzureBlobIaCStateStore` ported from workflow core.
  `plugin.json` `capabilities.iacStateBackends` advertises `azure_blob`.

### Migration note

Workflow core is dropping its in-core `azure_blob` IaC state backend. A
workflow config that uses `iac.state` with `backend: azure_blob` on a
post-deletion workflow engine version **MUST** load this plugin at
`v1.1.0` or later — the engine resolves the `azure_blob` backend by
dispatching to a loaded plugin that advertises it via
`ListBackendNames`. Older plugin versions do not serve the
`IaCStateBackend` contract and will not satisfy the engine's lookup.

## v1.0.0

- Strict-contracts force-cutover: plugin served via `sdk.ServeIaCPlugin`;
  typed `pb.IaCProvider*Server` surface only (legacy string-dispatch removed).
