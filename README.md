# workflow-plugin-azure

> ⚠️ **Experimental** — This plugin compiles and passes its unit tests but has not been validated in any active GoCodeAlone-internal production deployment. Use with caution. Please [open an issue](https://github.com/GoCodeAlone/workflow-plugin-azure/issues/new) if you adopt it so we can promote it to **verified** status.

[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/GoCodeAlone/workflow-plugin-azure.svg)](https://pkg.go.dev/github.com/GoCodeAlone/workflow-plugin-azure)

Azure provider plugin for workflow IaC — manages ACI, AKS, SQL, Redis, VNet, LB, DNS, ACR, APIM, NSG, MSI, Blob Storage, and App Service Certificates.

## What it provides

**Module types:**
- `iac.provider` — Azure infrastructure provider

**IaC state backends:**
- `azure_blob` — Azure Blob Storage state backend

## Install

```yaml
# In your wfctl.yaml
version: 1
plugins:
  - name: workflow-plugin-azure
    version: v2.0.0
    source: github.com/GoCodeAlone/workflow-plugin-azure
```

Then:
```sh
wfctl plugin install
```

## Minimal example

See `examples/minimal/config.yaml`.

Required env vars: `AZURE_SUBSCRIPTION_ID`, `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, `AZURE_CLIENT_SECRET`.

## Documentation

- [Plugin authoring guide (upstream)](https://github.com/GoCodeAlone/workflow/blob/main/docs/PLUGIN_AUTHORING.md)
- [Workflow engine docs](https://github.com/GoCodeAlone/workflow)

## License

MIT. See [LICENSE](LICENSE).
