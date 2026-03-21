package driver

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerinstance/armcontainerinstance/v2"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ACIClient is the narrow interface for Azure Container Instance operations.
type ACIClient interface {
	CreateOrUpdate(ctx context.Context, resourceGroup, name string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error)
	Get(ctx context.Context, resourceGroup, name string) (armcontainerinstance.ContainerGroup, error)
	Delete(ctx context.Context, resourceGroup, name string) error
}

// realACIClient wraps the SDK ContainerGroupsClient and handles polling.
type realACIClient struct {
	inner *armcontainerinstance.ContainerGroupsClient
}

func (c *realACIClient) CreateOrUpdate(ctx context.Context, rg, name string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
	poller, err := c.inner.BeginCreateOrUpdate(ctx, rg, name, cg, nil)
	if err != nil {
		return armcontainerinstance.ContainerGroup{}, err
	}
	res, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	if err != nil {
		return armcontainerinstance.ContainerGroup{}, err
	}
	return res.ContainerGroup, nil
}

func (c *realACIClient) Get(ctx context.Context, rg, name string) (armcontainerinstance.ContainerGroup, error) {
	res, err := c.inner.Get(ctx, rg, name, nil)
	if err != nil {
		return armcontainerinstance.ContainerGroup{}, err
	}
	return res.ContainerGroup, nil
}

func (c *realACIClient) Delete(ctx context.Context, rg, name string) error {
	poller, err := c.inner.BeginDelete(ctx, rg, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	return err
}

// ACIDriver manages Azure Container Instances (infra.container_service).
type ACIDriver struct {
	resourceGroup string
	location      string
	client        ACIClient
}

var _ interfaces.ResourceDriver = (*ACIDriver)(nil)

// NewACIDriver creates an ACI driver with the given client.
func NewACIDriver(resourceGroup, location string, client ACIClient) *ACIDriver {
	return &ACIDriver{resourceGroup: resourceGroup, location: location, client: client}
}

func (d *ACIDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	image := configStr(spec.Config, "image", "mcr.microsoft.com/hello-world")
	cpu := float64(1)
	mem := float64(1.5)

	cg := armcontainerinstance.ContainerGroup{
		Location: str(d.location),
		Properties: &armcontainerinstance.ContainerGroupPropertiesProperties{
			Containers: []*armcontainerinstance.Container{
				{
					Name: str(spec.Name),
					Properties: &armcontainerinstance.ContainerProperties{
						Image: str(image),
						Resources: &armcontainerinstance.ResourceRequirements{
							Requests: &armcontainerinstance.ResourceRequests{
								CPU:        &cpu,
								MemoryInGB: &mem,
							},
						},
					},
				},
			},
			OSType:        ptrOf(armcontainerinstance.OperatingSystemTypesLinux),
			RestartPolicy: ptrOf(armcontainerinstance.ContainerGroupRestartPolicyAlways),
		},
	}

	result, err := d.client.CreateOrUpdate(ctx, d.resourceGroup, spec.Name, cg)
	if err != nil {
		return nil, fmt.Errorf("aci: create %q: %w", spec.Name, err)
	}
	return aciToOutput(spec.Name, result), nil
}

func (d *ACIDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	result, err := d.client.Get(ctx, d.resourceGroup, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("aci: get %q: %w", ref.Name, err)
	}
	return aciToOutput(ref.Name, result), nil
}

func (d *ACIDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.Create(ctx, spec)
}

func (d *ACIDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.client.Delete(ctx, d.resourceGroup, ref.Name)
}

func (d *ACIDriver) Diff(_ context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	if current == nil {
		return &interfaces.DiffResult{NeedsUpdate: true}, nil
	}
	var changes []interfaces.FieldChange
	if desiredImage, ok := desired.Config["image"].(string); ok {
		if currentImage, ok := current.Outputs["image"].(string); ok && desiredImage != currentImage {
			changes = append(changes, interfaces.FieldChange{Path: "image", Old: currentImage, New: desiredImage})
		}
	}
	return &interfaces.DiffResult{NeedsUpdate: len(changes) > 0, Changes: changes}, nil
}

func (d *ACIDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	out, err := d.Read(ctx, ref)
	if err != nil {
		return &interfaces.HealthResult{Healthy: false, Message: err.Error()}, nil
	}
	healthy := out.Status == "Running"
	return &interfaces.HealthResult{Healthy: healthy, Message: out.Status}, nil
}

func (d *ACIDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, fmt.Errorf("aci: scale not supported — redeploy with updated container count")
}

func aciToOutput(name string, cg armcontainerinstance.ContainerGroup) *interfaces.ResourceOutput {
	status := "unknown"
	if cg.Properties != nil && cg.Properties.ProvisioningState != nil {
		status = strVal(cg.Properties.ProvisioningState)
	}
	outputs := map[string]any{}
	if len(cg.Properties.Containers) > 0 && cg.Properties.Containers[0].Properties != nil {
		outputs["image"] = strVal(cg.Properties.Containers[0].Properties.Image)
	}
	if cg.Properties.IPAddress != nil {
		outputs["ip"] = strVal(cg.Properties.IPAddress.IP)
	}
	return &interfaces.ResourceOutput{
		Name:       name,
		Type:       "infra.container_service",
		ProviderID: strVal(cg.ID),
		Outputs:    outputs,
		Status:     status,
	}
}

func ptrOf[T any](v T) *T { return &v }
