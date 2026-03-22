package driver

import (
	"context"
	"errors"
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

func TestVNetDriver_Create_Error(t *testing.T) {
	client := &mockVNetClient{
		createFn: func(_ context.Context, _, _ string, _ armnetwork.VirtualNetwork) (armnetwork.VirtualNetwork, error) {
			return armnetwork.VirtualNetwork{}, errors.New("quota exceeded")
		},
	}

	drv := NewVNetDriver("rg", "eastus", client)
	_, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-vnet",
		Config: map[string]any{"cidr": "10.0.0.0/16"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestVNetDriver_Read(t *testing.T) {
	prov := armnetwork.ProvisioningStateSucceeded
	cidr := "10.0.0.0/16"

	client := &mockVNetClient{
		getFn: func(_ context.Context, _, name string) (armnetwork.VirtualNetwork, error) {
			return armnetwork.VirtualNetwork{
				ID: str("/sub/rg/vnet/" + name),
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
	out, err := drv.Read(context.Background(), interfaces.ResourceRef{Name: "test-vnet", Type: "infra.vpc"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Outputs["cidr"] != cidr {
		t.Errorf("cidr = %v, want %s", out.Outputs["cidr"], cidr)
	}
}

func TestVNetDriver_Update(t *testing.T) {
	prov := armnetwork.ProvisioningStateSucceeded
	called := false

	client := &mockVNetClient{
		createFn: func(_ context.Context, _, name string, _ armnetwork.VirtualNetwork) (armnetwork.VirtualNetwork, error) {
			called = true
			return armnetwork.VirtualNetwork{
				ID: str("/sub/rg/vnet/" + name),
				Properties: &armnetwork.VirtualNetworkPropertiesFormat{
					ProvisioningState: &prov,
				},
			}, nil
		},
	}

	drv := NewVNetDriver("rg", "eastus", client)
	out, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-vnet"}, interfaces.ResourceSpec{
		Name:   "test-vnet",
		Config: map[string]any{"cidr": "10.0.0.0/16"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !called {
		t.Error("expected CreateOrUpdate to be called")
	}
	_ = out
}

func TestVNetDriver_Update_Error(t *testing.T) {
	client := &mockVNetClient{
		createFn: func(_ context.Context, _, _ string, _ armnetwork.VirtualNetwork) (armnetwork.VirtualNetwork, error) {
			return armnetwork.VirtualNetwork{}, errors.New("update failed")
		},
	}

	drv := NewVNetDriver("rg", "eastus", client)
	_, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-vnet"}, interfaces.ResourceSpec{
		Name:   "test-vnet",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestVNetDriver_Delete(t *testing.T) {
	deleted := false
	client := &mockVNetClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			deleted = true
			return nil
		},
	}

	drv := NewVNetDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-vnet"})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("expected Delete to be called")
	}
}

func TestVNetDriver_Delete_Error(t *testing.T) {
	client := &mockVNetClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			return errors.New("not found")
		},
	}

	drv := NewVNetDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-vnet"})
	if err == nil {
		t.Fatal("expected error, got nil")
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

func TestVNetDriver_Diff_NoChanges(t *testing.T) {
	drv := NewVNetDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"cidr": "10.0.0.0/16"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"cidr": "10.0.0.0/16"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=false when CIDR matches")
	}
}

func TestVNetDriver_Diff_NilCurrent(t *testing.T) {
	drv := NewVNetDriver("rg", "eastus", nil)
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{Name: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when current is nil")
	}
}

func TestVNetDriver_HealthCheck_Healthy(t *testing.T) {
	prov := armnetwork.ProvisioningStateSucceeded
	client := &mockVNetClient{
		getFn: func(_ context.Context, _, name string) (armnetwork.VirtualNetwork, error) {
			return armnetwork.VirtualNetwork{
				ID: str("/sub/rg/vnet/" + name),
				Properties: &armnetwork.VirtualNetworkPropertiesFormat{
					ProvisioningState: &prov,
				},
			}, nil
		},
	}

	drv := NewVNetDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-vnet"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if !h.Healthy {
		t.Errorf("expected healthy, got: %s", h.Message)
	}
}

func TestVNetDriver_HealthCheck_Unhealthy(t *testing.T) {
	client := &mockVNetClient{
		getFn: func(_ context.Context, _, _ string) (armnetwork.VirtualNetwork, error) {
			return armnetwork.VirtualNetwork{}, errors.New("vnet not found")
		},
	}

	drv := NewVNetDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-vnet"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if h.Healthy {
		t.Error("expected unhealthy when get fails")
	}
}
