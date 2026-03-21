package driver

import (
	"context"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/apimanagement/armapimanagement"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockAPIMClient struct {
	createFn func(ctx context.Context, rg, name string, params armapimanagement.ServiceResource) (armapimanagement.ServiceResource, error)
	getFn    func(ctx context.Context, rg, name string) (armapimanagement.ServiceResource, error)
	deleteFn func(ctx context.Context, rg, name string) error
}

func (m *mockAPIMClient) CreateOrUpdate(ctx context.Context, rg, name string, params armapimanagement.ServiceResource) (armapimanagement.ServiceResource, error) {
	return m.createFn(ctx, rg, name, params)
}

func (m *mockAPIMClient) Get(ctx context.Context, rg, name string) (armapimanagement.ServiceResource, error) {
	return m.getFn(ctx, rg, name)
}

func (m *mockAPIMClient) Delete(ctx context.Context, rg, name string) error {
	return m.deleteFn(ctx, rg, name)
}

func TestAPIMDriver_Create(t *testing.T) {
	provisioningState := "Succeeded"
	gatewayURL := "https://myapim.azure-api.net"
	skuName := armapimanagement.SKUTypeDeveloper
	client := &mockAPIMClient{
		createFn: func(_ context.Context, _, name string, _ armapimanagement.ServiceResource) (armapimanagement.ServiceResource, error) {
			return armapimanagement.ServiceResource{
				ID:  str("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ApiManagement/service/" + name),
				SKU: &armapimanagement.ServiceSKUProperties{Name: &skuName},
				Properties: &armapimanagement.ServiceProperties{
					ProvisioningState: &provisioningState,
					GatewayURL:        &gatewayURL,
				},
			}, nil
		},
	}

	drv := NewAPIMDriver("rg", "eastus", client)
	out, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name: "myapim",
		Type: "infra.api_gateway",
		Config: map[string]any{
			"sku":             "Developer",
			"publisher_email": "admin@example.com",
			"publisher_name":  "Admin",
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
	if out.Outputs["gateway_url"] != gatewayURL {
		t.Errorf("gateway_url = %v, want %s", out.Outputs["gateway_url"], gatewayURL)
	}
	if out.Outputs["sku"] != "Developer" {
		t.Errorf("sku = %v, want Developer", out.Outputs["sku"])
	}
}

func TestAPIMDriver_Read(t *testing.T) {
	provisioningState := "Succeeded"
	client := &mockAPIMClient{
		getFn: func(_ context.Context, _, name string) (armapimanagement.ServiceResource, error) {
			return armapimanagement.ServiceResource{
				ID: str("/subscriptions/sub/rg/" + name),
				Properties: &armapimanagement.ServiceProperties{
					ProvisioningState: &provisioningState,
				},
			}, nil
		},
	}

	drv := NewAPIMDriver("rg", "eastus", client)
	out, err := drv.Read(context.Background(), interfaces.ResourceRef{Name: "myapim", Type: "infra.api_gateway"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
}

func TestAPIMDriver_Diff_NilCurrent(t *testing.T) {
	drv := NewAPIMDriver("rg", "eastus", nil)
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{Name: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when current is nil")
	}
}
