# Changelog

All notable changes to `workflow-plugin-azure` are documented here.

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
