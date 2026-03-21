package driver

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// MSIClient is the narrow interface for Azure Managed Identity operations.
type MSIClient interface {
	CreateOrUpdate(ctx context.Context, resourceGroup, resourceName string, params armmsi.Identity) (armmsi.Identity, error)
	Get(ctx context.Context, resourceGroup, resourceName string) (armmsi.Identity, error)
	Delete(ctx context.Context, resourceGroup, resourceName string) error
}

type realMSIClient struct {
	inner *armmsi.UserAssignedIdentitiesClient
}

func (c *realMSIClient) CreateOrUpdate(ctx context.Context, rg, name string, params armmsi.Identity) (armmsi.Identity, error) {
	res, err := c.inner.CreateOrUpdate(ctx, rg, name, params, nil)
	if err != nil {
		return armmsi.Identity{}, err
	}
	return res.Identity, nil
}

func (c *realMSIClient) Get(ctx context.Context, rg, name string) (armmsi.Identity, error) {
	res, err := c.inner.Get(ctx, rg, name, nil)
	if err != nil {
		return armmsi.Identity{}, err
	}
	return res.Identity, nil
}

func (c *realMSIClient) Delete(ctx context.Context, rg, name string) error {
	_, err := c.inner.Delete(ctx, rg, name, nil)
	return err
}

// MSIDriver manages Azure Managed Identities (infra.iam_role).
type MSIDriver struct {
	resourceGroup string
	location      string
	client        MSIClient
}

var _ interfaces.ResourceDriver = (*MSIDriver)(nil)

func NewMSIDriver(resourceGroup, location string, client MSIClient) *MSIDriver {
	return &MSIDriver{resourceGroup: resourceGroup, location: location, client: client}
}

func (d *MSIDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	identity := armmsi.Identity{
		Location: str(d.location),
		Tags: map[string]*string{
			"managed-by": str("workflow"),
		},
	}

	result, err := d.client.CreateOrUpdate(ctx, d.resourceGroup, spec.Name, identity)
	if err != nil {
		return nil, fmt.Errorf("msi: create %q: %w", spec.Name, err)
	}
	return msiToOutput(spec.Name, result), nil
}

func (d *MSIDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	result, err := d.client.Get(ctx, d.resourceGroup, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("msi: get %q: %w", ref.Name, err)
	}
	return msiToOutput(ref.Name, result), nil
}

func (d *MSIDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.Create(ctx, spec)
}

func (d *MSIDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.client.Delete(ctx, d.resourceGroup, ref.Name)
}

func (d *MSIDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	if current == nil {
		return &interfaces.DiffResult{NeedsUpdate: true}, nil
	}
	return &interfaces.DiffResult{NeedsUpdate: false}, nil
}

func (d *MSIDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	out, err := d.Read(ctx, ref)
	if err != nil {
		return &interfaces.HealthResult{Healthy: false, Message: err.Error()}, nil
	}
	return &interfaces.HealthResult{Healthy: out.ProviderID != "", Message: "active"}, nil
}

func (d *MSIDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, fmt.Errorf("msi: scale not supported")
}

func msiToOutput(name string, identity armmsi.Identity) *interfaces.ResourceOutput {
	outputs := map[string]any{}
	if identity.Properties != nil {
		if identity.Properties.ClientID != nil {
			outputs["client_id"] = strVal(identity.Properties.ClientID)
		}
		if identity.Properties.PrincipalID != nil {
			outputs["principal_id"] = strVal(identity.Properties.PrincipalID)
		}
		if identity.Properties.TenantID != nil {
			outputs["tenant_id"] = strVal(identity.Properties.TenantID)
		}
	}
	return &interfaces.ResourceOutput{
		Name:       name,
		Type:       "infra.iam_role",
		ProviderID: strVal(identity.ID),
		Outputs:    outputs,
		Status:     "active",
	}
}
