package driver

import (
	"context"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockVNetClient struct {
	createFn func(ctx context.Context, rg, name string, vnet armnetwork.VirtualNetwork) (armnetwork.VirtualNetwork, error)
	getFn    func(ctx context.Context, rg, name string) (armnetwork.VirtualNetwork, error)
	deleteFn func(ctx context.Context, rg, name string) error
}

func (m *mockVNetClient) CreateOrUpdate(ctx context.Context, rg, name string, vnet armnetwork.VirtualNetwork) (armnetwork.VirtualNetwork, error) {
	return m.createFn(ctx, rg, name, vnet)
}

func (m *mockVNetClient) Get(ctx context.Context, rg, name string) (armnetwork.VirtualNetwork, error) {
	return m.getFn(ctx, rg, name)
}

func (m *mockVNetClient) Delete(ctx context.Context, rg, name string) error {
	return m.deleteFn(ctx, rg, name)
}

func TestVNetDriver_Create(t *testing.T) {
	prov := armnetwork.ProvisioningStateSucceeded
	cidr := "10.0.0.0/16"

	client := &mockVNetClient{
		createFn: func(_ context.Context, _, name string, _ armnetwork.VirtualNetwork) (armnetwork.VirtualNetwork, error) {
			return armnetwork.VirtualNetwork{
				ID: str("/subscriptions/sub/rg/vnet/" + name),
				Properties: &armnetwork.VirtualNetworkPropertiesFormat{
					ProvisioningState: &prov,
					AddressSpace: &armnetwork.AddressSpace{
						AddressPrefixes: []*string{&cidr},
					},
				},
			}, nil
		},
	}

	drv := NewVNetDriver("rg", "eastus", client)
	out, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-vnet",
		Type:   "infra.vpc",
		Config: map[string]any{"cidr": cidr},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Outputs["cidr"] != cidr {
		t.Errorf("cidr = %v, want %s", out.Outputs["cidr"], cidr)
	}
}

func TestVNetDriver_Diff_CIDRChange(t *testing.T) {
	drv := NewVNetDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"cidr": "10.0.0.0/16"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"cidr": "192.168.0.0/16"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when CIDR changes")
	}
	if !diff.NeedsReplace {
		t.Error("expected NeedsReplace=true for CIDR change")
	}
}
