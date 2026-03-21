package driver

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appservice/armappservice/v3"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// CertClient is the narrow interface for Azure App Service Certificate operations.
type CertClient interface {
	CreateOrUpdate(ctx context.Context, resourceGroup, name string, cert armappservice.AppCertificate) (armappservice.AppCertificate, error)
	Get(ctx context.Context, resourceGroup, name string) (armappservice.AppCertificate, error)
	Delete(ctx context.Context, resourceGroup, name string) error
}

type realCertClient struct {
	inner *armappservice.CertificatesClient
}

func (c *realCertClient) CreateOrUpdate(ctx context.Context, rg, name string, cert armappservice.AppCertificate) (armappservice.AppCertificate, error) {
	res, err := c.inner.CreateOrUpdate(ctx, rg, name, cert, nil)
	if err != nil {
		return armappservice.AppCertificate{}, err
	}
	return res.AppCertificate, nil
}

func (c *realCertClient) Get(ctx context.Context, rg, name string) (armappservice.AppCertificate, error) {
	res, err := c.inner.Get(ctx, rg, name, nil)
	if err != nil {
		return armappservice.AppCertificate{}, err
	}
	return res.AppCertificate, nil
}

func (c *realCertClient) Delete(ctx context.Context, rg, name string) error {
	_, err := c.inner.Delete(ctx, rg, name, nil)
	return err
}

// CertDriver manages Azure App Service Certificates (infra.certificate).
type CertDriver struct {
	resourceGroup string
	location      string
	client        CertClient
}

var _ interfaces.ResourceDriver = (*CertDriver)(nil)

func NewCertDriver(resourceGroup, location string, client CertClient) *CertDriver {
	return &CertDriver{resourceGroup: resourceGroup, location: location, client: client}
}

func (d *CertDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	hostNames := []*string{str(configStr(spec.Config, "hostname", spec.Name+".example.com"))}
	serverFarmID := configStr(spec.Config, "server_farm_id", "")

	cert := armappservice.AppCertificate{
		Location: str(d.location),
		Properties: &armappservice.AppCertificateProperties{
			HostNames:    hostNames,
			ServerFarmID: str(serverFarmID),
		},
	}

	result, err := d.client.CreateOrUpdate(ctx, d.resourceGroup, spec.Name, cert)
	if err != nil {
		return nil, fmt.Errorf("cert: create %q: %w", spec.Name, err)
	}
	return certToOutput(spec.Name, result), nil
}

func (d *CertDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	result, err := d.client.Get(ctx, d.resourceGroup, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("cert: get %q: %w", ref.Name, err)
	}
	return certToOutput(ref.Name, result), nil
}

func (d *CertDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.Create(ctx, spec)
}

func (d *CertDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.client.Delete(ctx, d.resourceGroup, ref.Name)
}

func (d *CertDriver) Diff(_ context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	if current == nil {
		return &interfaces.DiffResult{NeedsUpdate: true}, nil
	}
	var changes []interfaces.FieldChange
	if hostname, ok := desired.Config["hostname"].(string); ok {
		if cur, ok := current.Outputs["hostname"].(string); ok && hostname != cur {
			changes = append(changes, interfaces.FieldChange{Path: "hostname", Old: cur, New: hostname, ForceNew: true})
		}
	}
	return &interfaces.DiffResult{NeedsUpdate: len(changes) > 0, NeedsReplace: len(changes) > 0, Changes: changes}, nil
}

func (d *CertDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	out, err := d.Read(ctx, ref)
	if err != nil {
		return &interfaces.HealthResult{Healthy: false, Message: err.Error()}, nil
	}
	healthy := out.Status == "Issued"
	return &interfaces.HealthResult{Healthy: healthy, Message: out.Status}, nil
}

func (d *CertDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, fmt.Errorf("cert: scale not supported")
}

func certToOutput(name string, cert armappservice.AppCertificate) *interfaces.ResourceOutput {
	status := "unknown"
	outputs := map[string]any{}
	if cert.Properties != nil {
		if cert.Properties.IssueDate != nil {
			outputs["issue_date"] = cert.Properties.IssueDate.String()
		}
		if cert.Properties.ExpirationDate != nil {
			outputs["expiration_date"] = cert.Properties.ExpirationDate.String()
		}
		if len(cert.Properties.HostNames) > 0 {
			outputs["hostname"] = strVal(cert.Properties.HostNames[0])
		}
		status = "Issued"
	}
	return &interfaces.ResourceOutput{
		Name:       name,
		Type:       "infra.certificate",
		ProviderID: strVal(cert.ID),
		Outputs:    outputs,
		Status:     status,
	}
}

func toStrPtrs(ss []string) []*string {
	out := make([]*string, len(ss))
	for i := range ss {
		s := ss[i]
		out[i] = &s
	}
	return out
}
