package driver

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockMSIClient struct {
	createFn func(ctx context.Context, rg, name string, params armmsi.Identity) (armmsi.Identity, error)
	getFn    func(ctx context.Context, rg, name string) (armmsi.Identity, error)
	deleteFn func(ctx context.Context, rg, name string) error
}

func (m *mockMSIClient) CreateOrUpdate(ctx context.Context, rg, name string, params armmsi.Identity) (armmsi.Identity, error) {
	return m.createFn(ctx, rg, name, params)
}

func (m *mockMSIClient) Get(ctx context.Context, rg, name string) (armmsi.Identity, error) {
	return m.getFn(ctx, rg, name)
}

func (m *mockMSIClient) Delete(ctx context.Context, rg, name string) error {
	return m.deleteFn(ctx, rg, name)
}

func TestMSIDriver_Create(t *testing.T) {
	clientID := "00000000-0000-0000-0000-000000000001"
	principalID := "00000000-0000-0000-0000-000000000002"

	client := &mockMSIClient{
		createFn: func(_ context.Context, _, name string, _ armmsi.Identity) (armmsi.Identity, error) {
			return armmsi.Identity{
				ID: str("/subscriptions/sub/rg/msi/" + name),
				Properties: &armmsi.UserAssignedIdentityProperties{
					ClientID:    str(clientID),
					PrincipalID: str(principalID),
				},
			}, nil
		},
	}

	drv := NewMSIDriver("rg", "eastus", client)
	out, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-msi",
		Type:   "infra.iam_role",
		Config: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Outputs["client_id"] != clientID {
		t.Errorf("client_id = %v, want %s", out.Outputs["client_id"], clientID)
	}
}

func TestMSIDriver_Create_Error(t *testing.T) {
	client := &mockMSIClient{
		createFn: func(_ context.Context, _, _ string, _ armmsi.Identity) (armmsi.Identity, error) {
			return armmsi.Identity{}, errors.New("quota exceeded")
		},
	}

	drv := NewMSIDriver("rg", "eastus", client)
	_, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-msi",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMSIDriver_Read(t *testing.T) {
	clientID := "00000000-0000-0000-0000-000000000001"
	principalID := "00000000-0000-0000-0000-000000000002"

	client := &mockMSIClient{
		getFn: func(_ context.Context, _, name string) (armmsi.Identity, error) {
			return armmsi.Identity{
				ID: str("/sub/rg/msi/" + name),
				Properties: &armmsi.UserAssignedIdentityProperties{
					ClientID:    str(clientID),
					PrincipalID: str(principalID),
				},
			}, nil
		},
	}

	drv := NewMSIDriver("rg", "eastus", client)
	out, err := drv.Read(context.Background(), interfaces.ResourceRef{Name: "test-msi", Type: "infra.iam_role"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Outputs["client_id"] != clientID {
		t.Errorf("client_id = %v, want %s", out.Outputs["client_id"], clientID)
	}
}

func TestMSIDriver_Update(t *testing.T) {
	clientID := "00000000-0000-0000-0000-000000000001"
	called := false

	client := &mockMSIClient{
		createFn: func(_ context.Context, _, name string, _ armmsi.Identity) (armmsi.Identity, error) {
			called = true
			return armmsi.Identity{
				ID: str("/sub/rg/msi/" + name),
				Properties: &armmsi.UserAssignedIdentityProperties{
					ClientID: str(clientID),
				},
			}, nil
		},
	}

	drv := NewMSIDriver("rg", "eastus", client)
	out, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-msi"}, interfaces.ResourceSpec{
		Name:   "test-msi",
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

func TestMSIDriver_Update_Error(t *testing.T) {
	client := &mockMSIClient{
		createFn: func(_ context.Context, _, _ string, _ armmsi.Identity) (armmsi.Identity, error) {
			return armmsi.Identity{}, errors.New("update failed")
		},
	}

	drv := NewMSIDriver("rg", "eastus", client)
	_, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-msi"}, interfaces.ResourceSpec{
		Name:   "test-msi",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMSIDriver_Delete(t *testing.T) {
	deleted := false
	client := &mockMSIClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			deleted = true
			return nil
		},
	}

	drv := NewMSIDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-msi"})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("expected Delete to be called")
	}
}

func TestMSIDriver_Delete_Error(t *testing.T) {
	client := &mockMSIClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			return errors.New("not found")
		},
	}

	drv := NewMSIDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-msi"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMSIDriver_Diff_NilCurrent(t *testing.T) {
	drv := NewMSIDriver("rg", "eastus", nil)
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{Name: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when current is nil")
	}
}

func TestMSIDriver_Diff_NoChanges(t *testing.T) {
	drv := NewMSIDriver("rg", "eastus", nil)
	// MSI Diff always returns NeedsUpdate=false for non-nil current
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{Name: "x"}, &interfaces.ResourceOutput{})
	if err != nil {
		t.Fatal(err)
	}
	if diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=false for existing MSI")
	}
}

func TestMSIDriver_HealthCheck(t *testing.T) {
	client := &mockMSIClient{
		getFn: func(_ context.Context, _, name string) (armmsi.Identity, error) {
			return armmsi.Identity{
				ID:         str("/sub/rg/msi/" + name),
				Properties: &armmsi.UserAssignedIdentityProperties{},
			}, nil
		},
	}

	drv := NewMSIDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-msi"})
	if err != nil {
		t.Fatal(err)
	}
	if !h.Healthy {
		t.Errorf("expected healthy, got %q", h.Message)
	}
}

func TestMSIDriver_HealthCheck_Unhealthy(t *testing.T) {
	client := &mockMSIClient{
		getFn: func(_ context.Context, _, _ string) (armmsi.Identity, error) {
			return armmsi.Identity{}, errors.New("identity not found")
		},
	}

	drv := NewMSIDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-msi"})
	if err != nil {
		t.Fatal(err)
	}
	if h.Healthy {
		t.Error("expected unhealthy when get fails")
	}
}
