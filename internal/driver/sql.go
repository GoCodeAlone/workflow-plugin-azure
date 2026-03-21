package driver

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// SQLClient is the narrow interface for Azure SQL operations.
type SQLClient interface {
	CreateOrUpdateServer(ctx context.Context, resourceGroup, name string, srv armsql.Server) (armsql.Server, error)
	GetServer(ctx context.Context, resourceGroup, name string) (armsql.Server, error)
	DeleteServer(ctx context.Context, resourceGroup, name string) error
	CreateOrUpdateDB(ctx context.Context, resourceGroup, serverName, dbName string, db armsql.Database) (armsql.Database, error)
	GetDB(ctx context.Context, resourceGroup, serverName, dbName string) (armsql.Database, error)
}

type realSQLClient struct {
	servers   *armsql.ServersClient
	databases *armsql.DatabasesClient
}

func (c *realSQLClient) CreateOrUpdateServer(ctx context.Context, rg, name string, srv armsql.Server) (armsql.Server, error) {
	poller, err := c.servers.BeginCreateOrUpdate(ctx, rg, name, srv, nil)
	if err != nil {
		return armsql.Server{}, err
	}
	res, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	if err != nil {
		return armsql.Server{}, err
	}
	return res.Server, nil
}

func (c *realSQLClient) GetServer(ctx context.Context, rg, name string) (armsql.Server, error) {
	res, err := c.servers.Get(ctx, rg, name, nil)
	if err != nil {
		return armsql.Server{}, err
	}
	return res.Server, nil
}

func (c *realSQLClient) DeleteServer(ctx context.Context, rg, name string) error {
	poller, err := c.servers.BeginDelete(ctx, rg, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	return err
}

func (c *realSQLClient) CreateOrUpdateDB(ctx context.Context, rg, serverName, dbName string, db armsql.Database) (armsql.Database, error) {
	poller, err := c.databases.BeginCreateOrUpdate(ctx, rg, serverName, dbName, db, nil)
	if err != nil {
		return armsql.Database{}, err
	}
	res, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	if err != nil {
		return armsql.Database{}, err
	}
	return res.Database, nil
}

func (c *realSQLClient) GetDB(ctx context.Context, rg, serverName, dbName string) (armsql.Database, error) {
	res, err := c.databases.Get(ctx, rg, serverName, dbName, nil)
	if err != nil {
		return armsql.Database{}, err
	}
	return res.Database, nil
}

// SQLDriver manages Azure SQL databases (infra.database).
type SQLDriver struct {
	resourceGroup string
	location      string
	client        SQLClient
}

var _ interfaces.ResourceDriver = (*SQLDriver)(nil)

func NewSQLDriver(resourceGroup, location string, client SQLClient) *SQLDriver {
	return &SQLDriver{resourceGroup: resourceGroup, location: location, client: client}
}

func (d *SQLDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	adminUser := configStr(spec.Config, "admin_username", "sqladmin")
	adminPass := configStr(spec.Config, "admin_password", "P@ssw0rd1234!")
	// Default to GP_Gen5_2 (General Purpose vCore model).
	skuName := configStr(spec.Config, "sku_name", "GP_Gen5_2")

	srv := armsql.Server{
		Location: str(d.location),
		Properties: &armsql.ServerProperties{
			AdministratorLogin:         str(adminUser),
			AdministratorLoginPassword: str(adminPass),
			Version:                    str("12.0"),
		},
	}
	srvResult, err := d.client.CreateOrUpdateServer(ctx, d.resourceGroup, spec.Name, srv)
	if err != nil {
		return nil, fmt.Errorf("sql: create server %q: %w", spec.Name, err)
	}

	dbName := configStr(spec.Config, "database_name", "defaultdb")
	db := armsql.Database{
		Location: str(d.location),
		SKU:      &armsql.SKU{Name: str(skuName)},
	}
	dbResult, err := d.client.CreateOrUpdateDB(ctx, d.resourceGroup, spec.Name, dbName, db)
	if err != nil {
		return nil, fmt.Errorf("sql: create db %q on server %q: %w", dbName, spec.Name, err)
	}

	return sqlToOutput(spec.Name, srvResult, dbResult), nil
}

func (d *SQLDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	srv, err := d.client.GetServer(ctx, d.resourceGroup, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("sql: get server %q: %w", ref.Name, err)
	}
	return sqlToOutput(ref.Name, srv, armsql.Database{}), nil
}

func (d *SQLDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.Create(ctx, spec)
}

func (d *SQLDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.client.DeleteServer(ctx, d.resourceGroup, ref.Name)
}

func (d *SQLDriver) Diff(_ context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	if current == nil {
		return &interfaces.DiffResult{NeedsUpdate: true}, nil
	}
	var changes []interfaces.FieldChange
	if sku, ok := desired.Config["sku"].(string); ok {
		if cur, ok := current.Outputs["sku"].(string); ok && sku != cur {
			changes = append(changes, interfaces.FieldChange{Path: "sku", Old: cur, New: sku})
		}
	}
	return &interfaces.DiffResult{NeedsUpdate: len(changes) > 0, Changes: changes}, nil
}

func (d *SQLDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	out, err := d.Read(ctx, ref)
	if err != nil {
		return &interfaces.HealthResult{Healthy: false, Message: err.Error()}, nil
	}
	healthy := out.Status == "Ready"
	return &interfaces.HealthResult{Healthy: healthy, Message: out.Status}, nil
}

func (d *SQLDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, fmt.Errorf("sql: scale not supported — update DTU/vCore tier instead")
}

func sqlToOutput(name string, srv armsql.Server, db armsql.Database) *interfaces.ResourceOutput {
	status := "unknown"
	outputs := map[string]any{}
	if srv.Properties != nil {
		if srv.Properties.State != nil {
			status = strVal(srv.Properties.State)
		}
		if srv.Properties.FullyQualifiedDomainName != nil {
			outputs["fqdn"] = strVal(srv.Properties.FullyQualifiedDomainName)
		}
	}
	if db.SKU != nil && db.SKU.Name != nil {
		outputs["sku"] = strVal(db.SKU.Name)
	}
	return &interfaces.ResourceOutput{
		Name:       name,
		Type:       "infra.database",
		ProviderID: strVal(srv.ID),
		Outputs:    outputs,
		Status:     status,
	}
}
