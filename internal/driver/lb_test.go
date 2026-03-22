package driver

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockLBClient struct {
	createFn func(ctx context.Context, rg, name string, lb armnetwork.LoadBalancer) (armnetwork.LoadBalancer, error)
	getFn    func(ctx context.Context, rg, name string) (armnetwork.LoadBalancer, error)
	deleteFn func(ctx context.Context, rg, name string) error
}

func (m *mockLBClient) CreateOrUpdate(ctx context.Context, rg, name string, lb armnetwork.LoadBalancer) (armnetwork.LoadBalancer, error) {
	return m.createFn(ctx, rg, name, lb)
}

func (m *mockLBClient) Get(ctx context.Context, rg, name string) (armnetwork.LoadBalancer, error) {
	return m.getFn(ctx, rg, name)
}

func (m *mockLBClient) Delete(ctx context.Context, rg, name string) error {
	return m.deleteFn(ctx, rg, name)
}

func TestLBDriver_Create(t *testing.T) {
	ps := armnetwork.ProvisioningStateSucceeded
	client := &mockLBClient{
		createFn: func(_ context.Context, _, name string, _ armnetwork.LoadBalancer) (armnetwork.LoadBalancer, error) {
			return armnetwork.LoadBalancer{
				ID: str("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/loadBalancers/" + name),
				Properties: &armnetwork.LoadBalancerPropertiesFormat{
					ProvisioningState: &ps,
				},
			}, nil
		},
	}

	drv := NewLBDriver("rg", "eastus", client)
	out, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-lb",
		Type:   "infra.load_balancer",
		Config: map[string]any{"scheme": "Standard"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
	if out.Type != "infra.load_balancer" {
		t.Errorf("type = %q, want infra.load_balancer", out.Type)
	}
}

func TestLBDriver_Read(t *testing.T) {
	ps := armnetwork.ProvisioningStateSucceeded
	client := &mockLBClient{
		getFn: func(_ context.Context, _, name string) (armnetwork.LoadBalancer, error) {
			return armnetwork.LoadBalancer{
				ID: str("/subscriptions/sub/rg/" + name),
				Properties: &armnetwork.LoadBalancerPropertiesFormat{
					ProvisioningState: &ps,
				},
			}, nil
		},
	}

	drv := NewLBDriver("rg", "eastus", client)
	out, err := drv.Read(context.Background(), interfaces.ResourceRef{Name: "test-lb", Type: "infra.load_balancer"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
}

func TestLBDriver_Create_Error(t *testing.T) {
	client := &mockLBClient{
		createFn: func(_ context.Context, _, _ string, _ armnetwork.LoadBalancer) (armnetwork.LoadBalancer, error) {
			return armnetwork.LoadBalancer{}, errors.New("quota exceeded")
		},
	}

	drv := NewLBDriver("rg", "eastus", client)
	_, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-lb",
		Config: map[string]any{"scheme": "Standard"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLBDriver_Update(t *testing.T) {
	ps := armnetwork.ProvisioningStateSucceeded
	called := false

	client := &mockLBClient{
		createFn: func(_ context.Context, _, name string, _ armnetwork.LoadBalancer) (armnetwork.LoadBalancer, error) {
			called = true
			return armnetwork.LoadBalancer{
				ID: str("/sub/rg/lb/" + name),
				Properties: &armnetwork.LoadBalancerPropertiesFormat{
					ProvisioningState: &ps,
				},
			}, nil
		},
	}

	drv := NewLBDriver("rg", "eastus", client)
	out, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-lb"}, interfaces.ResourceSpec{
		Name:   "test-lb",
		Config: map[string]any{"scheme": "Standard"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !called {
		t.Error("expected CreateOrUpdate to be called")
	}
	_ = out
}

func TestLBDriver_Update_Error(t *testing.T) {
	client := &mockLBClient{
		createFn: func(_ context.Context, _, _ string, _ armnetwork.LoadBalancer) (armnetwork.LoadBalancer, error) {
			return armnetwork.LoadBalancer{}, errors.New("update failed")
		},
	}

	drv := NewLBDriver("rg", "eastus", client)
	_, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-lb"}, interfaces.ResourceSpec{
		Name:   "test-lb",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLBDriver_Delete(t *testing.T) {
	deleted := false
	client := &mockLBClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			deleted = true
			return nil
		},
	}

	drv := NewLBDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-lb"})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("expected Delete to be called")
	}
}

func TestLBDriver_Delete_Error(t *testing.T) {
	client := &mockLBClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			return errors.New("not found")
		},
	}

	drv := NewLBDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-lb"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLBDriver_Diff_SchemeChange(t *testing.T) {
	drv := NewLBDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"scheme": "Basic"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"scheme": "Standard"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true")
	}
	if !diff.NeedsReplace {
		t.Error("expected NeedsReplace=true for scheme change")
	}
}

func TestLBDriver_Diff_NoChanges(t *testing.T) {
	drv := NewLBDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"scheme": "Standard"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"scheme": "Standard"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=false when scheme matches")
	}
}

func TestLBDriver_Diff_NilCurrent(t *testing.T) {
	drv := NewLBDriver("rg", "eastus", nil)
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{Name: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when current is nil")
	}
}

func TestLBDriver_HealthCheck_Healthy(t *testing.T) {
	ps := armnetwork.ProvisioningStateSucceeded
	client := &mockLBClient{
		getFn: func(_ context.Context, _, name string) (armnetwork.LoadBalancer, error) {
			return armnetwork.LoadBalancer{
				ID: str("/sub/rg/lb/" + name),
				Properties: &armnetwork.LoadBalancerPropertiesFormat{
					ProvisioningState: &ps,
				},
			}, nil
		},
	}

	drv := NewLBDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-lb"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if !h.Healthy {
		t.Errorf("expected healthy, got: %s", h.Message)
	}
}

func TestLBDriver_HealthCheck_Unhealthy(t *testing.T) {
	client := &mockLBClient{
		getFn: func(_ context.Context, _, _ string) (armnetwork.LoadBalancer, error) {
			return armnetwork.LoadBalancer{}, errors.New("lb not found")
		},
	}

	drv := NewLBDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-lb"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if h.Healthy {
		t.Error("expected unhealthy when get fails")
	}
}
