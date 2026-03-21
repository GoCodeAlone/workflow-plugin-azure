package driver

import (
	"context"
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
