package driver

import (
	"context"
	"errors"
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

func TestAPIMDriver_Create_Error(t *testing.T) {
	client := &mockAPIMClient{
		createFn: func(_ context.Context, _, _ string, _ armapimanagement.ServiceResource) (armapimanagement.ServiceResource, error) {
			return armapimanagement.ServiceResource{}, errors.New("quota exceeded")
		},
	}

	drv := NewAPIMDriver("rg", "eastus", client)
	_, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "myapim",
		Config: map[string]any{"sku": "Developer", "publisher_email": "a@b.com", "publisher_name": "Admin"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAPIMDriver_Update(t *testing.T) {
	provisioningState := "Succeeded"
	skuName := armapimanagement.SKUTypeStandard
	called := false

	client := &mockAPIMClient{
		createFn: func(_ context.Context, _, name string, _ armapimanagement.ServiceResource) (armapimanagement.ServiceResource, error) {
			called = true
			return armapimanagement.ServiceResource{
				ID:  str("/sub/rg/apim/" + name),
				SKU: &armapimanagement.ServiceSKUProperties{Name: &skuName},
				Properties: &armapimanagement.ServiceProperties{
					ProvisioningState: &provisioningState,
				},
			}, nil
		},
	}

	drv := NewAPIMDriver("rg", "eastus", client)
	out, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "myapim"}, interfaces.ResourceSpec{
		Name:   "myapim",
		Config: map[string]any{"sku": "Standard"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !called {
		t.Error("expected CreateOrUpdate to be called")
	}
	_ = out
}

func TestAPIMDriver_Update_Error(t *testing.T) {
	client := &mockAPIMClient{
		createFn: func(_ context.Context, _, _ string, _ armapimanagement.ServiceResource) (armapimanagement.ServiceResource, error) {
			return armapimanagement.ServiceResource{}, errors.New("update failed")
		},
	}

	drv := NewAPIMDriver("rg", "eastus", client)
	_, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "myapim"}, interfaces.ResourceSpec{
		Name:   "myapim",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAPIMDriver_Delete(t *testing.T) {
	deleted := false
	client := &mockAPIMClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			deleted = true
			return nil
		},
	}

	drv := NewAPIMDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "myapim"})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("expected Delete to be called")
	}
}

func TestAPIMDriver_Delete_Error(t *testing.T) {
	client := &mockAPIMClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			return errors.New("not found")
		},
	}

	drv := NewAPIMDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "myapim"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAPIMDriver_Diff_HasChanges(t *testing.T) {
	drv := NewAPIMDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"sku": "Developer"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"sku": "Standard"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when sku changes")
	}
}

func TestAPIMDriver_Diff_NoChanges(t *testing.T) {
	drv := NewAPIMDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"sku": "Developer"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"sku": "Developer"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=false when sku matches")
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

func TestAPIMDriver_HealthCheck_Healthy(t *testing.T) {
	provisioningState := "Succeeded"
	client := &mockAPIMClient{
		getFn: func(_ context.Context, _, name string) (armapimanagement.ServiceResource, error) {
			return armapimanagement.ServiceResource{
				ID: str("/sub/rg/apim/" + name),
				Properties: &armapimanagement.ServiceProperties{
					ProvisioningState: &provisioningState,
				},
			}, nil
		},
	}

	drv := NewAPIMDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "myapim"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if !h.Healthy {
		t.Errorf("expected healthy, got: %s", h.Message)
	}
}

func TestAPIMDriver_HealthCheck_Unhealthy(t *testing.T) {
	client := &mockAPIMClient{
		getFn: func(_ context.Context, _, _ string) (armapimanagement.ServiceResource, error) {
			return armapimanagement.ServiceResource{}, errors.New("service not found")
		},
	}

	drv := NewAPIMDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "myapim"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if h.Healthy {
		t.Error("expected unhealthy when get fails")
	}
}
