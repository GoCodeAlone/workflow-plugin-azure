package driver

import (
	"context"
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
					State:                   &state,
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
		Config: map[string]any{"sku": skuName, "database_name": "mydb"},
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
