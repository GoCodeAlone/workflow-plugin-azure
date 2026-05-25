package driver

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerinstance/armcontainerinstance/v2"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockACIClient struct {
	createFn func(ctx context.Context, rg, name string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error)
	getFn    func(ctx context.Context, rg, name string) (armcontainerinstance.ContainerGroup, error)
	deleteFn func(ctx context.Context, rg, name string) error
}

func (m *mockACIClient) CreateOrUpdate(ctx context.Context, rg, name string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
	return m.createFn(ctx, rg, name, cg)
}

func (m *mockACIClient) Get(ctx context.Context, rg, name string) (armcontainerinstance.ContainerGroup, error) {
	return m.getFn(ctx, rg, name)
}

func (m *mockACIClient) Delete(ctx context.Context, rg, name string) error {
	return m.deleteFn(ctx, rg, name)
}

func TestACIDriver_Create(t *testing.T) {
	provisioningState := "Succeeded"
	ipAddr := "10.0.0.1"
	image := "mcr.microsoft.com/hello-world"

	client := &mockACIClient{
		createFn: func(_ context.Context, _, name string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
			return armcontainerinstance.ContainerGroup{
				ID: str("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/" + name),
				Properties: &armcontainerinstance.ContainerGroupPropertiesProperties{
					ProvisioningState: &provisioningState,
					Containers: []*armcontainerinstance.Container{
						{
							Name:       &name,
							Properties: &armcontainerinstance.ContainerProperties{Image: &image},
						},
					},
					IPAddress: &armcontainerinstance.IPAddress{IP: &ipAddr},
				},
			}, nil
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	out, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-aci",
		Type:   "infra.container_service",
		Config: map[string]any{"image": image},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
	if out.Outputs["ip"] != ipAddr {
		t.Errorf("ip = %v, want %s", out.Outputs["ip"], ipAddr)
	}
}

func TestACIDriver_Create_CollectorConfig(t *testing.T) {
	var created armcontainerinstance.ContainerGroup
	client := &mockACIClient{
		createFn: func(_ context.Context, _, name string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
			created = cg
			provisioningState := "Succeeded"
			image := "otel/opentelemetry-collector-contrib:latest"
			return armcontainerinstance.ContainerGroup{
				ID: str("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/" + name),
				Properties: &armcontainerinstance.ContainerGroupPropertiesProperties{
					ProvisioningState: &provisioningState,
					Containers: []*armcontainerinstance.Container{{
						Name:       &name,
						Properties: &armcontainerinstance.ContainerProperties{Image: &image},
					}},
				},
			}, nil
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	_, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name: "observability-collector",
		Type: "infra.container_service",
		Config: map[string]any{
			"image":   "otel/opentelemetry-collector-contrib:latest",
			"command": []any{"otelcol-contrib", "--config=env:OTELCOL_CONFIG"},
			"ports": []any{
				map[string]any{"port": 4317, "public": false},
				map[string]any{"port": 4318, "public": false},
				map[string]any{"port": 9464, "public": false},
				map[string]any{"port": 0, "public": false},
				map[string]any{"port": 70000, "public": false},
			},
			"env_vars": map[string]any{
				"OTELCOL_CONFIG":               "receivers: {}",
				"OTEL_EXPORTER_OTLP_ENDPOINT":  "${vars.otel_exporter_otlp_endpoint}",
				"LOKI_ENDPOINT":                "${vars.loki_endpoint}",
				"GRAFANA_OTLP_ENDPOINT":        "${vars.grafana_otlp_endpoint}",
				"NON_STRING_SHOULD_BE_IGNORED": 42,
			},
			"env_vars_secret": map[string]any{
				"DD_API_KEY":             "${secrets.datadog_api_key}",
				"GRAFANA_OTLP_HEADERS":   "${secrets.grafana_otlp_headers}",
				"NON_STRING_SECRET_SKIP": 42,
			},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Properties == nil || len(created.Properties.Containers) != 1 {
		t.Fatalf("created container group missing container: %+v", created.Properties)
	}
	container := created.Properties.Containers[0]
	if container.Properties == nil {
		t.Fatal("created container missing properties")
	}
	if got := ptrStrings(container.Properties.Command); len(got) != 2 || got[0] != "otelcol-contrib" || got[1] != "--config=env:OTELCOL_CONFIG" {
		t.Fatalf("command = %v, want collector command", got)
	}
	if got := envValue(container.Properties.EnvironmentVariables, "OTELCOL_CONFIG"); got != "receivers: {}" {
		t.Fatalf("OTELCOL_CONFIG = %q, want receivers config", got)
	}
	if got := secureEnvValue(container.Properties.EnvironmentVariables, "DD_API_KEY"); got != "${secrets.datadog_api_key}" {
		t.Fatalf("DD_API_KEY secure value = %q, want secret reference", got)
	}
	if got := containerPorts(container.Properties.Ports); len(got) != 3 || got[0] != 4317 || got[1] != 4318 || got[2] != 9464 {
		t.Fatalf("container ports = %v, want [4317 4318 9464]", got)
	}
	if created.Properties.IPAddress != nil {
		t.Fatalf("IPAddress = %+v, want none when all ports are private", created.Properties.IPAddress)
	}
}

func TestACIDriver_Read(t *testing.T) {
	provisioningState := "Running"
	client := &mockACIClient{
		getFn: func(_ context.Context, _, name string) (armcontainerinstance.ContainerGroup, error) {
			return armcontainerinstance.ContainerGroup{
				ID:         str("/subscriptions/sub/rg/" + name),
				Properties: &armcontainerinstance.ContainerGroupPropertiesProperties{ProvisioningState: &provisioningState},
			}, nil
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	out, err := drv.Read(context.Background(), interfaces.ResourceRef{Name: "test-aci", Type: "infra.container_service"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Status != "Running" {
		t.Errorf("status = %q, want Running", out.Status)
	}
}

func TestACIDriver_Delete(t *testing.T) {
	deleted := false
	client := &mockACIClient{
		deleteFn: func(_ context.Context, _, name string) error {
			deleted = true
			return nil
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-aci"})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !deleted {
		t.Error("expected Delete to be called")
	}
}

func TestACIDriver_Delete_Error(t *testing.T) {
	client := &mockACIClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			return errors.New("not found")
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-aci"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestACIDriver_HealthCheck_Healthy(t *testing.T) {
	running := "Running"
	client := &mockACIClient{
		getFn: func(_ context.Context, _, _ string) (armcontainerinstance.ContainerGroup, error) {
			return armcontainerinstance.ContainerGroup{
				ID:         str("/subscriptions/sub/rg/test"),
				Properties: &armcontainerinstance.ContainerGroupPropertiesProperties{ProvisioningState: &running},
			}, nil
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-aci"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if !h.Healthy {
		t.Errorf("expected healthy, got unhealthy: %s", h.Message)
	}
}

func TestACIDriver_Diff_NilCurrent(t *testing.T) {
	drv := NewACIDriver("rg", "eastus", nil)
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{Name: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when current is nil")
	}
}

func TestACIDriver_Create_Error(t *testing.T) {
	client := &mockACIClient{
		createFn: func(_ context.Context, _, _ string, _ armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
			return armcontainerinstance.ContainerGroup{}, errors.New("quota exceeded")
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	_, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-aci",
		Config: map[string]any{"image": "mcr.microsoft.com/hello-world"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestACIDriver_Update(t *testing.T) {
	provisioningState := "Succeeded"
	called := false
	client := &mockACIClient{
		createFn: func(_ context.Context, _, name string, _ armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
			called = true
			return armcontainerinstance.ContainerGroup{
				ID: str("/sub/rg/aci/" + name),
				Properties: &armcontainerinstance.ContainerGroupPropertiesProperties{
					ProvisioningState: &provisioningState,
				},
			}, nil
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	out, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-aci"}, interfaces.ResourceSpec{
		Name:   "test-aci",
		Config: map[string]any{"image": "nginx:latest"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !called {
		t.Error("expected CreateOrUpdate to be called")
	}
	_ = out
}

func TestACIDriver_Update_Error(t *testing.T) {
	client := &mockACIClient{
		createFn: func(_ context.Context, _, _ string, _ armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
			return armcontainerinstance.ContainerGroup{}, errors.New("update failed")
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	_, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-aci"}, interfaces.ResourceSpec{
		Name:   "test-aci",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestACIDriver_Diff_HasChanges(t *testing.T) {
	drv := NewACIDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"image": "nginx:1.23"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"image": "nginx:1.25"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when image changes")
	}
}

func TestACIDriver_Diff_NoChanges(t *testing.T) {
	drv := NewACIDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"image": "nginx:1.25"},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"image": "nginx:1.25"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=false when image matches")
	}
}

func TestACIDriver_HealthCheck_Unhealthy(t *testing.T) {
	client := &mockACIClient{
		getFn: func(_ context.Context, _, _ string) (armcontainerinstance.ContainerGroup, error) {
			return armcontainerinstance.ContainerGroup{}, errors.New("container group not found")
		},
	}

	drv := NewACIDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-aci"})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if h.Healthy {
		t.Error("expected unhealthy when get fails")
	}
}

func TestACIDriver_Scale_NotSupported(t *testing.T) {
	drv := NewACIDriver("rg", "eastus", nil)
	_, err := drv.Scale(context.Background(), interfaces.ResourceRef{}, 3)
	if err == nil {
		t.Fatal("expected error for Scale")
	}
}

func ptrStrings(values []*string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != nil {
			out = append(out, *value)
		}
	}
	return out
}

func envValue(values []*armcontainerinstance.EnvironmentVariable, name string) string {
	for _, value := range values {
		if value != nil && value.Name != nil && *value.Name == name && value.Value != nil {
			return *value.Value
		}
	}
	return ""
}

func secureEnvValue(values []*armcontainerinstance.EnvironmentVariable, name string) string {
	for _, value := range values {
		if value != nil && value.Name != nil && *value.Name == name && value.SecureValue != nil {
			return *value.SecureValue
		}
	}
	return ""
}

func containerPorts(values []*armcontainerinstance.ContainerPort) []int32 {
	out := make([]int32, 0, len(values))
	for _, value := range values {
		if value != nil && value.Port != nil {
			out = append(out, *value.Port)
		}
	}
	return out
}
