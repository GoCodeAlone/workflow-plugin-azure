package driver

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// BlobClientInterface is the narrow interface for Azure Blob Storage container operations.
type BlobClientInterface interface {
	CreateContainer(ctx context.Context, containerName string) error
	GetContainerProperties(ctx context.Context, containerName string) (map[string]string, error)
	DeleteContainer(ctx context.Context, containerName string) error
}

type realBlobClient struct {
	inner *azblob.Client
}

func (c *realBlobClient) CreateContainer(ctx context.Context, containerName string) error {
	_, err := c.inner.CreateContainer(ctx, containerName, nil)
	return err
}

func (c *realBlobClient) GetContainerProperties(ctx context.Context, containerName string) (map[string]string, error) {
	res, err := c.inner.ServiceClient().NewContainerClient(containerName).GetProperties(ctx, nil)
	if err != nil {
		return nil, err
	}
	props := map[string]string{}
	for k, v := range res.Metadata {
		if v != nil {
			props[k] = *v
		}
	}
	return props, nil
}

func (c *realBlobClient) DeleteContainer(ctx context.Context, containerName string) error {
	_, err := c.inner.DeleteContainer(ctx, containerName, nil)
	return err
}

// BlobDriver manages Azure Blob Storage containers (infra.storage).
type BlobDriver struct {
	resourceGroup string
	location      string
	client        BlobClientInterface
}

var _ interfaces.ResourceDriver = (*BlobDriver)(nil)

func NewBlobDriver(resourceGroup, location string, client BlobClientInterface) *BlobDriver {
	return &BlobDriver{resourceGroup: resourceGroup, location: location, client: client}
}

func (d *BlobDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	containerName := configStr(spec.Config, "container_name", spec.Name)

	if err := d.client.CreateContainer(ctx, containerName); err != nil {
		return nil, fmt.Errorf("blob: create container %q: %w", containerName, err)
	}
	return &interfaces.ResourceOutput{
		Name:       spec.Name,
		Type:       "infra.storage",
		ProviderID: containerName,
		Outputs:    map[string]any{"container_name": containerName},
		Status:     "active",
	}, nil
}

func (d *BlobDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	containerName := ref.Name
	if ref.ProviderID != "" {
		containerName = ref.ProviderID
	}

	props, err := d.client.GetContainerProperties(ctx, containerName)
	if err != nil {
		return nil, fmt.Errorf("blob: get container %q: %w", containerName, err)
	}
	outputs := map[string]any{"container_name": containerName}
	for k, v := range props {
		outputs[k] = v
	}
	return &interfaces.ResourceOutput{
		Name:       ref.Name,
		Type:       "infra.storage",
		ProviderID: containerName,
		Outputs:    outputs,
		Status:     "active",
	}, nil
}

func (d *BlobDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.Read(ctx, ref)
}

func (d *BlobDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	containerName := ref.Name
	if ref.ProviderID != "" {
		containerName = ref.ProviderID
	}
	return d.client.DeleteContainer(ctx, containerName)
}

func (d *BlobDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	if current == nil {
		return &interfaces.DiffResult{NeedsUpdate: true}, nil
	}
	return &interfaces.DiffResult{NeedsUpdate: false}, nil
}

func (d *BlobDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	_, err := d.Read(ctx, ref)
	if err != nil {
		return &interfaces.HealthResult{Healthy: false, Message: err.Error()}, nil
	}
	return &interfaces.HealthResult{Healthy: true, Message: "active"}, nil
}

func (d *BlobDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, fmt.Errorf("blob: scale not supported")
}
