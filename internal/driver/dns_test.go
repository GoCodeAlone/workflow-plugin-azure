package driver

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockDNSClient struct {
	createFn       func(ctx context.Context, rg, zoneName string, zone armdns.Zone) (armdns.Zone, error)
	getFn          func(ctx context.Context, rg, zoneName string) (armdns.Zone, error)
	deleteFn       func(ctx context.Context, rg, zoneName string) error
	listRecordsFn  func(ctx context.Context, rg, zoneName string) ([]*armdns.RecordSet, error)
	upsertRecordFn func(ctx context.Context, rg, zoneName, recordName string, recordType armdns.RecordType, record armdns.RecordSet) (armdns.RecordSet, error)
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

func (m *mockDNSClient) ListRecordSets(ctx context.Context, rg, zoneName string) ([]*armdns.RecordSet, error) {
	if m.listRecordsFn == nil {
		return nil, nil
	}
	return m.listRecordsFn(ctx, rg, zoneName)
}

func (m *mockDNSClient) CreateOrUpdateRecordSet(ctx context.Context, rg, zoneName, recordName string, recordType armdns.RecordType, record armdns.RecordSet) (armdns.RecordSet, error) {
	return m.upsertRecordFn(ctx, rg, zoneName, recordName, recordType, record)
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
	zoneType := armdns.ZoneTypePublic
	client := &mockDNSClient{
		getFn: func(_ context.Context, _, zoneName string) (armdns.Zone, error) {
			return armdns.Zone{
				ID:   str("/subscriptions/sub/rg/" + zoneName),
				Name: str(zoneName),
				Properties: &armdns.ZoneProperties{
					NameServers:        []*string{str("ns1-01.azure-dns.com."), str("ns2-01.azure-dns.net.")},
					NumberOfRecordSets: ptrOf(int64(3)),
					ZoneType:           &zoneType,
				},
			}, nil
		},
		listRecordsFn: func(_ context.Context, _, zoneName string) ([]*armdns.RecordSet, error) {
			return []*armdns.RecordSet{
				{
					Name: str("@"),
					Type: str("Microsoft.Network/dnszones/A"),
					Properties: &armdns.RecordSetProperties{
						TTL:      ptrOf(int64(300)),
						ARecords: []*armdns.ARecord{{IPv4Address: str("203.0.113.10")}},
					},
				},
				{
					Name: str("@"),
					Type: str("Microsoft.Network/dnszones/MX"),
					Properties: &armdns.RecordSetProperties{
						TTL:       ptrOf(int64(3600)),
						MxRecords: []*armdns.MxRecord{{Preference: ptrOf(int32(10)), Exchange: str("aspmx.l.google.com.")}},
					},
				},
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
	if out.Outputs["domain"] != "example.com" {
		t.Fatalf("domain = %v, want example.com", out.Outputs["domain"])
	}
	if out.Outputs["record_count"] != 2 {
		t.Fatalf("record_count = %v, want 2", out.Outputs["record_count"])
	}
	records, ok := out.Outputs["records"].([]map[string]any)
	if !ok || len(records) != 2 {
		t.Fatalf("records = %#v, want two normalized record sets", out.Outputs["records"])
	}
	if records[0]["type"] != "A" || records[0]["name"] != "@" {
		t.Fatalf("first record = %#v, want apex A", records[0])
	}
	if values, ok := records[0]["values"].([]string); !ok || len(values) != 1 || values[0] != "203.0.113.10" {
		t.Fatalf("first record values = %#v, want A value", records[0]["values"])
	}
	if mxValues, ok := records[1]["values"].([]map[string]any); !ok || len(mxValues) != 1 || mxValues[0]["exchange"] != "aspmx.l.google.com." {
		t.Fatalf("mx record values = %#v, want normalized MX value", records[1]["values"])
	}
	if out.Outputs["zone_type"] != "Public" {
		t.Fatalf("zone_type = %v, want Public", out.Outputs["zone_type"])
	}
	authority, ok := out.Outputs["authority"].(map[string]any)
	if !ok {
		t.Fatalf("authority = %T, want map[string]any", out.Outputs["authority"])
	}
	if got := authority["dns_host"]; got != "Azure DNS" {
		t.Fatalf("authority.dns_host = %v, want Azure DNS", got)
	}
	nameServers, ok := authority["name_servers"].([]string)
	if !ok || len(nameServers) != 2 || nameServers[0] != "ns1-01.azure-dns.com." {
		t.Fatalf("authority.name_servers = %#v, want Azure DNS nameservers", authority["name_servers"])
	}
}

func TestDNSDriver_Create_UpsertsConfiguredRecords(t *testing.T) {
	zoneType := armdns.ZoneTypePublic
	var upserts []string
	client := &mockDNSClient{
		createFn: func(_ context.Context, _, zoneName string, _ armdns.Zone) (armdns.Zone, error) {
			return armdns.Zone{
				ID:   str("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/dnszones/" + zoneName),
				Name: str(zoneName),
				Properties: &armdns.ZoneProperties{
					ZoneType: &zoneType,
				},
			}, nil
		},
		upsertRecordFn: func(_ context.Context, _, zoneName, recordName string, recordType armdns.RecordType, record armdns.RecordSet) (armdns.RecordSet, error) {
			upserts = append(upserts, zoneName+"/"+recordName+"/"+string(recordType))
			if record.Properties == nil || record.Properties.TTL == nil {
				t.Fatalf("upserted record missing TTL: %#v", record)
			}
			return record, nil
		},
	}

	drv := NewDNSDriver("rg", "global", client)
	_, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name: "example.com",
		Type: "infra.dns",
		Config: map[string]any{
			"records": []any{
				map[string]any{"name": "@", "type": "A", "ttl": 300, "values": []any{"203.0.113.10"}},
				map[string]any{"name": "@", "type": "MX", "ttl": 3600, "values": []any{map[string]any{"preference": 10, "exchange": "aspmx.l.google.com."}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(upserts) != 2 {
		t.Fatalf("upserts = %#v, want two record upserts", upserts)
	}
	if upserts[0] != "example.com/@/A" || upserts[1] != "example.com/@/MX" {
		t.Fatalf("upserts = %#v, want apex A and MX", upserts)
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
