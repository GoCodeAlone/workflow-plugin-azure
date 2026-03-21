package driver

import (
	"context"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v6"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockAKSClient struct {
	createFn func(ctx context.Context, rg, name string, c armcontainerservice.ManagedCluster) (armcontainerservice.ManagedCluster, error)
	getFn    func(ctx context.Context, rg, name string) (armcontainerservice.ManagedCluster, error)
	deleteFn func(ctx context.Context, rg, name string) error
}

func (m *mockAKSClient) CreateOrUpdate(ctx context.Context, rg, name string, c armcontainerservice.ManagedCluster) (armcontainerservice.ManagedCluster, error) {
	return m.createFn(ctx, rg, name, c)
}

func (m *mockAKSClient) Get(ctx context.Context, rg, name string) (armcontainerservice.ManagedCluster, error) {
	return m.getFn(ctx, rg, name)
}

func (m *mockAKSClient) Delete(ctx context.Context, rg, name string) error {
	return m.deleteFn(ctx, rg, name)
}

func TestAKSDriver_Create(t *testing.T) {
	provState := "Succeeded"
	fqdn := "test-aks.eastus.azmk8s.io"
	k8sVer := "1.30"

	client := &mockAKSClient{
		createFn: func(_ context.Context, _, name string, c armcontainerservice.ManagedCluster) (armcontainerservice.ManagedCluster, error) {
			return armcontainerservice.ManagedCluster{
				ID: str("/subscriptions/sub/rg/aks/" + name),
				Properties: &armcontainerservice.ManagedClusterProperties{
					ProvisioningState: &provState,
					KubernetesVersion: &k8sVer,
					Fqdn:              &fqdn,
				},
			}, nil
		},
	}

	drv := NewAKSDriver("rg", "eastus", client)
	out, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-aks",
		Type:   "infra.k8s_cluster",
		Config: map[string]any{"kubernetes_version": k8sVer, "node_count": 3},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
	if out.Outputs["fqdn"] != fqdn {
		t.Errorf("fqdn = %v, want %s", out.Outputs["fqdn"], fqdn)
	}
}

func TestAKSDriver_Scale(t *testing.T) {
	provState := "Succeeded"
	count := int32(2)
	called := false

	client := &mockAKSClient{
		getFn: func(_ context.Context, _, name string) (armcontainerservice.ManagedCluster, error) {
			return armcontainerservice.ManagedCluster{
				ID: str("/sub/rg/aks/" + name),
				Properties: &armcontainerservice.ManagedClusterProperties{
					ProvisioningState: &provState,
					AgentPoolProfiles: []*armcontainerservice.ManagedClusterAgentPoolProfile{
						{Name: str("default"), Count: &count},
					},
				},
			}, nil
		},
		createFn: func(_ context.Context, _, name string, c armcontainerservice.ManagedCluster) (armcontainerservice.ManagedCluster, error) {
			called = true
			return armcontainerservice.ManagedCluster{
				ID:         str("/sub/rg/aks/" + name),
				Properties: c.Properties,
			}, nil
		},
	}

	drv := NewAKSDriver("rg", "eastus", client)
	out, err := drv.Scale(context.Background(), interfaces.ResourceRef{Name: "test-aks"}, 5)
	if err != nil {
		t.Fatalf("Scale: %v", err)
	}
	if !called {
		t.Error("expected CreateOrUpdate to be called during scale")
	}
	_ = out
}

func TestAKSDriver_Diff_NoChange(t *testing.T) {
	drv := NewAKSDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"kubernetes_version": "1.30"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"kubernetes_version": "1.30"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=false when versions match")
	}
}
