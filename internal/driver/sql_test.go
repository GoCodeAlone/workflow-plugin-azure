package driver

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockSQLClient struct {
	createServerFn func(ctx context.Context, rg, name string, srv armsql.Server) (armsql.Server, error)
	getServerFn    func(ctx context.Context, rg, name string) (armsql.Server, error)
	deleteServerFn func(ctx context.Context, rg, name string) error
	createDBFn     func(ctx context.Context, rg, serverName, dbName string, db armsql.Database) (armsql.Database, error)
	getDBFn        func(ctx context.Context, rg, serverName, dbName string) (armsql.Database, error)
}

func (m *mockSQLClient) CreateOrUpdateServer(ctx context.Context, rg, name string, srv armsql.Server) (armsql.Server, error) {
	return m.createServerFn(ctx, rg, name, srv)
}

func (m *mockSQLClient) GetServer(ctx context.Context, rg, name string) (armsql.Server, error) {
	return m.getServerFn(ctx, rg, name)
}

func (m *mockSQLClient) DeleteServer(ctx context.Context, rg, name string) error {
	return m.deleteServerFn(ctx, rg, name)
}

func (m *mockSQLClient) CreateOrUpdateDB(ctx context.Context, rg, serverName, dbName string, db armsql.Database) (armsql.Database, error) {
	return m.createDBFn(ctx, rg, serverName, dbName, db)
}

func (m *mockSQLClient) GetDB(ctx context.Context, rg, serverName, dbName string) (armsql.Database, error) {
	return m.getDBFn(ctx, rg, serverName, dbName)
}

func TestSQLDriver_Create(t *testing.T) {
	state := "Ready"
	fqdn := "test-sql.database.windows.net"
	skuName := "S1"

	client := &mockSQLClient{
		createServerFn: func(_ context.Context, _, name string, _ armsql.Server) (armsql.Server, error) {
			return armsql.Server{
				ID: str("/subscriptions/sub/rg/sql/" + name),
				Properties: &armsql.ServerProperties{
					State:                    &state,
					FullyQualifiedDomainName: &fqdn,
				},
			}, nil
		},
		createDBFn: func(_ context.Context, _, _, dbName string, db armsql.Database) (armsql.Database, error) {
			return armsql.Database{
				ID:  str("/subscriptions/sub/rg/sql/server/databases/" + dbName),
				SKU: &armsql.SKU{Name: &skuName},
			}, nil
		},
	}

	drv := NewSQLDriver("rg", "eastus", client)
	out, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-sql",
		Type:   "infra.database",
		Config: map[string]any{"sku_name": skuName, "database_name": "mydb"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Status != "Ready" {
		t.Errorf("status = %q, want Ready", out.Status)
	}
	if out.Outputs["fqdn"] != fqdn {
		t.Errorf("fqdn = %v, want %s", out.Outputs["fqdn"], fqdn)
	}
}

func TestSQLDriver_Create_Error(t *testing.T) {
	client := &mockSQLClient{
		createServerFn: func(_ context.Context, _, _ string, _ armsql.Server) (armsql.Server, error) {
			return armsql.Server{}, errors.New("quota exceeded")
		},
	}

	drv := NewSQLDriver("rg", "eastus", client)
	_, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-sql",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSQLDriver_Read(t *testing.T) {
	state := "Ready"
	fqdn := "test-sql.database.windows.net"

	client := &mockSQLClient{
		getServerFn: func(_ context.Context, _, name string) (armsql.Server, error) {
			return armsql.Server{
				ID: str("/subscriptions/sub/rg/sql/" + name),
				Properties: &armsql.ServerProperties{
					State:                    &state,
					FullyQualifiedDomainName: &fqdn,
				},
			}, nil
		},
	}

	drv := NewSQLDriver("rg", "eastus", client)
	out, err := drv.Read(context.Background(), interfaces.ResourceRef{Name: "test-sql", Type: "infra.database"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Status != "Ready" {
		t.Errorf("status = %q, want Ready", out.Status)
	}
	if out.Outputs["fqdn"] != fqdn {
		t.Errorf("fqdn = %v, want %s", out.Outputs["fqdn"], fqdn)
	}
}

func TestSQLDriver_Update(t *testing.T) {
	state := "Ready"
	called := false

	client := &mockSQLClient{
		createServerFn: func(_ context.Context, _, name string, _ armsql.Server) (armsql.Server, error) {
			called = true
			return armsql.Server{
				ID: str("/sub/rg/sql/" + name),
				Properties: &armsql.ServerProperties{State: &state},
			}, nil
		},
		createDBFn: func(_ context.Context, _, _, dbName string, _ armsql.Database) (armsql.Database, error) {
			return armsql.Database{ID: str("/sub/rg/sql/server/databases/" + dbName)}, nil
		},
	}

	drv := NewSQLDriver("rg", "eastus", client)
	out, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-sql"}, interfaces.ResourceSpec{
		Name:   "test-sql",
		Config: map[string]any{"sku_name": "GP_Gen5_4"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !called {
		t.Error("expected CreateOrUpdateServer to be called")
	}
	_ = out
}

func TestSQLDriver_Update_Error(t *testing.T) {
	client := &mockSQLClient{
		createServerFn: func(_ context.Context, _, _ string, _ armsql.Server) (armsql.Server, error) {
			return armsql.Server{}, errors.New("update failed")
		},
	}

	drv := NewSQLDriver("rg", "eastus", client)
	_, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-sql"}, interfaces.ResourceSpec{
		Name:   "test-sql",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSQLDriver_Delete(t *testing.T) {
	deleted := false
	client := &mockSQLClient{
		deleteServerFn: func(_ context.Context, _, _ string) error {
			deleted = true
			return nil
		},
	}

	drv := NewSQLDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-sql"})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("expected Delete to be called")
	}
}

func TestSQLDriver_Delete_Error(t *testing.T) {
	client := &mockSQLClient{
		deleteServerFn: func(_ context.Context, _, _ string) error {
			return errors.New("not found")
		},
	}

	drv := NewSQLDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-sql"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSQLDriver_Diff_HasChanges(t *testing.T) {
	drv := NewSQLDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"sku": "GP_Gen5_2"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"sku": "GP_Gen5_4"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when sku changes")
	}
}

func TestSQLDriver_Diff_NoChanges(t *testing.T) {
	drv := NewSQLDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"sku": "GP_Gen5_2"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"sku": "GP_Gen5_2"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=false when sku matches")
	}
}

func TestSQLDriver_Diff_NilCurrent(t *testing.T) {
	drv := NewSQLDriver("rg", "eastus", nil)
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{Name: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when current is nil")
	}
}

func TestSQLDriver_HealthCheck_Healthy(t *testing.T) {
	state := "Ready"
	client := &mockSQLClient{
		getServerFn: func(_ context.Context, _, name string) (armsql.Server, error) {
			return armsql.Server{
				ID:         str("/sub/rg/sql/" + name),
				Properties: &armsql.ServerProperties{State: &state},
			}, nil
		},
	}

	drv := NewSQLDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-sql"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if !h.Healthy {
		t.Errorf("expected healthy, got: %s", h.Message)
	}
}

func TestSQLDriver_HealthCheck_Unhealthy(t *testing.T) {
	client := &mockSQLClient{
		getServerFn: func(_ context.Context, _, _ string) (armsql.Server, error) {
			return armsql.Server{}, errors.New("server not found")
		},
	}

	drv := NewSQLDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-sql"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if h.Healthy {
		t.Error("expected unhealthy when get fails")
	}
}
