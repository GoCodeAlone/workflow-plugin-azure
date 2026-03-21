package driver

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockBlobClient struct {
	createFn func(ctx context.Context, containerName string) error
	getFn    func(ctx context.Context, containerName string) (map[string]string, error)
	deleteFn func(ctx context.Context, containerName string) error
}

func (m *mockBlobClient) CreateContainer(ctx context.Context, containerName string) error {
	return m.createFn(ctx, containerName)
}

func (m *mockBlobClient) GetContainerProperties(ctx context.Context, containerName string) (map[string]string, error) {
	return m.getFn(ctx, containerName)
}

func (m *mockBlobClient) DeleteContainer(ctx context.Context, containerName string) error {
	return m.deleteFn(ctx, containerName)
}

func TestBlobDriver_Create(t *testing.T) {
	created := false
	client := &mockBlobClient{
		createFn: func(_ context.Context, containerName string) error {
			created = true
			if containerName == "" {
				return errors.New("empty container name")
			}
			return nil
		},
	}

	drv := NewBlobDriver("rg", "eastus", client)
	out, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-blob",
		Type:   "infra.storage",
		Config: map[string]any{"container_name": "mycontainer"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !created {
		t.Error("expected CreateContainer to be called")
	}
	if out.Outputs["container_name"] != "mycontainer" {
		t.Errorf("container_name = %v, want mycontainer", out.Outputs["container_name"])
	}
}

func TestBlobDriver_Read(t *testing.T) {
	client := &mockBlobClient{
		getFn: func(_ context.Context, containerName string) (map[string]string, error) {
			return map[string]string{"custom-tag": "value"}, nil
		},
	}

	drv := NewBlobDriver("rg", "eastus", client)
	out, err := drv.Read(context.Background(), interfaces.ResourceRef{Name: "test-blob", ProviderID: "mycontainer"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Outputs["custom-tag"] != "value" {
		t.Errorf("custom-tag = %v, want value", out.Outputs["custom-tag"])
	}
}

func TestBlobDriver_Delete(t *testing.T) {
	deleted := false
	client := &mockBlobClient{
		deleteFn: func(_ context.Context, containerName string) error {
			deleted = true
			return nil
		},
	}

	drv := NewBlobDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-blob", ProviderID: "mycontainer"})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("expected DeleteContainer to be called")
	}
}
