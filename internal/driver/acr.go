package driver

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerregistry/armcontainerregistry"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ACRClient is the narrow interface for Azure Container Registry operations.
type ACRClient interface {
	Create(ctx context.Context, resourceGroup, registryName string, registry armcontainerregistry.Registry) (armcontainerregistry.Registry, error)
	Get(ctx context.Context, resourceGroup, registryName string) (armcontainerregistry.Registry, error)
	Delete(ctx context.Context, resourceGroup, registryName string) error
}

type realACRClient struct {
	inner *armcontainerregistry.RegistriesClient
}

func (c *realACRClient) Create(ctx context.Context, rg, name string, registry armcontainerregistry.Registry) (armcontainerregistry.Registry, error) {
	poller, err := c.inner.BeginCreate(ctx, rg, name, registry, nil)
	if err != nil {
		return armcontainerregistry.Registry{}, err
	}
	res, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	if err != nil {
		return armcontainerregistry.Registry{}, err
	}
	return res.Registry, nil
}

func (c *realACRClient) Get(ctx context.Context, rg, name string) (armcontainerregistry.Registry, error) {
	res, err := c.inner.Get(ctx, rg, name, nil)
	if err != nil {
		return armcontainerregistry.Registry{}, err
	}
	return res.Registry, nil
}

func (c *realACRClient) Delete(ctx context.Context, rg, name string) error {
	poller, err := c.inner.BeginDelete(ctx, rg, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	return err
}

// ACRDriver manages Azure Container Registry (infra.registry).
type ACRDriver struct {
	resourceGroup string
	location      string
	client        ACRClient
}

var _ interfaces.ResourceDriver = (*ACRDriver)(nil)

func NewACRDriver(resourceGroup, location string, client ACRClient) *ACRDriver {
	return &ACRDriver{resourceGroup: resourceGroup, location: location, client: client}
}

func (d *ACRDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	sku := configStr(spec.Config, "sku", "Basic")

	registry := armcontainerregistry.Registry{
		Location: str(d.location),
		SKU:      &armcontainerregistry.SKU{Name: ptrOf(armcontainerregistry.SKUName(sku))},
		Properties: &armcontainerregistry.RegistryProperties{
			AdminUserEnabled: ptrOf(false),
		},
	}

	result, err := d.client.Create(ctx, d.resourceGroup, spec.Name, registry)
	if err != nil {
		return nil, fmt.Errorf("acr: create %q: %w", spec.Name, err)
	}
	return acrToOutput(spec.Name, result), nil
}

func (d *ACRDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	result, err := d.client.Get(ctx, d.resourceGroup, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("acr: get %q: %w", ref.Name, err)
	}
	return acrToOutput(ref.Name, result), nil
}

func (d *ACRDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.Create(ctx, spec)
}

func (d *ACRDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.client.Delete(ctx, d.resourceGroup, ref.Name)
}

func (d *ACRDriver) Diff(_ context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	if current == nil {
		return &interfaces.DiffResult{NeedsUpdate: true}, nil
	}
	var changes []interfaces.FieldChange
	if sku, ok := desired.Config["sku"].(string); ok {
		if cur, ok := current.Outputs["sku"].(string); ok && sku != cur {
			changes = append(changes, interfaces.FieldChange{Path: "sku", Old: cur, New: sku})
		}
	}
	return &interfaces.DiffResult{NeedsUpdate: len(changes) > 0, Changes: changes}, nil
}

func (d *ACRDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	out, err := d.Read(ctx, ref)
	if err != nil {
		return &interfaces.HealthResult{Healthy: false, Message: err.Error()}, nil
	}
	healthy := out.Status == "Succeeded"
	return &interfaces.HealthResult{Healthy: healthy, Message: out.Status}, nil
}

func (d *ACRDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, fmt.Errorf("acr: scale not supported")
}

func acrToOutput(name string, r armcontainerregistry.Registry) *interfaces.ResourceOutput {
	status := "unknown"
	outputs := map[string]any{}
	if r.Properties != nil {
		if r.Properties.ProvisioningState != nil {
			status = string(*r.Properties.ProvisioningState)
		}
		if r.Properties.LoginServer != nil {
			outputs["login_server"] = strVal(r.Properties.LoginServer)
		}
	}
	if r.SKU != nil && r.SKU.Name != nil {
		outputs["sku"] = string(*r.SKU.Name)
	}
	return &interfaces.ResourceOutput{
		Name:       name,
		Type:       "infra.registry",
		ProviderID: strVal(r.ID),
		Outputs:    outputs,
		Status:     status,
	}
}
