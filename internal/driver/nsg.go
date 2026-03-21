package driver

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// NSGClient is the narrow interface for Azure Network Security Group operations.
type NSGClient interface {
	CreateOrUpdate(ctx context.Context, resourceGroup, name string, nsg armnetwork.SecurityGroup) (armnetwork.SecurityGroup, error)
	Get(ctx context.Context, resourceGroup, name string) (armnetwork.SecurityGroup, error)
	Delete(ctx context.Context, resourceGroup, name string) error
}

type realNSGClient struct {
	inner *armnetwork.SecurityGroupsClient
}

func (c *realNSGClient) CreateOrUpdate(ctx context.Context, rg, name string, nsg armnetwork.SecurityGroup) (armnetwork.SecurityGroup, error) {
	poller, err := c.inner.BeginCreateOrUpdate(ctx, rg, name, nsg, nil)
	if err != nil {
		return armnetwork.SecurityGroup{}, err
	}
	res, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	if err != nil {
		return armnetwork.SecurityGroup{}, err
	}
	return res.SecurityGroup, nil
}

func (c *realNSGClient) Get(ctx context.Context, rg, name string) (armnetwork.SecurityGroup, error) {
	res, err := c.inner.Get(ctx, rg, name, nil)
	if err != nil {
		return armnetwork.SecurityGroup{}, err
	}
	return res.SecurityGroup, nil
}

func (c *realNSGClient) Delete(ctx context.Context, rg, name string) error {
	poller, err := c.inner.BeginDelete(ctx, rg, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	return err
}

// NSGDriver manages Azure Network Security Groups (infra.firewall).
type NSGDriver struct {
	resourceGroup string
	location      string
	client        NSGClient
}

var _ interfaces.ResourceDriver = (*NSGDriver)(nil)

func NewNSGDriver(resourceGroup, location string, client NSGClient) *NSGDriver {
	return &NSGDriver{resourceGroup: resourceGroup, location: location, client: client}
}

func (d *NSGDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	priority := int32(100)
	nsg := armnetwork.SecurityGroup{
		Location: str(d.location),
		Properties: &armnetwork.SecurityGroupPropertiesFormat{
			SecurityRules: []*armnetwork.SecurityRule{
				{
					Name: str("allow-https"),
					Properties: &armnetwork.SecurityRulePropertiesFormat{
						Protocol:                 ptrOf(armnetwork.SecurityRuleProtocolTCP),
						SourcePortRange:          str("*"),
						DestinationPortRange:     str("443"),
						SourceAddressPrefix:      str("*"),
						DestinationAddressPrefix: str("*"),
						Access:                   ptrOf(armnetwork.SecurityRuleAccessAllow),
						Priority:                 &priority,
						Direction:                ptrOf(armnetwork.SecurityRuleDirectionInbound),
					},
				},
			},
		},
	}

	result, err := d.client.CreateOrUpdate(ctx, d.resourceGroup, spec.Name, nsg)
	if err != nil {
		return nil, fmt.Errorf("nsg: create %q: %w", spec.Name, err)
	}
	return nsgToOutput(spec.Name, result), nil
}

func (d *NSGDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	result, err := d.client.Get(ctx, d.resourceGroup, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("nsg: get %q: %w", ref.Name, err)
	}
	return nsgToOutput(ref.Name, result), nil
}

func (d *NSGDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.Create(ctx, spec)
}

func (d *NSGDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.client.Delete(ctx, d.resourceGroup, ref.Name)
}

func (d *NSGDriver) Diff(_ context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	if current == nil {
		return &interfaces.DiffResult{NeedsUpdate: true}, nil
	}
	return &interfaces.DiffResult{NeedsUpdate: false}, nil
}

func (d *NSGDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	out, err := d.Read(ctx, ref)
	if err != nil {
		return &interfaces.HealthResult{Healthy: false, Message: err.Error()}, nil
	}
	healthy := out.Status == "Succeeded"
	return &interfaces.HealthResult{Healthy: healthy, Message: out.Status}, nil
}

func (d *NSGDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, fmt.Errorf("nsg: scale not supported")
}

func nsgToOutput(name string, nsg armnetwork.SecurityGroup) *interfaces.ResourceOutput {
	status := "unknown"
	outputs := map[string]any{}
	if nsg.Properties != nil && nsg.Properties.ProvisioningState != nil {
		status = string(*nsg.Properties.ProvisioningState)
		outputs["rule_count"] = len(nsg.Properties.SecurityRules)
	}
	return &interfaces.ResourceOutput{
		Name:       name,
		Type:       "infra.firewall",
		ProviderID: strVal(nsg.ID),
		Outputs:    outputs,
		Status:     status,
	}
}
