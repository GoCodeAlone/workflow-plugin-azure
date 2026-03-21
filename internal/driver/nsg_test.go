package driver

import (
	"context"
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
