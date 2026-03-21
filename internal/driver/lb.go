package driver

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// LBClient is the narrow interface for Azure Load Balancer operations.
type LBClient interface {
	CreateOrUpdate(ctx context.Context, resourceGroup, name string, lb armnetwork.LoadBalancer) (armnetwork.LoadBalancer, error)
	Get(ctx context.Context, resourceGroup, name string) (armnetwork.LoadBalancer, error)
	Delete(ctx context.Context, resourceGroup, name string) error
}

type realLBClient struct {
	inner *armnetwork.LoadBalancersClient
}

func (c *realLBClient) CreateOrUpdate(ctx context.Context, rg, name string, lb armnetwork.LoadBalancer) (armnetwork.LoadBalancer, error) {
	poller, err := c.inner.BeginCreateOrUpdate(ctx, rg, name, lb, nil)
	if err != nil {
		return armnetwork.LoadBalancer{}, err
	}
	res, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	if err != nil {
		return armnetwork.LoadBalancer{}, err
	}
	return res.LoadBalancer, nil
}

func (c *realLBClient) Get(ctx context.Context, rg, name string) (armnetwork.LoadBalancer, error) {
	res, err := c.inner.Get(ctx, rg, name, nil)
	if err != nil {
		return armnetwork.LoadBalancer{}, err
	}
	return res.LoadBalancer, nil
}

func (c *realLBClient) Delete(ctx context.Context, rg, name string) error {
	poller, err := c.inner.BeginDelete(ctx, rg, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	return err
}

// LBDriver manages Azure Load Balancers (infra.load_balancer).
type LBDriver struct {
	resourceGroup string
	location      string
	client        LBClient
}

var _ interfaces.ResourceDriver = (*LBDriver)(nil)

func NewLBDriver(resourceGroup, location string, client LBClient) *LBDriver {
	return &LBDriver{resourceGroup: resourceGroup, location: location, client: client}
}

func (d *LBDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	scheme := configStr(spec.Config, "scheme", "Standard")

	lb := armnetwork.LoadBalancer{
		Location: str(d.location),
		SKU:      &armnetwork.LoadBalancerSKU{Name: ptrOf(armnetwork.LoadBalancerSKUNameStandard)},
		Properties: &armnetwork.LoadBalancerPropertiesFormat{
			FrontendIPConfigurations: []*armnetwork.FrontendIPConfiguration{
				{
					Name: str("frontend"),
					Properties: &armnetwork.FrontendIPConfigurationPropertiesFormat{
						PrivateIPAllocationMethod: ptrOf(armnetwork.IPAllocationMethodDynamic),
					},
				},
			},
		},
	}
	_ = scheme

	result, err := d.client.CreateOrUpdate(ctx, d.resourceGroup, spec.Name, lb)
	if err != nil {
		return nil, fmt.Errorf("lb: create %q: %w", spec.Name, err)
	}
	return lbToOutput(spec.Name, result), nil
}

func (d *LBDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	result, err := d.client.Get(ctx, d.resourceGroup, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("lb: get %q: %w", ref.Name, err)
	}
	return lbToOutput(ref.Name, result), nil
}

func (d *LBDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.Create(ctx, spec)
}

func (d *LBDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.client.Delete(ctx, d.resourceGroup, ref.Name)
}

func (d *LBDriver) Diff(_ context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	if current == nil {
		return &interfaces.DiffResult{NeedsUpdate: true}, nil
	}
	var changes []interfaces.FieldChange
	if scheme, ok := desired.Config["scheme"].(string); ok {
		if cur, ok := current.Outputs["scheme"].(string); ok && scheme != cur {
			changes = append(changes, interfaces.FieldChange{Path: "scheme", Old: cur, New: scheme, ForceNew: true})
		}
	}
	return &interfaces.DiffResult{NeedsUpdate: len(changes) > 0, NeedsReplace: len(changes) > 0, Changes: changes}, nil
}

func (d *LBDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	out, err := d.Read(ctx, ref)
	if err != nil {
		return &interfaces.HealthResult{Healthy: false, Message: err.Error()}, nil
	}
	healthy := out.Status == "Succeeded"
	return &interfaces.HealthResult{Healthy: healthy, Message: out.Status}, nil
}

func (d *LBDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, fmt.Errorf("lb: scale not supported")
}

func lbToOutput(name string, lb armnetwork.LoadBalancer) *interfaces.ResourceOutput {
	status := "unknown"
	outputs := map[string]any{}
	if lb.Properties != nil && lb.Properties.ProvisioningState != nil {
		status = string(*lb.Properties.ProvisioningState)
	}
	return &interfaces.ResourceOutput{
		Name:       name,
		Type:       "infra.load_balancer",
		ProviderID: strVal(lb.ID),
		Outputs:    outputs,
		Status:     status,
	}
}
