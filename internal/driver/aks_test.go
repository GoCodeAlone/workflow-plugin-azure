package driver

import (
	"context"
	"errors"
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

func TestAKSDriver_Create_Error(t *testing.T) {
	client := &mockAKSClient{
		createFn: func(_ context.Context, _, _ string, _ armcontainerservice.ManagedCluster) (armcontainerservice.ManagedCluster, error) {
			return armcontainerservice.ManagedCluster{}, errors.New("quota exceeded")
		},
	}

	drv := NewAKSDriver("rg", "eastus", client)
	_, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-aks",
		Type:   "infra.k8s_cluster",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAKSDriver_Read(t *testing.T) {
	provState := "Succeeded"
	k8sVer := "1.30"

	client := &mockAKSClient{
		getFn: func(_ context.Context, _, name string) (armcontainerservice.ManagedCluster, error) {
			return armcontainerservice.ManagedCluster{
				ID: str("/sub/rg/aks/" + name),
				Properties: &armcontainerservice.ManagedClusterProperties{
					ProvisioningState: &provState,
					KubernetesVersion: &k8sVer,
				},
			}, nil
		},
	}

	drv := NewAKSDriver("rg", "eastus", client)
	out, err := drv.Read(context.Background(), interfaces.ResourceRef{Name: "test-aks", Type: "infra.k8s_cluster"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
	if out.Outputs["kubernetes_version"] != k8sVer {
		t.Errorf("kubernetes_version = %v, want %s", out.Outputs["kubernetes_version"], k8sVer)
	}
}

func TestAKSDriver_Update(t *testing.T) {
	provState := "Succeeded"
	called := false

	client := &mockAKSClient{
		createFn: func(_ context.Context, _, name string, _ armcontainerservice.ManagedCluster) (armcontainerservice.ManagedCluster, error) {
			called = true
			return armcontainerservice.ManagedCluster{
				ID: str("/sub/rg/aks/" + name),
				Properties: &armcontainerservice.ManagedClusterProperties{
					ProvisioningState: &provState,
				},
			}, nil
		},
	}

	drv := NewAKSDriver("rg", "eastus", client)
	out, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-aks"}, interfaces.ResourceSpec{
		Name:   "test-aks",
		Config: map[string]any{"kubernetes_version": "1.31"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !called {
		t.Error("expected CreateOrUpdate to be called")
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
}

func TestAKSDriver_Update_Error(t *testing.T) {
	client := &mockAKSClient{
		createFn: func(_ context.Context, _, _ string, _ armcontainerservice.ManagedCluster) (armcontainerservice.ManagedCluster, error) {
			return armcontainerservice.ManagedCluster{}, errors.New("update failed")
		},
	}

	drv := NewAKSDriver("rg", "eastus", client)
	_, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-aks"}, interfaces.ResourceSpec{
		Name:   "test-aks",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAKSDriver_Delete(t *testing.T) {
	deleted := false
	client := &mockAKSClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			deleted = true
			return nil
		},
	}

	drv := NewAKSDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-aks"})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !deleted {
		t.Error("expected Delete to be called")
	}
}

func TestAKSDriver_Delete_Error(t *testing.T) {
	client := &mockAKSClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			return errors.New("not found")
		},
	}

	drv := NewAKSDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-aks"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAKSDriver_Diff_HasChanges(t *testing.T) {
	drv := NewAKSDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"kubernetes_version": "1.29"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"kubernetes_version": "1.31"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when versions differ")
	}
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

func TestAKSDriver_Diff_NilCurrent(t *testing.T) {
	drv := NewAKSDriver("rg", "eastus", nil)
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{Name: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when current is nil")
	}
}

func TestAKSDriver_HealthCheck_Healthy(t *testing.T) {
	provState := "Succeeded"
	client := &mockAKSClient{
		getFn: func(_ context.Context, _, name string) (armcontainerservice.ManagedCluster, error) {
			return armcontainerservice.ManagedCluster{
				ID: str("/sub/rg/aks/" + name),
				Properties: &armcontainerservice.ManagedClusterProperties{
					ProvisioningState: &provState,
				},
			}, nil
		},
	}

	drv := NewAKSDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-aks"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if !h.Healthy {
		t.Errorf("expected healthy, got unhealthy: %s", h.Message)
	}
}

func TestAKSDriver_HealthCheck_Unhealthy(t *testing.T) {
	client := &mockAKSClient{
		getFn: func(_ context.Context, _, _ string) (armcontainerservice.ManagedCluster, error) {
			return armcontainerservice.ManagedCluster{}, errors.New("cluster not found")
		},
	}

	drv := NewAKSDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-aks"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if h.Healthy {
		t.Error("expected unhealthy when get fails")
	}
}
