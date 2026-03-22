package driver

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockDNSClient struct {
	createFn func(ctx context.Context, rg, zoneName string, zone armdns.Zone) (armdns.Zone, error)
	getFn    func(ctx context.Context, rg, zoneName string) (armdns.Zone, error)
	deleteFn func(ctx context.Context, rg, zoneName string) error
}

func (m *mockDNSClient) CreateOrUpdate(ctx context.Context, rg, zoneName string, zone armdns.Zone) (armdns.Zone, error) {
	return m.createFn(ctx, rg, zoneName, zone)
}

func (m *mockDNSClient) Get(ctx context.Context, rg, zoneName string) (armdns.Zone, error) {
	return m.getFn(ctx, rg, zoneName)
}

func (m *mockDNSClient) Delete(ctx context.Context, rg, zoneName string) error {
	return m.deleteFn(ctx, rg, zoneName)
}

func TestDNSDriver_Create(t *testing.T) {
	zoneType := armdns.ZoneTypePublic
	client := &mockDNSClient{
		createFn: func(_ context.Context, _, zoneName string, _ armdns.Zone) (armdns.Zone, error) {
			return armdns.Zone{
				ID: str("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/dnszones/" + zoneName),
				Properties: &armdns.ZoneProperties{
					ZoneType: &zoneType,
				},
			}, nil
		},
	}

	drv := NewDNSDriver("rg", "global", client)
	out, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "example.com",
		Type:   "infra.dns",
		Config: map[string]any{"zone_type": "Public"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Status != "active" {
		t.Errorf("status = %q, want active", out.Status)
	}
	if out.Type != "infra.dns" {
		t.Errorf("type = %q, want infra.dns", out.Type)
	}
}

func TestDNSDriver_Read(t *testing.T) {
	client := &mockDNSClient{
		getFn: func(_ context.Context, _, zoneName string) (armdns.Zone, error) {
			return armdns.Zone{
				ID: str("/subscriptions/sub/rg/" + zoneName),
			}, nil
		},
	}

	drv := NewDNSDriver("rg", "global", client)
	out, err := drv.Read(context.Background(), interfaces.ResourceRef{Name: "example.com", Type: "infra.dns"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Status != "active" {
		t.Errorf("status = %q, want active", out.Status)
	}
}

func TestDNSDriver_Create_Error(t *testing.T) {
	client := &mockDNSClient{
		createFn: func(_ context.Context, _, _ string, _ armdns.Zone) (armdns.Zone, error) {
			return armdns.Zone{}, errors.New("quota exceeded")
		},
	}

	drv := NewDNSDriver("rg", "global", client)
	_, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "example.com",
		Config: map[string]any{"zone_type": "Public"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDNSDriver_Update(t *testing.T) {
	zoneType := armdns.ZoneTypePublic
	called := false

	client := &mockDNSClient{
		createFn: func(_ context.Context, _, zoneName string, _ armdns.Zone) (armdns.Zone, error) {
			called = true
			return armdns.Zone{
				ID:         str("/sub/rg/dns/" + zoneName),
				Properties: &armdns.ZoneProperties{ZoneType: &zoneType},
			}, nil
		},
	}

	drv := NewDNSDriver("rg", "global", client)
	out, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "example.com"}, interfaces.ResourceSpec{
		Name:   "example.com",
		Config: map[string]any{"zone_type": "Public"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !called {
		t.Error("expected CreateOrUpdate to be called")
	}
	_ = out
}

func TestDNSDriver_Update_Error(t *testing.T) {
	client := &mockDNSClient{
		createFn: func(_ context.Context, _, _ string, _ armdns.Zone) (armdns.Zone, error) {
			return armdns.Zone{}, errors.New("update failed")
		},
	}

	drv := NewDNSDriver("rg", "global", client)
	_, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "example.com"}, interfaces.ResourceSpec{
		Name:   "example.com",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDNSDriver_Delete(t *testing.T) {
	deleted := false
	client := &mockDNSClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			deleted = true
			return nil
		},
	}

	drv := NewDNSDriver("rg", "global", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("expected Delete to be called")
	}
}

func TestDNSDriver_Delete_Error(t *testing.T) {
	client := &mockDNSClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			return errors.New("zone not found")
		},
	}

	drv := NewDNSDriver("rg", "global", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "example.com"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDNSDriver_Diff_ZoneTypeChange(t *testing.T) {
	drv := NewDNSDriver("rg", "global", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"zone_type": "Public"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"zone_type": "Private"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true")
	}
	if !diff.NeedsReplace {
		t.Error("expected NeedsReplace=true for zone_type change")
	}
}

func TestDNSDriver_Diff_NoChanges(t *testing.T) {
	drv := NewDNSDriver("rg", "global", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"zone_type": "Public"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"zone_type": "Public"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=false when zone_type matches")
	}
}

func TestDNSDriver_Diff_NilCurrent(t *testing.T) {
	drv := NewDNSDriver("rg", "global", nil)
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{Name: "example.com"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when current is nil")
	}
}

func TestDNSDriver_HealthCheck_Healthy(t *testing.T) {
	client := &mockDNSClient{
		getFn: func(_ context.Context, _, zoneName string) (armdns.Zone, error) {
			return armdns.Zone{
				ID: str("/sub/rg/dns/" + zoneName),
			}, nil
		},
	}

	drv := NewDNSDriver("rg", "global", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "example.com"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if !h.Healthy {
		t.Errorf("expected healthy, got: %s", h.Message)
	}
}

func TestDNSDriver_HealthCheck_Unhealthy(t *testing.T) {
	client := &mockDNSClient{
		getFn: func(_ context.Context, _, _ string) (armdns.Zone, error) {
			return armdns.Zone{}, errors.New("zone not found")
		},
	}

	drv := NewDNSDriver("rg", "global", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "example.com"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if h.Healthy {
		t.Error("expected unhealthy when get fails")
	}
}
