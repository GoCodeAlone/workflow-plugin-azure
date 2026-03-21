package driver

import (
	"context"
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
