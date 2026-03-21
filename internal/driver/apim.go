package driver

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/apimanagement/armapimanagement"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// APIMClient is the narrow interface for Azure API Management operations.
type APIMClient interface {
	CreateOrUpdate(ctx context.Context, resourceGroup, serviceName string, params armapimanagement.ServiceResource) (armapimanagement.ServiceResource, error)
	Get(ctx context.Context, resourceGroup, serviceName string) (armapimanagement.ServiceResource, error)
	Delete(ctx context.Context, resourceGroup, serviceName string) error
}

type realAPIMClient struct {
	inner *armapimanagement.ServiceClient
}

func (c *realAPIMClient) CreateOrUpdate(ctx context.Context, rg, name string, params armapimanagement.ServiceResource) (armapimanagement.ServiceResource, error) {
	poller, err := c.inner.BeginCreateOrUpdate(ctx, rg, name, params, nil)
	if err != nil {
		return armapimanagement.ServiceResource{}, err
	}
	res, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	if err != nil {
		return armapimanagement.ServiceResource{}, err
	}
	return res.ServiceResource, nil
}

func (c *realAPIMClient) Get(ctx context.Context, rg, name string) (armapimanagement.ServiceResource, error) {
	res, err := c.inner.Get(ctx, rg, name, nil)
	if err != nil {
		return armapimanagement.ServiceResource{}, err
	}
	return res.ServiceResource, nil
}

func (c *realAPIMClient) Delete(ctx context.Context, rg, name string) error {
	poller, err := c.inner.BeginDelete(ctx, rg, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	return err
}

// APIMDriver manages Azure API Management services (infra.api_gateway).
type APIMDriver struct {
	resourceGroup string
	location      string
	client        APIMClient
}

var _ interfaces.ResourceDriver = (*APIMDriver)(nil)

func NewAPIMDriver(resourceGroup, location string, client APIMClient) *APIMDriver {
	return &APIMDriver{resourceGroup: resourceGroup, location: location, client: client}
}

func (d *APIMDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	publisherEmail := configStr(spec.Config, "publisher_email", "admin@example.com")
	publisherName := configStr(spec.Config, "publisher_name", "Admin")
	skuName := configStr(spec.Config, "sku", "Developer")
	capacity := int32(1)

	params := armapimanagement.ServiceResource{
		Location: str(d.location),
		SKU: &armapimanagement.ServiceSKUProperties{
			Name:     ptrOf(armapimanagement.SKUType(skuName)),
			Capacity: &capacity,
		},
		Properties: &armapimanagement.ServiceProperties{
			PublisherEmail: str(publisherEmail),
			PublisherName:  str(publisherName),
		},
	}

	result, err := d.client.CreateOrUpdate(ctx, d.resourceGroup, spec.Name, params)
	if err != nil {
		return nil, fmt.Errorf("apim: create %q: %w", spec.Name, err)
	}
	return apimToOutput(spec.Name, result), nil
}

func (d *APIMDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	result, err := d.client.Get(ctx, d.resourceGroup, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("apim: get %q: %w", ref.Name, err)
	}
	return apimToOutput(ref.Name, result), nil
}

func (d *APIMDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.Create(ctx, spec)
}

func (d *APIMDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.client.Delete(ctx, d.resourceGroup, ref.Name)
}

func (d *APIMDriver) Diff(_ context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
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

func (d *APIMDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	out, err := d.Read(ctx, ref)
	if err != nil {
		return &interfaces.HealthResult{Healthy: false, Message: err.Error()}, nil
	}
	healthy := out.Status == "Succeeded"
	return &interfaces.HealthResult{Healthy: healthy, Message: out.Status}, nil
}

func (d *APIMDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, fmt.Errorf("apim: scale not supported — update capacity in spec")
}

func apimToOutput(name string, svc armapimanagement.ServiceResource) *interfaces.ResourceOutput {
	status := "unknown"
	outputs := map[string]any{}
	if svc.Properties != nil {
		if svc.Properties.ProvisioningState != nil {
			status = strVal(svc.Properties.ProvisioningState)
		}
		if svc.Properties.GatewayURL != nil {
			outputs["gateway_url"] = strVal(svc.Properties.GatewayURL)
		}
	}
	if svc.SKU != nil && svc.SKU.Name != nil {
		outputs["sku"] = string(*svc.SKU.Name)
	}
	return &interfaces.ResourceOutput{
		Name:       name,
		Type:       "infra.api_gateway",
		ProviderID: strVal(svc.ID),
		Outputs:    outputs,
		Status:     status,
	}
}
