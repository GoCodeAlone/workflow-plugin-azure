package azure_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/wftest"
)

// TestIntegration_AzureDeployACI validates a pipeline that deploys an Azure
// Container Instance. The step is mocked so no real Azure credentials are needed.
func TestIntegration_AzureDeployACI(t *testing.T) {
	h := wftest.New(t,
		wftest.WithYAML(`
pipelines:
  deploy-aci:
    steps:
      - name: deploy
        type: step.azure_deploy
        config:
          resource_type: infra.container_service
          resource_group: my-rg
          location: eastus
`),
		wftest.MockStep("step.azure_deploy", wftest.Returns(map[string]any{
			"resource_id": "/subscriptions/abc/resourceGroups/my-rg/providers/Microsoft.ContainerInstance/containerGroups/my-container",
			"status":      "Running",
			"fqdn":        "my-container.eastus.azurecontainer.io",
		})),
	)

	result := h.ExecutePipeline("deploy-aci", nil)
	if result.Error != nil {
		t.Fatalf("pipeline failed: %v", result.Error)
	}
	if !result.StepExecuted("deploy") {
		t.Error("deploy step should have executed")
	}
	out := result.StepOutput("deploy")
	if out["status"] != "Running" {
		t.Errorf("status = %v, want Running", out["status"])
	}
	if out["fqdn"] != "my-container.eastus.azurecontainer.io" {
		t.Errorf("fqdn = %v, want my-container.eastus.azurecontainer.io", out["fqdn"])
	}
}

// TestIntegration_AzureProvisionAKS validates a pipeline that provisions an
// AKS cluster. The step is mocked to avoid live Azure API calls.
func TestIntegration_AzureProvisionAKS(t *testing.T) {
	h := wftest.New(t,
		wftest.WithYAML(`
pipelines:
  provision-aks:
    steps:
      - name: provision
        type: step.azure_provision
        config:
          resource_type: infra.k8s_cluster
          resource_group: prod-rg
          location: westus2
          node_count: 3
          vm_size: Standard_D4s_v5
`),
		wftest.MockStep("step.azure_provision", wftest.Returns(map[string]any{
			"resource_id":   "/subscriptions/abc/resourceGroups/prod-rg/providers/Microsoft.ContainerService/managedClusters/my-aks",
			"status":        "Succeeded",
			"fqdn":          "my-aks-abc123.hcp.westus2.azmk8s.io",
			"node_count":    3,
			"kube_config":   "apiVersion: v1\nkind: Config\n...",
		})),
	)

	result := h.ExecutePipeline("provision-aks", nil)
	if result.Error != nil {
		t.Fatalf("pipeline failed: %v", result.Error)
	}
	if !result.StepExecuted("provision") {
		t.Error("provision step should have executed")
	}
	out := result.StepOutput("provision")
	if out["status"] != "Succeeded" {
		t.Errorf("status = %v, want Succeeded", out["status"])
	}
	if out["node_count"] != 3 {
		t.Errorf("node_count = %v, want 3", out["node_count"])
	}
}

// TestIntegration_AzureMultiStepPipeline validates a pipeline with storage
// provisioning followed by a compute deployment, verifying both steps execute
// in sequence and outputs are accessible.
func TestIntegration_AzureMultiStepPipeline(t *testing.T) {
	storageRec := wftest.RecordStep("step.azure_storage")
	storageRec.WithOutput(map[string]any{
		"resource_id":      "/subscriptions/abc/resourceGroups/shared-rg/providers/Microsoft.Storage/storageAccounts/mystore",
		"status":           "Succeeded",
		"primary_endpoint": "https://mystore.blob.core.windows.net/",
		"container_name":   "artifacts",
	})

	h := wftest.New(t,
		wftest.WithYAML(`
pipelines:
  deploy-storage-and-compute:
    steps:
      - name: provision-storage
        type: step.azure_storage
        config:
          resource_type: infra.storage
          resource_group: shared-rg
          location: eastus
          sku: Standard_LRS
          container: artifacts

      - name: deploy-compute
        type: step.azure_compute
        config:
          resource_type: infra.container_service
          resource_group: shared-rg
          location: eastus
          image: myapp:latest
`),
		storageRec,
		wftest.MockStep("step.azure_compute", wftest.Returns(map[string]any{
			"resource_id": "/subscriptions/abc/resourceGroups/shared-rg/providers/Microsoft.ContainerInstance/containerGroups/myapp",
			"status":      "Running",
			"ip_address":  "10.0.0.5",
		})),
	)

	result := h.ExecutePipeline("deploy-storage-and-compute", nil)
	if result.Error != nil {
		t.Fatalf("pipeline failed: %v", result.Error)
	}
	if result.StepCount() != 2 {
		t.Errorf("step count = %d, want 2", result.StepCount())
	}
	if !result.StepExecuted("provision-storage") {
		t.Error("provision-storage step should have executed")
	}
	if !result.StepExecuted("deploy-compute") {
		t.Error("deploy-compute step should have executed")
	}

	// Verify the storage recorder captured exactly one call.
	if storageRec.CallCount() != 1 {
		t.Errorf("storage step call count = %d, want 1", storageRec.CallCount())
	}

	// Verify storage step output.
	storageOut := result.StepOutput("provision-storage")
	if storageOut["container_name"] != "artifacts" {
		t.Errorf("container_name = %v, want artifacts", storageOut["container_name"])
	}

	// Verify compute step output.
	computeOut := result.StepOutput("deploy-compute")
	if computeOut["status"] != "Running" {
		t.Errorf("compute status = %v, want Running", computeOut["status"])
	}
}
