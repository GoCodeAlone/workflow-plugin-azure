package driver

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// DNSClient is the narrow interface for Azure DNS operations.
type DNSClient interface {
	CreateOrUpdate(ctx context.Context, resourceGroup, zoneName string, zone armdns.Zone) (armdns.Zone, error)
	Get(ctx context.Context, resourceGroup, zoneName string) (armdns.Zone, error)
	Delete(ctx context.Context, resourceGroup, zoneName string) error
}

type realDNSClient struct {
	inner *armdns.ZonesClient
}

func (c *realDNSClient) CreateOrUpdate(ctx context.Context, rg, zoneName string, zone armdns.Zone) (armdns.Zone, error) {
	res, err := c.inner.CreateOrUpdate(ctx, rg, zoneName, zone, nil)
	if err != nil {
		return armdns.Zone{}, err
	}
	return res.Zone, nil
}

func (c *realDNSClient) Get(ctx context.Context, rg, zoneName string) (armdns.Zone, error) {
	res, err := c.inner.Get(ctx, rg, zoneName, nil)
	if err != nil {
		return armdns.Zone{}, err
	}
	return res.Zone, nil
}

func (c *realDNSClient) Delete(ctx context.Context, rg, zoneName string) error {
	poller, err := c.inner.BeginDelete(ctx, rg, zoneName, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	return err
}

// DNSDriver manages Azure DNS zones (infra.dns).
type DNSDriver struct {
	resourceGroup string
	location      string
	client        DNSClient
}

var _ interfaces.ResourceDriver = (*DNSDriver)(nil)

func NewDNSDriver(resourceGroup, location string, client DNSClient) *DNSDriver {
	return &DNSDriver{resourceGroup: resourceGroup, location: location, client: client}
}

func (d *DNSDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	zoneName := configStr(spec.Config, "zone_name", spec.Name)
	zoneType := configStr(spec.Config, "zone_type", "Public")

	zone := armdns.Zone{
		Location: str("global"),
		Properties: &armdns.ZoneProperties{
			ZoneType: ptrOf(armdns.ZoneType(zoneType)),
		},
	}

	result, err := d.client.CreateOrUpdate(ctx, d.resourceGroup, zoneName, zone)
	if err != nil {
		return nil, fmt.Errorf("dns: create %q: %w", zoneName, err)
	}
	return dnsToOutput(spec.Name, result), nil
}

func (d *DNSDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	zoneName := ref.Name
	result, err := d.client.Get(ctx, d.resourceGroup, zoneName)
	if err != nil {
		return nil, fmt.Errorf("dns: get %q: %w", zoneName, err)
	}
	return dnsToOutput(ref.Name, result), nil
}

func (d *DNSDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.Create(ctx, spec)
}

func (d *DNSDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.client.Delete(ctx, d.resourceGroup, ref.Name)
}

func (d *DNSDriver) Diff(_ context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	if current == nil {
		return &interfaces.DiffResult{NeedsUpdate: true}, nil
	}
	var changes []interfaces.FieldChange
	if zoneType, ok := desired.Config["zone_type"].(string); ok {
		if cur, ok := current.Outputs["zone_type"].(string); ok && zoneType != cur {
			changes = append(changes, interfaces.FieldChange{Path: "zone_type", Old: cur, New: zoneType, ForceNew: true})
		}
	}
	return &interfaces.DiffResult{NeedsUpdate: len(changes) > 0, NeedsReplace: len(changes) > 0, Changes: changes}, nil
}

func (d *DNSDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	out, err := d.Read(ctx, ref)
	if err != nil {
		return &interfaces.HealthResult{Healthy: false, Message: err.Error()}, nil
	}
	return &interfaces.HealthResult{Healthy: out.Status != "unknown", Message: out.Status}, nil
}

func (d *DNSDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, fmt.Errorf("dns: scale not supported")
}

func dnsToOutput(name string, z armdns.Zone) *interfaces.ResourceOutput {
	outputs := map[string]any{}
	if z.Properties != nil {
		if z.Properties.NumberOfRecordSets != nil {
			outputs["record_sets"] = *z.Properties.NumberOfRecordSets
		}
	}
	status := "active"
	if z.ID == nil {
		status = "unknown"
	}
	return &interfaces.ResourceOutput{
		Name:       name,
		Type:       "infra.dns",
		ProviderID: strVal(z.ID),
		Outputs:    outputs,
		Status:     status,
	}
}
