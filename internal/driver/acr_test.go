package driver

import (
	"context"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerregistry/armcontainerregistry"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockACRClient struct {
	createFn func(ctx context.Context, rg, name string, registry armcontainerregistry.Registry) (armcontainerregistry.Registry, error)
	getFn    func(ctx context.Context, rg, name string) (armcontainerregistry.Registry, error)
	deleteFn func(ctx context.Context, rg, name string) error
}

func (m *mockACRClient) Create(ctx context.Context, rg, name string, registry armcontainerregistry.Registry) (armcontainerregistry.Registry, error) {
	return m.createFn(ctx, rg, name, registry)
}

func (m *mockACRClient) Get(ctx context.Context, rg, name string) (armcontainerregistry.Registry, error) {
	return m.getFn(ctx, rg, name)
}

func (m *mockACRClient) Delete(ctx context.Context, rg, name string) error {
	return m.deleteFn(ctx, rg, name)
}

func TestACRDriver_Create(t *testing.T) {
	ps := armcontainerregistry.ProvisioningStateSucceeded
	loginServer := "myregistry.azurecr.io"
	skuName := armcontainerregistry.SKUNameBasic
	client := &mockACRClient{
		createFn: func(_ context.Context, _, name string, _ armcontainerregistry.Registry) (armcontainerregistry.Registry, error) {
			return armcontainerregistry.Registry{
				ID:  str("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerRegistry/registries/" + name),
				SKU: &armcontainerregistry.SKU{Name: &skuName},
				Properties: &armcontainerregistry.RegistryProperties{
					ProvisioningState: &ps,
					LoginServer:       &loginServer,
				},
			}, nil
		},
	}

	drv := NewACRDriver("rg", "eastus", client)
	out, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "myregistry",
		Type:   "infra.registry",
		Config: map[string]any{"sku": "Basic"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
	if out.Outputs["login_server"] != loginServer {
		t.Errorf("login_server = %v, want %s", out.Outputs["login_server"], loginServer)
	}
	if out.Outputs["sku"] != "Basic" {
		t.Errorf("sku = %v, want Basic", out.Outputs["sku"])
	}
}

func TestACRDriver_Read(t *testing.T) {
	ps := armcontainerregistry.ProvisioningStateSucceeded
	client := &mockACRClient{
		getFn: func(_ context.Context, _, name string) (armcontainerregistry.Registry, error) {
			return armcontainerregistry.Registry{
				ID: str("/subscriptions/sub/rg/" + name),
				Properties: &armcontainerregistry.RegistryProperties{
					ProvisioningState: &ps,
				},
			}, nil
		},
	}

	drv := NewACRDriver("rg", "eastus", client)
	out, err := drv.Read(context.Background(), interfaces.ResourceRef{Name: "myregistry", Type: "infra.registry"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
}

func TestACRDriver_Diff_NilCurrent(t *testing.T) {
	drv := NewACRDriver("rg", "eastus", nil)
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{Name: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when current is nil")
	}
}
