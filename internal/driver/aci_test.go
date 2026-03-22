package driver

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerinstance/armcontainerinstance/v2"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockACIClient struct {
	createFn func(ctx context.Context, rg, name string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error)
	getFn    func(ctx context.Context, rg, name string) (armcontainerinstance.ContainerGroup, error)
	deleteFn func(ctx context.Context, rg, name string) error
}

func (m *mockACIClient) CreateOrUpdate(ctx context.Context, rg, name string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
	return m.createFn(ctx, rg, name, cg)
}

func (m *mockACIClient) Get(ctx context.Context, rg, name string) (armcontainerinstance.ContainerGroup, error) {
	return m.getFn(ctx, rg, name)
}

func (m *mockACIClient) Delete(ctx context.Context, rg, name string) error {
	return m.deleteFn(ctx, rg, name)
}

func TestACIDriver_Create(t *testing.T) {
	provisioningState := "Succeeded"
	ipAddr := "10.0.0.1"
	image := "mcr.microsoft.com/hello-world"

	client := &mockACIClient{
		createFn: func(_ context.Context, _, name string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
			return armcontainerinstance.ContainerGroup{
				ID: str("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/" + name),
				Properties: &armcontainerinstance.ContainerGroupPropertiesProperties{
					ProvisioningState: &provisioningState,
					Containers: []*armcontainerinstance.Container{
						{
							Name:       &name,
							Properties: &armcontainerinstance.ContainerProperties{Image: &image},
						},
					},
					IPAddress: &armcontainerinstance.IPAddress{IP: &ipAddr},
				},
			}, nil
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	out, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-aci",
		Type:   "infra.container_service",
		Config: map[string]any{"image": image},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
	if out.Outputs["ip"] != ipAddr {
		t.Errorf("ip = %v, want %s", out.Outputs["ip"], ipAddr)
	}
}

func TestACIDriver_Read(t *testing.T) {
	provisioningState := "Running"
	client := &mockACIClient{
		getFn: func(_ context.Context, _, name string) (armcontainerinstance.ContainerGroup, error) {
			return armcontainerinstance.ContainerGroup{
				ID:         str("/subscriptions/sub/rg/" + name),
				Properties: &armcontainerinstance.ContainerGroupPropertiesProperties{ProvisioningState: &provisioningState},
			}, nil
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	out, err := drv.Read(context.Background(), interfaces.ResourceRef{Name: "test-aci", Type: "infra.container_service"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Status != "Running" {
		t.Errorf("status = %q, want Running", out.Status)
	}
}

func TestACIDriver_Delete(t *testing.T) {
	deleted := false
	client := &mockACIClient{
		deleteFn: func(_ context.Context, _, name string) error {
			deleted = true
			return nil
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-aci"})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !deleted {
		t.Error("expected Delete to be called")
	}
}

func TestACIDriver_Delete_Error(t *testing.T) {
	client := &mockACIClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			return errors.New("not found")
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-aci"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestACIDriver_HealthCheck_Healthy(t *testing.T) {
	running := "Running"
	client := &mockACIClient{
		getFn: func(_ context.Context, _, _ string) (armcontainerinstance.ContainerGroup, error) {
			return armcontainerinstance.ContainerGroup{
				ID:         str("/subscriptions/sub/rg/test"),
				Properties: &armcontainerinstance.ContainerGroupPropertiesProperties{ProvisioningState: &running},
			}, nil
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-aci"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if !h.Healthy {
		t.Errorf("expected healthy, got unhealthy: %s", h.Message)
	}
}

func TestACIDriver_Diff_NilCurrent(t *testing.T) {
	drv := NewACIDriver("rg", "eastus", nil)
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{Name: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when current is nil")
	}
}

func TestACIDriver_Create_Error(t *testing.T) {
	client := &mockACIClient{
		createFn: func(_ context.Context, _, _ string, _ armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
			return armcontainerinstance.ContainerGroup{}, errors.New("quota exceeded")
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	_, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-aci",
		Config: map[string]any{"image": "mcr.microsoft.com/hello-world"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestACIDriver_Update(t *testing.T) {
	provisioningState := "Succeeded"
	called := false
	client := &mockACIClient{
		createFn: func(_ context.Context, _, name string, _ armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
			called = true
			return armcontainerinstance.ContainerGroup{
				ID: str("/sub/rg/aci/" + name),
				Properties: &armcontainerinstance.ContainerGroupPropertiesProperties{
					ProvisioningState: &provisioningState,
				},
			}, nil
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	out, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-aci"}, interfaces.ResourceSpec{
		Name:   "test-aci",
		Config: map[string]any{"image": "nginx:latest"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !called {
		t.Error("expected CreateOrUpdate to be called")
	}
	_ = out
}

func TestACIDriver_Update_Error(t *testing.T) {
	client := &mockACIClient{
		createFn: func(_ context.Context, _, _ string, _ armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
			return armcontainerinstance.ContainerGroup{}, errors.New("update failed")
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	_, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-aci"}, interfaces.ResourceSpec{
		Name:   "test-aci",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestACIDriver_Diff_HasChanges(t *testing.T) {
	drv := NewACIDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"image": "nginx:1.23"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"image": "nginx:1.25"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when image changes")
	}
}

func TestACIDriver_Diff_NoChanges(t *testing.T) {
	drv := NewACIDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"image": "nginx:1.25"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"image": "nginx:1.25"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=false when image matches")
	}
}

func TestACIDriver_HealthCheck_Unhealthy(t *testing.T) {
	client := &mockACIClient{
		getFn: func(_ context.Context, _, _ string) (armcontainerinstance.ContainerGroup, error) {
			return armcontainerinstance.ContainerGroup{}, errors.New("container group not found")
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-aci"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if h.Healthy {
		t.Error("expected unhealthy when get fails")
	}
}

func TestACIDriver_Scale_NotSupported(t *testing.T) {
	drv := NewACIDriver("rg", "eastus", nil)
	_, err := drv.Scale(context.Background(), interfaces.ResourceRef{}, 3)
	if err == nil {
		t.Fatal("expected error for Scale")
	}
}
