package driver

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appservice/armappservice/v3"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockCertClient struct {
	createFn func(ctx context.Context, rg, name string, cert armappservice.AppCertificate) (armappservice.AppCertificate, error)
	getFn    func(ctx context.Context, rg, name string) (armappservice.AppCertificate, error)
	deleteFn func(ctx context.Context, rg, name string) error
}

func (m *mockCertClient) CreateOrUpdate(ctx context.Context, rg, name string, cert armappservice.AppCertificate) (armappservice.AppCertificate, error) {
	return m.createFn(ctx, rg, name, cert)
}

func (m *mockCertClient) Get(ctx context.Context, rg, name string) (armappservice.AppCertificate, error) {
	return m.getFn(ctx, rg, name)
}

func (m *mockCertClient) Delete(ctx context.Context, rg, name string) error {
	return m.deleteFn(ctx, rg, name)
}

func TestCertDriver_Create(t *testing.T) {
	hostname := "example.com"
	issueDate := time.Now()
	expDate := time.Now().AddDate(1, 0, 0)
	client := &mockCertClient{
		createFn: func(_ context.Context, _, name string, _ armappservice.AppCertificate) (armappservice.AppCertificate, error) {
			return armappservice.AppCertificate{
				ID: str("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Web/certificates/" + name),
				Properties: &armappservice.AppCertificateProperties{
					HostNames:      []*string{&hostname},
					IssueDate:      &issueDate,
					ExpirationDate: &expDate,
				},
			}, nil
		},
	}

	drv := NewCertDriver("rg", "eastus", client)
	out, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "my-cert",
		Type:   "infra.certificate",
		Config: map[string]any{"hostname": hostname},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Status != "Issued" {
		t.Errorf("status = %q, want Issued", out.Status)
	}
	if out.Outputs["hostname"] != hostname {
		t.Errorf("hostname = %v, want %s", out.Outputs["hostname"], hostname)
	}
	if out.Type != "infra.certificate" {
		t.Errorf("type = %q, want infra.certificate", out.Type)
	}
}

func TestCertDriver_Read(t *testing.T) {
	hostname := "example.com"
	client := &mockCertClient{
		getFn: func(_ context.Context, _, name string) (armappservice.AppCertificate, error) {
			return armappservice.AppCertificate{
				ID: str("/subscriptions/sub/rg/" + name),
				Properties: &armappservice.AppCertificateProperties{
					HostNames: []*string{&hostname},
				},
			}, nil
		},
	}

	drv := NewCertDriver("rg", "eastus", client)
	out, err := drv.Read(context.Background(), interfaces.ResourceRef{Name: "my-cert", Type: "infra.certificate"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Status != "Issued" {
		t.Errorf("status = %q, want Issued", out.Status)
	}
}

func TestCertDriver_Diff_HostnameChange(t *testing.T) {
	drv := NewCertDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"hostname": "old.example.com"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"hostname": "new.example.com"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true")
	}
	if !diff.NeedsReplace {
		t.Error("expected NeedsReplace=true for hostname change")
	}
}
