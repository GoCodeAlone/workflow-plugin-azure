package driver

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// VNetClient is the narrow interface for Azure Virtual Network operations.
type VNetClient interface {
	CreateOrUpdate(ctx context.Context, resourceGroup, name string, vnet armnetwork.VirtualNetwork) (armnetwork.VirtualNetwork, error)
	Get(ctx context.Context, resourceGroup, name string) (armnetwork.VirtualNetwork, error)
	Delete(ctx context.Context, resourceGroup, name string) error
}

type realVNetClient struct {
	inner *armnetwork.VirtualNetworksClient
}

func (c *realVNetClient) CreateOrUpdate(ctx context.Context, rg, name string, vnet armnetwork.VirtualNetwork) (armnetwork.VirtualNetwork, error) {
	poller, err := c.inner.BeginCreateOrUpdate(ctx, rg, name, vnet, nil)
	if err != nil {
		return armnetwork.VirtualNetwork{}, err
	}
	res, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	if err != nil {
		return armnetwork.VirtualNetwork{}, err
	}
	return res.VirtualNetwork, nil
}

func (c *realVNetClient) Get(ctx context.Context, rg, name string) (armnetwork.VirtualNetwork, error) {
	res, err := c.inner.Get(ctx, rg, name, nil)
	if err != nil {
		return armnetwork.VirtualNetwork{}, err
	}
	return res.VirtualNetwork, nil
}

func (c *realVNetClient) Delete(ctx context.Context, rg, name string) error {
	poller, err := c.inner.BeginDelete(ctx, rg, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	return err
}

// VNetDriver manages Azure Virtual Networks (infra.vpc).
type VNetDriver struct {
	resourceGroup string
	location      string
	client        VNetClient
}

var _ interfaces.ResourceDriver = (*VNetDriver)(nil)

func NewVNetDriver(resourceGroup, location string, client VNetClient) *VNetDriver {
	return &VNetDriver{resourceGroup: resourceGroup, location: location, client: client}
}

func (d *VNetDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	cidr := configStr(spec.Config, "cidr", "10.0.0.0/16")
	subnetCIDR := configStr(spec.Config, "subnet_cidr", "10.0.1.0/24")

	vnet := armnetwork.VirtualNetwork{
		Location: str(d.location),
		Properties: &armnetwork.VirtualNetworkPropertiesFormat{
			AddressSpace: &armnetwork.AddressSpace{
				AddressPrefixes: []*string{str(cidr)},
			},
			Subnets: []*armnetwork.Subnet{
				{
					Name: str("default"),
					Properties: &armnetwork.SubnetPropertiesFormat{
						AddressPrefix: str(subnetCIDR),
					},
				},
			},
		},
	}

	result, err := d.client.CreateOrUpdate(ctx, d.resourceGroup, spec.Name, vnet)
	if err != nil {
		return nil, fmt.Errorf("vnet: create %q: %w", spec.Name, err)
	}
	return vnetToOutput(spec.Name, result), nil
}

func (d *VNetDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	result, err := d.client.Get(ctx, d.resourceGroup, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("vnet: get %q: %w", ref.Name, err)
	}
	return vnetToOutput(ref.Name, result), nil
}

func (d *VNetDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.Create(ctx, spec)
}

func (d *VNetDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.client.Delete(ctx, d.resourceGroup, ref.Name)
}

func (d *VNetDriver) Diff(_ context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	if current == nil {
		return &interfaces.DiffResult{NeedsUpdate: true}, nil
	}
	var changes []interfaces.FieldChange
	if cidr, ok := desired.Config["cidr"].(string); ok {
		if cur, ok := current.Outputs["cidr"].(string); ok && cidr != cur {
			changes = append(changes, interfaces.FieldChange{Path: "cidr", Old: cur, New: cidr, ForceNew: true})
		}
	}
	return &interfaces.DiffResult{NeedsUpdate: len(changes) > 0, NeedsReplace: len(changes) > 0, Changes: changes}, nil
}

func (d *VNetDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	out, err := d.Read(ctx, ref)
	if err != nil {
		return &interfaces.HealthResult{Healthy: false, Message: err.Error()}, nil
	}
	healthy := out.Status == "Succeeded"
	return &interfaces.HealthResult{Healthy: healthy, Message: out.Status}, nil
}

func (d *VNetDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, fmt.Errorf("vnet: scale not supported")
}

func vnetToOutput(name string, vn armnetwork.VirtualNetwork) *interfaces.ResourceOutput {
	status := "unknown"
	outputs := map[string]any{}
	if vn.Properties != nil {
		if vn.Properties.ProvisioningState != nil {
			status = string(*vn.Properties.ProvisioningState)
		}
		if vn.Properties.AddressSpace != nil && len(vn.Properties.AddressSpace.AddressPrefixes) > 0 {
			outputs["cidr"] = strVal(vn.Properties.AddressSpace.AddressPrefixes[0])
		}
	}
	return &interfaces.ResourceOutput{
		Name:       name,
		Type:       "infra.vpc",
		ProviderID: strVal(vn.ID),
		Outputs:    outputs,
		Status:     status,
	}
}
