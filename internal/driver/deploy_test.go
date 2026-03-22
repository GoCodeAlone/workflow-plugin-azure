package driver

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerinstance/armcontainerinstance/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func aciCG(name, image, state string, ip ...string) armcontainerinstance.ContainerGroup {
	cg := armcontainerinstance.ContainerGroup{
		ID:       str("/subscriptions/sub/rg/" + name),
		Location: str("eastus"),
		Properties: &armcontainerinstance.ContainerGroupPropertiesProperties{
			ProvisioningState: str(state),
			Containers: []*armcontainerinstance.Container{
				{
					Name:       str(name),
					Properties: &armcontainerinstance.ContainerProperties{Image: str(image)},
				},
			},
			OSType:        ptrOf(armcontainerinstance.OperatingSystemTypesLinux),
			RestartPolicy: ptrOf(armcontainerinstance.ContainerGroupRestartPolicyAlways),
		},
	}
	if len(ip) > 0 && ip[0] != "" {
		cg.Properties.IPAddress = &armcontainerinstance.IPAddress{IP: str(ip[0])}
	}
	return cg
}

// mockAppGatewayClient is a minimal App Gateway mock.
type mockAppGatewayClient struct {
	gw    armnetwork.ApplicationGateway
	err   error
	calls []string
}

func (m *mockAppGatewayClient) Get(_ context.Context, _, _ string) (armnetwork.ApplicationGateway, error) {
	m.calls = append(m.calls, "Get")
	return m.gw, m.err
}

func (m *mockAppGatewayClient) CreateOrUpdate(_ context.Context, _, _ string, gw armnetwork.ApplicationGateway) (armnetwork.ApplicationGateway, error) {
	m.calls = append(m.calls, "CreateOrUpdate")
	m.gw = gw
	return gw, m.err
}

// ─── ACIDeployDriver ─────────────────────────────────────────────────────────

func TestACIDeployDriver_Update(t *testing.T) {
	const newImage = "nginx:1.25"
	updated := false
	client := &mockACIClient{
		getFn: func(_ context.Context, _, name string) (armcontainerinstance.ContainerGroup, error) {
			return aciCG(name, "nginx:1.24", "Succeeded"), nil
		},
		createFn: func(_ context.Context, _, _ string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
			updated = true
			return cg, nil
		},
	}
	drv := NewACIDeployDriver("rg", "eastus", "myapp", client)
	if err := drv.Update(context.Background(), newImage); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !updated {
		t.Error("expected CreateOrUpdate to be called")
	}
}

func TestACIDeployDriver_Update_GetError(t *testing.T) {
	client := &mockACIClient{
		getFn: func(_ context.Context, _, _ string) (armcontainerinstance.ContainerGroup, error) {
			return armcontainerinstance.ContainerGroup{}, errors.New("not found")
		},
	}
	drv := NewACIDeployDriver("rg", "eastus", "myapp", client)
	if err := drv.Update(context.Background(), "nginx:latest"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestACIDeployDriver_HealthCheck_Healthy(t *testing.T) {
	client := &mockACIClient{
		getFn: func(_ context.Context, _, name string) (armcontainerinstance.ContainerGroup, error) {
			return aciCG(name, "nginx:latest", "Succeeded"), nil
		},
	}
	drv := NewACIDeployDriver("rg", "eastus", "myapp", client)
	if err := drv.HealthCheck(context.Background(), "/health"); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
}

func TestACIDeployDriver_HealthCheck_Unhealthy(t *testing.T) {
	client := &mockACIClient{
		getFn: func(_ context.Context, _, name string) (armcontainerinstance.ContainerGroup, error) {
			return aciCG(name, "nginx:latest", "Updating"), nil
		},
	}
	drv := NewACIDeployDriver("rg", "eastus", "myapp", client)
	if err := drv.HealthCheck(context.Background(), "/health"); err == nil {
		t.Fatal("expected error for non-Succeeded state")
	}
}

func TestACIDeployDriver_CurrentImage(t *testing.T) {
	const image = "myrepo/myapp:v3"
	client := &mockACIClient{
		getFn: func(_ context.Context, _, name string) (armcontainerinstance.ContainerGroup, error) {
			return aciCG(name, image, "Succeeded"), nil
		},
	}
	drv := NewACIDeployDriver("rg", "eastus", "myapp", client)
	got, err := drv.CurrentImage(context.Background())
	if err != nil {
		t.Fatalf("CurrentImage: %v", err)
	}
	if got != image {
		t.Errorf("CurrentImage = %q, want %q", got, image)
	}
}

func TestACIDeployDriver_ReplicaCount(t *testing.T) {
	drv := NewACIDeployDriver("rg", "eastus", "myapp", nil)
	n, err := drv.ReplicaCount(context.Background())
	if err != nil {
		t.Fatalf("ReplicaCount: %v", err)
	}
	if n != 1 {
		t.Errorf("ReplicaCount = %d, want 1", n)
	}
}

// ─── ACIBlueGreenDriver ───────────────────────────────────────────────────────

func TestACIBlueGreenDriver_CreateGreen(t *testing.T) {
	created := false
	client := &mockACIClient{
		getFn: func(_ context.Context, _, name string) (armcontainerinstance.ContainerGroup, error) {
			return aciCG(name, "nginx:1.24", "Succeeded", "10.0.0.1"), nil
		},
		createFn: func(_ context.Context, _, name string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
			if name != "myapp-green" {
				t.Errorf("green name = %q, want %q", name, "myapp-green")
			}
			created = true
			cg.Properties.IPAddress = &armcontainerinstance.IPAddress{IP: str("10.0.0.2")}
			return cg, nil
		},
	}
	drv := NewACIBlueGreenDriver("rg", "eastus", "myapp", client, nil, "", "")
	if err := drv.CreateGreen(context.Background(), "nginx:1.25"); err != nil {
		t.Fatalf("CreateGreen: %v", err)
	}
	if !created {
		t.Error("expected CreateOrUpdate to be called for green")
	}
	ep, err := drv.GreenEndpoint(context.Background())
	if err != nil {
		t.Fatalf("GreenEndpoint: %v", err)
	}
	if ep != "10.0.0.2" {
		t.Errorf("GreenEndpoint = %q, want 10.0.0.2", ep)
	}
}

func TestACIBlueGreenDriver_SwitchTraffic_NoGateway(t *testing.T) {
	drv := NewACIBlueGreenDriver("rg", "eastus", "myapp", nil, nil, "", "")
	drv.greenIP = "10.0.0.2"
	// No agwClient → no-op, no error.
	if err := drv.SwitchTraffic(context.Background()); err != nil {
		t.Fatalf("SwitchTraffic (no gateway): %v", err)
	}
}

func TestACIBlueGreenDriver_SwitchTraffic_WithGateway(t *testing.T) {
	agwMock := &mockAppGatewayClient{
		gw: armnetwork.ApplicationGateway{
			ID: str("/subscriptions/sub/rg/agw/myagw"),
			Properties: &armnetwork.ApplicationGatewayPropertiesFormat{
				BackendAddressPools: []*armnetwork.ApplicationGatewayBackendAddressPool{
					{
						Name: str("mypool"),
						Properties: &armnetwork.ApplicationGatewayBackendAddressPoolPropertiesFormat{
							BackendAddresses: []*armnetwork.ApplicationGatewayBackendAddress{
								{IPAddress: str("10.0.0.1")},
							},
						},
					},
				},
			},
		},
	}
	client := &mockACIClient{}
	drv := NewACIBlueGreenDriver("rg", "eastus", "myapp", client, agwMock, "myagw", "mypool")
	drv.greenIP = "10.0.0.2"
	if err := drv.SwitchTraffic(context.Background()); err != nil {
		t.Fatalf("SwitchTraffic: %v", err)
	}
	if len(agwMock.calls) == 0 {
		t.Error("expected App Gateway calls")
	}
}

func TestACIBlueGreenDriver_DestroyBlue(t *testing.T) {
	deleted := false
	client := &mockACIClient{
		deleteFn: func(_ context.Context, _, name string) error {
			if name != "myapp" {
				t.Errorf("deleted %q, want myapp", name)
			}
			deleted = true
			return nil
		},
	}
	drv := NewACIBlueGreenDriver("rg", "eastus", "myapp", client, nil, "", "")
	if err := drv.DestroyBlue(context.Background()); err != nil {
		t.Fatalf("DestroyBlue: %v", err)
	}
	if !deleted {
		t.Error("expected Delete to be called")
	}
}

func TestACIBlueGreenDriver_GreenEndpoint_NotSet(t *testing.T) {
	drv := NewACIBlueGreenDriver("rg", "eastus", "myapp", nil, nil, "", "")
	if _, err := drv.GreenEndpoint(context.Background()); err == nil {
		t.Fatal("expected error when green IP not set")
	}
}

// ─── ACICanaryDriver ──────────────────────────────────────────────────────────

func TestACICanaryDriver_CreateCanary(t *testing.T) {
	created := false
	client := &mockACIClient{
		getFn: func(_ context.Context, _, name string) (armcontainerinstance.ContainerGroup, error) {
			return aciCG(name, "nginx:1.24", "Succeeded"), nil
		},
		createFn: func(_ context.Context, _, name string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
			if name != "myapp-canary" {
				t.Errorf("canary name = %q, want myapp-canary", name)
			}
			created = true
			return cg, nil
		},
	}
	drv := NewACICanaryDriver("rg", "eastus", "myapp", client, nil, "")
	if err := drv.CreateCanary(context.Background(), "nginx:1.25"); err != nil {
		t.Fatalf("CreateCanary: %v", err)
	}
	if !created {
		t.Error("expected canary container group to be created")
	}
}

func TestACICanaryDriver_RoutePercent_Unsupported(t *testing.T) {
	drv := NewACICanaryDriver("rg", "eastus", "myapp", nil, nil, "")
	if err := drv.RoutePercent(context.Background(), 10); err == nil {
		t.Fatal("expected unsupported error for RoutePercent")
	}
}

func TestACICanaryDriver_CheckMetricGate_Pass(t *testing.T) {
	drv := NewACICanaryDriver("rg", "eastus", "myapp", nil, nil, "")
	if err := drv.CheckMetricGate(context.Background(), "error_rate"); err != nil {
		t.Fatalf("CheckMetricGate: %v", err)
	}
}

func TestACICanaryDriver_PromoteCanary(t *testing.T) {
	promoted := false
	client := &mockACIClient{
		getFn: func(_ context.Context, _, name string) (armcontainerinstance.ContainerGroup, error) {
			image := "nginx:1.24"
			if name == "myapp-canary" {
				image = "nginx:1.25"
			}
			return aciCG(name, image, "Succeeded"), nil
		},
		createFn: func(_ context.Context, _, name string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
			if name == "myapp" {
				promoted = true
			}
			return cg, nil
		},
		deleteFn: func(_ context.Context, _, _ string) error { return nil },
	}
	drv := NewACICanaryDriver("rg", "eastus", "myapp", client, nil, "")
	if err := drv.PromoteCanary(context.Background()); err != nil {
		t.Fatalf("PromoteCanary: %v", err)
	}
	if !promoted {
		t.Error("expected stable app to be updated with canary image")
	}
}

func TestACICanaryDriver_DestroyCanary(t *testing.T) {
	deleted := false
	client := &mockACIClient{
		deleteFn: func(_ context.Context, _, name string) error {
			if name != "myapp-canary" {
				t.Errorf("deleted %q, want myapp-canary", name)
			}
			deleted = true
			return nil
		},
	}
	drv := NewACICanaryDriver("rg", "eastus", "myapp", client, nil, "")
	if err := drv.DestroyCanary(context.Background()); err != nil {
		t.Fatalf("DestroyCanary: %v", err)
	}
	if !deleted {
		t.Error("expected Delete to be called for canary")
	}
}
