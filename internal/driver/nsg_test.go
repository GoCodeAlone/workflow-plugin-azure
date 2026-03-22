package driver

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockNSGClient struct {
	createFn func(ctx context.Context, rg, name string, nsg armnetwork.SecurityGroup) (armnetwork.SecurityGroup, error)
	getFn    func(ctx context.Context, rg, name string) (armnetwork.SecurityGroup, error)
	deleteFn func(ctx context.Context, rg, name string) error
}

func (m *mockNSGClient) CreateOrUpdate(ctx context.Context, rg, name string, nsg armnetwork.SecurityGroup) (armnetwork.SecurityGroup, error) {
	return m.createFn(ctx, rg, name, nsg)
}

func (m *mockNSGClient) Get(ctx context.Context, rg, name string) (armnetwork.SecurityGroup, error) {
	return m.getFn(ctx, rg, name)
}

func (m *mockNSGClient) Delete(ctx context.Context, rg, name string) error {
	return m.deleteFn(ctx, rg, name)
}

func TestNSGDriver_Create(t *testing.T) {
	ps := armnetwork.ProvisioningStateSucceeded
	client := &mockNSGClient{
		createFn: func(_ context.Context, _, name string, _ armnetwork.SecurityGroup) (armnetwork.SecurityGroup, error) {
			return armnetwork.SecurityGroup{
				ID: str("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/networkSecurityGroups/" + name),
				Properties: &armnetwork.SecurityGroupPropertiesFormat{
					ProvisioningState: &ps,
					SecurityRules:     []*armnetwork.SecurityRule{},
				},
			}, nil
		},
	}

	drv := NewNSGDriver("rg", "eastus", client)
	out, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-nsg",
		Type:   "infra.firewall",
		Config: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
	if out.Type != "infra.firewall" {
		t.Errorf("type = %q, want infra.firewall", out.Type)
	}
}

func TestNSGDriver_Read(t *testing.T) {
	ps := armnetwork.ProvisioningStateSucceeded
	client := &mockNSGClient{
		getFn: func(_ context.Context, _, name string) (armnetwork.SecurityGroup, error) {
			return armnetwork.SecurityGroup{
				ID: str("/subscriptions/sub/rg/" + name),
				Properties: &armnetwork.SecurityGroupPropertiesFormat{
					ProvisioningState: &ps,
				},
			}, nil
		},
	}

	drv := NewNSGDriver("rg", "eastus", client)
	out, err := drv.Read(context.Background(), interfaces.ResourceRef{Name: "test-nsg", Type: "infra.firewall"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
}

func TestNSGDriver_Create_Error(t *testing.T) {
	client := &mockNSGClient{
		createFn: func(_ context.Context, _, _ string, _ armnetwork.SecurityGroup) (armnetwork.SecurityGroup, error) {
			return armnetwork.SecurityGroup{}, errors.New("quota exceeded")
		},
	}

	drv := NewNSGDriver("rg", "eastus", client)
	_, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-nsg",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestNSGDriver_Update(t *testing.T) {
	ps := armnetwork.ProvisioningStateSucceeded
	called := false

	client := &mockNSGClient{
		createFn: func(_ context.Context, _, name string, _ armnetwork.SecurityGroup) (armnetwork.SecurityGroup, error) {
			called = true
			return armnetwork.SecurityGroup{
				ID: str("/sub/rg/nsg/" + name),
				Properties: &armnetwork.SecurityGroupPropertiesFormat{
					ProvisioningState: &ps,
				},
			}, nil
		},
	}

	drv := NewNSGDriver("rg", "eastus", client)
	out, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-nsg"}, interfaces.ResourceSpec{
		Name:   "test-nsg",
		Config: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !called {
		t.Error("expected CreateOrUpdate to be called")
	}
	_ = out
}

func TestNSGDriver_Update_Error(t *testing.T) {
	client := &mockNSGClient{
		createFn: func(_ context.Context, _, _ string, _ armnetwork.SecurityGroup) (armnetwork.SecurityGroup, error) {
			return armnetwork.SecurityGroup{}, errors.New("update failed")
		},
	}

	drv := NewNSGDriver("rg", "eastus", client)
	_, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-nsg"}, interfaces.ResourceSpec{
		Name:   "test-nsg",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestNSGDriver_Delete(t *testing.T) {
	deleted := false
	client := &mockNSGClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			deleted = true
			return nil
		},
	}

	drv := NewNSGDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-nsg"})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("expected Delete to be called")
	}
}

func TestNSGDriver_Delete_Error(t *testing.T) {
	client := &mockNSGClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			return errors.New("not found")
		},
	}

	drv := NewNSGDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-nsg"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestNSGDriver_Diff_HasChanges(t *testing.T) {
	drv := NewNSGDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"rules": "allow-http"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"rules": "allow-https"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when rules change")
	}
}

func TestNSGDriver_Diff_NoChanges(t *testing.T) {
	drv := NewNSGDriver("rg", "eastus", nil)
	// No rules in config or outputs — NeedsUpdate should be false
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{},
	}, &interfaces.ResourceOutput{Outputs: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=false when no rule changes")
	}
}

func TestNSGDriver_Diff_NilCurrent(t *testing.T) {
	drv := NewNSGDriver("rg", "eastus", nil)
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{Name: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when current is nil")
	}
}

func TestNSGDriver_HealthCheck_Healthy(t *testing.T) {
	ps := armnetwork.ProvisioningStateSucceeded
	client := &mockNSGClient{
		getFn: func(_ context.Context, _, name string) (armnetwork.SecurityGroup, error) {
			return armnetwork.SecurityGroup{
				ID: str("/sub/rg/nsg/" + name),
				Properties: &armnetwork.SecurityGroupPropertiesFormat{
					ProvisioningState: &ps,
				},
			}, nil
		},
	}

	drv := NewNSGDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-nsg"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if !h.Healthy {
		t.Errorf("expected healthy, got: %s", h.Message)
	}
}

func TestNSGDriver_HealthCheck_Unhealthy(t *testing.T) {
	client := &mockNSGClient{
		getFn: func(_ context.Context, _, _ string) (armnetwork.SecurityGroup, error) {
			return armnetwork.SecurityGroup{}, errors.New("nsg not found")
		},
	}

	drv := NewNSGDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-nsg"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if h.Healthy {
		t.Error("expected unhealthy when get fails")
	}
}
