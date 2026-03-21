package driver

import (
	"context"
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
