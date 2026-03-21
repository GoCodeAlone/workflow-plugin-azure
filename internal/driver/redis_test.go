package driver

import (
	"context"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/redis/armredis"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockRedisClient struct {
	createFn func(ctx context.Context, rg, name string, params armredis.CreateParameters) (armredis.ResourceInfo, error)
	getFn    func(ctx context.Context, rg, name string) (armredis.ResourceInfo, error)
	deleteFn func(ctx context.Context, rg, name string) error
}

func (m *mockRedisClient) Create(ctx context.Context, rg, name string, params armredis.CreateParameters) (armredis.ResourceInfo, error) {
	return m.createFn(ctx, rg, name, params)
}

func (m *mockRedisClient) Get(ctx context.Context, rg, name string) (armredis.ResourceInfo, error) {
	return m.getFn(ctx, rg, name)
}

func (m *mockRedisClient) Delete(ctx context.Context, rg, name string) error {
	return m.deleteFn(ctx, rg, name)
}

func TestRedisDriver_Create(t *testing.T) {
	prov := armredis.ProvisioningStateSucceeded
	host := "test-redis.redis.cache.windows.net"
	port := int32(6379)
	sslPort := int32(6380)

	client := &mockRedisClient{
		createFn: func(_ context.Context, _, name string, _ armredis.CreateParameters) (armredis.ResourceInfo, error) {
			return armredis.ResourceInfo{
				ID: str("/subscriptions/sub/rg/redis/" + name),
				Properties: &armredis.Properties{
					ProvisioningState: &prov,
					HostName:         &host,
					Port:             &port,
					SSLPort:          &sslPort,
				},
			}, nil
		},
	}

	drv := NewRedisDriver("rg", "eastus", client)
	out, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-redis",
		Type:   "infra.cache",
		Config: map[string]any{"capacity": 1},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
	if out.Outputs["host"] != host {
		t.Errorf("host = %v, want %s", out.Outputs["host"], host)
	}
}

func TestRedisDriver_HealthCheck(t *testing.T) {
	prov := armredis.ProvisioningStateSucceeded
	host := "test.redis.cache.windows.net"
	port := int32(6379)
	sslPort := int32(6380)

	client := &mockRedisClient{
		getFn: func(_ context.Context, _, name string) (armredis.ResourceInfo, error) {
			return armredis.ResourceInfo{
				ID: str("/sub/rg/" + name),
				Properties: &armredis.Properties{
					ProvisioningState: &prov,
					HostName:         &host,
					Port:             &port,
					SSLPort:          &sslPort,
				},
			}, nil
		},
	}

	drv := NewRedisDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-redis"})
	if err != nil {
		t.Fatal(err)
	}
	if !h.Healthy {
		t.Errorf("expected healthy, got %q", h.Message)
	}
}
