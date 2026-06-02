package driver

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockBlobClient struct {
	createdName string
	readName    string
	deletedName string
	createFn    func(ctx context.Context, containerName string) error
	getFn       func(ctx context.Context, containerName string) (map[string]string, error)
	deleteFn    func(ctx context.Context, containerName string) error
}

func (m *mockBlobClient) CreateContainer(ctx context.Context, containerName string) error {
	m.createdName = containerName
	return m.createFn(ctx, containerName)
}

func (m *mockBlobClient) GetContainerProperties(ctx context.Context, containerName string) (map[string]string, error) {
	m.readName = containerName
	return m.getFn(ctx, containerName)
}

func (m *mockBlobClient) DeleteContainer(ctx context.Context, containerName string) error {
	m.deletedName = containerName
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

	drv := NewBlobDriver("sub", "rg", "eastus", "account", client)
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
	wantID := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/account/blobServices/default/containers/mycontainer"
	if out.ProviderID != wantID {
		t.Errorf("ProviderID = %q, want %q", out.ProviderID, wantID)
	}
}

func TestBlobDriver_Read(t *testing.T) {
	client := &mockBlobClient{
		getFn: func(_ context.Context, containerName string) (map[string]string, error) {
			return map[string]string{"custom-tag": "value"}, nil
		},
	}

	drv := NewBlobDriver("sub", "rg", "eastus", "account", client)
	id := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/account/blobServices/default/containers/mycontainer"
	out, err := drv.Read(context.Background(), interfaces.ResourceRef{Name: "test-blob", ProviderID: id})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if client.readName != "mycontainer" {
		t.Errorf("readName = %q, want mycontainer", client.readName)
	}
	if out.ProviderID != id {
		t.Errorf("ProviderID = %q, want %q", out.ProviderID, id)
	}
	if out.Outputs["custom-tag"] != "value" {
		t.Errorf("custom-tag = %v, want value", out.Outputs["custom-tag"])
	}
}

func TestBlobDriver_Create_Error(t *testing.T) {
	client := &mockBlobClient{
		createFn: func(_ context.Context, _ string) error {
			return errors.New("container already exists")
		},
	}

	drv := NewBlobDriver("sub", "rg", "eastus", "account", client)
	_, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-blob",
		Config: map[string]any{"container_name": "mycontainer"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestBlobDriver_Update(t *testing.T) {
	client := &mockBlobClient{
		getFn: func(_ context.Context, containerName string) (map[string]string, error) {
			return map[string]string{"tag": "value"}, nil
		},
	}

	drv := NewBlobDriver("sub", "rg", "eastus", "account", client)
	out, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-blob", ProviderID: "mycontainer"}, interfaces.ResourceSpec{
		Name:   "test-blob",
		Config: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if out.Status != "active" {
		t.Errorf("status = %q, want active", out.Status)
	}
}

func TestBlobDriver_Update_Error(t *testing.T) {
	client := &mockBlobClient{
		getFn: func(_ context.Context, _ string) (map[string]string, error) {
			return nil, errors.New("container not found")
		},
	}

	drv := NewBlobDriver("sub", "rg", "eastus", "account", client)
	_, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-blob", ProviderID: "mycontainer"}, interfaces.ResourceSpec{
		Name:   "test-blob",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
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

	drv := NewBlobDriver("sub", "rg", "eastus", "account", client)
	id := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/account/blobServices/default/containers/mycontainer"
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-blob", ProviderID: id})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("expected DeleteContainer to be called")
	}
	if client.deletedName != "mycontainer" {
		t.Errorf("deletedName = %q, want mycontainer", client.deletedName)
	}
}

func TestBlobDriver_Delete_Error(t *testing.T) {
	client := &mockBlobClient{
		deleteFn: func(_ context.Context, _ string) error {
			return errors.New("container not found")
		},
	}

	drv := NewBlobDriver("sub", "rg", "eastus", "account", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-blob", ProviderID: "mycontainer"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestBlobDriver_Diff_NilCurrent(t *testing.T) {
	drv := NewBlobDriver("sub", "rg", "eastus", "account", nil)
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{Name: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when current is nil")
	}
}

func TestBlobDriver_Diff_NoChanges(t *testing.T) {
	drv := NewBlobDriver("sub", "rg", "eastus", "account", nil)
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{Name: "x"}, &interfaces.ResourceOutput{})
	if err != nil {
		t.Fatal(err)
	}
	if diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=false for existing container")
	}
}

func TestBlobDriver_HealthCheck_Healthy(t *testing.T) {
	client := &mockBlobClient{
		getFn: func(_ context.Context, containerName string) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}

	drv := NewBlobDriver("sub", "rg", "eastus", "account", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-blob", ProviderID: "mycontainer"})
	if err != nil {
		t.Fatal(err)
	}
	if !h.Healthy {
		t.Errorf("expected healthy, got: %s", h.Message)
	}
}

func TestBlobDriver_HealthCheck_Unhealthy(t *testing.T) {
	client := &mockBlobClient{
		getFn: func(_ context.Context, _ string) (map[string]string, error) {
			return nil, errors.New("container not found")
		},
	}

	drv := NewBlobDriver("sub", "rg", "eastus", "account", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-blob", ProviderID: "mycontainer"})
	if err != nil {
		t.Fatal(err)
	}
	if h.Healthy {
		t.Error("expected unhealthy when get fails")
	}
}
